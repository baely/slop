# Covers

A personal log of restaurants you've been to — clean, near-monochrome interface (Hanken Grotesk, hairline cards), entries grouped by month, and a map of everywhere you've eaten. Data lives server-side in a small JSON datastore, so your log follows you across devices. The log is **publicly readable**; editing is gated by a password.

## Features

- **Log visits** with the fields you'd want in a dining diary:
  - Required: restaurant name, date
  - Optional: location, meal (breakfast / brunch / lunch / dinner / treat), rating (0.5–5 stars), amount spent (with currency: $ AUD, RM, S$, HK$, ¥)
- **Itemised menu items** — record each dish/drink with an optional price; prices auto-total into the amount spent (overridable)
- **Repeat visits** — each visit is its own entry; the **Log again** action on any entry pre-fills a new visit with that restaurant + location, and the name field autocompletes places you've been. The map groups repeat visits per place
- **Map view** — visits with a location are pinned on a clean map (geocoded via OpenStreetMap/Nominatim). Pins group by place and list each visit; in the entry form you can **Find on map** and drag the pin to the exact spot
- **Public, read-only by default** — anyone visiting sees the full log (list, map, ratings, spend) but cannot modify it. The owner signs in (lock button) to add/edit/delete
- **Search** across restaurant, location, and dishes
- **Sort** by newest/oldest, highest rated, most spent, or name
- **At-a-glance stats**: total visits, spend (per currency), average rating, distinct places
- **Edit / delete / log-again** on any entry (owner only)
- **Backup & portability**: export as JSON or CSV, import from JSON

## Architecture

- **Frontend** (`public/index.html`) — a single self-contained page (HTML/CSS/JS, Leaflet for the map).
- **Backend** (`server.js`) — a tiny **zero-dependency** Node HTTP server. It serves the frontend and a small REST API, persisting to an atomically-written JSON file. No database engine, no native modules, no `npm install`.
- **Storage** — a JSON file at `DATA_DIR/db.json` (default `/data/db.json`), mounted as a Docker volume so it survives restarts/redeploys.
- **Auth** — `POST /api/login` exchanges the password for a bearer token (stored in `localStorage`). Reading the log is public; all **mutating** routes require the token.

### API

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `POST` | `/api/login` | — | `{ password }` → `{ token }` |
| `GET` | `/api/entries` | public | list all |
| `GET` | `/api/session` | owner | token check (200 / 401) |
| `POST` | `/api/logout` | owner | invalidate current token |
| `POST` | `/api/entries` | owner | create |
| `PUT` | `/api/entries/:id` | owner | replace |
| `DELETE` | `/api/entries/:id` | owner | delete one |
| `DELETE` | `/api/entries` | owner | clear all |
| `POST` | `/api/import` | owner | `{ entries: [...] }` bulk add |
| `GET` | `/api/health` | — | liveness |

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `APP_PASSWORD` | `covers` | Password gating editing (**set this!**) |
| `PORT` | `8080` | HTTP port |
| `DATA_DIR` | `/data` | Directory for `db.json` |

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
