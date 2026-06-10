package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/llenfan/torrent-proxy-lite/internal/config"
	"github.com/llenfan/torrent-proxy-lite/internal/logging"
	"github.com/llenfan/torrent-proxy-lite/internal/server"
	"github.com/llenfan/torrent-proxy-lite/internal/version"
)

const shutdownGrace = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "torrent-proxy:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to YAML config file; defaults apply when omitted")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("torrent-proxy", version.Version)
		return nil
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	log, err := logging.New(cfg.Logging.Level, cfg.Logging.Format, os.Stderr)
	if err != nil {
		return err
	}
	log.Info("starting torrent-proxy",
		"version", version.Version,
		"listen_addr", cfg.Server.ListenAddr,
		"health_addr", cfg.Server.HealthAddr,
		"deny_private_networks", cfg.Proxy.DenyPrivateNetworks,
		"allow_hosts", len(cfg.Proxy.AllowHosts),
		"redact_query_values", cfg.Logging.RedactQueryValues,
	)
	srv := server.New(cfg, log)
	if err := srv.Start(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var serveErr error
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case serveErr = <-srv.Errors():
		log.Error("server failed", "error", serveErr.Error())
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(serveErr, fmt.Errorf("shutdown: %w", err))
	}
	log.Info("shutdown complete")
	return serveErr
}
