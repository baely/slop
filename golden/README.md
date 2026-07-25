# golden

When the light is good. Sun times for a place and a date, computed offline.

Enter a latitude, longitude and date — or pick a preset, or use the browser's
geolocation — and golden returns sunrise, sunset, solar noon, the three
twilights, golden hour and blue hour, day length, and how much the day changed
since yesterday. A 24-hour bar shows the whole solar day banded by light phase.

Pairs with the exposure calculator at [stops.baileys.dev](https://stops.baileys.dev).

## How it works

The NOAA solar position algorithm, written from scratch in `site/app.js`. No
libraries, no APIs, no network calls at runtime — the only external request the
page makes is the Google Fonts stylesheet.

Given a Julian Day the code derives the sun's geometric mean longitude, mean
anomaly, equation of centre, apparent longitude and corrected obliquity, and
from those the declination and the equation of time. Solar noon comes from
`720 − 4·longitude − eqTime` minutes; each phase boundary is then solved from
the hour angle, iterated to convergence, with a bisection fallback for the
near-tangent cases at high latitude where the hour-angle iteration can overshoot
its half of the day.

### Definitions

| Phase | Sun altitude |
|---|---|
| Daylight | above +6° |
| Golden hour | −4° to +6° |
| Blue hour | −6° to −4° |
| Nautical twilight | −12° to −6° |
| Astronomical twilight | −18° to −12° |
| Night | below −18° |

Sunrise and sunset use the standard −0.833° threshold (upper limb, mean
refraction), which falls *inside* the golden band — so they appear as hairlines
on the bar rather than as band edges. Civil twilight (−6° to −0.833°) likewise
cuts across the blue and golden bands, so it gets its own table rather than a
band of its own.

### The day is a solar day

A date at a location means that location's solar day: solar midnight to solar
midnight, centred on solar noon. The day is anchored by longitude, not by UTC,
so `2026-06-21` at Reykjavík is Reykjavík's day and not London's. That is also
why the timeline bar spans solar midnight to solar midnight and its axis ticks
land off the round hours — every event is guaranteed to be inside the bar.

### Timezones

Times are shown on **your clock** by default — the browser's timezone, with the
UTC offset and zone name printed explicitly, DST-correct for the date chosen.
There is no offline timezone database here, so golden does not claim to know
what civil time the coordinates keep. When the longitude implies a mean solar
offset more than 45 minutes from your clock, the page says so plainly and
offers a **Mean Solar** time base instead, which puts solar noon near 12:00 by
construction. Events landing on a different calendar day than solar noon carry a
`+1` / `−1` marker.

### Polar cases

At high latitude the sun may never rise or never set, and there are transition
days on which exactly one of the two crossings happens. Those are reported as
`Sun does not rise.` / `Sun does not set.` rather than as `NaN`, and day length
is defined as the time spent above −0.833° within the solar day — 24h under a
polar day, 0 under a polar night, and a genuine partial figure on a transition
day. Reykjavík is a preset for exactly this reason; note that on the June
solstice its sun does still set (it bottoms out at −2.4°), giving a 21h 09m day.
Svalbard is where the true polar cases live.

## Verified

Checked against published tables with node before the UI existed, and again
through a DOM shim afterwards:

| Case | golden | published |
|---|---|---|
| Melbourne 2026-06-21 | 07:35:37 → 17:08:08, 9h 32m 32s | 07:35 → 17:08, 9h 32m |
| Melbourne 2026-12-21 | 05:54:20 → 20:41:42, 14h 47m 22s | 05:54 → 20:41, 14h 47m |
| London 2026-03-20 | 06:03:22 → 18:13:31, 12h 10m 09s | 06:02 → 18:14, ~12h 10m |
| Reykjavík 2026-06-21 | 02:55:08 → 00:04:01 (+1), 21h 08m 53s | 02:55 → 00:04, 21h 09m |
| Svalbard 2026-06-21 | `Sun does not set.` | polar day |
| Svalbard 2026-12-21 | `Sun does not rise.` | polar night |

Equation of time extremes land at −14.23 min on 11 Feb and +16.49 min on 3 Nov
(published: −14.2 and +16.4); declination hits ±23.44° at the solstices. Solved
crossings sit within 6.2 × 10⁻⁴ degrees of their target altitude across a
lat/date grid, and agree with a brute-force 1-second altitude scan to under a
second. The phase bands were checked to tile exactly 24h with no gaps or
overlaps across 2,920 location-days, and the whole render path was exercised
across 9 timezones — including +05:30, +12:45, +14:00 and −03:30 — with no
`NaN` reaching the DOM.

## Limits

- Accuracy is the NOAA algorithm's: about a minute against published tables,
  degrading above 60° latitude where the sun crosses the horizon at a shallow
  angle. Seconds are shown because they are the model's precision, not the sky's.
- No terrain, no horizon elevation, no atmospheric conditions. A hill to your
  east moves sunrise; this does not know about it.
- No civil timezone lookup, by design — see above.
- Refraction is the standard mean value baked into −0.833°, not computed from
  temperature and pressure.

## Deployment

Static; no build step.

```sh
staticer deploy --dir site --domain golden.baileys.dev --expires never
```

Live at <https://golden.baileys.dev>.
