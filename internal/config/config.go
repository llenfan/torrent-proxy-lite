package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Server  Server  `yaml:"server"`
	Proxy   Proxy   `yaml:"proxy"`
	Logging Logging `yaml:"logging"`
}

type Server struct {
	ListenAddr string `yaml:"listen_addr"`
	HealthAddr string `yaml:"health_addr"`
}

type Proxy struct {
	ConnectTimeout        Duration `yaml:"connect_timeout"`
	IdleTimeout           Duration `yaml:"idle_timeout"`
	ResponseHeaderTimeout Duration `yaml:"response_header_timeout"`
	AllowHosts            []string `yaml:"allow_hosts"`
	DenyPrivateNetworks   bool     `yaml:"deny_private_networks"`
}

type Logging struct {
	Level             string `yaml:"level"`
	Format            string `yaml:"format"`
	RedactQueryValues bool   `yaml:"redact_query_values"`
}

func Default() *Config {
	return &Config{
		Server: Server{
			ListenAddr: "127.0.0.1:8080",
			HealthAddr: "127.0.0.1:8081",
		},
		Proxy: Proxy{
			ConnectTimeout:        Duration(10 * time.Second),
			IdleTimeout:           Duration(60 * time.Second),
			ResponseHeaderTimeout: Duration(15 * time.Second),
			DenyPrivateNetworks:   true,
		},
		Logging: Logging{
			Level:             "info",
			Format:            "json",
			RedactQueryValues: true,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open config: %w", err)
		}
		defer f.Close()
		decoder := yaml.NewDecoder(f)
		decoder.KnownFields(true)
		if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	var errs []error
	if err := validateAddr("server.listen_addr", c.Server.ListenAddr); err != nil {
		errs = append(errs, err)
	}
	if err := validateAddr("server.health_addr", c.Server.HealthAddr); err != nil {
		errs = append(errs, err)
	}
	if _, port, err := net.SplitHostPort(c.Server.ListenAddr); err == nil && port != "0" && c.Server.ListenAddr == c.Server.HealthAddr {
		errs = append(errs, errors.New("server.listen_addr and server.health_addr must differ"))
	}
	if c.Proxy.ConnectTimeout <= 0 {
		errs = append(errs, errors.New("proxy.connect_timeout must be positive"))
	}
	if c.Proxy.IdleTimeout <= 0 {
		errs = append(errs, errors.New("proxy.idle_timeout must be positive"))
	}
	if c.Proxy.ResponseHeaderTimeout <= 0 {
		errs = append(errs, errors.New("proxy.response_header_timeout must be positive"))
	}
	for i, host := range c.Proxy.AllowHosts {
		normalized, err := normalizeHostPattern(host)
		if err != nil {
			errs = append(errs, fmt.Errorf("proxy.allow_hosts[%d]: %w", i, err))
			continue
		}
		c.Proxy.AllowHosts[i] = normalized
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level %q is not one of debug, info, warn, error", c.Logging.Level))
	}
	switch c.Logging.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("logging.format %q is not one of json, text", c.Logging.Format))
	}
	return errors.Join(errs...)
}

func validateAddr(field, addr string) error {
	if addr == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("%s: %v", field, err)
	}
	return nil
}

func normalizeHostPattern(pattern string) (string, error) {
	p := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(pattern)), ".")
	if p == "" {
		return "", errors.New("host must not be empty")
	}
	if strings.ContainsAny(p, "/@:? ") {
		return "", fmt.Errorf("%q must be a bare hostname without scheme, port, or path", p)
	}
	rest := strings.TrimPrefix(p, "*.")
	if rest == "" || strings.Contains(rest, "*") {
		return "", fmt.Errorf("%q: wildcard is only allowed as a leading *. prefix", p)
	}
	return p, nil
}
