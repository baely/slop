# GPXer

A browser-based GPX route builder and exporter. Click on a map to place waypoints and track points, then export as a `.gpx` file.

## Features

- Dark-themed Leaflet map with CartoDB tiles
- Two modes: waypoint placement and track point placement
- Name and describe waypoints
- Drag markers to reposition
- Reorder track points
- Export valid GPX files
- Coordinate display on hover

## Usage

Open `index.html` in a browser. Toggle between waypoint and track point modes, click the map to add points, then hit Export GPX to download.

## Deployment

Static single-file app — deploy with staticer:

```sh
staticer deploy --domain gpxer.baileys.dev --expires never
```
