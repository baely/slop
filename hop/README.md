# hop

Two services in one module: **hop**, the redirector, and **hop-writer**,
the internal page that edits its links file.

## hop

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
=https://baileybutler.com
```

- The first `=` splits key from URL, so URLs may contain `=`.
- The key is matched byte-for-byte against the request path after the
  leading slash, with the query string cut off. `linkedin` serves exactly
  `/linkedin`: `/LinkedIn` and `/linkedin/` are 404s. No case folding, no
  slash trimming, no percent-decoding.
- An empty key is the root. Without one the root serves a small index page.
- Keys may not start with `/` or contain whitespace, `?` or `#`, since those
  could never match.
- URLs must be absolute (`https://…`, `mailto:…`). Whitespace, control bytes
  and non-ASCII are rejected at load time so nothing can break out of the
  `Location` header.
- Duplicate keys are an error. A bad file fails startup, and fails a reload
  while keeping the previous links.

Every link answers with a 302. hop polls the file every two seconds and
reloads when it changes, so edits by hop-writer or by hand take effect
without a restart. `SIGHUP` reloads immediately.

## Request handling

- `GET` and `HEAD` are served. Anything else gets a 405 with an `Allow`
  header.
- The request target must be a path. Everything up to the first `?` is the
  map key; there is no other processing.
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

## hop-writer

The internal admin page at https://hop.int.xbd.au (LAN only, via Traefik's
`internal-only` middleware). It lists every link with its short URL, adds
links, changes a target in place, deletes, and mints random keys.

- **Random keys per character rules**: length (1 to 32), any mix of `a-z`,
  `A-Z` and `0-9`, and a switch to skip `0 O 1 l I`, which read alike.
  Leave the key blank and Add Link mints one; the Generate button previews
  one first. Keys are drawn with `crypto/rand` and never collide with an
  existing key.
- **Edits are exact**: hop-writer rewrites only the affected line, so
  comments, blank lines and order in `links.txt` survive.
- **Every write is validated with hop's own parser** (the shared `links`
  package) and then swapped in with an atomic rename, so hop never loads a
  half-written or invalid file. If the file was hand-edited into something
  hop cannot parse, hop-writer refuses to write until it is fixed.
- No auth of its own; the internal-only middleware is the gate.

Flags: `-addr` (`:8080`), `-links` (`/data/links.txt`), `-base`
(`https://bly.au`, used to display short URLs).

## Running

```
go run .                                # hop, serves ./links.txt on :8080
go run . -addr :9000 -links my.txt
go run ./writer -addr :9001 -links my.txt -base http://localhost:9000
go test -race ./...                     # needs loopback network access
```

## Deployment

```
docker build --platform linux/amd64 -t registry.baileys.dev/hop:latest --push .
docker build --platform linux/amd64 -t registry.baileys.dev/hop-writer:latest --push -f writer/Dockerfile .
```

Both run on the home server from `~/manual-deploys/hop/deploy.yaml` as one
compose project sharing `./data/links.txt`: hop mounts `./data` read-only,
hop-writer read-write as the host user so the file stays hand-editable.

Traefik routes `bly.au` to hop with a TCP router (TLS terminated by SNI,
plaintext piped through; hop handles HTTP/2 or HTTP/1.1, whichever the
browser negotiated) and `hop.int.xbd.au` to hop-writer with an HTTP router
behind `internal-only@file`.
