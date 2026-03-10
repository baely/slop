# Corkmap

An interactive world map for pinning locations you've visited. Built as a single-page static app using D3.js and TopoJSON.

## Features

- Pan and zoom a world map
- Click to drop pins with a label and year
- Edit or delete existing pins
- Export pins as JSON
- Download a self-contained static HTML file with embedded pins
- Touch-friendly with mobile bottom-sheet UI
- Local mode (localhost) supports editing; deployed mode is read-only

## Usage

Open `index.html` in a browser. When running locally, click anywhere on the map to add a pin. Use the toolbar buttons to export your data or download a static copy with your pins baked in.

## Deployment

Static single-file app — deploy with staticer:

```sh
staticer deploy --domain corkmap.baileys.dev --expires never
```
