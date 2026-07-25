# sheet

Drop a CSV, understand it in ten seconds. Column types, per-column summaries, a
sortable table and a quick chart — all client-side, nothing uploaded.

Built for the CSVs that actually land on this machine: `statement-parser`
transaction exports and Letterboxd data exports.

<https://sheet.baileys.dev>

## What it does

- **Load** by drag-drop (anywhere on the page), file picker, pasting into the
  text box, or just pressing Cmd-V on the page.
- **Parse** with a hand-written RFC 4180 state machine — quoted fields, `""`
  escapes, embedded commas, embedded newlines, `\n` and `\r\n`, BOM. The
  delimiter (comma, tab, semicolon, pipe) is auto-detected and named in the UI.
- **Infer** a type per column — number, date, boolean, text — shown in the table
  header and on each column card, and overridable per column.
- **Summarise** each column on a card: numbers get count/min/max/mean/median/sum,
  dates get range and span, text gets distinct count and the top five values,
  booleans get true/false counts.
- **Table**: click a header to sort (ascending → descending → original), filter
  box narrows rows live, numerics right-aligned in mono.
- **Chart**: pick X and Y, choose Sum / Mean / Count, choose Bar or Line. Drawn
  as hand-written SVG, single teal series, one y-axis, hover tooltip, and a
  "Plotted Data" table of exactly what was plotted.

## Decisions worth knowing

**Numbers.** `1,234.50` is a number — grouping must be correct, so `12,34` is
text. `007` is *not* a number: a leading zero on a multi-digit integer means
identifier, so account numbers, BSBs and zero-padded codes stay text. One leading
currency symbol is allowed (`$1,234.50`, `-$12.00`) because bank exports carry
them. Scientific notation is accepted. Percentages (`12.5%`) and accounting
negatives (`(12.00)`) are not — they stay text.

**Dates.** ISO (`2026-07-25`, with optional time), `YYYY/MM/DD`, `3 Jan 2024`,
`Jan 3, 2024`, and slash/dash forms like `01/02/2024`. This is Australia, so
day-first wins: the *column* decides the order, not the cell. If any value has a
first component above 12 the column is definitively day-first; if any has a
second component above 12 it's month-first; if every value fits both orders the
card says so — *"Day-first assumed — every value fits either order."* If the
source mixes both orders, day-first is used and the cells that don't fit are
reported as unparsed rather than silently read the other way. Two-digit years map
00–69 → 2000s, 70–99 → 1900s. The type chip only claims an order (`date d/m/y`)
when the column actually contains ambiguous forms; an ISO column just says
`date`.

**Type threshold.** A column takes a type when 90% of its non-empty values fit.
Blanks never count. `0`/`1` columns are numbers, not booleans.

**Charting.** Dates sort chronologically and bucket by day, month or year
depending on spread — stated in words (`Bucketed by month.`). Categorical X is
aggregated and cut to the top 20, stated (`Top 20 of 137 categories.`). Numeric X
with more than 200 distinct values is binned into 40 equal ranges, stated. The
chart follows the table filter and says so. A column of four-digit integers in a
plausible year range is treated as a label, not a measure, so the default
aggregate becomes Count rather than a meaningless sum of years.

## Limits

- **Rendering is capped at 500 rows** with a "Show 500 More" button; the cap is
  stated in the UI. Parsing, sorting, filtering and stats always cover the whole
  file — only the DOM is capped. A 50k-row / 1.9 MB file parses in ~35 ms and
  infers + summarises in ~60 ms.
- Files over 25 MB are refused with a message.
- Only the first row is treated as the header. Ragged rows are reported by line
  number and then padded or trimmed to the header width; blank lines are skipped
  and counted.
- The last dataset is kept in `localStorage` if it's under 400 KB, so a reload
  restores it. Type overrides and chart settings persist alongside. "Clear"
  removes it. All storage access is wrapped in try/catch for private mode.

## Verification

Core parsing, type inference, stats and scales are pure functions exported from
`site/app.js` under `module.exports`, so they run directly under node. 150 unit
assertions plus a jsdom pass over the real UI (sorting, filtering, overrides,
chart controls, ragged rows, header-only, JSON-not-CSV, the render cap) were run
against hand-derived values, plus a run over a real Letterboxd export and a
`statement-parser`-shaped transaction CSV.

## Deployment

Static; `site/` is the whole app.

```sh
staticer deploy --dir site --domain sheet.baileys.dev --expires never
```
