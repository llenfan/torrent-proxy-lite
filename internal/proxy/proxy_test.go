package proxy

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/announce", "announce"},
		{http.MethodGet, "/announce.php", "announce"},
		{http.MethodGet, "/scrape", "scrape"},
		{http.MethodGet, "/scrape.php", "scrape"},
		{http.MethodGet, "/index.html", "http"},
		{http.MethodConnect, "", "connect"},
	}
	for _, tc := range cases {
		r := &http.Request{Method: tc.method, URL: &url.URL{Path: tc.path}}
		if got := classify(r); got != tc.want {
			t.Errorf("classify(%s %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRemoveHopByHopHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "X-Custom-Hop, Keep-Alive")
	h.Set("X-Custom-Hop", "1")
	h.Set("TE", "trailers")
	h.Set("Proxy-Authorization", "Basic abc")
	h.Set("User-Agent", "test-client/1.0")
	removeHopByHopHeaders(h)
	for _, gone := range []string{"Connection", "X-Custom-Hop", "TE", "Proxy-Authorization"} {
		if h.Get(gone) != "" {
			t.Errorf("%s should have been removed", gone)
		}
	}
	if h.Get("User-Agent") != "test-client/1.0" {
		t.Error("User-Agent should have been kept")
	}
}

func TestSanitizeErrorHidesURLQuery(t *testing.T) {
	inner := errors.New("connection refused")
	err := &url.Error{Op: "Get", URL: "http://tracker.example.org/announce?passkey=supersecret", Err: inner}
	got := sanitizeError(err).Error()
	if strings.Contains(got, "supersecret") {
		t.Errorf("sanitized error still contains the secret: %s", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("sanitized error lost the cause: %s", got)
	}
}
