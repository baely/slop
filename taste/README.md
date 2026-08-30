# taste

A design-language picker for bailey's slop apps. The goal: land on one house
style so every app reads as "that must be bailey's slop app".

## What it does

24 design-language dimensions, each presented as selectable variant cards
rendered in context:

- **surface**: display/body/mono type, heading treatment, UI casing, color
  mode, neutrals, accent + intensity, corner radius, borders, shadows,
  density, background texture, surface fill, motion, app signature mark
- **structure & language**: app shell (top nav / sidebar / centered /
  dashboard), data display (table / cards / list / timeline), platform stance
  (desktop-first / mobile-first / adaptive), prose vs data, iconography
  (emoji / unicode / ascii / words), microcopy tone, numbers & time formats

The trick: every pick writes CSS custom properties onto `:root`, so **the whole
tool restyles itself live** as you choose. A sticky "specimen" panel shows a
fake bailey app (nav, stat tiles, chart, records, form, microcopy) wearing the
current picks — including shell layout, record shape, tone, and icon style —
with a desktop/phone toggle to preview the platform stance.

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
staticer deploy --domain taste.baileys.dev --expires never
```

Redeploy gotcha: each deploy stacks a NEW deployment behind the domain and the
oldest keeps serving (`--replace` does not help — it's for `--name` conflicts).
After redeploying, delete the previous deployment by its random subdomain:
`echo yes | staticer delete <old-subdomain>` — note the subdomain printed at
deploy time.
