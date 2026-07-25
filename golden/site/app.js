'use strict';

/* ============================================================================
   NOAA solar position, implemented from scratch. No libraries, no network.
   Verified against published tables for Melbourne, London, Tokyo, New York,
   Reykjavik and Svalbard; see README.
   ========================================================================= */

const RAD = Math.PI / 180;
const sin = (d) => Math.sin(d * RAD);
const cos = (d) => Math.cos(d * RAD);
const mod360 = (d) => ((d % 360) + 360) % 360;

// Gregorian calendar date -> Julian Day at 00:00 UTC (Meeus 7.1).
function julianDay(y, m, d) {
  if (m <= 2) { y -= 1; m += 12; }
  const A = Math.floor(y / 100);
  const B = 2 - A + Math.floor(A / 4);
  return Math.floor(365.25 * (y + 4716)) + Math.floor(30.6001 * (m + 1)) + d + B - 1524.5;
}

const jdToDate = (jd) => new Date((jd - 2440587.5) * 86400000);
const dateToJd = (dt) => dt.getTime() / 86400000 + 2440587.5;

// Sun declination (deg) and equation of time (minutes) at a Julian Day (UT).
function sunPos(jd) {
  const T = (jd - 2451545.0) / 36525;
  const L0 = mod360(280.46646 + T * (36000.76983 + T * 0.0003032));      // geometric mean longitude
  const M = 357.52911 + T * (35999.05029 - 0.0001537 * T);               // geometric mean anomaly
  const e = 0.016708634 - T * (0.000042037 + 0.0000001267 * T);          // orbital eccentricity
  const C = sin(M) * (1.914602 - T * (0.004817 + 0.000014 * T))          // equation of centre
          + sin(2 * M) * (0.019993 - 0.000101 * T)
          + sin(3 * M) * 0.000289;
  // omega is the Moon's ascending node; it drives the nutation term on both the
  // apparent longitude and the obliquity.
  const omega = 125.04 - 1934.136 * T;
  const lambda = L0 + C - 0.00569 - 0.00478 * sin(omega);                // apparent longitude
  const seconds = 21.448 - T * (46.8150 + T * (0.00059 - T * 0.001813));
  const eps = 23 + (26 + seconds / 60) / 60 + 0.00256 * cos(omega);      // corrected obliquity
  const decl = Math.asin(sin(eps) * sin(lambda)) / RAD;
  const y = Math.tan(eps / 2 * RAD) ** 2;
  const eqTime = 4 / RAD * (
      y * sin(2 * L0)
    - 2 * e * sin(M)
    + 4 * e * y * sin(M) * cos(2 * L0)
    - 0.5 * y * y * sin(4 * L0)
    - 1.25 * e * e * sin(2 * M)
  );
  return { decl: decl, eqTime: eqTime };
}

// Geometric sun altitude in degrees. Refraction is not modelled here; it is
// carried by the -0.833 threshold instead, per the NOAA convention.
function altitude(jd, lat, lon) {
  const p = sunPos(jd);
  const minutesUTC = (jd + 0.5 - Math.floor(jd + 0.5)) * 1440;
  const trueSolar = minutesUTC + p.eqTime + 4 * lon;   // true solar time, minutes
  const ha = trueSolar / 4 - 180;                      // hour angle, degrees
  return Math.asin(sin(lat) * sin(p.decl) + cos(lat) * cos(p.decl) * cos(ha)) / RAD;
}

// Solar noon for the solar day labelled by jd0 (00:00 UTC of a calendar date).
// The -4*lon term anchors the day to the location's mean solar time rather than
// to UTC, so "2026-06-21 at Reykjavik" means that place's day, not London's.
function solarNoon(jd0, lon) {
  let jd = jd0 + (720 - 4 * lon) / 1440;
  for (let i = 0; i < 3; i++) jd = jd0 + (720 - 4 * lon - sunPos(jd).eqTime) / 1440;
  return jd;
}

/* Crossing of altitude h0 on one side of solar noon. dir = -1 morning, +1 evening.
   Existence is decided from the window edges, not from acos() going out of range:
   on the transition days at high latitude exactly one of the two crossings exists
   and the acos test cannot tell which one. */
function crossing(jd0, noon, lat, lon, h0, dir) {
  const edge = noon + dir * 0.5;
  if (altitude(noon, lat, lon) <= h0) return { none: 'below' };  // never gets that high
  if (altitude(edge, lat, lon) >= h0) return { none: 'above' };  // never gets that low
  let jd = noon + dir * 0.25;
  for (let i = 0; i < 4; i++) {
    const p = sunPos(jd);
    const cosHA = (sin(h0) - sin(lat) * sin(p.decl)) / (cos(lat) * cos(p.decl));
    if (cosHA < -1 || cosHA > 1) break;
    jd = jd0 + (720 - 4 * lon - p.eqTime + dir * 4 * (Math.acos(cosHA) / RAD)) / 1440;
  }
  // Safety net for the near-tangent case, where the hour-angle iteration can land
  // outside its half of the day. Bisection cannot fail here: altitude is monotonic
  // in hour angle between solar noon and the window edge.
  if (!(jd > Math.min(noon, edge) && jd < Math.max(noon, edge)) ||
      Math.abs(altitude(jd, lat, lon) - h0) > 0.005) {
    let lo = noon, hi = edge;                        // alt(lo) > h0 > alt(hi)
    for (let i = 0; i < 48; i++) {
      const mid = (lo + hi) / 2;
      if (altitude(mid, lat, lon) > h0) lo = mid; else hi = mid;
    }
    jd = (lo + hi) / 2;
  }
  return { jd: jd };
}

/* Light phases, darkest first; each band runs from its own altitude up to the
   next one's. Sunrise (-0.833, upper limb plus mean refraction) sits inside the
   golden band, so it is a marker on the ramp rather than a band edge. */
const BANDS = [
  { key: 'night',    name: 'Night',                 short: 'Night',        lo: -90 },
  { key: 'astro',    name: 'Astronomical twilight', short: 'Astronomical', lo: -18 },
  { key: 'nautical', name: 'Nautical twilight',     short: 'Nautical',     lo: -12 },
  { key: 'blue',     name: 'Blue hour',             short: 'Blue hour',    lo: -6  },
  { key: 'golden',   name: 'Golden hour',           short: 'Golden hour',  lo: -4  },
  { key: 'day',      name: 'Daylight',              short: 'Daylight',     lo: 6   },
];

const SUN_ALT = -0.833;
const EDGE_ALTS = [-18, -12, -6, -4, 6];
const EDGE_NAME = { '-18': 'astro', '-12': 'nautical', '-6': 'civil', '-4': 'goldenLo', '6': 'goldenHi' };

function bandAt(alt) {
  let b = BANDS[0];
  for (let i = 0; i < BANDS.length; i++) if (alt >= BANDS[i].lo) b = BANDS[i];
  return b;
}

/* Everything for one solar day at one place. Times come back as Julian Days —
   absolute UT instants — and the UI formats them into a display time base. */
function computeDay(jd0, lat, lon) {
  const noon = solarNoon(jd0, lon);
  const start = noon - 0.5, end = noon + 0.5;   // solar midnight to solar midnight
  const at = (jd) => altitude(jd, lat, lon);

  const ev = {};
  for (let i = 0; i < EDGE_ALTS.length; i++) {
    const h = EDGE_ALTS[i], n = EDGE_NAME[String(h)];
    ev[n + 'Am'] = crossing(jd0, noon, lat, lon, h, -1);
    ev[n + 'Pm'] = crossing(jd0, noon, lat, lon, h, +1);
  }
  ev.sunrise = crossing(jd0, noon, lat, lon, SUN_ALT, -1);
  ev.sunset = crossing(jd0, noon, lat, lon, SUN_ALT, +1);

  // Timeline segments. Every band edge that actually occurs is a cut; the band
  // for a segment is read off the altitude at its midpoint, which stays correct
  // however many of the crossings exist. Adjacent equal bands merge, which is
  // what collapses a day with no true daylight into one continuous golden window.
  const cuts = [start, end];
  for (const k in ev) {
    if (k === 'sunrise' || k === 'sunset') continue;
    if (ev[k].jd !== undefined) cuts.push(ev[k].jd);
  }
  cuts.sort((a, b) => a - b);
  const bands = [];
  for (let i = 0; i < cuts.length - 1; i++) {
    if (cuts[i + 1] - cuts[i] < 1e-9) continue;
    const b = bandAt(at((cuts[i] + cuts[i + 1]) / 2));
    const last = bands[bands.length - 1];
    if (last && last.key === b.key) last.end = cuts[i + 1];
    else bands.push({ key: b.key, name: b.name, start: cuts[i], end: cuts[i + 1] });
  }

  // Day length = time above the sunrise altitude within this solar day. Defined
  // that way it degrades correctly: 24h under a polar day, 0 under a polar night,
  // and a partial figure on the transition days that have only one crossing.
  const upAtNoon = at(noon) > SUN_ALT;
  let dayStart = null, dayEnd = null, daySec = 0;
  if (upAtNoon) {
    dayStart = ev.sunrise.jd !== undefined ? ev.sunrise.jd : start;
    dayEnd = ev.sunset.jd !== undefined ? ev.sunset.jd : end;
    daySec = (dayEnd - dayStart) * 86400;
  }

  return {
    jd0: jd0, noon: noon, start: start, end: end, ev: ev, bands: bands,
    dayStart: dayStart, dayEnd: dayEnd, daySec: daySec,
    golden: bands.filter((b) => b.key === 'golden'),
    blue: bands.filter((b) => b.key === 'blue'),
    maxAlt: at(noon),
    minAlt: Math.min(at(start), at(end)),
    polarDay: ev.sunrise.jd === undefined && ev.sunset.jd === undefined && upAtNoon,
    polarNight: !upAtNoon,
  };
}

/* ============================================================================
   State
   ========================================================================= */

const PRESETS = [
  { label: 'Melbourne', lat: -37.8136, lon: 144.9631 },
  { label: 'Sydney',    lat: -33.8688, lon: 151.2093 },
  { label: 'Tokyo',     lat: 35.6762,  lon: 139.6503 },
  { label: 'London',    lat: 51.5072,  lon: -0.1276 },
  { label: 'New York',  lat: 40.7128,  lon: -74.0060 },
  { label: 'Reykjavík', lat: 64.1466,  lon: -21.9426 },
];

const STORE = 'golden.v1';

const state = {
  lat: -37.8136,
  lon: 144.9631,
  label: 'Melbourne',
  date: null,          // { y, m, d }
  tzMode: 'clock',     // 'clock' | 'solar'
};

const $ = (id) => document.getElementById(id);
const el = {
  presets: $('presets'), lat: $('lat'), lon: $('lon'), date: $('date'),
  geo: $('geo'), today: $('today'), prev: $('prev'), next: $('next'),
  status: $('status'), now: $('now'),
  goldenAm: $('goldenAm'), goldenAmSub: $('goldenAmSub'),
  goldenPm: $('goldenPm'), goldenPmSub: $('goldenPmSub'), goldenNote: $('goldenNote'),
  sunrise: $('sunrise'), sunriseSub: $('sunriseSub'),
  noon: $('noon'), noonSub: $('noonSub'),
  sunset: $('sunset'), sunsetSub: $('sunsetSub'),
  daylen: $('daylen'), daylenSub: $('daylenSub'),
  barSpan: $('barSpan'), bar: $('bar'), ticks: $('ticks'), legend: $('legend'),
  phases: $('phases'), twilight: $('twilight'),
  tzClock: $('tzClock'), tzSolar: $('tzSolar'), tzNote: $('tzNote'), tzWarn: $('tzWarn'),
};

let today = null;      // computed day for the selected date
let yesterday = null;

function load() {
  try {
    const raw = localStorage.getItem(STORE);
    if (!raw) return;
    const s = JSON.parse(raw);
    if (isFinite(s.lat) && isFinite(s.lon) && Math.abs(s.lat) <= 90 && Math.abs(s.lon) <= 180) {
      state.lat = s.lat; state.lon = s.lon;
      state.label = typeof s.label === 'string' ? s.label : '';
    }
    if (s.tzMode === 'solar' || s.tzMode === 'clock') state.tzMode = s.tzMode;
  } catch (e) { /* private mode, or corrupt value — defaults are fine */ }
}

function save() {
  try {
    localStorage.setItem(STORE, JSON.stringify({
      lat: state.lat, lon: state.lon, label: state.label, tzMode: state.tzMode,
    }));
  } catch (e) { /* nothing to do; the app works without persistence */ }
}

/* ============================================================================
   Formatting
   ========================================================================= */

const pad = (n) => String(n).padStart(2, '0');
const NEG = '−';   // proper minus sign, for degrees in prose

// Longitude -> mean solar offset from UTC, in milliseconds.
const solarOffsetMs = () => (state.lon / 15) * 3600000;

/* Display-frame breakdown of an instant. Everything downstream works from Date
   objects rather than round-tripping through Julian Days, because a JD -> Date
   round trip can land a whole-hour boundary a fraction of a millisecond early
   and knock the axis ticks onto :59. */
function partsOf(dt) {
  if (state.tzMode === 'solar') {
    const s = new Date(dt.getTime() + solarOffsetMs());
    return { h: s.getUTCHours(), mi: s.getUTCMinutes(), s: s.getUTCSeconds(),
             key: s.getUTCFullYear() + '-' + pad(s.getUTCMonth() + 1) + '-' + pad(s.getUTCDate()) };
  }
  return { h: dt.getHours(), mi: dt.getMinutes(), s: dt.getSeconds(),
           key: dt.getFullYear() + '-' + pad(dt.getMonth() + 1) + '-' + pad(dt.getDate()) };
}

const parts = (jd) => partsOf(jdToDate(jd));
const clockOf = (dt) => { const p = partsOf(dt); return pad(p.h) + ':' + pad(p.mi) + ':' + pad(p.s); };
const clock = (jd) => clockOf(jdToDate(jd));

// Suffix marking an event that lands on a different calendar day than solar noon.
function dayShift(jd, refKey) {
  const k = parts(jd).key;
  if (k === refKey) return '';
  const diff = Math.round((Date.parse(k) - Date.parse(refKey)) / 86400000);
  return diff > 0 ? ' +' + diff : ' ' + NEG + Math.abs(diff);
}

function timeCell(jd, refKey) {
  const shift = dayShift(jd, refKey);
  return clock(jd) + (shift ? '<span class="off">' + shift + '</span>' : '');
}

function dur(sec) {
  sec = Math.max(0, Math.round(sec));
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  return (h ? h + 'h ' + pad(m) + 'm ' : m + 'm ') + pad(s) + 's';
}

// Day length always carries the hours field, so a polar night reads 0h 00m 00s
// against its neighbours rather than collapsing to 0m 00s.
function durH(sec) {
  sec = Math.max(0, Math.round(sec));
  return Math.floor(sec / 3600) + 'h ' + pad(Math.floor((sec % 3600) / 60)) + 'm ' + pad(sec % 60) + 's';
}

// Altitudes in prose use the typographic minus, matching the thresholds elsewhere.
const degStr = (v) => {
  const a = Math.abs(v).toFixed(1);
  return (v < 0 && a !== '0.0' ? NEG : '') + a + '°';   // avoid a signed zero
};

function delta(sec) {
  const sign = sec < 0 ? NEG : '+';
  const a = Math.abs(Math.round(sec));
  const h = Math.floor(a / 3600), m = Math.floor((a % 3600) / 60), s = a % 60;
  return sign + (h ? h + 'h ' + pad(m) + 'm ' : m + 'm ') + pad(s) + 's';
}

function offsetLabel(minutes) {
  const sign = minutes < 0 ? '-' : '+';
  const a = Math.abs(minutes);
  return 'UTC' + sign + pad(Math.floor(a / 60)) + ':' + pad(Math.round(a % 60));
}

// Browser UTC offset in minutes at a given instant (DST-correct for that date).
const browserOffsetAt = (jd) => -jdToDate(jd).getTimezoneOffset();

function browserZoneName(jd) {
  try {
    const f = new Intl.DateTimeFormat(undefined, { timeZoneName: 'short' });
    const p = f.formatToParts(jdToDate(jd)).find((x) => x.type === 'timeZoneName');
    return p ? p.value : '';
  } catch (e) { return ''; }
}

const fmtLat = (v) => Math.abs(v).toFixed(4) + '°' + (v < 0 ? 'S' : 'N');
const fmtLon = (v) => Math.abs(v).toFixed(4) + '°' + (v < 0 ? 'W' : 'E');

/* ============================================================================
   Render
   ========================================================================= */

const bandColorCache = {};
function bandColor(key) {
  if (!(key in bandColorCache)) {
    bandColorCache[key] = getComputedStyle(document.documentElement)
      .getPropertyValue('--band-' + key).trim();
  }
  return bandColorCache[key];
}

function render() {
  const jd0 = julianDay(state.date.y, state.date.m, state.date.d);
  today = computeDay(jd0, state.lat, state.lon);
  yesterday = computeDay(jd0 - 1, state.lat, state.lon);
  const refKey = parts(today.noon).key;

  renderHero(refKey);
  renderStats(refKey);
  renderBar(refKey);
  renderPhases(refKey);
  renderTwilight(refKey);
  renderTimeBase();
  tick();
}

function renderHero(refKey) {
  const g = today.golden;
  const b = today.blue;
  const cells = [
    { v: el.goldenAm, s: el.goldenAmSub, win: g[0], blue: b[0] },
    { v: el.goldenPm, s: el.goldenPmSub, win: g.length > 1 ? g[1] : null, blue: b.length > 1 ? b[1] : null },
  ];
  for (const c of cells) {
    if (c.win) {
      c.v.innerHTML = timeCell(c.win.start, refKey) + ' → ' + timeCell(c.win.end, refKey);
      const d = dur((c.win.end - c.win.start) * 86400);
      c.s.innerHTML = '<span class="num">' + d + '</span> · blue hour ' +
        (c.blue ? '<span class="num">' + clock(c.blue.start) + ' → ' + clock(c.blue.end) + '</span>' : 'none');
    } else {
      c.v.textContent = '—';
      // One golden band means it spans solar noon, so it is already in the other
      // cell; zero means the sun sat outside -4..+6 for the whole day.
      c.s.textContent = g.length === 1 ? 'One continuous window, listed under Morning.'
        : today.polarDay ? 'Sun stays above 6° all day.'
        : 'Sun stays below ' + NEG + '4° all day.';
    }
  }

  let note;
  if (today.polarDay) {
    note = 'Sun does not set. It never drops below 6°, so there is no golden hour on this date — the light stays flat all day.';
  } else if (today.polarNight) {
    note = 'Sun does not rise. It peaks at ' + degStr(today.maxAlt) + ', below the horizon.';
  } else if (g.length === 1) {
    note = 'One continuous window: the sun peaks at ' + degStr(today.maxAlt) + ' and never clears 6°, so it stays inside the golden band from first to last light.';
  } else if (g.length === 0) {
    note = 'No golden hour. The sun never reaches ' + NEG + '4°.';
  } else {
    note = 'Sun between ' + NEG + '4° and 6°. Peaks at ' + degStr(today.maxAlt) + ' at solar noon.';
  }
  el.goldenNote.textContent = note;
}

function renderStats(refKey) {
  if (today.ev.sunrise.jd !== undefined) {
    el.sunrise.innerHTML = timeCell(today.ev.sunrise.jd, refKey);
    el.sunriseSub.textContent = 'Upper limb clears the horizon.';
  } else {
    el.sunrise.textContent = today.polarNight ? 'Sun does not rise.' : 'Already up.';
    el.sunriseSub.textContent = today.polarNight
      ? 'Peaks at ' + degStr(today.maxAlt) + '.'
      : 'Above the horizon at the start of this solar day.';
  }

  el.noon.innerHTML = timeCell(today.noon, refKey);
  el.noonSub.textContent = 'Sun at ' + degStr(today.maxAlt) + ', due ' + (state.lat >= 0 ? 'south' : 'north') + '.';

  if (today.ev.sunset.jd !== undefined) {
    el.sunset.innerHTML = timeCell(today.ev.sunset.jd, refKey);
    el.sunsetSub.textContent = 'Upper limb touches the horizon.';
  } else {
    el.sunset.textContent = today.polarNight ? 'Sun does not rise.' : 'Sun does not set.';
    el.sunsetSub.textContent = today.polarNight
      ? 'Below the horizon all day.'
      : 'Still up at the end of this solar day.';
  }

  el.daylen.textContent = durH(today.daySec);
  el.daylenSub.textContent = delta(today.daySec - yesterday.daySec) + ' vs yesterday';
}

function renderBar(refKey) {
  const span = today.end - today.start;
  el.bar.textContent = '';

  const labelled = [];
  for (const b of today.bands) {
    const d = document.createElement('div');
    d.className = 'seg-band';
    d.dataset.band = b.key;
    d.style.left = (b.start - today.start) / span * 100 + '%';
    d.style.width = (b.end - b.start) / span * 100 + '%';
    d.title = b.name + '  ' + clock(b.start) + ' → ' + clock(b.end) + '  (' + dur((b.end - b.start) * 86400) + ')';
    d.textContent = (BANDS.find((x) => x.key === b.key) || b).short;
    el.bar.appendChild(d);
    labelled.push(d);
  }
  // Drop labels that would be clipped mid-word. Measured rather than guessed from
  // the percentage, because band widths and viewport widths both vary wildly.
  for (const d of labelled) if (d.scrollWidth > d.clientWidth + 1) d.textContent = '';

  // Sunrise and sunset sit inside the golden band, so mark them as hairlines.
  for (const k of ['sunrise', 'sunset']) {
    const e = today.ev[k];
    if (e.jd === undefined) continue;
    const m = document.createElement('div');
    m.className = 'mark';
    m.style.left = (e.jd - today.start) / span * 100 + '%';
    m.title = (k === 'sunrise' ? 'Sunrise ' : 'Sunset ') + clock(e.jd);
    el.bar.appendChild(m);
  }

  const nowMark = document.createElement('div');
  nowMark.className = 'mark-now';
  nowMark.id = 'nowMark';
  nowMark.hidden = true;
  el.bar.appendChild(nowMark);

  // Axis: every clock hour inside the window that lands on the step, labelled in
  // the display time base. The window is a solar day, so its edges rarely fall on
  // a round hour — the ticks are positioned proportionally rather than evenly.
  const step = window.matchMedia('(min-width: 620px)').matches ? 3 : 6;
  const startMs = jdToDate(today.start).getTime();
  const endMs = jdToDate(today.end).getTime();
  el.ticks.textContent = '';

  let cursorMs;
  if (state.tzMode === 'solar') {
    // Display frame is a fixed UTC offset, so hour boundaries are exact.
    const off = solarOffsetMs();
    cursorMs = Math.ceil((startMs + off) / 3600000) * 3600000 - off;
  } else {
    const d = new Date(startMs);
    d.setMinutes(0, 0, 0);
    cursorMs = d.getTime() + 3600000;
  }
  for (let i = 0; i < 30 && cursorMs <= endMs; i++, cursorMs += 3600000) {
    const p = partsOf(new Date(cursorMs));
    if (p.h % step !== 0 || p.mi !== 0 || p.s !== 0) continue;   // skips DST half-hours
    const pct = (cursorMs - startMs) / (endMs - startMs) * 100;
    const t = document.createElement('div');
    t.className = 'tick ' + (pct < 4 ? 'start' : pct > 96 ? 'end' : 'mid');
    t.style.left = pct + '%';
    t.textContent = pad(p.h) + ':' + pad(p.mi);
    el.ticks.appendChild(t);
  }

  el.barSpan.innerHTML = 'Solar midnight to solar midnight, <span class="num">' +
    timeCell(today.start, refKey) + '</span> → <span class="num">' + timeCell(today.end, refKey) + '</span>.';

  // Legend: one entry per band present, carrying its total for the day.
  el.legend.textContent = '';
  for (const def of BANDS) {
    const windows = today.bands.filter((b) => b.key === def.key);
    if (!windows.length) continue;
    const total = windows.reduce((a, b) => a + (b.end - b.start), 0) * 86400;
    const item = document.createElement('div');
    item.className = 'legend-item';
    const sw = document.createElement('span');
    sw.className = 'legend-sw';
    sw.style.background = bandColor(def.key);
    const txt = document.createElement('span');
    txt.innerHTML = def.name + ' <span class="num">' + dur(total) + '</span>';
    item.appendChild(sw);
    item.appendChild(txt);
    el.legend.appendChild(item);
  }
}

function renderPhases(refKey) {
  el.phases.textContent = '';
  for (const b of today.bands) {
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td><span class="row-sw" style="background:' + bandColor(b.key) + '"></span>' + b.name + '</td>' +
      '<td class="num">' + timeCell(b.start, refKey) + '</td>' +
      '<td class="num">' + timeCell(b.end, refKey) + '</td>' +
      '<td class="num">' + dur((b.end - b.start) * 86400) + '</td>';
    el.phases.appendChild(tr);
  }
}

// Altitude each solved crossing belongs to, so a missing one can name its own
// threshold rather than the row's.
const ALT_OF = {
  astroAm: -18, astroPm: -18, nauticalAm: -12, nauticalPm: -12,
  civilAm: -6, civilPm: -6, goldenLoAm: -4, goldenLoPm: -4,
  goldenHiAm: 6, goldenHiPm: 6, sunrise: SUN_ALT, sunset: SUN_ALT,
};
const degLabel = (v) => (v < 0 ? NEG : '') + Math.abs(v) + '°';

function renderTwilight(refKey) {
  // The classical definitions cut across the bands: civil runs from -6 to
  // sunrise, so it holds all of blue hour and the lower part of golden.
  const rows = [
    ['Civil', -6, SUN_ALT, 'civilAm', 'sunrise', 'sunset', 'civilPm'],
    ['Nautical', -12, -6, 'nauticalAm', 'civilAm', 'civilPm', 'nauticalPm'],
    ['Astronomical', -18, -12, 'astroAm', 'nauticalAm', 'nauticalPm', 'astroPm'],
  ];
  const span = (aKey, bKey) => {
    const a = today.ev[aKey], b = today.ev[bKey];
    if (a.jd !== undefined && b.jd !== undefined)
      return timeCell(a.jd, refKey) + ' → ' + timeCell(b.jd, refKey);
    const missKey = a.jd === undefined ? aKey : bKey;
    const miss = today.ev[missKey];
    return '<span class="off">sun stays ' + (miss.none === 'above' ? 'above ' : 'below ') +
      degLabel(ALT_OF[missKey]) + '</span>';
  };
  el.twilight.textContent = '';
  for (const r of rows) {
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td>' + r[0] + '</td>' +
      '<td class="num">' + degLabel(r[1]) + ' to ' + degLabel(r[2]) + '</td>' +
      '<td class="num">' + span(r[3], r[4]) + '</td>' +
      '<td class="num">' + span(r[5], r[6]) + '</td>';
    el.twilight.appendChild(tr);
  }
}

function renderTimeBase() {
  el.tzClock.setAttribute('aria-pressed', String(state.tzMode === 'clock'));
  el.tzSolar.setAttribute('aria-pressed', String(state.tzMode === 'solar'));

  const browserMin = browserOffsetAt(today.noon);
  const solarMin = (state.lon / 15) * 60;
  const zone = browserZoneName(today.noon);
  let iana = '';
  try { iana = Intl.DateTimeFormat().resolvedOptions().timeZone || ''; } catch (e) { /* ignore */ }

  if (state.tzMode === 'clock') {
    el.tzNote.innerHTML = 'Times shown on <strong>your clock</strong>: <span class="num">' +
      offsetLabel(browserMin) + '</span>' + (zone ? ' (' + zone + ')' : '') +
      (iana ? ', from your browser (<span class="num">' + iana + '</span>)' : '') + '.';
  } else {
    el.tzNote.innerHTML = 'Times shown in <strong>mean solar time at ' + fmtLon(state.lon) +
      '</strong>: <span class="num">' + offsetLabel(solarMin) +
      '</span>. Solar noon lands near 12:00 by construction; this is not a civil timezone.';
  }

  const gap = solarMin - browserMin;
  if (Math.abs(gap) >= 45) {
    const dir = gap > 0 ? 'ahead of' : 'behind';
    el.tzWarn.innerHTML = fmtLon(state.lon) + ' puts mean solar time <span class="num">' +
      dur(Math.abs(gap) * 60).replace(/ \d\ds$/, '') + '</span> ' + dir +
      ' your clock, so this location almost certainly keeps a different civil timezone. ' +
      'Offline there is no way to know which one, and nothing here has been adjusted for it' +
      (state.tzMode === 'clock' ? ' — switch to Mean Solar for times as the sun sees them.' : '.');
  } else {
    el.tzWarn.innerHTML = 'Mean solar time at this longitude is within <span class="num">' +
      Math.round(Math.abs(gap)) + 'm</span> of your clock, so the two agree closely. ' +
      'Civil timezones and daylight saving are still not modelled.';
  }
}

/* Live: the now marker, the running clock and the countdown to good light.
   Content updating on a timer, not decoration. */
function tick() {
  if (!today) return;
  const now = new Date();
  const jdNow = dateToJd(now);
  const mark = document.getElementById('nowMark');
  const inWindow = jdNow >= today.start && jdNow <= today.end;

  if (mark) {
    mark.hidden = !inWindow;
    if (inWindow) mark.style.left = (jdNow - today.start) / (today.end - today.start) * 100 + '%';
  }

  if (!inWindow) {
    el.now.textContent = 'Now ' + clockOf(now) + ' — outside the solar day shown.';
    return;
  }

  const alt = altitude(jdNow, state.lat, state.lon);
  const band = bandAt(alt);
  let tail = '';
  const current = today.golden.find((b) => jdNow >= b.start && jdNow < b.end);
  if (current) {
    tail = ' · <strong>golden hour now</strong>, ' + dur((current.end - jdNow) * 86400) + ' left';
  } else {
    const next = today.golden.find((b) => b.start > jdNow);
    if (next) tail = ' · golden hour in <strong>' + dur((next.start - jdNow) * 86400) + '</strong>';
    else if (today.golden.length) tail = ' · no golden hour left today';
  }
  el.now.innerHTML = 'Now ' + clockOf(now) + ' · ' + band.name.toLowerCase() +
    ' · sun ' + degStr(alt) + tail;
}

/* ============================================================================
   Input
   ========================================================================= */

function setStatus(msg, isError) {
  el.status.textContent = msg;
  el.status.classList.toggle('err', !!isError);
}

function syncPresets() {
  const kids = el.presets.children;
  for (let i = 0; i < kids.length; i++) {
    const p = PRESETS[i];
    const match = Math.abs(p.lat - state.lat) < 1e-4 && Math.abs(p.lon - state.lon) < 1e-4;
    kids[i].setAttribute('aria-pressed', String(match));
  }
}

function parseCoord(raw) {
  const v = parseFloat(String(raw).trim().replace(',', '.'));
  return isFinite(v) ? v : null;
}

function readInputs() {
  const lat = parseCoord(el.lat.value);
  const lon = parseCoord(el.lon.value);
  const dateOk = /^\d{4}-\d{2}-\d{2}$/.test(el.date.value);

  el.lat.classList.toggle('bad', lat === null || Math.abs(lat) > 90);
  el.lon.classList.toggle('bad', lon === null || Math.abs(lon) > 180);
  el.date.classList.toggle('bad', !dateOk);

  if (lat === null) return setStatus('Latitude is not a number. Last valid position kept.', true);
  if (Math.abs(lat) > 90) return setStatus('Latitude out of range: must be between ' + NEG + '90 and 90.', true);
  if (lon === null) return setStatus('Longitude is not a number. Last valid position kept.', true);
  if (Math.abs(lon) > 180) return setStatus('Longitude out of range: must be between ' + NEG + '180 and 180.', true);
  if (!dateOk) return setStatus('No date selected. Last valid date kept.', true);

  const bits = el.date.value.split('-').map(Number);
  state.lat = lat; state.lon = lon;
  state.date = { y: bits[0], m: bits[1], d: bits[2] };
  const preset = PRESETS.find((p) => Math.abs(p.lat - lat) < 1e-4 && Math.abs(p.lon - lon) < 1e-4);
  state.label = preset ? preset.label : '';

  syncPresets();
  save();
  render();
  setStatus((state.label ? state.label + ' · ' : '') + fmtLat(lat) + ' ' + fmtLon(lon) + ' · ' + el.date.value + '.');
}

function setDate(dt) {
  el.date.value = dt.getFullYear() + '-' + pad(dt.getMonth() + 1) + '-' + pad(dt.getDate());
}

function shiftDate(days) {
  const bits = el.date.value.split('-').map(Number);
  if (bits.length !== 3 || !bits.every(isFinite)) return;
  const dt = new Date(bits[0], bits[1] - 1, bits[2] + days);
  setDate(dt);
  readInputs();
}

function useGeolocation() {
  if (!navigator.geolocation) {
    setStatus('Geolocation is not available in this browser. Enter coordinates or pick a preset.', true);
    return;
  }
  if (!window.isSecureContext) {
    setStatus('Geolocation needs a secure context (https). Enter coordinates or pick a preset.', true);
    return;
  }
  el.geo.disabled = true;
  setStatus('Asking the browser for your position…');
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      el.geo.disabled = false;
      el.lat.value = pos.coords.latitude.toFixed(4);
      el.lon.value = pos.coords.longitude.toFixed(4);
      readInputs();
      const acc = pos.coords.accuracy;
      setStatus(fmtLat(pos.coords.latitude) + ' ' + fmtLon(pos.coords.longitude) +
        (isFinite(acc) ? ' · ±' + Math.round(acc) + 'm' : '') + ' · ' + el.date.value + '.');
    },
    (err) => {
      el.geo.disabled = false;
      const msg = err.code === 1 ? 'Location denied. Enter coordinates or pick a preset.'
        : err.code === 2 ? 'Position unavailable — the device could not get a fix. Enter coordinates or pick a preset.'
        : err.code === 3 ? 'Location request timed out. Enter coordinates or pick a preset.'
        : 'Location failed. Enter coordinates or pick a preset.';
      setStatus(msg, true);
    },
    { enableHighAccuracy: false, timeout: 10000, maximumAge: 300000 }
  );
}

/* ============================================================================
   Boot
   ========================================================================= */

function init() {
  load();

  for (const p of PRESETS) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn preset';
    b.textContent = p.label;
    b.setAttribute('aria-pressed', 'false');
    b.addEventListener('click', () => {
      el.lat.value = p.lat.toFixed(4);
      el.lon.value = p.lon.toFixed(4);
      readInputs();
    });
    el.presets.appendChild(b);
  }

  el.lat.value = state.lat.toFixed(4);
  el.lon.value = state.lon.toFixed(4);
  setDate(new Date());

  el.lat.addEventListener('change', readInputs);
  el.lon.addEventListener('change', readInputs);
  el.date.addEventListener('change', readInputs);
  el.today.addEventListener('click', () => { setDate(new Date()); readInputs(); });
  el.prev.addEventListener('click', () => shiftDate(-1));
  el.next.addEventListener('click', () => shiftDate(1));
  el.geo.addEventListener('click', useGeolocation);
  el.tzClock.addEventListener('click', () => { state.tzMode = 'clock'; save(); render(); });
  el.tzSolar.addEventListener('click', () => { state.tzMode = 'solar'; save(); render(); });

  // Tick spacing depends on viewport width, so redraw the bar when it changes.
  let last = window.innerWidth;
  window.addEventListener('resize', () => {
    if (Math.abs(window.innerWidth - last) < 40) return;
    last = window.innerWidth;
    if (today) render();
  });

  readInputs();
  setInterval(tick, 1000);
}

init();
