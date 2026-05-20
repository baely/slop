# Plus Codes

An interactive map visualisation of the [Open Location Code](https://maps.google.com/pluscodes/) (Plus Codes) grid system. As you zoom in, each cell breaks up into the next-finer Plus Code subdivision so you can see the hierarchy emerge.

## How the grid works

Plus Codes encode latitude/longitude in a hierarchical grid. Each successive pair of digits divides the world into a 20×20 sub-grid:

| Digits | Cell size       | Approx. width |
| ------ | --------------- | ------------- |
| 2      | 20° × 20°       | 2,200 km      |
| 4      | 1° × 1°         | 111 km        |
| 6      | 0.05° × 0.05°   | 5.6 km        |
| 8      | 0.0025° × 0.0025° | 275 m       |
| 10     | 0.000125° × 0.000125° | 14 m    |
| 11     | 4×5 refinement  | ~3 m          |
| 12     | further 4×5     | ~0.6 m        |

After 10 digits, refinement digits use a 4-row × 5-column sub-grid (so cells become rectangular rather than square).

## Files

- `index.html` — page shell, Leaflet/CARTO basemap, cursor readout, legend
- `style.css` — dark amber styling
- `main.js` — OLC encoder, custom Leaflet canvas layer that draws every active grid level with smooth opacity transitions

## Local dev

```sh
python3 -m http.server 8773
open http://localhost:8773/
```

## Deployment

Static site, deployed via `staticer`:

```sh
staticer deploy --domain pluscodes.baileys.dev --expires never
```
