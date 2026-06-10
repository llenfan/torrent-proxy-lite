package proxy

import (
	"errors"
	"testing"
)

func TestPolicyAllowsAnyHostWithEmptyAllowlist(t *testing.T) {
	p := NewPolicy(nil, false)
	for _, host := range []string{"tracker.example.org", "192.168.0.10", "8.8.8.8"} {
		if err := p.CheckHost(host); err != nil {
			t.Errorf("CheckHost(%q) = %v, want nil", host, err)
		}
	}
}

func TestPolicyMatchesExactAndWildcardPatterns(t *testing.T) {
	p := NewPolicy([]string{"tracker.example.org", "*.example.net"}, false)
	cases := []struct {
		host    string
		allowed bool
	}{
		{"tracker.example.org", true},
		{"TRACKER.EXAMPLE.ORG.", true},
		{"other.example.org", false},
		{"a.example.net", true},
		{"deep.a.example.net", true},
		{"example.net", false},
		{"evilexample.net", false},
	}
	for _, tc := range cases {
		err := p.CheckHost(tc.host)
		if tc.allowed && err != nil {
			t.Errorf("CheckHost(%q) = %v, want allowed", tc.host, err)
		}
		if !tc.allowed && !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("CheckHost(%q) = %v, want ErrHostNotAllowed", tc.host, err)
		}
	}
}

func TestPolicyDeniesPrivateAddresses(t *testing.T) {
	p := NewPolicy(nil, true)
	denied := []string{
		"127.0.0.1",
		"10.1.2.3",
		"192.168.1.5",
		"172.16.0.1",
		"169.254.10.10",
		"100.64.0.7",
		"0.0.0.0",
		"224.0.0.1",
		"::1",
		"fe80::1",
		"fc00::1",
		"::ffff:10.0.0.1",
	}
	for _, host := range denied {
		if err := p.CheckHost(host); !errors.Is(err, ErrForbiddenAddress) {
			t.Errorf("CheckHost(%q) = %v, want ErrForbiddenAddress", host, err)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	for _, host := range allowed {
		if err := p.CheckHost(host); err != nil {
			t.Errorf("CheckHost(%q) = %v, want allowed", host, err)
		}
	}
}

func TestPolicyAllowsPrivateAddressesWhenDisabled(t *testing.T) {
	p := NewPolicy(nil, false)
	for _, host := range []string{"127.0.0.1", "192.168.1.5", "::1"} {
		if err := p.CheckHost(host); err != nil {
			t.Errorf("CheckHost(%q) = %v, want allowed", host, err)
		}
	}
}
