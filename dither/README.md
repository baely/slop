# dither

Downscale a video, dither it, preview it. Entirely client-side — the video
never leaves the browser.

https://dither.baileys.dev

## How it works

1. Drop in a video file (mp4, webm, mov — anything the browser can decode).
2. Each frame is drawn to a small offscreen canvas at the chosen pixel width
   (40–400 px, aspect preserved) — that's the downscale.
3. The frame is converted to luminance (with optional brightness/contrast
   adjustment) and quantized to the chosen palette through a dither:
   - **Floyd–Steinberg** — classic error diffusion
   - **Atkinson** — lighter diffusion, the old Mac look
   - **Bayer 4×4 / 8×8** — ordered dithering, temporally stable
   - **Posterize** — straight quantization, no dither
4. Palettes: Black & White, Grayscale ×4, Game Boy (DMG greens), Amber.
5. The result is blitted to the visible canvas at an integer scale with
   smoothing off, so pixels stay crisp — never scaled beyond the source's
   own dimensions. Playback re-dithers every frame in real time.

**Export WebM** replays the clip from the start while recording the canvas
with `MediaRecorder`, then downloads the result (video only, no audio track).
Note: browser-recorded WebM files carry no duration metadata until fully
scanned; most players cope.

## Stack

Static HTML/CSS/JS, no dependencies, no build. Styled per the repo
[house style](../house-style/README.md).

## Deploy

```
staticer deploy --domain dither.baileys.dev --expires never
```

## Development

Serve the directory with anything, e.g.:

```
python3 -m http.server 8080
```
