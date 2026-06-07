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

`get` always reads the **original** photo, so neighbour lookups never see your
in-progress edits. Reads outside the image clamp to the nearest edge.

Also in scope: `width`, `height`, and the global `Math`.

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
- CodeMirror editor with a dozen ready-made presets (grayscale, invert, sepia,
  posterize, box blur, sharpen, Sobel edges, emboss, chromatic aberration,
  pixelate, ripple)
- `⌘/Ctrl + Enter` to run; progress bar while developing
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
