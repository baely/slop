# resizer

Browser-only image resizer. Upload a JPG or HEIC, set output dimensions, pick a fit mode, download as JPG.

## Modes

- **Stretched** — fills the output exactly, distorting if aspect ratios differ.
- **Fit** — scales the whole image to fit inside the output, padding the remainder white.
- **Cover** — scales to fill the output, cropping the overflow.

## Run locally

Open `index.html` in a browser. No build step. HEIC decoding uses the `heic2any` library loaded from a CDN.

## Deploy

```
staticer deploy
```
