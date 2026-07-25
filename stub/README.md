# stub

A link shortener on a domain that will still be here next year, with click
stats that admit when the clicker was a robot.

Short links are `https://stub.baileys.dev/<slug>`. Creating one needs the
token. Following one does not — that is the whole point.

Most shorteners quietly inflate their numbers: every Slack unfurl, every
Googlebot pass, every browser prefetch lands in the same "clicks" counter as a
person who actually chose to follow the link. stub classifies each request into
`browser` / `bot` / `unknown` and **shows bot traffic in its own column** rather
than folding it in or throwing it away. If the heuristic gets one wrong, the
click moves columns — it never disappears.

## Endpoints

### Public

| method | path | behaviour |
|---|---|---|
| `GET` | `/<slug>` | `302` to the target. No HTML body, `Cache-Control: no-store`. Records the click. |
| `GET` | `/<slug>` (dead link) | `410 Gone` with a plain page saying which limit it hit — never a redirect, never a 404 |
| `GET` | `/<unknown>` | `404` |
| `GET` | `/healthz` | `200 ok` |
| `GET` | `/static/…` | stylesheet and the one small script |
| `GET` | `/robots.txt` | `Disallow: /` |

Slug lookup is case-insensitive, so `/Demo` and `/demo` are the same link and
the second one can never be registered separately.

### Token-gated

| method | path | behaviour |
|---|---|---|
| `GET` | `/` | login form, or the link table once signed in |
| `POST` | `/login` | token → session cookie |
| `POST` | `/logout` | clears it |
| `GET` | `/admin/link/<slug>` | the stats page |
| `POST` | `/admin/create` | create from the UI (CSRF-checked) |
| `POST` | `/admin/state` | enable / disable |
| `POST` | `/admin/delete` | delete |
| `POST` | `/api/links` | create → `201`, `400` on a bad target or reserved slug, `409` on a collision |
| `GET` | `/api/links` | list → `200` |
| `GET` | `/api/links/<slug>` | full stats including per-day counts and referrers |
| `DELETE` | `/api/links/<slug>` | `204`, or `404` |

The API accepts `Authorization: Bearer <APP_TOKEN>`; the UI accepts either that
or the session cookie.

### Creating a link from the command line

```sh
curl -sS -X POST https://stub.baileys.dev/api/links \
  -H "Authorization: Bearer $APP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"target":"https://example.com/a/very/long/path","slug":"demo","expires_in":"720h","max_clicks":0}'
```

Every field except `target` is optional. `slug` blank means one is generated.
`expires_in` is a Go duration; `expires_at` takes RFC3339 instead. `max_clicks`
of `0` means unlimited. The response carries the `short_url` and a `Location`
header.

## Slugs

Generated slugs are 7 characters from a 31-symbol alphabet
(`23456789abcdefghjkmnpqrstuvwxyz`) — no `0`/`O`, no `1`/`l`/`I`, so a link read
off a screen or a photo can be typed back correctly. Drawn from `crypto/rand`
with rejection sampling, so the distribution is flat.

Custom slugs allow letters, digits, `-` and `_`, up to 64 characters. Two
things are refused with a message that says why:

- **collisions** — `Slug "demo" is already taken.` (`409`)
- **reserved paths** — the app's own routes (`/`, `/healthz`, `/static/…`,
  `/admin`, `/api`, `/login`, `/logout`, `/robots.txt`, `/favicon.ico` and a
  handful of likely future ones) can never be claimed, so a link cannot shadow
  the UI. (`400`)

Slugs are deliberately **not** capabilities. The redirect they guard is public
anyway; everything worth protecting (the table, the stats, delete) sits behind
`APP_TOKEN`. Session ids, which *are* capabilities, come from `crypto/rand`
with ~99 bits.

## Target validation

This is the one place a shortener turns into a weapon, so it is strict. Only
absolute `http` and `https` URLs with a host are accepted. Everything else is
refused with a message naming the specific problem:

| target | result |
|---|---|
| `javascript:alert(1)` | `Scheme "javascript" is not allowed.` |
| `data:text/html;base64,…` | `Scheme "data" is not allowed.` |
| `file:///etc/passwd` | `Scheme "file" is not allowed.` |
| `vbscript:` `blob:` `intent:` `mailto:` `ftp:` … | same shape, naming the scheme |
| `//evil.example.com/x` | `Target is scheme-relative.` |
| `example.com/path` | `Target has no scheme.` |
| `http:///path` | `Target has no host.` |
| `java\tscript:alert(1)` | `Target contains a control character.` |

There is a table-driven test for each of these. stub never fetches a target, so
there is no SSRF surface — it only ever hands the URL to the visitor's browser.

## Stats, honestly

Per link:

- **Clicks** — human and unclassified traffic.
- **Bots** — crawlers, chat-app link-preview fetchers and browser prefetches,
  shown separately with their share of the total.
- **Unique-ish** — distinct visitors. See the privacy note below, and take the
  name seriously: shared NAT under-counts, someone switching networks or
  browsers over-counts.
- **First / last click**, humanized with the exact timestamp alongside.
- **Clicks per day, last 30 days** — a hand-written SVG bar chart. Single teal
  series, one y-axis, mono values, no motion. Bot traffic is excluded from the
  chart and the excluded count is stated underneath.
- **Referrers** — origins only (`scheme://host`); paths and query strings are
  discarded on the way in, because referrer query strings routinely carry
  tokens. Bot traffic is not counted here.

### How a click is classified

Two unambiguous substring lists plus a word-boundary list: things that announce
themselves (`curl/`, `python-requests`, `Googlebot`, `facebookexternalhit`,
`Slackbot`, `HeadlessChrome`, …), plus `Sec-Purpose`/`Purpose`/`X-Moz` prefetch
hints, which win outright — a browser speculatively warming a link is not a
person choosing to follow it. An empty User-Agent is `unknown`, not `bot`.

It is a heuristic, not a bot database, and it errs toward calling traffic
automated. That is a deliberate trade: the cost of a false positive is a number
in the wrong column, which you can see, not a click silently added to a total
you would have believed.

### Privacy

No raw IP address and no raw User-Agent is ever written to disk. Uniqueness is
`HMAC-SHA256(salt, ip + "\0" + user-agent)` truncated to 12 hex characters,
where the salt is 32 random bytes generated once per install and kept in the
data file. Two installs produce different hashes for the same visitor, and the
hash cannot be walked back to an address without the salt. Request logs carry a
masked IP only (`203.0.113.x`).

## Per-link options

- **Expiry** — after it passes, the link returns `410`. The boundary is
  inclusive: at the exact expiry time the link is already gone.
- **Max clicks** — the link stops resolving after N. It counts **every**
  resolution, bots included, because it is a limit rather than a metric. The
  request that hits the limit is refused and is not counted.
- **Enabled / disabled** — a toggle, reversible, no data lost.

All three produce `410 Gone` with a plain page saying which one applied. Never
a redirect, never a `404` — the link existed; it just does not resolve any more.

## No QR codes

Deliberately left out. A QR encoder is Reed–Solomon over GF(256), mask
selection, and version/mode tables — several hundred lines that are either
correct or silently produce a code that some scanners read and others do not.
Pulling in a library would break the standard-library-only rule; writing one
without proving it against known-good vectors would ship a coin flip. Any phone
camera or `qrencode "$(pbpaste)"` does the job. If it ever earns its place it
gets its own file and its own test vectors.

## Configuration

| var | default | meaning |
|---|---|---|
| `ADDR` | `:8080` | listen address |
| `DATA_DIR` | `/data` | where `stub.json` lives |
| `BASE_URL` | `https://stub.baileys.dev` | used to build the short URLs shown in the UI |
| `APP_TOKEN` | — | **mandatory**; the process logs a line and exits 1 if it is empty |
| `TZ` | set by the deployment | day bucketing for the chart uses local time |

There is no default token and none is generated. Rotating `APP_TOKEN`
invalidates every session cookie, since the cookie is an HMAC keyed by the
token rather than the token itself.

## Caps and retention

| thing | cap |
|---|---|
| links stored | 2000 |
| target length | 2048 characters |
| slug length | 64 characters |
| request body | 64 KiB |
| per-day click counters | 90 days per link (totals are never pruned) |
| unique-visitor hashes | 5000 per link, then the stats page says it is capped |
| referrer buckets | 100 per link, the rest collect in `(other)` |
| failed token checks | 10 per IP per minute, then `429` |

## Storage

One JSON file, `$DATA_DIR/stub.json`, loaded once at startup and held in memory
behind a `sync.RWMutex`. Writes go to a temp file in the same directory,
`fsync`, `rename`, then `fsync` the directory — a crash leaves either the whole
old file or the whole new one, never half of either. The file is `0600`.

Creates, deletes and state changes are written through synchronously. Clicks
mark the store dirty and a ticker flushes every two seconds, so a burst of
traffic costs one `fsync` per two seconds instead of one per request; the
remaining flush happens on `SIGTERM`. The trade is that an unclean kill can
lose up to two seconds of click counts. Link records themselves are never at
risk.

## Development

```sh
gofmt -l . && go vet ./... && go test ./...
APP_TOKEN=dev DATA_DIR=$(mktemp -d) ADDR=:8837 BASE_URL=http://localhost:8837 go run .
```

## Deployment

Runs on the host at `~/stub/docker-compose.yaml`, behind Traefik at
<https://stub.baileys.dev>. Built directly via docker + ssh, not through the
infra repo.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/stub:latest --push .
```

The container runs as a non-root user, so the mounted volume for `DATA_DIR`
must be writable by uid 10001.
