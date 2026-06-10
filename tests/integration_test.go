package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/llenfan/torrent-proxy-lite/internal/config"
	"github.com/llenfan/torrent-proxy-lite/internal/logging"
	"github.com/llenfan/torrent-proxy-lite/internal/server"
)

func startProxy(t *testing.T, logs io.Writer, logLevel string, mutate func(*config.Config)) *server.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.Server.HealthAddr = "127.0.0.1:0"
	cfg.Proxy.DenyPrivateNetworks = false
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	if logs == nil {
		logs = io.Discard
	}
	log, err := logging.New(logLevel, "json", logs)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	srv := server.New(cfg, log)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

func proxiedClient(t *testing.T, srv *server.Server, tlsConfig *tls.Config) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse("http://" + srv.ProxyAddr())
	if err != nil {
		t.Fatalf("proxy url: %v", err)
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: tlsConfig},
		Timeout:   10 * time.Second,
	}
}

func rawRequest(t *testing.T, addr, request string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("write: %v", err)
	}
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	return statusLine
}

func TestHTTPForwardingIsTransparent(t *testing.T) {
	const rawQuery = "info_hash=%01%02%03&passkey=supersecret&uploaded=123&downloaded=0&left=42&port=6881"
	const body = "d8:intervali1800e5:peers0:e"
	var mu sync.Mutex
	var gotRequestURI, gotUserAgent string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRequestURI = r.RequestURI
		gotUserAgent = r.UserAgent()
		mu.Unlock()
		w.Header().Set("X-Tracker", "yes")
		fmt.Fprint(w, body)
	}))
	defer origin.Close()
	srv := startProxy(t, nil, "error", nil)
	client := proxiedClient(t, srv, nil)
	req, err := http.NewRequest(http.MethodGet, origin.URL+"/announce?"+rawQuery, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("User-Agent", "test-torrent-client/1.0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	if resp.Header.Get("X-Tracker") != "yes" {
		t.Error("response header X-Tracker was not preserved")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotRequestURI != "/announce?"+rawQuery {
		t.Errorf("tracker saw %q, want the query forwarded byte for byte", gotRequestURI)
	}
	if gotUserAgent != "test-torrent-client/1.0" {
		t.Errorf("tracker saw User-Agent %q, want the client value", gotUserAgent)
	}
}

func TestConnectTunnelsTLSWithoutInterception(t *testing.T) {
	const body = "d8:completei5ee"
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer origin.Close()
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	srv := startProxy(t, nil, "error", nil)
	client := proxiedClient(t, srv, &tls.Config{RootCAs: pool})
	resp, err := client.Get(origin.URL + "/announce?passkey=tls-secret")
	if err != nil {
		t.Fatalf("https request through proxy: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestAllowlistBlocksUnlistedHTTPHost(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	srv := startProxy(t, nil, "error", func(c *config.Config) {
		c.Proxy.AllowHosts = []string{"tracker.example.org"}
	})
	client := proxiedClient(t, srv, nil)
	resp, err := client.Get(origin.URL + "/announce")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAllowlistAllowsListedHost(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	srv := startProxy(t, nil, "error", func(c *config.Config) {
		c.Proxy.AllowHosts = []string{"127.0.0.1"}
	})
	client := proxiedClient(t, srv, nil)
	resp, err := client.Get(origin.URL + "/announce")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAllowlistBlocksUnlistedConnectTarget(t *testing.T) {
	srv := startProxy(t, nil, "error", func(c *config.Config) {
		c.Proxy.AllowHosts = []string{"tracker.example.org"}
	})
	statusLine := rawRequest(t, srv.ProxyAddr(), "CONNECT 203.0.113.10:443 HTTP/1.1\r\nHost: 203.0.113.10:443\r\n\r\n")
	if !strings.Contains(statusLine, "403") {
		t.Errorf("status line = %q, want 403", statusLine)
	}
}

func TestDenyPrivateNetworksBlocksLoopbackLiteral(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	srv := startProxy(t, nil, "error", func(c *config.Config) {
		c.Proxy.DenyPrivateNetworks = true
	})
	client := proxiedClient(t, srv, nil)
	resp, err := client.Get(origin.URL + "/announce")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestDenyPrivateNetworksBlocksPrivateDNSResults(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	srv := startProxy(t, nil, "error", func(c *config.Config) {
		c.Proxy.DenyPrivateNetworks = true
	})
	client := proxiedClient(t, srv, nil)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	if err != nil {
		t.Fatalf("origin addr: %v", err)
	}
	resp, err := client.Get("http://localhost:" + port + "/announce")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 from the dial-time address check", resp.StatusCode)
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv := startProxy(t, nil, "error", nil)
	resp, err := http.Get("http://" + srv.HealthAddr() + "/healthz")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health payload: %v", err)
	}
	if payload.Status != "ok" || payload.Version == "" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestNonProxyRequestIsRejected(t *testing.T) {
	srv := startProxy(t, nil, "error", nil)
	resp, err := http.Get("http://" + srv.ProxyAddr() + "/")
	if err != nil {
		t.Fatalf("direct request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAbsoluteHTTPSFormIsRejected(t *testing.T) {
	srv := startProxy(t, nil, "error", nil)
	statusLine := rawRequest(t, srv.ProxyAddr(), "GET https://tracker.example.org/announce HTTP/1.1\r\nHost: tracker.example.org\r\n\r\n")
	if !strings.Contains(statusLine, "400") {
		t.Errorf("status line = %q, want 400", statusLine)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogsNeverContainSecrets(t *testing.T) {
	logs := &syncBuffer{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	srv := startProxy(t, logs, "debug", nil)
	client := proxiedClient(t, srv, nil)
	resp, err := client.Get(origin.URL + "/announce?passkey=supersecretvalue&info_hash=%01%02&peer_id=-TESTPEERID0001-")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	resp.Body.Close()
	output := logs.String()
	for _, secret := range []string{"supersecretvalue", "TESTPEERID0001"} {
		if strings.Contains(output, secret) {
			t.Errorf("logs contain secret %q:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, "REDACTED") {
		t.Errorf("expected redaction placeholder in debug logs:\n%s", output)
	}
}
