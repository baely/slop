# cron

Write a cron expression, see what it actually means and when it actually fires.

Live: https://cron.baileys.dev

## What it does

- Parses standard 5-field cron — `minute hour day-of-month month day-of-week` —
  with `*`, `*/n`, `a-b`, `a-b/n`, lists, month names (`JAN`–`DEC`), day names
  (`SUN`–`SAT`), all case-insensitive. `7` and `0` both mean Sunday. The
  `@hourly`/`@daily`/`@weekly`/`@monthly`/`@yearly`/`@midnight` aliases expand.
- Translates it into one plain-English sentence:
  `At 03:15 on the 1st and 15th of every month.` / `Every 5 minutes.`
- Lists the **next 10 fire times** in the visitor's local timezone as absolute
  timestamps plus a humanized relative ("in 4 minutes", "tomorrow 03:15"), with a
  live countdown to the next one.
- Breaks the expression down field by field, so a wrong field is obvious:
  `dom  1,15  →  1st, 15th`.
- Names the offending field on bad input:
  `Field 2 (hour): 25 is out of range 0-23.`

Everything runs client-side. No network requests, no storage beyond the last
expression in `localStorage`. The current expression is mirrored into the URL
hash, so a link is shareable.

## How it works

**The day-of-month / day-of-week OR rule.** When *both* `dom` and `dow` are
restricted, cron fires when **either** matches — not both. `0 0 13 * FRI` runs on
every Friday *and* on the 13th of every month, not only on Friday the 13th. When
one of them is `*`, only the other selects days. Following Vixie cron, a field
that *begins* with `*` (including `*/2`) counts as unrestricted here.

**Fire times.** Calendar stepping runs on a `Date` in UTC used purely as a
wall-clock container, so month lengths and leap years are exact and no timezone
offset can distort the arithmetic. Each matched wall time is then converted to a
real local `Date`. The search is capped at 100 years, so an impossible expression
(`0 0 30 2 *`, `0 0 31 4 *`) terminates and reports `Never fires.` instead of
hanging.

**Daylight saving.** Fire times are wall-clock times, like real cron. On a
spring-forward transition, wall times that don't exist locally are dropped and
the count is reported under the table. On a fall-back transition the repeated
hour is listed once, so the list is always strictly increasing with no
duplicates.

## Limits

- 5-field cron only. Six/seven-field forms (Quartz seconds, Spring, year fields)
  are rejected with a message saying so. `?` is accepted in `dom`/`dow` as a
  synonym for `*`.
- No `L`, `W`, `#` or `LW` extensions.
- Non-POSIX `a/n` (e.g. `5/15`) is accepted and read as `5-59/15`, matching most
  implementations in the wild.
- The timezone is whatever the browser reports; there's no timezone picker.

## Verification

Core parsing, description and scheduling logic live in `site/app.js` as pure
functions and are exported for node. They were checked against hand-derived
answers across `TZ=Australia/Melbourne`, `TZ=America/New_York` and `TZ=UTC`,
including the OR rule, leap years (2028…2064, correctly skipping 2100), weekend
skipping, impossible dates, and both DST transitions.

## Deployment

```sh
staticer deploy --dir site --domain cron.baileys.dev --expires never
```
