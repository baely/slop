# officetracker-mirror

An anonymous, **read-only** mirror of [officetracker.com.au](https://officetracker.com.au),
served at `https://officetracker.baileys.app`.

It is a stateless reverse proxy (a Backend-for-Frontend): it holds a single
officetracker API token tied to your account and injects it into every upstream
request. Anonymous visitors see the **real UI and read API** with your data — no
login required — while every write/update endpoint and sensitive management
surface is blocked.

## How it works

officetracker server-renders its entire UI when a request carries a valid bearer
token. So a transparent proxy that injects the token reproduces an identical UI
(`/`, `/{YYYY}-{MM}`, `/settings`, static assets) and the full read API with no
template duplication.

The proxy enforces the read-only contract:

| Concern | Behaviour |
| --- | --- |
| Capability advert | `GET /api/v1/meta` is served locally (public, never proxied) and returns `{"auth":"none","read_only":true}` so mobile apps know to skip login and lock the UI to read-only. |
| Token injection | `Authorization: Bearer <OFFICETRACKER_TOKEN>` added to every upstream request; incoming `Cookie`/`Authorization` stripped. |
| Writes | Only `GET`/`HEAD` allowed. `PUT`/`POST`/`DELETE`/`PATCH` → `403`. `OPTIONS` → `204`. |
| Sensitive paths | `/api/v1/developer*`, `/api/v1/account/link`, `/mcp*`, `/auth*`, `/login`, `/logout`, `/developer` → `404`. |
| Cookie leakage | Upstream `Set-Cookie` headers stripped from every response. |
| Read-only UI | A small CSS/JS snippet is injected into HTML: notes textarea and settings controls are disabled, calendar/schedule clicks do nothing, export buttons are disabled, and the logout/developer/"link account" controls are hidden. |
| Linked accounts | Left visible (provider + nickname is account metadata, not a secret). |

## Configuration

| Env var | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | Listen address. |
| `UPSTREAM` | `https://officetracker.com.au` | Upstream officetracker base URL. |
| `OFFICETRACKER_TOKEN` | _(required)_ | API token for your account, `officetracker:<64 chars>`. |

### Obtaining the token

Log in to officetracker.com.au, open **/developer**, and create an API token.
The value looks like `officetracker:<64 alphanumeric chars>`. Supply it as
`OFFICETRACKER_TOKEN`. To rotate it, update the env var and redeploy.

## Run locally

```sh
OFFICETRACKER_TOKEN=officetracker:xxxxxxxx... go run ./cmd/server
# then browse http://localhost:8080/
```

## Deploy

```sh
# Build for the deployment platform and push to the registry.
docker build --platform linux/amd64 -t registry.baileys.dev/officetracker-mirror .
docker push registry.baileys.dev/officetracker-mirror

# On the host (Traefik routes officetracker.baileys.app to this service):
OFFICETRACKER_TOKEN=officetracker:xxxx... docker compose up -d
```

## Known limitations

- **PDF/CSV export does not work.** officetracker's report endpoints require an
  SSO session, not a bearer token, so they reject the proxy's auth. The export
  buttons are disabled in the mirrored UI.
- The MCP endpoint is blocked entirely (it exposes a write tool).
- All anonymous visitors see the single configured account's data — by design.

## Project layout

```
cmd/server/main.go        entry point: config + HTTP server
internal/proxy/proxy.go   reverse proxy, token injection, method/path guard
internal/proxy/inject.go  read-only UI CSS/JS injected into HTML responses
```
