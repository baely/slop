# taste

A design-language picker for bailey's slop apps. The goal: land on one house
style so every app reads as "that must be bailey's slop app".

## What it does

17 visual dimensions — display/body/mono type, heading treatment, UI casing,
color mode, neutrals, accent + intensity, corner radius, borders, shadows,
density, background texture, surface fill, motion, and an app signature mark —
each presented as selectable variant cards rendered in context.

The trick: every pick writes CSS custom properties onto `:root`, so **the whole
tool restyles itself live** as you choose. A sticky "specimen" panel shows a
fake bailey app (nav, stat tiles, chart, table, form) wearing the current picks.

- **Presets** — `swiss · now` and `instrument · now` approximate the two design
  families the existing apps already cluster into; the rest are new directions.
- **Shuffle / reset** — explore random languages or return to defaults.
- **State** — persisted to localStorage and encoded in the URL hash (shareable).
- **Export** — a prose summary + `tokens.json`, both designed to be pasted back
  to Claude to generate the final design language and restyle every app.

## Stack

Static, no build: `index.html` + `styles.css` + `app.js`, Google Fonts.

## Deploy

```
staticer deploy --domain taste.baileys.dev --expires never --replace
```

(`--replace` matters on redeploys — without it staticer stacks a new deployment
behind the same domain and the oldest one keeps serving.)
