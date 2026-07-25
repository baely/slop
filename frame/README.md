# frame

Aspect ratios, drawn to scale. Pick a source format (Scope, 6×6, 9:16, anything
you type), pick a target display, and see the letterbox bars, the fitted pixel
size and the crop — with the numbers underneath.

## What it does

- **Source formats**: cinema (silent 1.33, Academy 1.37, IMAX 1.43, flat 1.85,
  Univisium 2.00, 70mm 2.20, Scope 2.39), stills (half-frame 18×24, 6×4.5, 6×6,
  6×7, 4×5, 35mm), digital (4:3, 1:1, 16:9, 9:16). Each carries one line of
  context.
- **Targets**: 1920×1080, 2560×1080 ultrawide, 1600×1200, 1170×2532 phone, or a
  custom width × height.
- **Fit** letterboxes (nothing lost, panel not filled); **Fill** covers (panel
  full, edges cut). Fit mode still reports what Fill *would* cut.
- **Free-form input** accepts `2.39`, `16:9`, `16 9`, `3/2`, `4 by 5`,
  `1920x1080`, `1920px × 1080px`, and can be applied as either source or target.
- Reports the ratio as a decimal and as a GCD-reduced integer ratio
  (`1920×1080` → `16:9`, `2560×1080` → `64:27`, `1170×2532` → `195:422`), the
  fitted pixel size, percentage of the panel used, bar thickness per side, and
  the crop in percent and pixels.

## How it works

Plain HTML boxes, no canvas: a target rectangle in `--surface-2`, the live image
area filled teal, and in Fill mode the cropped overflow drawn outside the
rectangle in faded teal. The whole diagram is sized so the rectangle *plus*
its overflow always fits the stage, so switching modes never overflows or jumps.

Decimal ratios are reduced by lifting them to whole numbers first — `2.39:1`
becomes `239:100` before the GCD, which is why Scope reports `239:100` and not a
rounded `12:5`.

## Notable behaviour and limits

- Two whole numbers ≥ 100 are read as a pixel size (`1920x1080`); anything
  smaller is read as a ratio (`21:9`). A bare ratio used as a target is scaled to
  a 1080-tall panel so the pixel figures mean something.
- Ratios outside 0.05–20 are rejected, as are zero, negatives, exponent notation
  and more than two numbers — each with a specific message, never silently.
- Medium format uses actual frame sizes, not the nominal name: 6×4.5 is 56×42mm
  (4:3), 6×7 is 56×70mm (5:4).
- Selection, custom size, fit mode and the typed value persist in
  `localStorage`.
- Percentages are area-based in Fit mode; since bars only ever appear on one
  axis, "screen used" equals the used fraction of that axis.

## Deployment

```sh
staticer deploy --dir site --domain frame.baileys.dev --expires never
```

Live: https://frame.baileys.dev
