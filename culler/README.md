# Culler

Two tools for working through a large batch of film scans, picked from a home screen. Built for the `2026-06-01` rolls, but it works on any folder of images.

Every photo's original path (e.g. `01 5702 Fujifilm Colour Negative 200/5702-0019.jpg`) is shown throughout and in every results view, so any set you end up with maps straight back to the source files. **Rotation** (`R` / `↻`) is available in both tools — 90° at a time, in memory only for the session, and it carries between tools. Nothing is written to disk.

## Tool 1 — Shortlist

Round-based culling. Go through the photos one at a time in a full-screen theatre, **liking** the ones you want to keep. Finish a round and everything you *didn't* like is discarded; the survivors begin a fresh round. Repeat until you've narrowed it down to your tops.

- **Like** (`space`) — toggle keep on the current photo
- **Navigate** (`←` / `→`)
- **Finish round** (`enter`) — discards unliked photos, survivors continue
- **Survivors** (`S` / `▦`) — grid of current survivors; click a thumb to jump, or **Copy filenames**

## Tool 2 — Rank

Pairwise ranking for a definitive 1–N order.

1. **Select a batch** — tap thumbnails to choose which photos to rank (or **Survivors** to pull in your shortlist, **All** / **Clear**).
2. **Compare** — the app shows two photos at a time; pick the better one (click, or `←` / `→`). It uses an interactive merge sort, so it asks the fewest comparisons needed for a true total order (~`n·log₂n`). **Undo** (`U`) steps back a comparison at any time.
3. **Ranking** — the finished order, numbered 1–N with the top 3 highlighted. **Copy ranking** exports the numbered list of filenames.

## Build and deploy

Source defaults to `../gallery/Bailey Butler 2026-06-01`. Override with `SRC_DIR`.

```bash
# Rebuild (resizes images into dist/img, embeds thumbnails, writes dist/index.html)
./build.sh

# Point at a different folder
SRC_DIR="/path/to/photos" ./build.sh

# Deploy (temporary)
staticer deploy --dir dist

# Deploy (permanent)
staticer deploy --dir dist --domain culler.baileys.dev --expires never --replace
```
