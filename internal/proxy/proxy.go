package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/llenfan/torrent-proxy-lite/internal/redact"
)

type Options struct {
	Policy                *Policy
	Logger                *slog.Logger
	ConnectTimeout        time.Duration
	IdleTimeout           time.Duration
	ResponseHeaderTimeout time.Duration
	RedactQueryValues     bool
}

type Proxy struct {
	opts      Options
	transport *http.Transport
	mu        sync.Mutex
	tunnels   map[net.Conn]struct{}
}

func New(opts Options) *Proxy {
	p := &Proxy{opts: opts, tunnels: make(map[net.Conn]struct{})}
	p.transport = &http.Transport{
		DialContext:           p.dialContext,
		DisableCompression:    true,
		MaxIdleConns:          100,
		IdleConnTimeout:       opts.IdleTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return p
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.handleConnect(w, r)
	case r.URL.IsAbs():
		p.handleForward(w, r)
	default:
		http.Error(w, "torrent-proxy only accepts proxy requests; configure it as an HTTP proxy in your torrent client", http.StatusBadRequest)
	}
}

func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for conn := range p.tunnels {
		conn.Close()
	}
	clear(p.tunnels)
	return nil
}

func (p *Proxy) handleForward(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	kind := classify(r)
	host := r.URL.Hostname()
	if r.URL.Scheme != "http" {
		p.fail(w, r, kind, host, start, http.StatusBadRequest, fmt.Errorf("unsupported scheme %q, HTTPS goes through CONNECT", r.URL.Scheme))
		return
	}
	if err := p.opts.Policy.CheckHost(host); err != nil {
		p.fail(w, r, kind, host, start, http.StatusForbidden, err)
		return
	}
	p.opts.Logger.Debug("forwarding", "type", kind, "method", r.Method, "url", redact.URL(r.URL, p.opts.RedactQueryValues))
	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	removeHopByHopHeaders(outbound.Header)
	resp, err := p.transport.RoundTrip(outbound)
	if err != nil {
		p.fail(w, r, kind, host, start, http.StatusBadGateway, sanitizeError(err))
		return
	}
	defer resp.Body.Close()
	removeHopByHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	written, copyErr := io.Copy(w, resp.Body)
	p.opts.Logger.Info("request",
		"type", kind,
		"method", r.Method,
		"host", host,
		"status", resp.StatusCode,
		"duration_ms", millisecondsSince(start),
		"bytes", written,
	)
	if copyErr != nil {
		p.opts.Logger.Warn("response copy interrupted", "type", kind, "host", host, "error", sanitizeError(copyErr).Error())
	}
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		p.fail(w, r, "connect", r.Host, start, http.StatusBadRequest, errors.New("CONNECT target must be host:port"))
		return
	}
	if err := p.opts.Policy.CheckHost(host); err != nil {
		p.fail(w, r, "connect", host, start, http.StatusForbidden, err)
		return
	}
	upstream, err := p.dialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		p.fail(w, r, "connect", host, start, http.StatusBadGateway, sanitizeError(err))
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		p.fail(w, r, "connect", host, start, http.StatusInternalServerError, errors.New("client connection does not support hijacking"))
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		p.fail(w, r, "connect", host, start, http.StatusInternalServerError, err)
		return
	}
	untrack := p.track(client, upstream)
	defer untrack()
	client.SetDeadline(time.Time{})
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		p.opts.Logger.Warn("connect handshake failed", "host", host, "error", err.Error())
		return
	}
	if pending := buffered.Reader.Buffered(); pending > 0 {
		if _, err := io.CopyN(upstream, buffered, int64(pending)); err != nil {
			p.opts.Logger.Warn("connect preface relay failed", "host", host, "error", err.Error())
			return
		}
	}
	bytesUp, bytesDown := pipe(client, upstream, p.opts.IdleTimeout)
	p.opts.Logger.Info("request",
		"type", "connect",
		"method", r.Method,
		"host", host,
		"status", http.StatusOK,
		"duration_ms", millisecondsSince(start),
		"bytes_up", bytesUp,
		"bytes_down", bytesDown,
	)
}

func (p *Proxy) fail(w http.ResponseWriter, r *http.Request, kind, host string, start time.Time, status int, err error) {
	http.Error(w, err.Error(), status)
	p.opts.Logger.Warn("request",
		"type", kind,
		"method", r.Method,
		"host", host,
		"status", status,
		"duration_ms", millisecondsSince(start),
		"error", err.Error(),
	)
}

func (p *Proxy) track(conns ...net.Conn) func() {
	p.mu.Lock()
	for _, c := range conns {
		p.tunnels[c] = struct{}{}
	}
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		for _, c := range conns {
			c.Close()
			delete(p.tunnels, c)
		}
		p.mu.Unlock()
	}
}

func (p *Proxy) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: p.opts.ConnectTimeout, Control: p.checkDialAddress}
	return dialer.DialContext(ctx, network, addr)
}

func (p *Proxy) checkDialAddress(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	return p.opts.Policy.CheckAddr(addr)
}

func classify(r *http.Request) string {
	if r.Method == http.MethodConnect {
		return "connect"
	}
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.Contains(path, "scrape"):
		return "scrape"
	case strings.Contains(path, "announce"):
		return "announce"
	default:
		return "http"
	}
}

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHopHeaders(h http.Header) {
	for _, field := range strings.Split(h.Get("Connection"), ",") {
		if field = textproto.TrimString(field); field != "" {
			h.Del(field)
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

func copyHeader(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func sanitizeError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s: %w", urlErr.Op, sanitizeError(urlErr.Err))
	}
	return err
}

func millisecondsSince(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
