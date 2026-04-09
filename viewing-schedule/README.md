# Viewing Schedule

A static page showing a curated film viewing calendar. Today's films are featured on the left with posters, details, and synopses sourced from TMDB. The full schedule is listed on the right with past viewings faded out.

## Build

Requires Node.js and a TMDB API read access token.

```bash
TMDB_TOKEN="your-token" bash build.sh
```

This fetches movie metadata from TMDB and generates `dist/index.html`.

## Deploy

```bash
staticer deploy --dir dist --domain viewing.baileys.dev --expires never
```
