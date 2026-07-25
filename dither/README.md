# dither

Drop a photo, get a 1-bit version fit for an e-ink display or a zine.

Static, dependency-free, and entirely client-side — the image never leaves the tab.
Built for the trip from a film scan to a MicroPython e-ink panel or a Supernote.

Live: https://dither.baileys.dev

## What it does

Load an image by drag-and-drop, file picker, or clipboard paste. It gets downscaled
to a target width, converted to greyscale, tone-adjusted, and reduced to pure black
and white by one of five algorithms. Out the other end: a PNG at the processed size,
or the raw 1-bit bytes ready to hand to a panel driver.

### Algorithms

| | |
|---|---|
| Floyd–Steinberg | Error diffusion, all 16/16 of the error over 4 neighbours. The default look. |
| Atkinson | Error diffusion, only **6/8** of the error over 6 neighbours. The discarded 2/8 is deliberate — it's what blows out the extremes and gives Atkinson its high-contrast, airy character. |
| Bayer 4×4 | Ordered dither against a recursively generated Bayer matrix. Coarse, visible crosshatch. |
| Bayer 8×8 | Same construction at 64 levels. Finer texture, still obviously mechanical. |
| Threshold | Hard cut. No dithering at all. |

Bayer matrices are generated from `[[0,2],[3,1]]` by the standard recursion
`M(2n) = [[4M, 4M+2], [4M+3, 4M+1]]`, so 4×4 comes out as the canonical
`0 8 2 10 / 12 4 14 6 / 3 11 1 9 / 15 7 13 5`.

### Controls

Target width in pixels, with presets for common panels — 800×480, 400×300, and
1872×1404 (Supernote-ish) — plus Original. Presets set **width only**; aspect ratio
is always preserved. Then brightness, contrast, gamma, threshold, and invert.
Everything recomputes on change.

### Exports

- **Download PNG** — `canvas.toDataURL('image/png')` at the processed size.
- **Copy Bytes** — the packed bitmap as a C array or as MicroPython that builds a
  `framebuf.FrameBuffer`.

Packing is **1 bit per pixel, MSB first, row-major**, with each row padded out to a
whole number of bytes. That is `framebuf.MONO_HLSB`. **Bit 1 = black**, matching the
on-screen preview; Invert flips both together. An 800×480 frame is therefore
100 bytes/row × 480 = 48,000 bytes; 1872×1404 is 234 × 1404 = 328,536 bytes.

## Colour handling

Greyscale uses the Rec.709 luma weights `0.2126 / 0.7152 / 0.0722`, not a naive
channel average. Those coefficients are defined against **linear** light, so the sRGB
transfer function is undone first, the weights applied, and the result re-encoded.
Pure red therefore lands at 0.498 rather than the 0.333 a naive average would give.
Neutrals round-trip exactly.

Tone controls and the dithering itself run in that perceptual (gamma-encoded) space,
not in linear light. Diffusing error in linear light preserves mean luminance exactly,
which is the right model for an emissive display — but e-ink is reflective (white
reflects ~35–40%, black ~5%, and the ink spreads), so that model doesn't hold, and
linear diffusion drags midtones far darker than the source looks. The Gamma control
is there for anyone who wants to push toward it.

Tone is applied gamma → contrast (pivoting on mid-grey) → brightness, baked into a
2048-entry LUT rather than calling `Math.pow` a few million times per slider tick.

## Notable behaviour and limits

- **Working raster is capped** at 4096 px on either side and 6 MP total. A 60 MP scan
  is downscaled rather than hanging the tab; the UI says so when the cap bites.
- Downscaling halves repeatedly before the final draw — a single large step loses
  detail badly.
- Alpha is flattened onto white before conversion.
- The byte dump shows only the first 512 bytes. **Copy copies all of them**; the full
  text is only formatted when you click, since a 1872-wide frame runs to ~1.4 MB.
- At 1872×1404 a slider drag costs ~55 ms per frame for Floyd–Steinberg (~15 fps).
  Usable, not silky. 800×480 is ~8 ms. Renders are coalesced through
  `requestAnimationFrame`, so drags never queue up a backlog.
- Non-image files, undecodable images, and vector files with no intrinsic size are all
  reported in the status line.
- Control state persists in `localStorage`, wrapped in try/catch for private mode.
- Error that falls off the edge of the raster is discarded, as in the original
  implementations.

## Deployment

```sh
staticer deploy --dir site --domain dither.baileys.dev --expires never
```
