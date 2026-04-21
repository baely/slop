# Viewing Schedule

A self-hosted film viewing calendar. Today's films are featured prominently with
posters and metadata sourced from TMDB; the full schedule is listed alongside
with past viewings faded out.

The service is a single Go 1.25 binary that:

- serves the public viewer at `/`
- exposes a JSON API at `/api/schedule`
- ships an admin panel at `/admin` that can only be reached from private networks

Schedule data lives in a single SQLite file (`/data/viewing.db` by default).

## Quick start (docker compose)

```bash
cp .env.example .env
# Fill in TMDB_TOKEN and any other settings
docker compose up -d --build
```

The viewer is then available on `http://<host>:8080/` and the admin panel on
`http://<host>:8080/admin` (only reachable from a private IP — see below).

To stop and keep data:

```bash
docker compose down
```

To stop and wipe the database:

```bash
docker compose down -v
```

## Configuration

All configuration is via environment variables (see `.env.example`):

| Variable      | Default              | Purpose                                                  |
|---------------|----------------------|----------------------------------------------------------|
| `ADDR`        | `:8080`              | HTTP listen address                                      |
| `DB_PATH`     | `/data/viewing.db`   | SQLite database path                                     |
| `TMDB_TOKEN`  | _(empty)_            | TMDB v4 read-access token; enables metadata lookups      |
| `TITLE`       | `Viewing Schedule`   | Title displayed in the viewer header                     |
| `DATE_RANGE`  | _(empty)_            | Optional date-range badge (e.g. `Apr 10 — May 9, 2026`)  |
| `TRUST_PROXY` | `0`                  | Set to `1` behind a reverse proxy to honour `X-Forwarded-For` for admin IP checks |
| `LETTERBOXD_USER` | _(empty)_        | When set, the user's Letterboxd diary is synced into the local store and made available at `/history`. |
| `SYNC_INTERVAL` | `6h`              | How often to refresh the diary in the background (Go duration; `0` disables periodic sync). |

## Admin panel access control

The admin panel is mounted at `/admin` and is gated by source IP. Only requests
originating from the following ranges are allowed:

- `10.0.0.0/8`
- `192.168.0.0/16`
- `127.0.0.0/8` and `::1` (loopback, useful for local development)

Anything else receives a `403 Forbidden`. When the service is fronted by a
reverse proxy (Traefik, Nginx, Caddy, etc.) set `TRUST_PROXY=1` so the
real client IP from `X-Forwarded-For` is used for the check; otherwise the
proxy's own IP would be evaluated.

> Note: the spec lists `192.168.x.x` (formally `192.168.0.0/16`). The common
> `172.16.0.0/12` range is intentionally _not_ included.

## Admin workflow

From the admin panel you can:

1. Add or update an entry by date with up to two movies and an optional reason.
2. Movie titles use the format `Name (Year)`, e.g. `The Apartment (1960)`.
3. When `TMDB_TOKEN` is configured, posters and metadata are fetched
   automatically on save.
4. Delete entries you no longer want.
5. **Import from Letterboxd** — paste any Letterboxd URL and the films are
   scraped and previewed before import.

Changes are persisted to SQLite immediately and the public viewer reflects
them on the next page load.

## Letterboxd diary sync, history calendar & CSV export

When `LETTERBOXD_USER` is set, the service runs a background job (default
every 6 hours, configurable via `SYNC_INTERVAL`) that screen-scrapes the
user's diary and stores every viewing locally as an immutable record:

- viewing id, watched date, film slug, title, year
- rating (stars), liked, rewatch, has_review
- cached TMDB metadata: director, runtime, genres, posters, overview, tagline,
  release date, TMDB ID + rating

Sync is **idempotent on `viewing_id`**, so running it repeatedly only updates
mutable fields (rating changes, etc.). The incremental mode early-exits once
it has seen ~30 already-known viewings in a row, making routine syncs fast.

### History calendar

`/history` renders a year-at-a-glance grid (12 month panels per year) with one
square per day. Days that include a viewing are filled in; clicking a day opens
a modal with full details (poster, director, runtime, genres, rating with
stars, liked/rewatch indicators, overview, link back to Letterboxd).

The viewer header has a small **History** link to jump between the upcoming
schedule and the historic calendar.

### CSV export

`GET /admin/viewings.csv` downloads every stored viewing with every available
field, including rating, liked, rewatch, director, genres, posters, and a
direct Letterboxd URL. The columns are stable so this works as a backup or
spreadsheet pivot source.

### Admin controls

The admin panel surfaces a **Letterboxd diary sync** card showing the
configured username, periodic interval, total viewings stored, and last-sync
timestamp. The buttons are:

- **Sync now** — incremental sync (fast, stops once it sees known viewings)
- **Force full sync** — re-scrape the entire diary
- **Download CSV** — full export
- **View history →** — jump to `/history`

## Letterboxd import

The Letterboxd public API has been retired, so the importer screen-scrapes
public pages using a real browser User-Agent. From the admin panel you can
paste any of:

- a watchlist: `https://letterboxd.com/<user>/watchlist/`
- all rated films: `https://letterboxd.com/<user>/films/`
- the diary: `https://letterboxd.com/<user>/films/diary/`
- a custom list: `https://letterboxd.com/<user>/list/<slug>/`

Pagination is followed automatically (capped at 50 pages). After scraping,
you'll see a preview where you can:

- pick a start date and cadence (weekly, weekdays, daily) used to suggest
  dates for each film
- toggle which rows to import
- edit titles and dates inline before saving

Imported films are then enriched via TMDB if a token is configured, exactly
like a manually-added entry.

## API

- `GET /api/schedule` — JSON array of all entries with their movies.
- `GET /healthz` — liveness probe.

## Local development (without Docker)

Requires Go 1.25.

```bash
TMDB_TOKEN="your-token" \
DB_PATH="./viewing.db" \
go run ./cmd/server
```

Then open `http://localhost:8080/`. The admin panel is reachable from the same
machine via loopback at `http://localhost:8080/admin`.

## Project layout

```
cmd/server/             # entry point
internal/store/         # SQLite-backed store
internal/tmdb/          # TMDB API client
internal/server/        # HTTP handlers, IP guard, embedded templates
  templates/viewer.html # public viewer
  templates/admin.html  # admin panel
Dockerfile              # multi-stage, distroless final image
docker-compose.yaml     # one-command deployment
```
