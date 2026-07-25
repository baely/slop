# catch

A webhook and request inspector. Create a bin, point a sender at its URL, and
read exactly what it sent — method, sub-path, query, every header, the body
byte for byte.

Built for debugging the things that talk to this estate: the `bssid-reporter`
iOS app POSTing WiFi BSSIDs, a Traccar server, `messenger` forwarding into
Chatwoot. When one of them misbehaves, point it here instead and read the truth.

## How it works

1. Sign in with the app token (or send `Authorization: Bearer <token>`).
2. Create a bin. It gets an unguessable 20-character id and a URL:
   `https://catch.baileys.dev/b/<id>`.
3. Send anything to that URL. Any method — `GET`, `POST`, `PUT`, `PATCH`,
   `DELETE`, `HEAD`, `OPTIONS`, or something you made up. Any path below it:
   `/b/<id>/anything/here` is captured with the sub-path recorded. Any content
   type.
4. The bin replies with a one-line plain-text ack and the bin's configured
   status (default `200`; set it to `202`, `204`, `500`, whatever the sender
   needs to see). `204` replies with no body.
5. Open the bin page to read the captures, newest first. It polls itself every
   4 seconds; it also works perfectly with JavaScript switched off — reload.

Bodies are pretty-printed when they are JSON, decoded into a table when they
are `application/x-www-form-urlencoded`, and shown as a hex dump when they are
not valid UTF-8. A body that claims to be JSON but is not says
`Body is not valid JSON.` and is shown raw.

## Endpoints

| route | auth | what |
|---|---|---|
| `ANY /b/<id>` and `ANY /b/<id>/<sub-path>` | none | capture. This is the only public write path. |
| `GET /healthz` | none | `200 ok` |
| `GET /` | token | index: every bin, request count, last seen, expiry |
| `POST /bins` | token | create a bin (`label`, `status`) |
| `GET /bin/<id>` | token | read a bin |
| `GET /bin/<id>/rows` | token | the request list as an HTML fragment (used by the poll) |
| `GET /bin/<id>/r/<rid>/raw` | token | the raw captured body, as an inert download |
| `POST /bin/<id>/settings` | token | change label and reply status |
| `POST /bin/<id>/clear` | token | drop every captured request, keep the bin |
| `POST /bin/<id>/delete` | token | delete the bin and everything in it |
| `GET /login`, `POST /login`, `POST /logout` | — | token → session cookie |

Auth is either `Authorization: Bearer <token>` or the login form, which sets an
`HttpOnly; Secure; SameSite=Lax` cookie holding an HMAC of a random session id
keyed by the token — never the token itself. Ten failed token checks per IP per
minute earns a `429`.

## Caps and retention

| thing | limit |
|---|---|
| requests kept per bin | the newest **100** |
| body stored per request | **64 KB** (the page says `Body truncated at 64 KB.`; the true size is still recorded) |
| body read off the wire | 8 MB, then discarded |
| bin lifetime | deleted **30 days** after it is last written to or viewed |
| label | 80 characters |

Both caps are stated on the index and on every bin page. A sweeper runs at
startup and hourly.

## Security notes

A captured body is attacker-supplied content, and `*.baileys.dev` is a shared
cookie domain, so:

- raw bodies are served `text/plain; charset=utf-8` with
  `X-Content-Type-Options: nosniff`, `Content-Disposition: attachment` and
  `default-src 'none'; sandbox` — never rendered;
- everything in the HTML view goes through `html/template` escaping. There is a
  test (`TestCapturedContentIsEscaped`) that captures `<script>alert(1)</script>`
  in a body, a header and a query string and asserts none of it survives into
  the page;
- every response carries `nosniff`, `Referrer-Policy: no-referrer`,
  `X-Frame-Options: DENY` and a CSP with `script-src 'self'`;
- bin ids are 100 bits from `crypto/rand` over a Crockford-style base32
  alphabet, so a bin URL is a capability;
- cross-site form posts are refused on `Origin` mismatch, on top of
  `SameSite=Lax`;
- request logs carry the method, path (no query string), status, duration and a
  truncated client IP (`/24`, `/48`). No bodies, no headers, no secrets.

## Configuration

| var | default | meaning |
|---|---|---|
| `ADDR` | `:8080` | listen address |
| `DATA_DIR` | `/data` | one JSON file per bin, written atomically |
| `BASE_URL` | `http://localhost:8080` | used to build capture URLs and to check `Origin` |
| `APP_TOKEN` | — | **mandatory**; the service exits 1 if it is empty |
| `TZ` | — | `Australia/Melbourne` in the deployment |

## Running locally

```sh
APP_TOKEN=$(openssl rand -hex 24) DATA_DIR=$(mktemp -d) ADDR=:8842 \
  BASE_URL=http://localhost:8842 go run .
```

```sh
go test ./...
```

## Deployment

Go standard library only, no third-party modules. Built and pushed directly
with docker over ssh (not through the infra repo):

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/catch:latest --push .
```

It runs on the host from `~/catch/docker-compose.yaml`, joined to the external
`web` network, behind Traefik at `https://catch.baileys.dev`. `DATA_DIR` is a
volume chowned to uid 10001 (the container runs as a non-root user).
`APP_TOKEN` comes from the host env file next to the compose file.
