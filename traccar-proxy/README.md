# traccar-proxy

A fan-out proxy for the [Traccar client](https://www.traccar.org/client/).
The phone app only accepts one server URL; point it here and every position
report is redistributed to as many upstream services as you like: a Traccar
server, a Home Assistant webhook, another tracker, anything that speaks the
OsmAnd HTTP protocol.

## How it works

The Traccar client posts each position as an HTTP request with the fields in
the query string (`/?id=…&lat=…&lon=…&timestamp=…&speed=…&batt=…`). The proxy
does not interpret this. It captures the method, path, query, body and content
type, writes the request to a durable per-target queue on disk, and answers
`200 OK` straight away. A worker per target then replays the queue in order:

- **Transient failures** (connection errors, timeouts, 5xx, 429) are retried
  with exponential backoff from 1 second to 5 minutes. Nothing is lost while an
  upstream is down, and the other upstreams are unaffected.
- **Rejections** (other 4xx, e.g. an unregistered device) are dropped and
  counted, so one bad request cannot block the queue behind it.
- Queues survive restarts. Each is capped (default 20,000 requests); beyond
  that the oldest are dropped and the drop is shown on the status page.

Because the proxy acknowledges immediately, the phone never retries against
healthy upstreams and they never see duplicates.

## Configuration

Everything is environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `TARGET_<NAME>` | required, at least one | Base URL of an upstream. `<NAME>` is a label shown on the status page. |
| `TOKEN` | unset | Secret path segment. When set, the client must post to `/<TOKEN>` and everything else is 404. |
| `ADDR` | `:8080` | Listen address. |
| `DATA_DIR` | `./data` (`/data` in Docker) | Where queues are persisted. Mount a volume. |
| `TIMEOUT` | `15s` | Per-delivery HTTP timeout. |
| `MAX_QUEUE` | `20000` | Per-target queue cap. |

Target URL rules:

- The client's path is appended to the target's path, and the query string is
  forwarded byte-for-byte. `TARGET_T=https://t.example.com:5055` receives
  `https://t.example.com:5055/?id=…`.
- Query parameters fixed on the target URL override the client's. Use this to
  give one upstream a different device id:
  `TARGET_HA=https://ha.example.com/api/webhook/abc?id=bailey-phone`.
- Userinfo in the URL becomes HTTP Basic auth: `https://user:pass@host/`.
- Redirects are not followed, so use the final URL.

Example:

```sh
TOKEN=k7Qm2x \
TARGET_TRACCAR=https://traccar.example.com:5055 \
TARGET_HA='https://ha.example.com/api/webhook/traccar_abc123?id=phone' \
go run .
```

Then set the Traccar client's server URL to `https://your-host/k7Qm2x`
(the status page prints the exact URL).

## Endpoints

| Path | Purpose |
|---|---|
| `POST /<TOKEN>?…` (any method, any sub-path) | Ingest. Fans out to every target. |
| `GET /<TOKEN>` (no query string) | Status page: per-target queue depth, deliveries, rejections, last delivery, retry state. |
| `GET /<TOKEN>/api/status` | Same as JSON. |
| `GET /healthz` | Liveness, always public. |

Without `TOKEN`, drop the prefix: the client posts to `/` and the status page
is at `/`.

## Run locally

```sh
TARGET_ECHO=http://localhost:9000 go run .
curl -X POST 'http://localhost:8080/?id=1&lat=-37.81&lon=144.96&timestamp=1757145600'
open http://localhost:8080
```

Tests: `go test -race ./...`

## Deploy

Dynamic app, deployed via [baely/infra](https://github.com/baely/infra):

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/traccar-proxy:latest --push .
```

Then `docker/github.com_baely_slop_traccar-proxy/deploy.yaml` in the infra
repo (service on the external `web` network, Traefik host rule, `/data`
volume) with the real `TOKEN` and `TARGET_*` values in the server-side `.env`.
Bump `# Ref:` in `deploy.yaml` to redeploy after pushing a new image.
