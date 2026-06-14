# Darkroom

A single-page playground for editing photos with code. Upload an image, write a
JavaScript `pixel(x, y)` function, and it runs once for every pixel to produce a
new image. Your function can read the colour of any pixel in the original photo —
including the neighbours of the current one — which is enough to build blurs,
sharpening, edge detection, warps and more.

No backend, no build step. Everything runs in the browser.

## The API

Your code must define one function:

```js
pixel(x, y) → [r, g, b]   // or [r, g, b, a]
```

It is called for every pixel. Channel values are `0–255`; returned values are
rounded and clamped for you.

Inside `pixel` you can call:

```js
get(x, y) → [r, g, b, a]
```

`get` reads the **source for the current pass** — the original photo on pass one,
and the previous pass's output after that — so neighbour lookups never see edits
from the pass they're in. Reads outside the image clamp to the nearest edge.

Also in scope: `width`, `height`, and the global `Math`.

### Sliders

Declare any value you want to tune by eye and a live slider appears below the
editor; dragging it re-develops the photo:

```js
slider(name, default, min, max, step?)   // returns the current value
```

```js
const amt = slider("amount", 1.5, 0, 4);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const l = 0.299*r + 0.587*g + 0.114*b;
  return [l + (r-l)*amt, l + (g-l)*amt, l + (b-l)*amt];
}
```

If `min`/`max` are omitted they default to a sensible range; `step` is inferred.

### Iterations

The `×` stepper next to **Develop** runs your function several times in a row
(1–50), each pass feeding into the next. Useful for effects that build up — a
box blur ×10 becomes a strong blur, a swirl ×N winds tighter, and so on.

### Example — a box blur using neighbours

```js
function pixel(x, y) {
  let r = 0, g = 0, b = 0;
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      r += p[0]; g += p[1]; b += p[2];
    }
  }
  return [r / 9, g / 9, b / 9];
}
```

## Features

- Drag-and-drop, browse, paste, or load a procedural sample image
- CodeMirror editor with 30 ready-made presets (grayscale, invert, sepia,
  posterize, threshold, brightness/contrast/saturation, hue rotate, box/gaussian/
  motion blur, sharpen, unsharp, Sobel edges, emboss, outline, dilate, erode,
  vignette, duotone, solarize, dither, thermal, chromatic aberration, pixelate,
  ripple, frosted glass, swirl)
- Inline `slider()` controls and an iterations stepper for live, no-retyping tweaks
- `⌘/Ctrl + Enter` to run; progress bar (with pass count) while developing
- Hold **compare** to peek at the original; **download** the result as PNG
- Large uploads are scaled down to a 1600px long edge so runs stay responsive

## Running locally

It's a static site — open `index.html` via any static server:

```sh
python3 -m http.server 8000
# then visit http://localhost:8000
```

(CodeMirror and the fonts load from CDNs, so an internet connection is needed.)

## Deployment

Deployed as a static site with `staticer`:

```sh
staticer deploy --domain darkroom.baileys.dev --expires never
```
