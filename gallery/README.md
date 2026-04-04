# Gallery

A single-page photo gallery displaying film scan images with a theater carousel viewer.

## Features

- Grid layout with lazy-loaded inline thumbnails
- Click any image to open full-screen theater view
- Keyboard navigation (left/right arrows, Escape to close)
- Info bar showing index, film stock, date, and optional location with Google Maps link
- Automatic detection and removal of "Missing Frames" slides

## Updating metadata

Edit `metadata.json` to add location and maps info for each image:

```json
{
  "src": "Bailey Butler 2023-01-10/5662 Agfa Vista 200/...",
  "stock": "Agfa Vista 200",
  "date": "2023-01-10",
  "location": "Melbourne",
  "maps": "https://maps.google.com/?q=Melbourne,Australia"
}
```

- `stock` and `date` are pre-populated from directory names
- `location` is optional — omit or leave empty to hide
- `maps` is optional — when present, location becomes a clickable link

## Build and deploy

```bash
# Rebuild (processes images, generates thumbnails, creates dist/)
./build.sh

# Deploy
staticer deploy --dir dist --domain gallery.baileys.dev --expires never --replace
```
