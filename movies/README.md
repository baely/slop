# movies

A thin, iPad-friendly frontend for Radarr. Search TMDB, see posters, add a
movie with a quality profile, and either let Radarr auto-grab the best release
or browse every release from the indexers (filterable by resolution) and pick
the exact one to send to the download client. Includes a live download queue
view.

The Go backend proxies the Radarr v3 API so the API key never reaches the
browser. The frontend is a single embedded static page in the bailey house
style.

## Endpoints

| Endpoint | Radarr call |
|---|---|
| `GET /api/search?q=` | `/movie/lookup?term=` |
| `GET /api/recent` | `/movie` (sorted by added, top 30) |
| `GET /api/profiles` | `/qualityprofile` |
| `POST /api/add` | `/movie/lookup/tmdb` + `POST /movie` |
| `GET /api/releases?movieId=` | `/release?movieId=` (interactive search) |
| `POST /api/grab` | `POST /release` (guid + indexerId) |
| `POST /api/autosearch` | `POST /command` (MoviesSearch) |
| `GET /api/queue` | `/queue?includeMovie=true` |
| `GET /api/poster/{id}` | `/mediacover/{id}/poster-500.jpg` |

Note: `POST /api/grab` must follow a `GET /api/releases` for the same movie —
Radarr grabs from its cached interactive-search results, which expire after
~30 minutes.

## Configuration

| Env | Default | |
|---|---|---|
| `RADARR_URL` | `http://radarr:7878` | Base URL of the Radarr instance |
| `RADARR_API_KEY` | — | Required. Settings → General in Radarr |
| `ADDR` | `:8080` | Listen address |

## Run locally

```sh
RADARR_URL=https://radarr.int.xbd.au RADARR_API_KEY=xxx go run .
```

## Deployment

Dynamic app, deployed via [baely/infra](https://github.com/baely/infra) as
`docker/github.com_baely_slop_movies/`. Routed by traefik to
https://movies.int.xbd.au behind the `internal-only@file` middleware (LAN
only). The container joins the external `web` network and reaches Radarr
directly as `http://radarr:7878` on that network.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/movies:latest --push .
```

The real `RADARR_API_KEY` lives in `.env` on the deploy host (written
manually, chmod 600); `sample.env` in the infra repo is the committed
template.
