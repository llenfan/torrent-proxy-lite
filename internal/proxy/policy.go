package proxy

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

var (
	ErrHostNotAllowed   = errors.New("host is not in the allowlist")
	ErrForbiddenAddress = errors.New("destination address is private, loopback, or otherwise local")
)

var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

type Policy struct {
	allowHosts  []string
	denyPrivate bool
}

func NewPolicy(allowHosts []string, denyPrivateNetworks bool) *Policy {
	normalized := make([]string, 0, len(allowHosts))
	for _, host := range allowHosts {
		normalized = append(normalized, normalizeHost(host))
	}
	return &Policy{allowHosts: normalized, denyPrivate: denyPrivateNetworks}
}

func (p *Policy) CheckHost(host string) error {
	h := normalizeHost(host)
	if addr, err := netip.ParseAddr(h); err == nil {
		if err := p.CheckAddr(addr); err != nil {
			return err
		}
	}
	if len(p.allowHosts) == 0 {
		return nil
	}
	for _, pattern := range p.allowHosts {
		if matchesHostPattern(h, pattern) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrHostNotAllowed, h)
}

func (p *Policy) CheckAddr(addr netip.Addr) error {
	if !p.denyPrivate {
		return nil
	}
	a := addr.Unmap()
	forbidden := a.IsLoopback() ||
		a.IsPrivate() ||
		a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() ||
		a.IsMulticast() ||
		a.IsUnspecified() ||
		carrierGradeNAT.Contains(a)
	if forbidden {
		return fmt.Errorf("%w: %s", ErrForbiddenAddress, a)
	}
	return nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func matchesHostPattern(host, pattern string) bool {
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return strings.HasSuffix(host, "."+suffix)
	}
	return host == pattern
}
