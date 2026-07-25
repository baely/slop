# snip

A pastebin worth trusting. Paste text, get a link, control exactly how long it
lives.

- **Creating a paste requires the token.** Either log in through the browser or
  send `Authorization: Bearer <token>` from the command line.
- **Reading a paste is public but requires the unguessable slug.** There is no
  listing, no sequential ids and nothing to enumerate. A slug is 100 bits from
  `crypto/rand` rendered in a 32-symbol alphabet with no `i`, `l`, `o` or `u`.
- Per paste: an optional title, an expiry (10 minutes, 1 hour, 1 day, 1 week,
  30 days, never) and optional **burn after reading**.

Runs at <https://snip.baileys.dev>.

## No syntax highlighting

There is deliberately none, and there will not be. A highlighter means either a
third-party dependency (this app is Go standard library only) or a hand-rolled
tokenizer per language, which is a trap: it is a large amount of code that is
subtly wrong for every language it claims to support, and it puts
attacker-controlled text through a parser that then emits markup. Text is
served as text. What you get instead is line numbers, a Copy button, a Raw
link, and a soft-wrap toggle for long lines.

## Endpoints

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/` | token | The create form, plus the curl one-liner |
| `POST` | `/` | token | Create from the form; redirects to `/done/{slug}` |
| `POST` | `/api/pastes` | token | Create from the CLI. Body is the paste; options are query params. `201` with the paste URL as plain text |
| `GET` | `/p/{slug}` | none | The paste, with line numbers |
| `GET` | `/raw/{slug}` | none | `text/plain; charset=utf-8`, `nosniff`, `Content-Disposition: attachment` |
| `HEAD` | `/p/{slug}`, `/raw/{slug}` | none | Existence check. A HEAD is not a read, so it never burns a paste |
| `GET` | `/pastes` | token | Live pastes: slug, title, size, created, expiry |
| `POST` | `/pastes/delete` | token | Delete one paste (form field `slug`) |
| `GET` | `/done/{slug}` | token | The share links for a paste you just made |
| `GET` | `/login`, `POST` `/login`, `POST` `/logout` | — | Browser session |
| `GET` | `/healthz` | none | `200 ok` |
| `GET` | `/static/…` | none | Stylesheet and the small progressive-enhancement script |

### Creating a paste from the command line

```sh
curl -sS -H "Authorization: Bearer $SNIP_TOKEN" \
  --data-binary @notes.txt \
  "https://snip.baileys.dev/api/pastes?title=Notes&expiry=1d"
```

It prints the paste URL. Query parameters:

| param | values | default |
|---|---|---|
| `title` | free text, trimmed to 120 characters | none |
| `expiry` | `10m`, `1h`, `1d`, `1w`, `30d`, `never` | `1w` |
| `burn` | `1`/`true`/`yes`/`on` | off |

Piping works the same way:

```sh
git diff | curl -sS -H "Authorization: Bearer $SNIP_TOKEN" \
  --data-binary @- "https://snip.baileys.dev/api/pastes?expiry=1h&burn=1"
```

## Burn after reading

A burn paste is destroyed on its first successful read, whether that read is
`/p/{slug}` or `/raw/{slug}`. The whole operation — existence check, body read,
unlink, `fsync` of the directory — happens under the store's exclusive lock, so
two simultaneous readers cannot both get it, and the destroy is durable before
a single byte of the body is written to the response. If the unlink fails, the
body is not served at all. `TestBurnAfterReadingIsAtomicAndDurable` races 64
goroutines at one paste and asserts exactly one winner.

Because a burn paste is gone after the first read, its page carries no Raw link
(it could only 404) and says plainly that the page you are looking at is the
only remaining copy.

## Expiry

Expired pastes are removed two ways, and the second is the one that matters:

1. A background sweeper runs every minute and reclaims disk.
2. Every read re-checks the expiry, so an expired paste is never served even if
   the sweeper has not run yet.

A missing paste and an expired paste return the identical 404 page. Nothing
distinguishes "never existed" from "gone", by design.

## Caps

| thing | limit |
|---|---|
| Paste body | 1 MB (1,000,000 bytes). Larger is rejected with `413` and a plain message |
| Title | 120 characters |
| Live pastes | 5000 |
| Failed token checks | 10 per client per minute, then `429` |

## Security

- Token compared with `crypto/subtle.ConstantTimeCompare`. The browser session
  cookie is `HttpOnly`, `Secure`, `SameSite=Lax` and holds an HMAC of a random
  session id keyed by the token — never the token.
- Raw bodies are `text/plain; charset=utf-8` with `X-Content-Type-Options:
  nosniff` and `Content-Disposition: attachment`, so a paste of HTML cannot
  render as a page on `*.baileys.dev`. HTML views escape everything through
  `html/template`.
- Every response carries `nosniff`, `Referrer-Policy: no-referrer`,
  `X-Frame-Options: DENY` and a CSP of
  `default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'`.
- Slugs are validated against the alphabet before they can reach the
  filesystem.
- Request logs carry method, path, status, duration and a truncated client
  address (`/24` for IPv4, `/48` for IPv6). No query strings, no bodies, no
  headers.

## Configuration

| var | meaning | default |
|---|---|---|
| `ADDR` | listen address | `:8080` |
| `DATA_DIR` | persistence directory | `/data` |
| `BASE_URL` | absolute base for generated links | `http://localhost$ADDR` |
| `APP_TOKEN` | the shared secret | **required** — the process exits 1 if unset |
| `TZ` | display timezone | set by the deployment |

## Storage

One JSON file per paste under `$DATA_DIR/pastes/<slug>.json`, written to a temp
file in the same directory, `fsync`ed, then `os.Rename`d into place. Metadata
is held in memory behind a mutex and loaded once at startup; bodies stay on
disk and are read on demand, because 5000 pastes of 1 MB is not something to
keep resident. No database.

## Development

```sh
go vet ./... && gofmt -l . && go test ./...
APP_TOKEN=dev DATA_DIR=$(mktemp -d) ADDR=:8847 BASE_URL=http://127.0.0.1:8847 go run .
```

## Deployment

Built and pushed directly with docker and ssh, not through the infra repo:

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/snip:latest --push .
```

It runs on the host at `~/snip/docker-compose.yaml`, joined to the external
`web` network, behind Traefik at <https://snip.baileys.dev>. `APP_TOKEN` comes
from the `.env` file beside the compose file, and `/data` is a volume owned by
uid 10001.
