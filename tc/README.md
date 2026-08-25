# tc

A total compensation calculator for a package that mixes AUD salary with
USD grants. It converts everything to AUD, adds super where it applies, and
shows what the number looks like year by year and under different definitions
of "TC".

Static page, no build step, no backend. State lives in `localStorage`.

## The model

There is no fixed list of award kinds. An award is **composed** from three
independent parts, so new shapes fall out of combining them rather than
needing new code.

### Value — where the number comes from

| Mode | Meaning |
|---|---|
| **Fixed Amount** | A number in any supported currency |
| **Percent Of** | A percent of the total of one *or more* groups, in the same year |

`Percent Of` resolves per year against live values, so a bonus set to 15% of
Base automatically tracks a pay rise, and can be pointed at several groups at
once (15% of Base + Cash). Circular references are detected, reported inline,
and treated as zero rather than hanging.

### Schedule — how it lands across years

| Type | Meaning |
|---|---|
| **One-Off** | Lands entirely in a single year |
| **Recurring** | Repeats each year, optionally with compounding `Growth %/Yr`. Leave Years blank for "ongoing" |
| **Spread** | The amount is a *total*, distributed across N years |

`Spread` vests evenly unless given a **vest split** in percentages:
`25/25/25/25`, `40/30/20/10`, `0/33/33/34` for a one-year cliff, `60/40`.
The split needs one number per year or it's ignored (the award says so
inline). Values are normalised, so `1/1/2` means the same as `25/25/50`.

Awards can start **before** the display window. A grant from 2025 vesting over
four years correctly contributes to 2026, 2027 and 2028 only.

### Group — the category it belongs to

Groups are user-defined. They're what the view toggles switch on and off, what
the By Year table columns are, and what a `Percent Of` award can point at.
Rename, add, or delete them in the Groups card; a group in use can't be
deleted. Each group carries a `Super By Default` flag that new awards in it
inherit.

## Award types

Types are saved templates of a composition — a value mode, a schedule, a
group, and a super flag, without the money. Pick one and hit **Add Award** to
get an award pre-shaped that way.

Ships with Base Salary, Target Bonus (% Of Base), Equity Grant (4-Year Even),
Equity Grant (1-Year Cliff), Cash Grant (Multi-Year), One-Off Payment and
Recurring Allowance. **Save As Type** on any award turns its shape into a new
type; saving over a type of the same name updates it. Types are renamable and
deletable, and `Reset To Example` restores the built-ins.

## Super

Super is calculated on the awards marked `Super Applies` that are **currently
visible**, and added on top — never assumed to be inside a quoted figure.
Because it follows the toggles, hiding a group also drops the super earned on
it, which is what makes a "Guaranteed" view honest.

A USD cash grant that attracts AUD super is just an award with the box ticked.

Optionally cap the calculation at the **maximum contribution base**
(AUD 250,000 p.a. for FY2025-26, editable). Off by default, since not every
employer applies it.

If you need a second super-like derived amount — a pension, an employer match
— model it as a normal award: `Percent Of` the groups it accrues on.

## FX

Rates are fetched at load from [frankfurter.dev](https://frankfurter.dev),
falling back to [open.er-api.com](https://open.er-api.com). No key, no CORS
proxy. Cached in `localStorage` for 12 hours; `Refresh` forces a re-fetch.

Everything converts to AUD internally, then to whatever display currency you
pick. Any rate can be overridden by hand to model a different assumption — the
header says `manual override active` when one is in effect.

## Views

The headline number changes with the toggles. Two views are always present and
follow whatever groups exist:

- **Everything** — every group plus super
- **Ex-Super** — every group, minus super

Everything else is yours: toggle the groups you want, name it, and hit
**Save View**. Ships with *Cash Only* and *Guaranteed* as examples. Excluded
components stay on screen, muted, so you can see what you're leaving out
rather than guessing.

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
- Group totals used by `Percent Of` are computed from every enabled award in
  that group, whether or not the group is visible. Hiding a component changes
  the total, not the definition.
- Combining `Percent Of` with `Spread` is allowed: the percent is evaluated in
  each year, then the vest weight is applied to it.
- Configs saved by the earlier fixed-kind version are migrated automatically
  on first load — old `base`/`bonus`/`cash`/`equity`/`other` kinds become
  groups, and bonuses become `Percent Of` Base.
- Follows the system light/dark preference. House style v1.
