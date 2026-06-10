package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/llenfan/torrent-proxy-lite/internal/config"
	"github.com/llenfan/torrent-proxy-lite/internal/proxy"
	"github.com/llenfan/torrent-proxy-lite/internal/version"
)

type Server struct {
	cfg       *config.Config
	log       *slog.Logger
	proxy     *proxy.Proxy
	proxySrv  *http.Server
	healthSrv *http.Server
	proxyLn   net.Listener
	healthLn  net.Listener
	errs      chan error
	started   time.Time
}

func New(cfg *config.Config, log *slog.Logger) *Server {
	p := proxy.New(proxy.Options{
		Policy:                proxy.NewPolicy(cfg.Proxy.AllowHosts, cfg.Proxy.DenyPrivateNetworks),
		Logger:                log,
		ConnectTimeout:        time.Duration(cfg.Proxy.ConnectTimeout),
		IdleTimeout:           time.Duration(cfg.Proxy.IdleTimeout),
		ResponseHeaderTimeout: time.Duration(cfg.Proxy.ResponseHeaderTimeout),
		RedactQueryValues:     cfg.Logging.RedactQueryValues,
	})
	s := &Server{cfg: cfg, log: log, proxy: p, errs: make(chan error, 2)}
	errorLog := slog.NewLogLogger(log.Handler(), slog.LevelWarn)
	s.proxySrv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Duration(cfg.Proxy.IdleTimeout),
		ErrorLog:          errorLog,
	}
	s.healthSrv = &http.Server{
		Handler:           s.healthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          errorLog,
	}
	return s
}

func (s *Server) Start() error {
	proxyLn, err := net.Listen("tcp", s.cfg.Server.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Server.ListenAddr, err)
	}
	healthLn, err := net.Listen("tcp", s.cfg.Server.HealthAddr)
	if err != nil {
		proxyLn.Close()
		return fmt.Errorf("listen on %s: %w", s.cfg.Server.HealthAddr, err)
	}
	s.proxyLn, s.healthLn = proxyLn, healthLn
	s.started = time.Now()
	s.warnIfNotLoopback(proxyLn.Addr())
	go s.serve(s.proxySrv, proxyLn)
	go s.serve(s.healthSrv, healthLn)
	s.log.Info("proxy listening", "addr", proxyLn.Addr().String())
	s.log.Info("health endpoint listening", "addr", healthLn.Addr().String(), "path", "/healthz")
	return nil
}

func (s *Server) serve(srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.errs <- err
	}
}

func (s *Server) Errors() <-chan error { return s.errs }

func (s *Server) ProxyAddr() string { return s.proxyLn.Addr().String() }

func (s *Server) HealthAddr() string { return s.healthLn.Addr().String() }

func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error
	if err := s.proxySrv.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("proxy server shutdown: %w", err))
	}
	if err := s.healthSrv.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("health server shutdown: %w", err))
	}
	if err := s.proxy.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close tunnels: %w", err))
	}
	return errors.Join(errs...)
}

func (s *Server) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"version":        version.Version,
			"uptime_seconds": int64(time.Since(s.started).Seconds()),
		})
	})
	return mux
}

func (s *Server) warnIfNotLoopback(addr net.Addr) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.IP.IsLoopback() {
		return
	}
	s.log.Warn("proxy is listening on a non-loopback address; anyone who can reach it can use it as an open proxy", "addr", addr.String())
}
