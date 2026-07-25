# pulse

An uptime monitor for bailey's own estate. It checks a list of HTTP targets on a
schedule, records what happened, derives incidents from the up/down transitions,
and shows one honest public page.

Honesty is the point:

- A target that has never been checked reads `Unchecked.`, not `100%`.
- A DNS failure, a TLS failure, a refused connection and a 500 are four
  different error strings.
- Uptime never rounds up to `100%` — 99.96% reads `99.9%`.
- A window with no samples in it reads `—`, not zero and not a hundred.
- A down target has no "current latency"; time spent failing lives in the
  history table, not the headline.

## What it does

**Targets** are configured through the UI (token required): a name, an http or
https URL, GET or HEAD, a check interval, a timeout, an expected status, and an
optional keyword that must appear in the body. Nothing is seeded — a fresh
install says `0 targets.`

Any http/https URL is allowed on purpose, private and link-local addresses
included: the intended targets are internal services behind the same Traefik.
The bound is the per-target timeout, not a blocklist.

**Checking** is one scheduler goroutine waking every second, dispatching due
targets into a pool of 16 workers. Each probe:

- uses a context deadline of the target's timeout (1–60s),
- follows at most 3 redirects,
- reads at most 256 KiB of the body, and only when a keyword is configured,
- is wrapped in `recover()` — a panicking check records
  `internal check error` and the process keeps running.

**Incidents** are derived, not stored by hand. A check that fails while the
target was not already down opens an incident; the next passing check closes it
and fixes the duration. Consecutive failures do not open a second incident.

**The status page** at `/` is public and shows only: name, state in words
(`Up` / `Down` / `Unchecked`), uptime over 24h and 7d, humanized last-check
time, current latency, a latency sparkline, and the incident list with start,
end and duration. It does not show target URLs, target ids or error strings —
those are behind the token.

## Routes

| method | path | auth | what |
|---|---|---|---|
| GET | `/` | public | status page: state, uptime, latency, sparkline, incidents |
| GET | `/healthz` | public | `200 ok` |
| GET | `/static/style.css` | public | embedded stylesheet |
| GET | `/login` | public | token form |
| POST | `/login` | public | sets the session cookie; rate limited |
| POST | `/logout` | public | clears the session cookie |
| GET | `/admin` | token | target list and the add form |
| POST | `/admin/targets` | token | add a target |
| POST | `/admin/targets/{id}/update` | token | edit a target |
| POST | `/admin/targets/{id}/delete` | token | delete a target and its history |
| POST | `/admin/targets/{id}/check` | token | Check Now, run synchronously |
| GET | `/t/{id}` | token | detail: config, incidents with causes, full recent history, day rollups |

Auth is `Authorization: Bearer <APP_TOKEN>` or a session cookie set by the login
form. The cookie is a random 128-bit session id plus an HMAC-SHA256 of that id
keyed by the token — never the token itself — and is `HttpOnly`, `SameSite=Lax`
and `Secure` unless `BASE_URL` starts with `http://`. Tokens are compared with
`crypto/subtle.ConstantTimeCompare`, and 10 failed checks from one address in a
minute earns a `429`.

## Configuration

| var | default | meaning |
|---|---|---|
| `ADDR` | `:8080` | listen address |
| `DATA_DIR` | `/data` | persistence directory |
| `BASE_URL` | empty | public origin; an `http://` value relaxes the Secure cookie flag for local runs |
| `APP_TOKEN` | — | **mandatory**; the process exits 1 if it is empty |
| `TZ` | — | set to `Australia/Melbourne` by the deployment |

## Caps and retention

Stated in the UI as well as here.

| thing | cap |
|---|---|
| checks kept per target | 500, then rolled up |
| rolled-up day summaries | 90 days |
| incidents per target | 100 |
| targets | 200 |
| response body read for keyword matching | 256 KiB |
| redirects followed | 3 |
| request body | 32 KiB |
| interval / timeout | 10–86400s / 1–60s, timeout ≤ interval |

When a check falls out of the 500-deep window it is folded into a summary for
its day: total, passes, latency sum, and the first/last timestamp it covers.
The window and the summaries are therefore **disjoint** — nothing is counted
twice. A 24h or 7d uptime query counts individual checks exactly, and if the
window reaches further back than the oldest retained check it tops up from the
day summaries, prorated across the span each summary covers. The detail page
says so when a figure includes rolled-up data.

## Storage

One JSON file, `${DATA_DIR}/pulse.json`, loaded once at startup and held in
memory behind a `sync.RWMutex`. Writes go to a temp file in the same directory,
are fsynced, then renamed over the target, and the directory is fsynced — a
crash never leaves a half-written file.

Persistence is debounced: checks mark the store dirty and a flusher writes at
most every 3 seconds, so 70 targets on 60-second intervals do not rewrite the
file once per probe. Configuration changes flush immediately, and SIGTERM
flushes before exit.

## Development

```sh
go vet ./... && gofmt -l . && go test ./...
APP_TOKEN=dev-token DATA_DIR=$(mktemp -d) ADDR=:8843 BASE_URL=http://localhost:8843 go run .
```

Go standard library only. No third-party modules, no database.

## Deployment

Runs on the host at `~/pulse/docker-compose.yaml`, behind Traefik at
<https://pulse.baileys.dev>. Built and shipped directly with docker over ssh,
not through the infra repo.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/pulse:latest --push .
```

The compose service needs `APP_TOKEN` and `BASE_URL=https://pulse.baileys.dev`
in its environment, a volume mounted at `/data` owned by uid 10001 (the
container runs as a non-root user), and membership of the external `web`
network so Traefik can route to port 8080.
