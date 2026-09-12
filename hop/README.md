# hop

A URL shortener that answers from prebaked HTTP responses. Links are
registered in a text file, one `key=url` per line. The server is a bare TCP
listener: it reads the request head, strips the query, looks the path up in a
map, and writes a response that was fully serialised at startup. No
`net/http`, no per-request allocation, no dependencies.

```
$ curl -si https://hop.baileys.dev/linkedin?utm_source=share
HTTP/1.1 302 Found
Location: https://linkedin.com/in/baileybutler1
Content-Length: 0
```

## Links file

```
# comments and blank lines are ignored
linkedin=https://linkedin.com/in/baileybutler1
github=https://github.com/baely
docs/api=https://example.com/docs?x=1&y=2
/=https://baileybutler.com
```

- The first `=` splits key from URL, so URLs may contain `=`.
- Keys are case-insensitive and matched without surrounding slashes:
  `/LinkedIn`, `/linkedin` and `/linkedin/` are the same link. Keys may
  contain `/`, so nested paths work. Matching is exact otherwise: there is no
  prefix matching and no percent-decoding.
- Allowed key characters: letters, digits and `- _ . ~ / + @ :`.
- URLs must be absolute (`https://…`, `mailto:…`). Whitespace, control bytes
  and non-ASCII are rejected at load time so nothing can break out of the
  `Location` header.
- Duplicate keys are an error. A bad file fails startup, and fails a reload
  while keeping the previous links.
- A key of `/` redirects the root path. Without it the root serves a small
  index page.

Send `SIGHUP` to reload the file without dropping connections.

## Request handling

- `GET` and `HEAD` are served. Anything else gets a baked 405 with an
  `Allow` header.
- The query string and fragment are dropped before lookup. Absolute-form
  targets (`GET http://host/path HTTP/1.1`) are accepted.
- HTTP/1.1 connections are kept alive and pipelined requests are answered in
  order. HTTP/1.0 connections always close. A request that declares a body
  is answered, then closed, since the body is never read.
- Unknown paths get a 404 page. Index and error pages are static, so no
  request data is ever reflected into a response.

## Slowloris and friends

Every limit exists to bound what a slow or hostile client can hold open.

| Limit | Default | What it stops |
|---|---|---|
| `HEAD_TIMEOUT` | `5s` | Time from a request's first byte to its blank line. A client trickling headers is cut off with a 408 no matter how many bytes it has sent. |
| `IDLE_TIMEOUT` | `10s` | A kept-alive connection sitting with nothing in flight is closed silently. |
| `WRITE_TIMEOUT` | `5s` | A client that never reads its response. |
| `MAX_HEAD_BYTES` | `8192` | Request line plus headers. Larger heads get a 431 and are closed. |
| `MAX_CONNS` | `4096` | Concurrent open connections. New ones beyond that get an instant baked 503 with `Retry-After: 1`, never a goroutine. |
| `MAX_CONNS_PER_IP` | `0` (off) | Connections per remote address. Leave off behind a reverse proxy, where every connection carries the proxy's IP. |

An attacker therefore has to sustain roughly `MAX_CONNS / HEAD_TIMEOUT` new
connections per second (about 800/s at the defaults) to keep the pool full,
and each one costs the server one goroutine and one 8 KiB buffer for at most
five seconds.

## Configuration

All configuration is by environment variable.

| Variable | Default | Meaning |
|---|---|---|
| `ADDR` | `:8080` | Listen address. |
| `LINKS` | `links.txt` | Path to the links file. |
| `REDIRECT_STATUS` | `302` | One of `301`, `302`, `307`, `308`. A 301 lets browsers cache the redirect, which is faster but makes later edits sticky. |
| timeouts and limits | see above | |

## Running

```
go run .                       # serves links.txt on :8080
LINKS=my.txt ADDR=:9000 go run .
kill -HUP $(pgrep hop)         # reload links
go test -race ./...
```

Integration tests bind loopback ports, so they need network access.

## Performance

On an M-series laptop, 100 keep-alive connections from a Go client on the
same machine:

| Scenario | Throughput |
|---|---|
| HTTP/1.1 keep-alive, redirect | ~104k req/s, 0 errors |
| HTTP/1.1 keep-alive, 404 page (2 KiB body) | ~112k req/s |
| HTTP/1.0 `ab`, new connection per request | ~24k req/s |
| Parse and lookup, one request head | ~350 ns, 0 allocs |

Resident memory after the runs above: under 7 MiB. The client was the
bottleneck in the keep-alive runs.

## Deployment

The image is `registry.baileys.dev/hop:latest`, built with

```
docker build --platform linux/amd64 -t registry.baileys.dev/hop:latest --push .
```

It runs as `nonroot` from a distroless base with the sample `links.txt`
baked in at `/etc/hop/links.txt`. The compose file in
[baely/infra](https://github.com/baely/infra) under
`docker/github.com_baely_slop_hop/` overlays the real links at that path and
routes `hop.baileys.dev` through Traefik. Editing a link is a PR to that
file, and merging it redeploys.
