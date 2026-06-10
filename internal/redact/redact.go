package redact

import (
	"net/url"
	"strings"
)

const placeholder = "REDACTED"

var sensitiveKeys = map[string]struct{}{
	"announce_key": {},
	"apikey":       {},
	"api_key":      {},
	"auth":         {},
	"authkey":      {},
	"auth_key":     {},
	"infohash":     {},
	"info_hash":    {},
	"ip":           {},
	"ipv4":         {},
	"ipv6":         {},
	"key":          {},
	"localip":      {},
	"pass":         {},
	"passkey":      {},
	"pass_key":     {},
	"password":     {},
	"peerid":       {},
	"peer_id":      {},
	"rsskey":       {},
	"secret":       {},
	"secret_key":   {},
	"sk":           {},
	"token":        {},
	"torrent_pass": {},
	"trackerid":    {},
	"tracker_id":   {},
	"uid":          {},
	"user":         {},
	"userid":       {},
	"user_id":      {},
	"username":     {},
}

func URL(u *url.URL, redactQueryValues bool) string {
	c := *u
	if c.User != nil {
		c.User = url.User(placeholder)
	}
	c.Path = redactedPath(c.Path)
	c.RawPath = ""
	if redactQueryValues {
		c.RawQuery = redactedQuery(c.RawQuery)
	}
	return c.String()
}

func redactedQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return placeholder
	}
	for key := range values {
		if isSensitiveKey(key) {
			values[key] = []string{placeholder}
		}
	}
	return values.Encode()
}

func redactedPath(path string) string {
	segments := strings.Split(path, "/")
	changed := false
	for i, segment := range segments {
		if looksLikeSecret(segment) {
			segments[i] = placeholder
			changed = true
		}
	}
	if !changed {
		return path
	}
	return strings.Join(segments, "/")
}

func isSensitiveKey(key string) bool {
	_, ok := sensitiveKeys[strings.ToLower(key)]
	return ok
}

func looksLikeSecret(segment string) bool {
	hexDigits := 0
	for _, r := range segment {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			hexDigits++
		case r == '-':
		default:
			return false
		}
	}
	return hexDigits >= 16
}
