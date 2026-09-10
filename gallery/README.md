# gallery

Film scans, one page. A grid of every frame grouped by roll (newest first),
and a full-screen theater view with keyboard, swipe and back-button
navigation. Each frame has a shareable URL (`#5702-0012`).

Live at https://gallery.baileys.dev

## Layout

```
gallery/
  import.py       Google Drive "Film" folder -> photos/ (normalised)
  build.py        photos/ -> dist/ (resizes, thumbnails, index.html)
  _worker.py      per-image processing used by build.py
  template.html   the page; build.py embeds the roll manifest into it
  build.sh        wrapper around build.py
  rolls.json      optional per-roll labels (stock, location, maps link)
  photos/         normalised originals (gitignored, ~6.6 GB)
  dist/           built site (gitignored, ~800 MB)
```

## Importing from Drive

The lab drops scans into `My Drive/Media/Film` in several layouts
(`Bailey Butler 2023-01-10/5662 Agfa Vista 200/5662 Agfa Vista 200/000036.JPG`,
`20260601/01 5702 Fujifilm Colour Negative 200/5702-0001.jpg`,
`20260907/B26159-Ilford-HP5-Plus/…`). `import.py` normalises all of them to

```
photos/YYYY-MM-DD/ROLL Stock Name/frame.jpg
```

It copies (never moves), only fetches files that are missing or changed,
skips `resized` directories, contact sheets, `FND_MissingFrames` slides,
`.xmp` sidecars and `.zip` archives, and ignores an undated top-level roll
when the same roll id already exists under a dated directory.

```bash
python3 import.py --dry-run     # show what would be copied
python3 import.py               # copy from the default Drive path
python3 import.py /path/to/Film # copy from somewhere else
python3 import.py --prune       # also delete photos/ files gone from Drive
```

Rolls whose directory has no stock name (e.g. `18954`) show only the roll id.
Add a label in `rolls.json`:

```json
{
  "18954": { "stock": "Kodak Gold 200" },
  "5702":  { "location": "Melbourne", "maps": "https://maps.google.com/?q=Melbourne" }
}
```

## Build

```bash
./build.sh            # incremental: only new or changed frames are processed
./build.sh --clean    # rebuild everything (~80 s for 1400 frames)
```

Output: `dist/index.html` (about 30 kB), `dist/img/<id>.jpg` theater images
with a 2000 px long edge, and `dist/t/<id>.jpg` thumbnails with a 480 px long
edge. Frame ids are `ROLL-frame`, so URLs stay stable when rolls are added.
Blank near-white frames are dropped.

Requires Python 3 with Pillow and numpy.

## Deploy

```bash
staticer deploy --dir dist --domain gallery.baileys.dev --expires never --replace
```
