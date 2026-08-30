# Film Downscaler

A local web tool to batch-downscale and recompress the JPGs in a folder, then
export them to a single **flat** zip (all files at the zip root, no subfolders).
Built for a Google Drive film-scan library, but works on any folder of JPGs.

## What it does

- Recursively finds every `.jpg` / `.jpeg` under the target folder.
- Three downscale modes:
  - **Longest edge** — cap the longest side at N px (best for mixed orientations).
  - **Max width** — cap width at N px, height scales proportionally.
  - **Percentage** — scale every image to N% of its own size.
- Adjustable **JPEG quality** (1–100).
- **Roll picker** — images are grouped into rolls by their folder and listed with a
  checkbox and a strip of up to 10 thumbnails each (lazy-loaded, disk-cached). Pick
  exactly which rolls go into the export. Pre-existing `resized …` rolls are flagged
  and unchecked by default; quick `Select all` / `None` / `Exclude resized` actions.
- **Live estimate** of the resulting total size, file count, and % reduction for the
  current selection — computed by processing a deterministic 24-image sample and
  extrapolating.
- **Export** processes every selected image and writes a flat zip whose files are
  renamed sequentially: `0001.jpg`, `0002.jpg`, … (sorted by source path, so the
  numbering is stable). Sequential naming sidesteps the heavy filename collisions
  in the source (only ~142 unique names across ~1000+ files).
- EXIF orientation is applied; other metadata is dropped to keep files small.

The zip is written to `output/` and is also offered as a browser download.

## Run it

Requires Python 3 with Flask and Pillow (both already present on the build
machine; otherwise `pip install -r requirements.txt`).

```sh
cd film-downscaler
python3 app.py
# open http://127.0.0.1:5000
```

By default it targets the film library:

```
/Users/bailey/Library/CloudStorage/GoogleDrive-…/My Drive/Media/Film
```

Point it at any other folder with `FILM_DIR`:

```sh
FILM_DIR="/path/to/photos" python3 app.py
```

Reads are processed concurrently (12 workers by default) because the bottleneck
is Google Drive streaming each file. Tune it with `WORKERS`:

```sh
WORKERS=20 python3 app.py    # more parallel downloads
```

## Notes

- **First estimate / export is slower on Drive folders.** Google Drive stores files
  "online only" and streams each one on first access (~3–5 MB apiece). Once a file
  has been read it's cached locally and subsequent reads are fast. Downscaling
  fundamentally has to read every image, so the initial export of the full library
  pulls everything down once — the progress bar tracks it. Reads run concurrently
  (`WORKERS`), so this is bound by your Drive download bandwidth, not one-file-at-a-time
  latency.
- Estimates are sample-based, so the final zip size will be close but not exact.
- Output files in `output/*.zip` are git-ignored.

## Deployment

This is a **local utility** — it reads files from the user's machine (a Google
Drive folder), so it is not deployed to a public host. It runs on
`127.0.0.1:5000` via `python3 app.py`.
