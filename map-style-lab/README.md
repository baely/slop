# Flyover Lab

A quick, single-file tool for comparing map **styles and providers** for travel-montage flyovers. It renders several maps side by side and flies the same route through every panel simultaneously, so you can judge which provider/style looks best for cinematic flyover footage before committing.

## Usage

Just open `index.html` in a browser — no build, no server.

```sh
open index.html
```

## What it does

- **Side-by-side grid** of map panels, one per selected style. The camera in every panel is driven by the same route so comparisons are frame-for-frame.
- **Fly route** — animates `flyTo` between waypoints with a configurable pitch, arrival zoom, and per-leg duration. The camera faces the direction of travel on each leg, with an optional slow orbit on arrival.
- **3D terrain** on every style via the keyless AWS/Mapzen Terrarium DEM.
- **Route editor** — search places (OpenStreetMap Nominatim), drag to reorder, remove.
- **Record** — one click captures the whole flight and saves a separate WebM for **every selected panel**, in selection order, each at the chosen frame resolution. The maps jump to a wide framing of the route, then fly it once (loop is ignored during recording). Files are named `flyover-NN-{style}-{WxH}.webm`. Your browser may ask to allow multiple downloads.
- **Per-panel capture** — screenshot a single panel to PNG (`⤓`) or record just that one to WebM (`●`) while the montage plays.
- **Sync** — pan/zoom any map and the rest follow, for manual side-by-side inspection.
- **Solo** (`◳`) any panel to fill the stage.
- **Frame** — render each panel (and its recording) at an exact output size: Fit (responsive), 1920×1080, 1280×720, 1080×1920 (vertical 9:16), 1080×1080 (square), or a custom W×H. Panels are laid out at true pixel size then scaled as a group to fit the stage, so a WebM recorded from a panel comes out at the chosen resolution.
- **North lock** — keep north up for the whole montage instead of turning the camera to face the direction of travel (disables the arrival orbit).

Keyboard: `space` play/stop, `f` fit route.

## Providers

**Keyless (work out of the box):** OpenFreeMap (Liberty / Bright / Positron), CARTO (Dark Matter / Voyager / Positron), Esri (World Imagery / Topographic), MapLibre demo tiles.

**Key required (entered in the sidebar, stored in `localStorage`):**
- **MapTiler** — Satellite, Hybrid, Streets, Outdoor, Topo, Winter, Basic. [Get a key](https://cloud.maptiler.com/account/keys/).
- **Mapbox** — Satellite Streets, Outdoors, Streets, Light, Dark (loaded through MapLibre by rewriting `mapbox://` references). [Get a token](https://account.mapbox.com/access-tokens/).

## Built with

[MapLibre GL JS](https://maplibre.org/) (one engine for every provider, so the camera animation stays identical across panels). Terrain DEM from AWS Open Data; geocoding from OpenStreetMap Nominatim.

## Notes

- This is a throwaway comparison tool, not a production app — settings persist only in your browser's `localStorage`.
- The Mapbox **Standard** (v3) style is intentionally omitted because its 3D features aren't supported by MapLibre; the classic raster/vector styles render fine.
- Nominatim geocoding is rate-limited — add places one at a time.
