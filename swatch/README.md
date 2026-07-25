# swatch

Pull a palette out of a photograph and get back usable design tokens.

Drop, pick or paste an image; swatch extracts 3–12 dominant colours and prints
each one as hex, RGB, HSL and OKLCH with the share of the image it covers, its
WCAG contrast against white and black, and export blocks for CSS, JSON and SVG.

Everything runs in the browser. No image is uploaded, no network requests are
made at runtime beyond the Google Fonts stylesheet.

## How it works

**Sampling.** The image is drawn to an offscreen canvas capped at 320px on the
long edge, with `imageSmoothingEnabled = false`. Nearest-neighbour is
deliberate — smoothing would blend neighbouring pixels into colours the
photograph never contained. The cap means a 40-megapixel scan costs the same to
quantise as a thumbnail (~102k sample points, single-digit milliseconds).

**Quantisation.** Median cut, implemented from scratch, chosen over k-means
because it is deterministic: the same image gives the same palette every time.
For a tool emitting tokens someone pastes into a stylesheet, a palette that
drifts between runs is a bug. Boxes are split on the widest raw channel range
(not luma-weighted — weighting biases every split toward green and flattens the
blues a film scan actually has), picking the box with the largest
population × spread. Because splits partition pixels, the shares fall out of the
algorithm rather than being counted afterwards.

**Colour maths.** OKLCH via Ottosson's OKLab matrices over linear-light sRGB,
then Lab → LCh. Contrast is the real WCAG relative-luminance formula
(`0.2126R + 0.7152G + 0.0722B` on linearised channels, `(L1+0.05)/(L2+0.05)`).
Displayed ratios are truncated rather than rounded, so a printed `4.5:1` always
means at least 4.5 and never contradicts the verdict beside it.

## Behaviour and limits

- **Transparent pixels** with `alpha == 0` are skipped, not composited to black.
  Shares are a percentage of the opaque pixels sampled; the count of ignored
  pixels is shown.
- **Duplicate swatches are collapsed.** If two median-cut buckets average to the
  same rounded colour, or the image simply has fewer distinct colours than
  requested, you get the smaller set and a line saying so
  (`Only 2 distinct colours found.`).
- **Sorting** is by share (default), OKLCH lightness, or OKLCH hue. Near-
  achromatic colours (chroma < 0.02) sort to the end of hue order, since their
  hue angle is meaningless.
- Export variables are numbered `--swatch-1 … --swatch-N` in the current sort
  order. The SVG strip uses widths proportional to share.
- The contrast preview panes are fixed white and black in both themes — that is
  exactly what the printed ratios measure.
- Colour count and sort order persist in `localStorage`; the image does not.
- Rejects non-image files, undecodable images and fully transparent images with
  an explicit message rather than failing silently.
- Hue and saturation in the HSL readout are rounded to integers, so two distinct
  hex values can display the same HSL string. Hex is the identity.

## Deployment

```sh
staticer deploy --dir site --domain swatch.baileys.dev --expires never
```

Live at https://swatch.baileys.dev
