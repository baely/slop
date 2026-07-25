# plot

Generative 1-bit pattern maker. Seeded, parameterised, exportable — output aimed
at e-ink screens, Supernote note templates and pen plotters. Pure black on white
(or inverted), no grey, no colour in the artwork.

`supernote-templates` covers ruled pages; this is the other end, for pattern and
texture.

## Generators

| generator | what it draws | controls |
|---|---|---|
| Flow Field | particles advected through a noise field, traced as thin polylines | particles, max steps, step length, field scale, octaves, turn rate, separation |
| Contours | nested contour bands lifted off the noise field by marching squares | grid resolution, bands, field scale, octaves |
| Truchet | the classic quarter-arc tiling, plus a straight-chord variant | variant (arcs / diagonals / mixed), tile size, lines per tile, fill |
| Hatching | crosshatch fills at a set angle, with jitter and an optional noise mask | angle, spacing, jitter, passes, cross angle, noise mask, mask scale |

Shared across all four: canvas presets (Supernote 1404 × 1872, e-ink panel
800 × 480, A4 at 300 dpi 2480 × 3508, square 1000 × 1000, or custom), line
weight, density multiplier, margin, and invert.

## How it works

- **PRNG**: mulberry32. The seed string is hashed with FNV-1a into a 32-bit
  state. `Math.random()` is not used anywhere in generation — the only place
  entropy enters is the "New Seed" button, which draws from
  `crypto.getRandomValues`.
- **Noise**: gradient (Perlin-style) noise written from scratch. Its permutation
  table and 256 unit gradient vectors are shuffled off a seeded stream, with fBm
  on top (lacunarity 2, gain 0.5).
- **Two streams**: the noise field is seeded from `seed ^ 0x9e3779b9` and the
  generator from `seed`, so changing a particle count reshapes the drawing
  without reshuffling the underlying field.
- **Determinism**: same seed + same parameters ⇒ byte-identical path data.
  Coordinates are rounded to 2 dp before they reach the string, so there is no
  float-formatting drift between runs.
- **Output**: real SVG paths, never a wrapped raster. Subpaths are concatenated
  500 at a time into each `<path>` element to keep the DOM small; both the
  subpath count and the element count are shown. PNG export rasterises that same
  SVG through a canvas at full resolution.
- Everything runs client-side. No network requests at runtime beyond the Google
  Fonts stylesheet.

## Limits and guards

Dense settings at A4/300dpi are capped rather than allowed to lock the tab, and
every clamp is stated in the UI instead of being silently applied:

| cap | value |
|---|---|
| flow field segments | 150,000 (generation stops, reports how many particles were used) |
| flow field particles | 60,000 |
| contour grid | 250,000 vertices; grid cells × bands ≤ 4,000,000 |
| truchet tiles | 30,000 (tile size is grown instead) |
| hatch lines | 12,000 across all passes (spacing is widened instead) |
| subpaths, any generator | 100,000 |

Worst-case measured render: 77 ms for a dense flow field at 2480 × 3508
(2.2 MB of SVG). Canvas sides outside 100–6000 px are refused with a message
rather than rendered.

Settings persist in `localStorage` (wrapped in try/catch, so private mode just
falls back to defaults).

## Deployment

```sh
staticer deploy --dir site --domain plot.baileys.dev --expires never
```

Live at https://plot.baileys.dev
