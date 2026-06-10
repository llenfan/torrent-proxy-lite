package redact

import (
	"net/url"
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestURLRedactsSensitiveQueryValues(t *testing.T) {
	u := mustParse(t, "http://tracker.example.org/announce?passkey=supersecret&uploaded=123&port=6881")
	got := URL(u, true)
	if strings.Contains(got, "supersecret") {
		t.Errorf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "passkey=REDACTED") {
		t.Errorf("passkey not redacted: %s", got)
	}
	if !strings.Contains(got, "uploaded=123") || !strings.Contains(got, "port=6881") {
		t.Errorf("non-sensitive values should be preserved: %s", got)
	}
}

func TestURLRedactsKeysCaseInsensitively(t *testing.T) {
	u := mustParse(t, "http://h/announce?PassKey=hidden1&Info_Hash=%01%02&PEER_ID=hidden2")
	got := URL(u, true)
	for _, secret := range []string{"hidden1", "hidden2", "%01%02"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q leaked: %s", secret, got)
		}
	}
}

func TestURLKeepsQueryWhenRedactionDisabled(t *testing.T) {
	u := mustParse(t, "http://h/0123456789abcdef0123456789abcdef/announce?passkey=visible")
	got := URL(u, false)
	if !strings.Contains(got, "passkey=visible") {
		t.Errorf("query should be untouched when disabled: %s", got)
	}
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("hex path segment should still be redacted: %s", got)
	}
}

func TestURLRedactsHexPathSegments(t *testing.T) {
	u := mustParse(t, "http://h/0123456789abcdef0123456789abcdef/announce")
	got := URL(u, true)
	if got != "http://h/REDACTED/announce" {
		t.Errorf("got %s", got)
	}
}

func TestURLRedactsUUIDPathSegments(t *testing.T) {
	u := mustParse(t, "http://h/550e8400-e29b-41d4-a716-446655440000/announce")
	got := URL(u, true)
	if strings.Contains(got, "550e8400") {
		t.Errorf("uuid leaked: %s", got)
	}
}

func TestURLKeepsNormalPathSegments(t *testing.T) {
	for _, raw := range []string{
		"http://h/announce",
		"http://h/announce.php",
		"http://h/abc123/scrape",
		"http://h/deadbeef-cafe/x",
	} {
		u := mustParse(t, raw)
		if got := URL(u, true); got != raw {
			t.Errorf("URL(%q) = %q, want unchanged", raw, got)
		}
	}
}

func TestURLRedactsUserinfo(t *testing.T) {
	u := mustParse(t, "http://user:hunter2@h/announce")
	got := URL(u, true)
	if strings.Contains(got, "hunter2") || strings.Contains(got, "user:") {
		t.Errorf("userinfo leaked: %s", got)
	}
	if !strings.Contains(got, "REDACTED@") {
		t.Errorf("userinfo placeholder missing: %s", got)
	}
}

func TestURLRedactsUnparsableQueryEntirely(t *testing.T) {
	u := mustParse(t, "http://h/announce")
	u.RawQuery = "passkey=s%zz&x=1"
	got := URL(u, true)
	if strings.Contains(got, "passkey=s") {
		t.Errorf("unparsable query leaked: %s", got)
	}
	if !strings.HasSuffix(got, "?REDACTED") {
		t.Errorf("unparsable query should collapse to placeholder: %s", got)
	}
}
