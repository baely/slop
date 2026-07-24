# hindsight

A guessing game against your own taste. Two films you've rated on Letterboxd,
one question: which did you score higher? Pick, get the verdict, keep the
streak alive.

Built from a Letterboxd data export — 180 rated films, 13,176 possible pairs.
Two difficulty modes: **Any Pair** (any two films with different ratings) and
**Close Calls** (half a star apart — 5,384 pairs, genuinely hard). The reveal
shows both ratings, when you last watched each, your review if you wrote one,
and links back to the entries.

Score, current streak and best streak persist in `localStorage`. Keyboard:
`1` / `2` or `←` / `→` to pick, `Enter` for the next round.

Styled to the [bailey house style](../house-style/) — flat neutral surfaces,
one loud teal, Bricolage headlines, JetBrains Mono for every value, zero
motion, `b.` in the corner. Follows the system color scheme.

## How it works

- `build.py` reads the CSVs inside a Letterboxd export zip (Settings → Data →
  Export Your Data on letterboxd.com) and writes the rated films to
  `site/data.js`. The raw export is never committed or deployed — only title,
  year, rating, last watch date and any review text.
- `site/` is the deployable static site: `index.html` + `style.css` + `app.js`.
  No dependencies, no build tooling beyond the one Python script. Pair
  generation, scoring and persistence all happen client-side.

## Updating the data

```sh
python3 build.py path/to/letterboxd-<user>-<date>-utc.zip
```

Then redeploy.

## Deployment

```sh
staticer deploy --dir site --domain hindsight.baileys.dev --expires never
```

Live at https://hindsight.baileys.dev

Note: re-running a domain deploy stacks a new deployment behind the same
domain and the oldest keeps serving — delete the previous random subdomain
(printed at deploy time) with `staticer delete <subdomain>` after redeploying.
