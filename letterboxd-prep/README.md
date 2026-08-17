# letterboxd-prep

A small web app that takes a raw Letterboxd export zip and returns a cleaner one:

- strips `deleted/` and `orphaned/` entries
- collapses `watched.csv` + `diary.csv` + `ratings.csv` + `reviews.csv` + `likes/films.csv` into a single **per-watch** `watched.csv` (rewatches get their own row)
- enriches every film with TMDB metadata: `tmdb_id`, `tmdb_title`, `imdb_id`, runtime, genres, director, overview, poster URL
- enriches `watchlist.csv` with the same TMDB columns

Auto-matches against TMDB when the title and year match exactly, queues anything ambiguous for manual review in the browser, and lets you free-text search TMDB for overrides.

## Output zip layout

```
profile.csv     # passthrough
watched.csv     # one row per watch (or one row per untracked film)
watchlist.csv   # enriched
README.txt      # column reference + match summary
```

### `watched.csv` columns

| col | source |
| --- | --- |
| `name`, `year`, `letterboxd_uri` | Letterboxd film identity |
| `watched_date` | diary `Watched Date` (empty if film was never logged with a date) |
| `log_date` | diary `Date` |
| `rating` | per-watch rating from diary; falls back to `ratings.csv` for films with no diary entry |
| `liked` | true if the film is in `likes/films.csv` |
| `rewatch` | true if the diary entry was flagged as a rewatch |
| `review` | matching review text from `reviews.csv` (matched on `URI` + `Watched Date`) |
| `tags` | diary tags |
| `tmdb_id`, `tmdb_title`, `imdb_id` | TMDB IDs |
| `runtime_minutes`, `original_language`, `genres`, `studios` | TMDB metadata |
| `director`, `writers`, `dop`, `producers`, `cast` | TMDB credits (DP = Director of Photography; cast = top 5 by billing) |
| `overview`, `poster_url` | TMDB |

## Run locally

```sh
TMDB_READ_TOKEN=eyJ... go run .
# open http://localhost:8080
```

## Deploy

The app is deployed at https://letterboxd.baileys.app via Traefik + Docker registry.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/letterboxd-prep:latest .
docker push registry.baileys.dev/letterboxd-prep:latest
docker compose up -d
```

`docker-compose.yaml` reads `TMDB_READ_TOKEN` from the environment (or a `.env` file alongside it).

## TMDB API

Uses the v3 `/search/movie` and `/movie/{id}?append_to_response=credits` endpoints. Requires a TMDB v4 read access token (Bearer auth) in `TMDB_READ_TOKEN`.

## Notes / limits

- max upload size: 50 MB
- job state lives in memory; restarting the server drops in-flight jobs
- TMDB details are cached per-process (so the same film in watched + watchlist only fetches once)
- search concurrency is capped at 10 workers
