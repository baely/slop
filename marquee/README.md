# Marquee

A personal cinema you operate. Letterboxd is a diary — it ends at *"I watched this."*
Marquee closes the loop Letterboxd can't, because it runs on **your** infrastructure:

> **Discover → want it → Radarr grabs it → it lands in your library → you watch → you log & rate it.**

Self-hosted, single-user, your data in your SQLite. TMDB for metadata and posters,
Radarr for the download loop.

## Features

- **Discover** — live search + weekly trending via TMDB, posters and all.
- **A watchlist that *does* something** — adding a film isn't a sticky note. One tap
  hands it to Radarr and the card shows a live status badge:
  `+ Add → ◌ Wanted → ⟳ Downloading 47% → ● In library`. Badges poll and update in place.
- **Library** — your real Radarr collection, browsable like a wall of spines, with
  on-disk vs. monitored state.
- **Diary, ratings & reviews** — the Letterboxd core: log viewings, rate (½–5★),
  review, mark rewatches and likes.
- **You** — a living taste portrait (mean rating, ratings histogram, genre map,
  recent watches) that updates as you log.

## Architecture

| Piece | Choice |
|------|--------|
| Backend | FastAPI (async — ideal for fanning out to TMDB/Radarr) |
| Storage | SQLite — single-user, file-backed, zero-ops |
| Frontend | Jinja templates + vanilla JS (live search, action buttons, status polling). No build step. |
| Metadata | TMDB API (search, trending, credits, posters) |
| Loop | Radarr v3 API (library, add+search, download queue progress) |

```
app/
  main.py        FastAPI routes (pages + JSON action/polling endpoints)
  db.py          SQLite: films cache, watchlist, diary logs, stats
  tmdb.py        TMDB client (+ fixture fallback)
  radarr.py      Radarr v3 client — the loop (+ simulated fallback)
  fixtures.py    Demo data + a time-based simulated download loop
  templates/     base, home, discover, watchlist, library, diary, you, film
  static/        app.css, app.js
```

## Config (all via env)

Everything is config-driven. Set the vars and the real integrations light up;
leave them blank and Marquee runs in **demo mode** — a sample library with a
*simulated* Radarr loop (films you "send" visibly progress wanted → downloading →
available over ~25s), so the whole app is explorable with no backend.

| Var | Purpose |
|-----|---------|
| `TMDB_API_KEY` | TMDB v3 key — metadata + posters |
| `RADARR_URL` | e.g. `http://radarr:7878` |
| `RADARR_API_KEY` | Radarr → Settings → General → API Key |
| `RADARR_QUALITY_PROFILE_ID` | optional; first profile auto-used if blank |
| `RADARR_ROOT_FOLDER` | optional; first root folder auto-used if blank |
| `DB_PATH` | SQLite path (default `/data/marquee.db`) |

See `.env.example`.

## Run locally

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload          # http://127.0.0.1:8000  (demo mode)
# or wire it up:
TMDB_API_KEY=… RADARR_URL=http://… RADARR_API_KEY=… uvicorn app.main:app
```

## Deploy (Docker + Traefik)

Dynamic app → image pushed to `registry.baileys.dev`, routed by Traefik. The
container is stateless; all state lives in the `marquee-data` volume (`/data`).

```bash
# 1. build for the server platform + push (from a dev machine)
docker build --platform linux/amd64 -t registry.baileys.dev/marquee:latest .
docker push registry.baileys.dev/marquee:latest

# 2. on the Docker host: compose + .env, then
docker compose pull && docker compose up -d
```

`.env` on the host (see `.env.example`):

```
TMDB_BEARER=…            # or TMDB_API_KEY
RADARR_URL=http://192.168.0.82:7878   # must be reachable from the container
RADARR_API_KEY=…
MARQUEE_HOST=marquee.example.com      # the Traefik Host() rule
MARQUEE_AUTH=                         # forward-auth middleware (e.g. authelia@docker) — leave blank for none
```

Traefik labels are templated from those vars:

```
traefik.http.routers.marquee.rule=Host(`${MARQUEE_HOST}`)
traefik.http.services.marquee.loadbalancer.server.port=8000
# traefik.http.routers.marquee.middlewares=${MARQUEE_AUTH}   # uncomment to gate behind auth
```

> ⚠️ Marquee has no built-in login and "Order via Radarr" triggers real
> downloads. If exposed publicly, gate it with `MARQUEE_AUTH` (or Traefik
> basic-auth) unless you intend it to be open.

### Seeding history into prod

The volume starts empty. To carry an existing Letterboxd import across, copy the
populated SQLite DB into the volume after first boot:

```bash
docker compose up -d
docker cp ./marquee.db marquee:/data/marquee.db   # the DB produced by scripts/import_letterboxd.py
docker compose restart marquee
```

Or run `scripts/import_letterboxd.py <export-dir>` directly against the volume's DB.

## Roadmap ideas

- Plex/Jellyfin link-out ("Play" on available films)
- Import an existing Letterboxd export to seed diary + ratings
- Multiple watchlist "shelves" and smart lists
- Sonarr-style "wanted but not released yet" calendar
