# stops

An exposure calculator for shooting film. Pick the light, move one dial, and
the others follow.

Nine scene presets anchored to Sunny 16 (EV 15) set the light. Aperture,
shutter and film speed each step in third-stops, and whichever one you nominate
as the compensator absorbs every change so the exposure stays honest. Move the
compensator itself and the scene is re-metered instead — that's how you dial in
deliberate over- or under-exposure.

- **Equivalent Exposures** lists every full-stop aperture at the current light
  with its matching shutter speed and how far off exact it lands. Pick a row to
  use it.
- **Reciprocity** appears once the shutter passes a second, with corrected times
  for HP5+/FP4+/Delta, Tri-X, T-Max, Fomapan, Acros II and Portra 400.
- Out-of-range answers are called out rather than silently clamped: *"needs
  17m 35s. Clamped — 2 stops under."*

Settings persist in `localStorage`. No dependencies, no build step, no data
files — the whole app is three static files.

## The math

`EV100 = log2(N² / t) − log2(ISO / 100)`, with the compensating dial solved
from the other two and snapped to the nearest real scale position. Deviations
inside half a scale step read as *Exact* — beyond that it's nominal f-number
drift (f/2.8 is really 2√2), not an error worth reporting.

Reciprocity uses the Schwarzschild form `t_corrected = t^p`: Ilford's published
`p = 1.31`, Tri-X `1.28`, T-Max `1.06`, Fomapan `1.62`. Acros II needs no
correction under two minutes; Portra 400 is flat to a second and about
+1/2 stop at ten, with nothing published past that. All approximations —
bracket anything critical.

Styled to the [bailey house style](../house-style/) — flat neutral surfaces,
one loud teal, Bricolage headlines, JetBrains Mono for every value, zero
motion, `b.` in the corner. Follows the system color scheme.

## Deployment

```sh
staticer deploy --dir site --domain stops.baileys.dev --expires never
```

Live at https://stops.baileys.dev

Note: re-running a domain deploy stacks a new deployment behind the same
domain and the oldest keeps serving — delete the previous random subdomain
(printed at deploy time) with `staticer delete <subdomain>` after redeploying.
