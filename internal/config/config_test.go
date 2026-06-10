package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadWithoutFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("listen_addr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.HealthAddr != "127.0.0.1:8081" {
		t.Errorf("health_addr = %q", cfg.Server.HealthAddr)
	}
	if !cfg.Proxy.DenyPrivateNetworks {
		t.Error("deny_private_networks should default to true")
	}
	if !cfg.Logging.RedactQueryValues {
		t.Error("redact_query_values should default to true")
	}
	if time.Duration(cfg.Proxy.ConnectTimeout) != 10*time.Second {
		t.Errorf("connect_timeout = %v", time.Duration(cfg.Proxy.ConnectTimeout))
	}
	if cfg.Logging.Level != "info" || cfg.Logging.Format != "json" {
		t.Errorf("logging defaults = %q/%q", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestLoadParsesFullFile(t *testing.T) {
	path := writeConfig(t, `
server:
  listen_addr: "127.0.0.1:9090"
  health_addr: "127.0.0.1:9091"
proxy:
  connect_timeout: "5s"
  idle_timeout: "30s"
  response_header_timeout: "7s"
  allow_hosts:
    - "Tracker.Example.ORG."
    - "*.example.net"
  deny_private_networks: false
logging:
  level: "debug"
  format: "text"
  redact_query_values: false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("listen_addr = %q", cfg.Server.ListenAddr)
	}
	if time.Duration(cfg.Proxy.ConnectTimeout) != 5*time.Second {
		t.Errorf("connect_timeout = %v", time.Duration(cfg.Proxy.ConnectTimeout))
	}
	if time.Duration(cfg.Proxy.IdleTimeout) != 30*time.Second {
		t.Errorf("idle_timeout = %v", time.Duration(cfg.Proxy.IdleTimeout))
	}
	if cfg.Proxy.DenyPrivateNetworks {
		t.Error("deny_private_networks should be false")
	}
	if cfg.Proxy.AllowHosts[0] != "tracker.example.org" {
		t.Errorf("allow_hosts[0] = %q, want normalized lowercase", cfg.Proxy.AllowHosts[0])
	}
	if cfg.Proxy.AllowHosts[1] != "*.example.net" {
		t.Errorf("allow_hosts[1] = %q", cfg.Proxy.AllowHosts[1])
	}
	if cfg.Logging.Level != "debug" || cfg.Logging.Format != "text" || cfg.Logging.RedactQueryValues {
		t.Errorf("logging = %+v", cfg.Logging)
	}
}

func TestLoadKeepsDefaultsForAbsentFields(t *testing.T) {
	path := writeConfig(t, "logging:\n  level: \"debug\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("listen_addr = %q, want default", cfg.Server.ListenAddr)
	}
	if !cfg.Proxy.DenyPrivateNetworks {
		t.Error("deny_private_networks should keep its default")
	}
	if !cfg.Logging.RedactQueryValues {
		t.Error("redact_query_values should keep its default")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("level = %q", cfg.Logging.Level)
	}
}

func TestValidateAllowsEphemeralPortsOnBothListeners(t *testing.T) {
	cfg := Default()
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.Server.HealthAddr = "127.0.0.1:0"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v, want port 0 to be exempt from the differ rule", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, "proxy:\n  connect_timout: \"5s\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	path := writeConfig(t, "proxy:\n  connect_timeout: \"10 seconds\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"empty listen addr", func(c *Config) { c.Server.ListenAddr = "" }, "listen_addr"},
		{"listen addr without port", func(c *Config) { c.Server.ListenAddr = "127.0.0.1" }, "listen_addr"},
		{"identical addrs", func(c *Config) { c.Server.HealthAddr = c.Server.ListenAddr }, "must differ"},
		{"zero connect timeout", func(c *Config) { c.Proxy.ConnectTimeout = 0 }, "connect_timeout"},
		{"zero idle timeout", func(c *Config) { c.Proxy.IdleTimeout = 0 }, "idle_timeout"},
		{"zero response header timeout", func(c *Config) { c.Proxy.ResponseHeaderTimeout = 0 }, "response_header_timeout"},
		{"unknown level", func(c *Config) { c.Logging.Level = "verbose" }, "logging.level"},
		{"unknown format", func(c *Config) { c.Logging.Format = "xml" }, "logging.format"},
		{"host with scheme", func(c *Config) { c.Proxy.AllowHosts = []string{"https://x.org"} }, "allow_hosts"},
		{"inner wildcard", func(c *Config) { c.Proxy.AllowHosts = []string{"tracker.*.org"} }, "allow_hosts"},
		{"empty host", func(c *Config) { c.Proxy.AllowHosts = []string{""} }, "allow_hosts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}
