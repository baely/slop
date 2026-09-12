# hop

A URL shortener that answers from prebaked HTTP responses. Links are
registered in a text file, one `key=url` per line. The server is a bare TCP
listener: it reads the request head, strips the query, looks the path up in a
map, and writes a response that was fully serialised at startup. It speaks
HTTP/1.1 and HTTP/2 (plaintext, as handed over by a TLS-terminating proxy)
without `net/http`.

```
$ curl -si https://bly.au/linkedin?utm_source=share
HTTP/2 302
location: https://linkedin.com/in/baileybutler1
content-length: 0
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
  contain `/`. Matching is otherwise exact: no prefix matching, no
  percent-decoding.
- Allowed key characters: letters, digits and `- _ . ~ / + @ :`.
- URLs must be absolute (`https://…`, `mailto:…`). Whitespace, control bytes
  and non-ASCII are rejected at load time so nothing can break out of the
  `Location` header.
- Duplicate keys are an error. A bad file fails startup, and fails a reload
  while keeping the previous links.
- A key of `/` redirects the root path. Without it the root serves a small
  index page.

Every link answers with a 302. `SIGHUP` reloads the file without dropping
connections.

## Request handling

- `GET` and `HEAD` are served. Anything else gets a 405 with an `Allow`
  header.
- The query string and fragment are dropped before lookup.
- HTTP/1.1 connections are kept alive and pipelined requests are answered in
  order. HTTP/1.0 connections always close. A request that declares a body
  is answered, then closed, since the body is never read.
- A connection that opens with the HTTP/2 preface is served as HTTP/2: each
  stream is answered from a prebaked HPACK header block as soon as its
  headers are complete. Only header decoding (via `x/net/http2/hpack`) does
  work per request.
- Unknown paths get a 404 page. Index and error pages are static, so no
  request data is ever reflected into a response.

## Slowloris and friends

Limits are fixed in `main.go`; they exist to bound hostile clients, not to
be tuned per deployment.

| Limit | Value | What it stops |
|---|---|---|
| head timeout | 5s | Time from a request's first byte to its blank line, or from a HEADERS frame to the end of its block. A client trickling headers is cut off no matter how many bytes it has sent. |
| idle timeout | 10s | A kept-alive connection sitting with nothing in flight is closed (HTTP/2 gets a `GOAWAY`). |
| write timeout | 5s | A client that never reads its response. |
| head size | 8 KiB | Request line plus headers, or an HTTP/2 header block. Larger gets a 431 and the connection is closed. |
| connections | 4096 | Beyond it new connections get an instant baked 503 with `Retry-After: 1`, never a goroutine. |

An attacker therefore has to sustain roughly 800 new connections per second
to keep the pool full, and each one costs one goroutine and one buffer for
at most five seconds.

## Running

```
go run .                                # serves ./links.txt on :8080
go run . -addr :9000 -links my.txt
kill -HUP $(pgrep hop)                  # reload links
go test -race ./...                     # needs loopback network access
```

## Performance

100 keep-alive HTTP/1.1 connections from a Go client on the same laptop:
about 130k redirects/s with zero errors and 8 MiB resident. Parsing and
routing one request takes about 350 ns with no allocation.

## Deployment

```
docker build --platform linux/amd64 -t registry.baileys.dev/hop:latest --push .
```

hop runs on the home server from `~/manual-deploys/hop/`, which holds
`deploy.yaml` and the live `links.txt`. The container runs
`/hop -links /links.txt` with that file bind-mounted, so adding a link is an
edit there followed by `docker compose up -d` (or `docker kill -s HUP` to
reload in place). Pulling a new image is `docker compose pull && docker
compose up -d`.

Traefik routes `bly.au` with a TCP router: it terminates TLS by SNI and
pipes the plaintext stream to hop. Whatever the browser negotiated via ALPN,
HTTP/2 or HTTP/1.1, hop handles it.
