# Testing the restyled apps locally

Nothing here is deployed — all 18 restyles live only on the `house-style` branch.
This is how to run each one on your machine to review the new look. Toggle your
OS between light and dark to see both modes (or add `data-theme="dark"` /
`data-theme="light"` to the `<html>` tag).

Most dynamic apps default to **port 8080**, so run them one at a time (or set
`PORT` / `ADDR`). Static apps can each get their own port.

## Static — just serve the folder (full visual fidelity)

```sh
python3 -m http.server 9001 --directory corkmap
python3 -m http.server 9002 --directory gpxer
python3 -m http.server 9003 --directory map-style-lab
python3 -m http.server 9004 --directory ranker
python3 -m http.server 9005 --directory statement-parser
python3 -m http.server 9006 --directory supernote-templates
python3 -m http.server 9007 --directory pool-physics
python3 -m http.server 9008 --directory darkroom
python3 -m http.server 9009 --directory ipv6-page
```

Then open `http://localhost:900N/`. These render exactly as they will ship —
darkroom, pool-physics, and gallery are pinned dark by design; the rest follow
your system theme.

## Dynamic — need a runtime, but start with one command

| App | Command (from repo root) | URL | Notes |
|---|---|---|---|
| covers | `APP_PASSWORD=covers node covers/server.js` | http://localhost:8080 | Node stdlib only, no `npm install`. Log in with `covers`. |
| messenger | `cd messenger && go run .` | http://localhost:8080 | The restyled page is the test form at `/`. |
| einktimetable | `cd einktimetable && python3 app.py` | http://localhost:8080 | Flask + Pillow (already installed). Restyled editor is `/`. |
| trackui | `cd trackui && python3 server.py` | http://localhost:8080 | Chrome renders; the GPS/spend/music panels stay empty without API tokens — that's fine for a look. |
| viewing-schedule | `cd viewing-schedule && go run ./cmd/server` | http://localhost:8080 | SQLite auto-creates. Public viewer at `/`, admin at `/admin`. |
| voyage | `cd voyage && go run ./cmd/server` | http://localhost:8080 | SQLite auto-creates. |

## Needs more infra (preview a subset)

- **staticer** — full platform is heavy, but the restyled dashboard is static:
  `python3 -m http.server 9010 --directory staticer/web/dashboard` → open `/`.
  (Live data won't load, but every dashboard component renders.)
- **listing** — `cd listing && go run ./cmd/listing` runs, but it reads the
  Docker socket to populate the service list; expect an empty/placeholder list
  locally. The chrome (nav band, record rows, empty state) still shows.
- **gallery** — output is produced by `_build.py`, which needs the film-scan
  photo assets. The restyle lives in the CSS string inside `_build.py`; running
  it requires the real photos, so it's easiest to eyeball on deploy.

## What to look for

Consistency across all of them: the deep-teal `#0891b2` accent (nav band,
primary buttons, selected states), Bricolage Grotesque headlines, Inter body,
JetBrains Mono for every value/timestamp, flat borderless surfaces, 3px corners,
Title Case controls, no motion, and the `b.` glyph bottom-right.
