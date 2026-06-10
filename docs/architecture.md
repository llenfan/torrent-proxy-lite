# Architecture

## Overview

```
torrent client ──HTTP proxy──▶ torrent-proxy ──▶ tracker
                                   │
                                   ├─ plain HTTP: forwarded via http.Transport, untouched
                                   ├─ HTTPS: CONNECT, opaque TCP tunnel, no TLS termination
                                   └─ /healthz on a separate listener
```

Two listeners run inside one process: the proxy itself and a small health endpoint. They are separate so the proxy port speaks only the proxy protocol.

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/torrent-proxy` | Flags, config loading, logger construction, signal handling, exit codes. |
| `internal/config` | YAML config with defaults, strict parsing (unknown fields rejected), validation at startup. |
| `internal/logging` | slog construction from config (level, json/text). |
| `internal/redact` | Pure functions that produce log-safe URL strings. Never touches forwarded requests. |
| `internal/proxy` | The http.Handler: absolute-form HTTP forwarding, CONNECT tunneling, host policy, guarded dialer. |
| `internal/server` | Wires config plus proxy into two http.Servers, graceful shutdown, health endpoint. |
| `internal/version` | Version string injected via ldflags. |

## Request flow, plain HTTP

1. Client sends an absolute-form request (`GET http://tracker/announce?... HTTP/1.1`).
2. Scheme and host policy checks (allowlist, private-network denial for IP literals).
3. The request is cloned, hop-by-hop headers are removed, and it is sent through a dedicated `http.Transport` with `DisableCompression: true` so the body and `Accept-Encoding` pass through exactly as the client sent them. The transport ignores proxy environment variables, never follows redirects, and never rewrites the URL, query, or body.
4. The response is relayed with hop-by-hop headers stripped. One `info` log line records type, method, host, status, latency, and bytes.

The proxy does not add `Via` or `X-Forwarded-For`. Transparency toward the tracker takes priority over RFC 7230's SHOULD for `Via`, because some trackers treat proxy headers as a reason to reject announces.

## Request flow, CONNECT

1. Policy check on the target host.
2. Outbound dial through the same guarded dialer used for HTTP.
3. The client connection is hijacked, `200 Connection Established` is written, and any bytes the client pipelined ahead are flushed upstream.
4. Both directions are copied concurrently. A supervisor goroutine tracks last activity in either direction and closes the tunnel after `idle_timeout` of total silence. Half-closes propagate via `CloseWrite`.

The tunnel is a byte pipe. Nothing inside it is parsed, logged, or modified.

## Private network denial

`Policy.CheckHost` rejects IP literals immediately (403). For hostnames the check happens in the dialer's `Control` hook, which runs after DNS resolution and immediately before `connect(2)`, so every address actually dialed is vetted. This closes the DNS rebinding gap, and a denied dial surfaces as 502.

## Redaction

Logging never emits raw URLs. `redact.URL` copies the URL and replaces values of sensitive query keys, hex or UUID style path segments of 16 or more characters, and userinfo. Transport errors are unwrapped from `url.Error` before logging because `url.Error.Error()` embeds the full URL including the query.

## Shutdown

On SIGINT or SIGTERM: listeners stop accepting, in-flight HTTP requests get up to 10 seconds to finish, then remaining CONNECT tunnels are force-closed. Hijacked connections are tracked in the proxy for exactly this purpose.
