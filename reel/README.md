# reel

baely's film log, by the numbers. A static stats page generated from a
Letterboxd data export: films watched, ratings distribution, release decades,
logging pace, five-star films, watchlist debt, where things were watched, and
the occasional review.

Styled to the [bailey house style](../house-style/) — flat neutral surfaces,
one loud teal, Bricolage Grotesque headlines, JetBrains Mono for anything that
is a value, zero motion, `b.` in the corner.

## How it works

- `build.py` reads the CSVs inside a Letterboxd export zip
  (Settings → Data → Export Your Data on letterboxd.com) and writes aggregated
  stats to `site/data.js`. The raw export is never committed or deployed —
  only aggregates and public-profile film data.
- `site/` is the deployable static site: `index.html` + `style.css` + `app.js`
  render everything from `data.js` client-side. No dependencies, no build
  tooling beyond the one Python script.

## Updating the data

```sh
python3 build.py path/to/letterboxd-<user>-<date>-utc.zip
```

Then redeploy.

## Deployment

```sh
staticer deploy --dir site --domain reel.baileys.dev --expires never --replace
```

Live at https://reel.baileys.dev
