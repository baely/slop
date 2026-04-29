# letterboxd-companion

A static companion site for a Letterboxd export. It ingests the CSVs from
[`letterboxd.com/settings/data`](https://letterboxd.com/settings/data), enriches
each unique film with TMDB metadata (poster, runtime, genres, directors), and
generates three views:

- **Dashboard** (`/`) — hero stats, GitHub-style viewing-activity calendar,
  ratings histogram, watchlist progress, decade chart with average rating per
  decade, top directors / genres, the 5★ hall of fame, and an
  original-language breakdown.
- **Roulette** (`/roulette.html`) — picks an unwatched film from the watchlist,
  with optional decade / runtime / genre filters and a "tuned to your taste"
  mode that biases toward directors, genres, and decades you've rated highly.
- **Recap** (`/recap.html`) — pick any date range (or use a year preset) for a
  scoped recap: stats, standout films, decade breakdown, full timeline.

Data is frozen at export time, so the site is fully static — deploy via
`staticer`, no backend required.

## Layout

```
data/         Letterboxd export CSVs (drop them here)
build/        Python build script + tmdb-cache.json (gitignored)
web/          HTML/CSS/JS shell (source of truth)
dist/         Generated output: web/ contents + data.json (gitignored)
```

## Build

Requires Python 3.11+ and a TMDB v4 read-access token.

```bash
TMDB_TOKEN=eyJ... python3 build/build.py
```

The build:

1. Merges `watched.csv`, `ratings.csv`, `diary.csv`, `watchlist.csv`, and
   `likes/films.csv` into one record per unique Letterboxd film.
2. Looks each title up on TMDB (cached in `build/tmdb-cache.json`, so reruns
   are free) and pulls poster, backdrop, runtime, genres, directors, top-5
   cast, original language, and TMDB vote average.
3. Emits `dist/data.json` and copies `web/*` over the top.

The first build hits TMDB ~2× per film. ~500 films takes ~30 seconds. Cached
runs finish in well under a second.

## Refresh the export

```bash
unzip -o letterboxd-USERNAME-YYYY-MM-DD-HH-MM-utc.zip -d data
TMDB_TOKEN=... python3 build/build.py
```

## Deploy

```bash
staticer deploy --domain cinema.baileys.dev --expires never dist
```

Or for a temporary preview: `staticer deploy dist`.
