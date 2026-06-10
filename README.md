# torrent-proxy-lite

A small local HTTP/HTTPS proxy for BitTorrent tracker traffic. Point your torrent client at it as a regular HTTP proxy and it forwards everything to the real trackers, untouched, while logging what actually happened: which tracker, what status code, how long it took, how many bytes. Passkeys and other secrets never reach the logs.

I wanted a way to watch what my client says to its trackers without reaching for Wireshark every time. The tools that used to fill this niche (GreedyTorrent and friends) are long dead, Windows-only, and mostly existed to fake stats anyway. So: a proxy that forwards everything as-is and just tells you what's going on.

## What it does

- Forwards plain HTTP tracker requests exactly as the client sent them. Same query string, same headers, same body.
- Tunnels HTTPS through standard `CONNECT`. It never looks inside the TLS stream, it just moves bytes.
- Writes one structured log line per request: type (announce, scrape, connect, other), method, host, status, latency, bytes.
- Redacts `passkey`, `token`, `info_hash`, `peer_id` and similar values before anything is logged.
- Can restrict which tracker hosts are reachable, and blocks private network ranges by default.

## What it doesn't do

Worth being explicit, given the family history of this kind of tool:

- It does not modify `uploaded`, `downloaded`, `left`, `event` or any other announce parameter. There is no code path for it and there won't be one. If you're looking for a ratio faker, this isn't it.
- It does not intercept TLS. No generated certificates, no MITM, nothing to add to your trust store.
- It does not anonymize anything. Trackers and peers still see your real IP.

## When it's useful

- A tracker keeps rejecting announces and the client UI tells you nothing useful.
- You want to know which of your trackers are slow or flaky, with actual numbers.
- You need to check how a client behaves when forced through an HTTP proxy.
- You want log excerpts you can paste into a bug report without leaking your passkey.

## Getting started

Needs Go 1.25 or newer.

```bash
go install github.com/llenfan/torrent-proxy-lite/cmd/torrent-proxy@latest
```

Or from a checkout:

```bash
make build
./bin/torrent-proxy
```

Run it with no arguments and it listens on `127.0.0.1:8080`, with a health endpoint on `127.0.0.1:8081`:

```bash
torrent-proxy
curl http://127.0.0.1:8081/healthz
```

Use a config file to change anything:

```bash
torrent-proxy -config config.example.yaml
```

## Configuration

Every field is optional, whatever you leave out keeps its default. Unknown keys make startup fail instead of being silently ignored, so typos surface immediately.

```yaml
server:
  listen_addr: "127.0.0.1:8080"   # where the proxy listens
  health_addr: "127.0.0.1:8081"   # /healthz, must differ from listen_addr

proxy:
  connect_timeout: "10s"          # outbound TCP connect timeout
  idle_timeout: "60s"             # tunnels silent for this long get closed
  response_header_timeout: "15s"  # max wait for a tracker to start responding
  allow_hosts: []                 # empty allows everything; or list hosts, "*.example.org" works
  deny_private_networks: true     # refuse loopback, RFC 1918, link-local, CGNAT targets

logging:
  level: "info"                   # debug, info, warn, error
  format: "json"                  # json or text
  redact_query_values: true       # read "Logging" below before touching this
```

About `deny_private_networks`: the check runs again after DNS resolution, right before the actual connect, so a hostname that resolves to `192.168.x.x` is blocked too (this also covers DNS rebinding). Set it to `false` when testing against a tracker on your own machine.

## Pointing a client at it

Anything that supports an HTTP proxy works. For qBittorrent: Tools → Options → Connection, proxy type `HTTP`, host `127.0.0.1`, port `8080`, no authentication, and enable the proxy for BitTorrent/tracker connections.

UDP trackers can't go through an HTTP proxy; that's a protocol limitation, not a missing feature. Depending on its settings your client will hit them directly or fail. If the client routes peer connections through the proxy too, they become CONNECT tunnels and pass through like everything else.

## Logging

`info` level gives you one line per request:

```json
{"time":"2026-06-10T20:35:58-03:00","level":"INFO","msg":"request","type":"announce","method":"GET","host":"tracker.example.org","status":200,"duration_ms":48,"bytes":291}
```

`connect` lines carry `bytes_up` and `bytes_down` instead of `bytes`. At `debug` level the proxy also logs the URL it is about to forward, redacted:

```json
{"level":"DEBUG","msg":"forwarding","type":"announce","method":"GET","url":"http://tracker.example.org/announce?downloaded=0&info_hash=REDACTED&left=0&passkey=REDACTED&peer_id=REDACTED&port=6881&uploaded=0"}
```

The redacted URL exists only for the log (its query even gets re-sorted by the encoder). What goes to the tracker is the client's original request, byte for byte; there's a test asserting exactly that.

Redaction covers the usual suspects (`passkey`, `key`, `token`, `auth`, `uid`, `user`, `password`, `peer_id`, `info_hash`, `ip` and variants), path segments that look like hex keys or UUIDs, and URL credentials. Full list in `internal/redact/redact.go`. Setting `redact_query_values: false` only makes sense against a test tracker you own; path and credential redaction stay on regardless.

There's no dry-run mode, on purpose. A proxy that doesn't forward just breaks the client. If you want to inspect, run with `level: debug`.

## Security notes

- Binds to loopback by default and has no authentication. Binding to anything else logs a startup warning, because at that point anyone who can reach the port has an open proxy. Don't.
- TLS is never terminated. For HTTPS trackers the proxy learns the hostname from the CONNECT request and nothing else.
- Logs contain no secrets, but they do contain hostnames and timing. Treat them like any network log.
- Transport errors are sanitized before logging; Go's `url.Error` happily embeds the full URL, query included.

## Limitations

- No UDP trackers (see above).
- HTTP proxy only, no SOCKS5 for now.
- Path redaction is a heuristic: hex or UUID-looking segments of 16 or more characters. A plain-HTTP tracker embedding some other secret format in its path would get past it. HTTPS trackers don't have this problem, their paths are never visible here.
- The allowlist takes hostnames and IPv4 literals, not IPv6 literals.
- On shutdown, in-flight HTTP requests get 10 seconds to finish; remaining tunnels are then cut.

## Maybe later

Prometheus metrics, optional proxy auth, a SOCKS5 listener, a Docker image, and opt-in per-tracker stats (latency, failure rate), strictly read-only. No timeline on any of it.

## Development

```bash
make test   # unit + integration
make lint   # go vet + gofmt check
```

Unit tests live next to their packages. The ones in `tests/` start the real proxy and run real requests against local HTTP and TLS origins: transparent forwarding, CONNECT tunneling, allowlist and private-network enforcement, the health endpoint, and a test that plants secrets in a request and greps the logs for them.

## License

MIT, see [LICENSE](LICENSE).
