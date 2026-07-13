# bailey house style — v1

The shared design language for every slop app, generated 2026-07-11 from
bailey's picks in [taste](https://taste.baileys.dev). The goal: someone lands
on any of these apps and thinks "that must be bailey's slop app."

Source of truth: this spec + `house.css` (canonical tokens and component
recipes). Apps are self-contained, so they **copy the token block** from
`house.css` into their own styles and adapt the recipes — they don't link to a
shared stylesheet.

## The language in one paragraph

Flat, pure-neutral surfaces with no borders and no shadows — separation comes
from background steps. One loud color: deep teal `#0891b2`, worn confidently
(filled nav band, filled primary buttons, filled selected states), with a
subtle gradient sheen on interactive fills. Massive tight Bricolage Grotesque
headlines over quiet Inter body text, JetBrains Mono for anything that smells
like data. Corners barely rounded (3px). Title Case controls, words instead of
icons, technical microcopy, humanized timestamps, zero animation. Every app
follows the visitor's system color scheme and signs itself with a small `b.`
in the corner.

## Tokens

| token | light | dark |
|---|---|---|
| `--bg` | `#fafafa` | `#0b0b0d` |
| `--surface` (cards, inputs) | `#f1f1f3` | `#17171a` |
| `--surface-2` (hover, nested) | `#e9e9ed` | `#212126` |
| `--text` | `#131316` | `#f2f2f4` |
| `--text-2` | `#55555e` | `#a3a3ad` |
| `--muted` | `#9b9ba6` | `#5b5b66` |
| `--line` (table rules, focus rings only) | `#e4e4e9` | `#26262c` |
| `--accent` (the brand teal) | `#0891b2` | `#0891b2` |
| `--accent-deep` (fills carrying small text, hover) | `#0e7490` | `#0e7490` |
| `--accent-text` (teal as text/link color) | `#0e7490` | `#3aa8c4` |
| `--accent-soft` (bands, chips, selected rows) | `#e2f0f4` | `#0c2229` |
| `--accent-ink` (text on teal fills) | `#ffffff` | `#ffffff` |
| `--r-ctl` / `--r-card` | `3px` / `4px` | same |
| `--sheen` (gradient overlay on fills) | `linear-gradient(180deg, rgb(255 255 255 / .12), rgb(0 0 0 / .08))` | same |

Mode policy: **follow the system** (`prefers-color-scheme`) and honor a manual
`data-theme="light|dark"` override on `<html>` if the app offers a toggle.
Exception: an app may pin one mode when its domain demands it (darkroom and
gallery stay dark). Both palettes must be defined either way.

## Type

- Google Fonts: `Bricolage Grotesque` (600, 800), `Inter` (400, 500, 600, 700),
  `JetBrains Mono` (400, 500, 700).
- **Display** (`--font-display`): Bricolage Grotesque. Page titles, wordmarks,
  hero numbers, card titles. Headlines are *massive & tight*: weight 800,
  `letter-spacing: -0.03em`, h1 around `clamp(28px, 6vw, 44px)`. Card titles
  ~15–16px/800.
- **Body** (`--font-body`): Inter, 14–15px, line-height ~1.55. Should disappear.
- **Mono** (`--font-mono`): JetBrains Mono for stats, timestamps, IDs, code,
  table numerics, coordinates. If it's a value, it's mono.
- **App wordmarks stay lowercase** (covers, darkroom, gpxer…), Bricolage 800.
- **UI controls are Title Case**: buttons, tab labels, table headers, form
  labels ("Save Changes", "Add Note"). Written in the markup, not via
  `text-transform`. Headings and prose are normal sentence case.

## Color rules

- Teal is **loud**: the top-nav band is `--accent-soft`; primary buttons are
  filled teal; the active nav item / selected row / focused state is teal.
  One big teal moment per screen is right; three is noise.
- Teal as text uses `--accent-text` (contrast-safe per mode). Fills that carry
  small text use `--accent-deep`.
- Everything else is a pure neutral. No second hue in the chrome.
- Domain colors are content, not chrome: map tiles, pool felt, film scans,
  album art, e-ink 1-bit output all keep their own colors.
- Charts: single series is teal. Multiple series use, in fixed order:
  `#0891b2`, `#b45309`, `#6d28d9`, `#be185d` — never cycled, never re-mapped
  when filtering. Status is reserved and never decorative: good `#15803d`,
  warning `#b45309`, bad `#b91c1c`, always with a text label, never
  color-alone. Thin marks, quiet grid, no rainbows, one y-axis.

## Surfaces

- **No borders. No shadows.** A card is a background step: `--surface` on
  `--bg`; nesting or hover goes to `--surface-2`.
- Hairlines (`--line`) exist only *inside* data: table row rules, and nothing
  else.
- Interactive fills (buttons, filled chips) get the `--sheen` gradient overlay
  (`background-image: var(--sheen)` over the solid `background-color`).
  Non-interactive surfaces stay dead flat.
- Radius: 3px on controls, 4px on cards. Never pills.

## Components (recipes in `house.css`)

- **Top nav**: full-width `--accent-soft` band, lowercase wordmark left,
  Title Case links right, active link in `--accent-text` weight 600.
- **Primary button**: teal fill + sheen, white text, 600, 3px radius, generous
  hit area (min 44px tall on touch). **Secondary**: `--surface` fill, `--text`.
  No outline/ghost buttons — flat world, fills only.
- **Inputs**: `--surface` fill, no border, 3px radius; focus = 2px solid
  `--accent` outline, 2px offset.
- **Tables** are the default record shape: Title Case headers in 12px
  `--text-2`, rows separated by 1px `--line`, numerics right-aligned mono,
  selected row `--accent-soft`.
- **Chips/badges**: `--accent-soft` fill, `--accent-text` text, 3px radius.
- **Stat**: label 12px `--text-2` Title Case, value in Bricolage 800 (or mono
  for precise quantities), optional context line in `--text-2`.
- **Empty states**: one technical line, e.g. `0 rows.` `Nothing logged yet.`

## Voice & copy

- **Data-forward**: lead with the number/value; explain only when asked.
  Prefer a stat row over a paragraph.
- **Technical, deadpan-adjacent tone**: `0 rows.` `Saved.` `3 of 5 selected.`
  No exclamation marks, no cheerleading.
- **Humanized time**: "4 minutes ago", "yesterday", "3 weeks ago" for display.
  Exact timestamps go in a `title` attribute or mono detail line when
  precision matters.
- **Words, not icons**: no emoji, no icon fonts, no SVG glyph buttons.
  "Delete", not a trash can. Unicode arrows for pure direction (`→`) are fine
  in data (routes, diffs), not as button decoration.

## Layout & platform

- **Top nav shell** by default; single column that grows into wider layouts
  with `min-width` media queries (**mobile-first**). Comfortable density:
  spacing on a 4px scale — 8/12/16/24 as the working set.
- Tap targets ≥ 44px; primary action reachable near the thumb on phone.
- Content max-width ~1100px unless the app is a map/canvas tool.

## Motion

None. No transitions, no animations, no hover lifts. State changes are
instant. (Remove existing `transition:`/`@keyframes` chrome when restyling.)

## Signature

Every app carries the corner glyph — the one shared mark:

```html
<a class="b-glyph" href="https://index.baileys.app" title="A Bailey App">b.</a>
```

Fixed bottom-right, Bricolage 800 13px, `--text-2` on `--surface`, 3px radius.
Quiet. It links to the app index. If it would cover a map/canvas control,
nudge it, don't drop it.

## Don't

- No borders or box-shadows on chrome. No pills. No letter-in-a-box monogram
  logos. No emoji or icon sets. No purple gradients, no second accent hue.
- No headers/footers/eyebrow labels that only carry decoration — the style is
  the signature; keep the chrome minimal.
- Don't restyle content: photos, maps, game surfaces, generated output.
- Don't change behavior while restyling — markup may change, logic shouldn't.
