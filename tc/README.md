# tc

A total compensation calculator for a package that mixes AUD salary with
USD grants. Everything is a list of **awards**; the app converts them to AUD,
adds super where it applies, and shows what the number looks like year by year
and under different definitions of "TC".

Static page, no build step, no backend. State lives in `localStorage`.

## The model

An award is one line of the package. Five kinds:

| Kind | What it is | Super by default |
|---|---|---|
| Base | Salary | yes |
| Bonus | A percent of base salary that year | yes |
| Cash Grant | A cash award, often USD, often multi-year | yes |
| Equity Grant | An equity award, usually USD, usually multi-year | no |
| Other | Anything else — allowance, car, whatever | no |

Every award has a **basis**:

- **Per Year** — the amount repeats each year. Leave Years blank for "ongoing",
  meaning it runs to the end of the window.
- **Multi-Year** — the amount is the *total*, spread over Years. This is the
  one that matters for grants: `USD 160,000 over 4 years` is `USD 40,000` a year.

Multi-year awards vest evenly unless you give a **vest split**, written as
percentages: `25/25/25/25`, `40/30/20/10`, `60/40`. The split must have one
number per year or it's ignored (the award says so inline). The numbers are
normalised, so `1/1/2` works the same as `25/25/50`.

Awards can start **before** the display window. A grant from 2025 vesting over
four years correctly contributes to 2026, 2027 and 2028 only.

### Super

Super is calculated on the super-eligible awards that are **currently visible**
and added on top — it is never assumed to be inside a quoted figure. Because it
follows the toggles, hiding Bonus also drops the super earned on it, which is
what makes the "Guaranteed" view honest.

`Super Applies` is per-award, so a USD cash grant that attracts AUD super is
just a cash award with the box ticked — which is the default.

Optionally cap the calculation at the **maximum contribution base**
(AUD 250,000 p.a. for FY2025-26, editable). Off by default, since not every
employer applies it.

### FX

Rates are fetched at load from [frankfurter.dev](https://frankfurter.dev),
falling back to [open.er-api.com](https://open.er-api.com). No key, no CORS
proxy. Cached in `localStorage` for 12 hours; `Refresh` forces a re-fetch.

Everything converts to AUD internally, then to whatever display currency you
pick. Any rate can be overridden by hand to model a different assumption — the
header says `manual override active` when one is in effect.

## Views

The headline number changes with the toggles. Four presets:

- **Everything** — the whole package including super and equity
- **Ex-Super** — the same, minus super
- **Cash Only** — base + bonus + cash grants; no equity, no super
- **Guaranteed** — base + super; the part that isn't contingent

Toggle the six components individually and the preset reads `Custom View`.
Excluded components stay on screen, muted, so you can see what you're leaving
out rather than guessing.

Pick any single year, the **Average** year, or the **cumulative total** across
the window.

## Running it

No dependencies. Serve the directory:

```sh
cd tc
python3 -m http.server 8422
```

Then open <http://localhost:8422>. Opening `index.html` over `file://` works
too, except the FX fetch — enter rates manually if you do that.

## Deployment

Not deployed. It's a static directory, so `staticer deploy` (or
`staticer deploy --domain tc.baileys.dev --expires never`) would do it, but
note that a saved package sits in the browser's `localStorage` on whatever
device you use.

## Notes

- Years are labels, not dates. Use FY start years if you think in financial
  years — nothing in the maths cares.
- Bonus is always a percent of base for that year, and it's computed from base
  regardless of whether Base is visible. Hiding a component changes the total,
  not the definition.
- Follows the system light/dark preference. House style v1.
