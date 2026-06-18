# Covers

A personal, password-protected log of restaurants you've been to — with an editorial "dining journal" aesthetic (warm paper tones, Fraunces + Newsreader serifs), menu-style entries grouped by month, and a map of everywhere you've eaten. Data lives server-side in a small JSON datastore, so your log follows you across devices.

## Features

- **Log visits** with the fields you'd want in a dining diary:
  - Required: restaurant name, location, date
  - Optional: time, rating (0.5–5 stars), amount spent (with currency)
- **Itemised menu items** — record each dish/drink with an optional price; prices auto-total into the amount spent (overridable)
- **Map view** — every visit is pinned on a clean map (locations geocoded via OpenStreetMap/Nominatim). Pins group by place and list each visit; in the entry form you can **Find on map** and drag the pin to the exact spot
- **Pin from your tracker** (optional) — if `TRACK_TOKEN` is set, a **From tracker** button pulls your real GPS position (from a Traccar instance, e.g. `track.baileys.dev`, the same source trackui uses) at the visit's date/time and drops the pin where you actually were. The token is held server-side and never exposed to the browser
- **Search** across restaurant, location, and dishes
- **Sort** by newest/oldest, highest rated, most spent, or name
- **Month grouping** with a running visit count, magazine-style
- **At-a-glance stats**: total visits, spend (per currency), average rating, distinct places
- **Edit / delete** any entry
- **Backup & portability**: export as JSON or CSV, import from JSON
- **Single-user auth** — gated by a shared password (`APP_PASSWORD`)

## Architecture

- **Frontend** (`public/index.html`) — a single self-contained page (HTML/CSS/JS, Leaflet for the map).
- **Backend** (`server.js`) — a tiny **zero-dependency** Node HTTP server. It serves the frontend and a small REST API, persisting to an atomically-written JSON file. No database engine, no native modules, no `npm install`.
- **Storage** — a JSON file at `DATA_DIR/db.json` (default `/data/db.json`), mounted as a Docker volume so it survives restarts/redeploys.
- **Auth** — `POST /api/login` exchanges the password for a bearer token (stored in `localStorage`); all data routes require it.

### API

| Method | Path | Notes |
| --- | --- | --- |
| `POST` | `/api/login` | `{ password }` → `{ token }` |
| `POST` | `/api/logout` | invalidate current token |
| `GET` | `/api/entries` | list all |
| `POST` | `/api/entries` | create |
| `PUT` | `/api/entries/:id` | replace |
| `DELETE` | `/api/entries/:id` | delete one |
| `DELETE` | `/api/entries` | clear all |
| `POST` | `/api/import` | `{ entries: [...] }` bulk add |
| `GET` | `/api/health` | liveness |

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `APP_PASSWORD` | `covers` | Password gating the app (**set this!**) |
| `PORT` | `8080` | HTTP port |
| `DATA_DIR` | `/data` | Directory for `db.json` |
| `TRACK_TOKEN` | _(unset)_ | Traccar bearer token; enables the "From tracker" button |
| `TRACK_BASE` | `https://track.baileys.dev/api` | Traccar API base |
| `TRACK_DEVICE` | `1` | Traccar device id to query |

Geocoding runs in the browser against OpenStreetMap/Nominatim and is cached in `localStorage`, so each place is only looked up once. The location text you type is the only thing sent off-device.

## Run locally

```sh
APP_PASSWORD=secret DATA_DIR=./data node server.js
# open http://localhost:8080
```

## Deployment

Dynamic app — Docker image pushed to `registry.baileys.dev`, routed by Traefik.

```sh
# 1. Build for the server's architecture
docker buildx build --platform linux/amd64 -t registry.baileys.dev/covers:latest --load .

# 2. Push
docker push registry.baileys.dev/covers:latest

# 3. On the host: set the password and bring it up
cp .env.example .env   # then edit APP_PASSWORD
docker compose up -d
```

`docker-compose.yaml` exposes the service to Traefik on port `8080` at `covers.baileys.dev` and mounts the `covers-data` volume for the datastore. The password is read from `.env` (`APP_PASSWORD`), which is git-ignored.
