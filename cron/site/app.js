'use strict';

/* cron — parse a 5-field cron expression, say what it means, show when it fires.
   All logic is pure and DOM-free above the init block so it can be required by
   node for testing. */

/* ---------- names & field specs ---------- */

const MONTH_ABBR = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];
const MONTH_FULL = ['January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December'];
const DAY_ABBR = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'];
const DAY_FULL = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

// `count` is the number of distinct values a field can hold. day-of-week accepts
// 0-7 but 7 folds onto 0, so it holds 7 distinct values, not 8.
const SPECS = [
  { key: 'min', name: 'minute', min: 0, max: 59, count: 60 },
  { key: 'hour', name: 'hour', min: 0, max: 23, count: 24 },
  { key: 'dom', name: 'day-of-month', min: 1, max: 31, count: 31 },
  { key: 'mon', name: 'month', min: 1, max: 12, count: 12, names: MONTH_ABBR, full: MONTH_FULL, base: 1 },
  { key: 'dow', name: 'day-of-week', min: 0, max: 7, count: 7, names: DAY_ABBR, full: DAY_FULL, base: 0 }
];

const NAME_MAPS = SPECS.map(function (spec) {
  if (!spec.names) return null;
  const map = Object.create(null);
  spec.names.forEach(function (n, i) { map[n] = i + spec.base; });
  spec.full.forEach(function (n, i) { map[n.toUpperCase()] = i + spec.base; });
  return map;
});

const ALIASES = {
  '@yearly': '0 0 1 1 *',
  '@annually': '0 0 1 1 *',
  '@monthly': '0 0 1 * *',
  '@weekly': '0 0 * * 0',
  '@daily': '0 0 * * *',
  '@midnight': '0 0 * * *',
  '@hourly': '0 * * * *'
};

/* ---------- parsing ---------- */

function parseField(text, spec, index, nameMap) {
  const label = 'Field ' + (index + 1) + ' (' + spec.name + ')';
  const raw = String(text).trim();

  if (raw === '') return { error: label + ': empty.' };

  // Quartz-style "no specific value"; equivalent to * for our purposes.
  if (raw === '?') {
    if (spec.key !== 'dom' && spec.key !== 'dow') {
      return { error: label + ': "?" is only allowed in day-of-month or day-of-week.' };
    }
    return finishField(raw, fullList(spec), spec, true);
  }

  const values = [];
  const items = raw.split(',');

  for (let i = 0; i < items.length; i++) {
    const item = items[i].trim();
    if (item === '') return { error: label + ': empty item in list "' + raw + '".' };

    const slash = item.split('/');
    if (slash.length > 2) return { error: label + ': "' + item + '" has more than one step.' };

    let step = 1;
    if (slash.length === 2) {
      const s = slash[1].trim();
      if (!/^\d+$/.test(s)) return { error: label + ': step "' + s + '" in "' + item + '" is not a positive integer.' };
      step = parseInt(s, 10);
      if (step === 0) return { error: label + ': step 0 in "' + item + '" is not allowed.' };
    }

    const rangeText = slash[0].trim();
    let lo, hi;

    if (rangeText === '*') {
      lo = spec.min;
      hi = spec.max;
    } else {
      const bits = rangeText.split('-');
      if (bits.length > 2) return { error: label + ': "' + rangeText + '" is not a valid range.' };

      lo = toValue(bits[0], nameMap);
      if (lo === null) return { error: label + ': "' + bits[0].trim() + '" is not a valid ' + spec.name + ' value.' };
      if (lo < spec.min || lo > spec.max) {
        return { error: label + ': ' + bits[0].trim() + ' is out of range ' + spec.min + '-' + spec.max + '.' };
      }

      if (bits.length === 2) {
        hi = toValue(bits[1], nameMap);
        if (hi === null) return { error: label + ': "' + bits[1].trim() + '" is not a valid ' + spec.name + ' value.' };
        if (hi < spec.min || hi > spec.max) {
          return { error: label + ': ' + bits[1].trim() + ' is out of range ' + spec.min + '-' + spec.max + '.' };
        }
        if (hi < lo) {
          return { error: label + ': range "' + rangeText + '" runs backwards (' + bits[0].trim() + ' > ' + bits[1].trim() + ').' };
        }
      } else {
        // "5/15" is not in POSIX but is widely accepted as "5 to max, step 15".
        hi = slash.length === 2 ? spec.max : lo;
      }
    }

    for (let v = lo; v <= hi; v += step) values.push(fold(v, spec));
  }

  // Vixie cron sets its DOM_STAR / DOW_STAR flag when the field *begins* with a
  // star, so "*/2" counts as unrestricted for the OR rule below.
  return finishField(raw, values, spec, raw.charAt(0) === '*');
}

function toValue(text, nameMap) {
  const t = String(text).trim();
  if (/^\d+$/.test(t)) return parseInt(t, 10);
  if (nameMap) {
    const v = nameMap[t.toUpperCase()];
    if (v !== undefined) return v;
  }
  return null;
}

function fold(v, spec) {
  return spec.key === 'dow' && v === 7 ? 0 : v;
}

function fullList(spec) {
  const out = [];
  for (let v = spec.min; v <= spec.max; v++) out.push(fold(v, spec));
  return out;
}

function finishField(raw, values, spec, wildcard) {
  const list = values.slice().sort(function (a, b) { return a - b; })
    .filter(function (v, i, arr) { return i === 0 || v !== arr[i - 1]; });
  const set = Object.create(null);
  list.forEach(function (v) { set[v] = true; });
  return {
    raw: raw,
    spec: spec,
    list: list,
    has: function (v) { return set[v] === true; },
    wildcard: wildcard,
    full: list.length === spec.count
  };
}

function parseExpression(text) {
  const raw = String(text == null ? '' : text).trim();
  if (raw === '') {
    return { ok: false, error: 'Empty. Enter 5 fields: minute hour day-of-month month day-of-week.' };
  }

  const lower = raw.toLowerCase();
  if (lower === '@reboot') {
    return { ok: false, error: '@reboot has no schedule — it runs once at startup.' };
  }

  const expanded = Object.prototype.hasOwnProperty.call(ALIASES, lower) ? ALIASES[lower] : null;
  const source = expanded || raw;
  const parts = source.split(/\s+/);

  if (parts.length !== 5) {
    if (parts.length === 6 || parts.length === 7) {
      return {
        ok: false,
        error: 'Got ' + parts.length + ' fields. This parser is 5-field cron (minute hour day-of-month month day-of-week) — drop the seconds/year field.'
      };
    }
    return {
      ok: false,
      error: 'Expected 5 fields, got ' + parts.length + '. Format: minute hour day-of-month month day-of-week.'
    };
  }

  const fields = [];
  for (let i = 0; i < 5; i++) {
    const parsed = parseField(parts[i], SPECS[i], i, NAME_MAPS[i]);
    if (parsed.error) return { ok: false, error: parsed.error };
    fields.push(parsed);
  }

  return {
    ok: true,
    alias: expanded ? lower : null,
    normalized: parts.join(' '),
    fields: fields,
    min: fields[0],
    hour: fields[1],
    dom: fields[2],
    mon: fields[3],
    dow: fields[4]
  };
}

/* ---------- the day-of-month / day-of-week OR rule ---------- */

function dayMatches(sched, domValue, dowValue) {
  const domStar = sched.dom.wildcard;
  const dowStar = sched.dow.wildcard;
  if (domStar && dowStar) return true;
  if (domStar) return sched.dow.has(dowValue);
  if (dowStar) return sched.dom.has(domValue);
  // Both restricted: cron fires when EITHER matches.
  return sched.dom.has(domValue) || sched.dow.has(dowValue);
}

/* ---------- next fire times ---------- */

// How far ahead the search gives up. Real cron caps at a few years; a visualizer
// is more useful if it can still show ten Feb 29ths.
const HORIZON_YEARS = 100;

// Calendar arithmetic runs on a UTC Date used purely as a wall-clock container,
// so month lengths and leap years are handled without any DST distortion. Each
// matched wall time is converted to a real local Date at the end.
function nextFireTimes(sched, from, count, horizonYears) {
  const want = count || 10;
  const years = horizonYears || HORIZON_YEARS;
  const out = [];
  let skippedByDst = 0;

  const startYear = from.getFullYear();
  let cur = Date.UTC(from.getFullYear(), from.getMonth(), from.getDate(),
    from.getHours(), from.getMinutes() + 1, 0, 0);

  let guard = 0;
  while (out.length < want) {
    if (guard++ > 200000) break;
    const c = new Date(cur);
    if (c.getUTCFullYear() > startYear + years) break;

    if (!sched.mon.has(c.getUTCMonth() + 1)) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth() + 1, 1, 0, 0, 0, 0);
      continue;
    }
    if (!dayMatches(sched, c.getUTCDate(), c.getUTCDay())) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate() + 1, 0, 0, 0, 0);
      continue;
    }
    const h = nextAtLeast(sched.hour.list, c.getUTCHours());
    if (h === null) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate() + 1, 0, 0, 0, 0);
      continue;
    }
    if (h !== c.getUTCHours()) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate(), h, 0, 0, 0);
      continue;
    }
    const m = nextAtLeast(sched.min.list, c.getUTCMinutes());
    if (m === null) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate(), c.getUTCHours() + 1, 0, 0, 0);
      continue;
    }
    if (m !== c.getUTCMinutes()) {
      cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate(), c.getUTCHours(), m, 0, 0);
      continue;
    }

    const wall = {
      y: c.getUTCFullYear(), mo: c.getUTCMonth() + 1, d: c.getUTCDate(),
      h: c.getUTCHours(), mi: c.getUTCMinutes()
    };
    const local = new Date(wall.y, wall.mo - 1, wall.d, wall.h, wall.mi, 0, 0);
    const exists = local.getFullYear() === wall.y && local.getMonth() === wall.mo - 1 &&
      local.getDate() === wall.d && local.getHours() === wall.h && local.getMinutes() === wall.mi;

    if (exists && (out.length === 0 || local.getTime() > out[out.length - 1].getTime())) {
      out.push(local);
    } else if (!exists) {
      // Wall time fell in the gap of a spring-forward transition. It never
      // happens locally, so it is not a fire time.
      skippedByDst++;
    }

    cur = Date.UTC(c.getUTCFullYear(), c.getUTCMonth(), c.getUTCDate(), c.getUTCHours(), c.getUTCMinutes() + 1, 0, 0);
  }

  return { times: out, skippedByDst: skippedByDst, exhausted: out.length < want };
}

function nextAtLeast(list, v) {
  for (let i = 0; i < list.length; i++) if (list[i] >= v) return list[i];
  return null;
}

/* ---------- small formatters ---------- */

function pad2(n) { return (n < 10 ? '0' : '') + n; }

function ord(n) {
  const s = ['th', 'st', 'nd', 'rd'];
  const v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}

function joinAnd(arr) {
  if (arr.length === 0) return '';
  if (arr.length === 1) return arr[0];
  if (arr.length === 2) return arr[0] + ' and ' + arr[1];
  return arr.slice(0, -1).join(', ') + ' and ' + arr[arr.length - 1];
}

// Returns {start, step, end} when the list is an arithmetic progression of at
// least 3 terms, else null. Used to say "every 5 minutes" instead of listing.
function arith(list) {
  if (list.length < 3) return null;
  const step = list[1] - list[0];
  for (let i = 2; i < list.length; i++) if (list[i] - list[i - 1] !== step) return null;
  return { start: list[0], step: step, end: list[list.length - 1] };
}

function contiguous(list) {
  const a = arith(list);
  if (list.length === 2) return list[1] - list[0] === 1 ? { start: list[0], end: list[1] } : null;
  if (a && a.step === 1) return { start: a.start, end: a.end };
  return null;
}

/* ---------- plain-English description ---------- */

function describeHours(hour) {
  if (hour.list.length === 1) return 'during the ' + pad2(hour.list[0]) + ':00 hour';
  const cont = contiguous(hour.list);
  if (cont) return 'between ' + pad2(cont.start) + ':00 and ' + pad2(cont.end) + ':59';
  return 'during hours ' + joinAnd(hour.list.map(pad2));
}

function describeTime(sched) {
  const M = sched.min;
  const H = sched.hour;
  const mins = M.list.map(function (m) { return ':' + pad2(m); });

  if (M.full) {
    if (H.full) return { text: 'Every minute', frequency: true };
    return { text: 'Every minute ' + describeHours(H), frequency: true };
  }

  const a = arith(M.list);
  if (a && a.step > 1) {
    const coversAll = a.start === 0 && a.end + a.step > 59;
    let base = 'Every ' + a.step + ' minutes';
    if (!coversAll) base += ' from :' + pad2(a.start) + ' through :' + pad2(a.end);
    if (H.full) return { text: base, frequency: true };
    return { text: base + ' ' + describeHours(H), frequency: true };
  }

  if (H.full) {
    return { text: 'At ' + joinAnd(mins) + ' past every hour', frequency: true };
  }

  // Small cross-products read best fully enumerated: "At 09:00, 09:30 and 17:00".
  if (H.list.length * M.list.length <= 6) {
    const stamps = [];
    H.list.forEach(function (h) {
      M.list.forEach(function (m) { stamps.push(pad2(h) + ':' + pad2(m)); });
    });
    stamps.sort();
    return { text: 'At ' + joinAnd(stamps), frequency: false };
  }

  return { text: 'At ' + joinAnd(mins) + ' past hours ' + joinAnd(H.list.map(pad2)), frequency: false };
}

function describeDom(dom) {
  if (dom.full) return null;
  if (dom.list.length === 1) return 'the ' + ord(dom.list[0]);
  const cont = contiguous(dom.list);
  if (cont && dom.list.length > 2) return 'the ' + ord(cont.start) + ' through the ' + ord(cont.end);
  const a = arith(dom.list);
  if (a && a.step > 1 && a.start === 1) return 'every ' + ord(a.step) + ' day from the 1st';
  return 'the ' + joinAnd(dom.list.map(ord));
}

function describeDow(dow) {
  if (dow.full) return null;
  const names = dow.list.map(function (d) { return DAY_FULL[d]; });
  if (dow.list.length === 1) return { text: 'every ' + names[0], bare: true };
  const cont = contiguous(dow.list);
  if (cont && dow.list.length > 2) {
    return { text: DAY_FULL[cont.start] + ' through ' + DAY_FULL[cont.end], bare: false };
  }
  return { text: joinAnd(names), bare: false };
}

function describeMonths(mon) {
  if (mon.full) return null;
  const names = mon.list.map(function (m) { return MONTH_FULL[m - 1]; });
  if (mon.list.length === 1) return names[0];
  const cont = contiguous(mon.list);
  if (cont && mon.list.length > 2) return MONTH_FULL[cont.start - 1] + ' through ' + MONTH_FULL[cont.end - 1];
  return joinAnd(names);
}

function describe(sched) {
  const time = describeTime(sched);
  const domText = describeDom(sched.dom);
  const dowInfo = describeDow(sched.dow);
  const monText = describeMonths(sched.mon);

  const bothRestricted = domText !== null && dowInfo !== null &&
    !sched.dom.wildcard && !sched.dow.wildcard;

  let day;

  if (domText === null && dowInfo === null) {
    // "every day" is redundant after a frequency phrase ("Every 5 minutes"),
    // which already implies daily recurrence.
    day = time.frequency ? '' : 'every day';
  } else if (bothRestricted) {
    day = 'on ' + domText + ' of the month or ' + orAny(dowInfo);
  } else if (domText !== null && dowInfo !== null) {
    // One side is a star-prefixed step, so cron AND's them.
    day = 'on ' + domText + ' when it falls on ' + andDay(dowInfo);
  } else if (domText !== null) {
    day = 'on ' + domText + (monText ? ' of ' + monText : ' of every month');
  } else {
    day = (dowInfo.bare ? '' : 'on ') + dowInfo.text;
  }

  let out = time.text;
  if (day) out += ' ' + day;

  if (monText && domText === null) out += ' in ' + monText;
  else if (monText && (bothRestricted || (domText !== null && dowInfo !== null))) out += ', in ' + monText;

  return out + '.';
}

function orAny(dowInfo) {
  return dowInfo.bare ? 'any ' + dowInfo.text.replace(/^every /, '') : 'any ' + dowInfo.text;
}

function andDay(dowInfo) {
  return dowInfo.bare ? 'a ' + dowInfo.text.replace(/^every /, '') : dowInfo.text;
}

/* ---------- per-field resolution strings ---------- */

function resolveField(field) {
  const spec = field.spec;
  const list = field.list;

  if (spec.key === 'min') {
    if (field.full) return 'every minute (0–59)';
    return truncate(list.map(function (m) { return ':' + pad2(m); }), list.length);
  }
  if (spec.key === 'hour') {
    if (field.full) return 'every hour (0–23)';
    return truncate(list.map(function (h) { return pad2(h) + ':00'; }), list.length);
  }
  if (spec.key === 'dom') {
    if (field.full) return 'every day of the month';
    return truncate(list.map(ord), list.length);
  }
  if (spec.key === 'mon') {
    if (field.full) return 'every month';
    return truncate(list.map(function (m) { return MONTH_FULL[m - 1]; }), list.length);
  }
  if (field.full) return 'every day of the week';
  return truncate(list.map(function (d) { return DAY_FULL[d]; }), list.length);
}

function truncate(parts, total) {
  if (parts.length <= 8) return parts.join(', ');
  return parts.slice(0, 8).join(', ') + ' … (' + total + ' values)';
}

/* ---------- humanized relative time ---------- */

function calendarDayDiff(now, target) {
  const a = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const b = new Date(target.getFullYear(), target.getMonth(), target.getDate());
  return Math.round((b - a) / 86400000);
}

function relative(target, now) {
  const ms = target.getTime() - now.getTime();
  if (ms < 60000) return 'in under a minute';

  const mins = Math.round(ms / 60000);
  if (mins < 60) return 'in ' + mins + ' minute' + (mins === 1 ? '' : 's');

  const clock = pad2(target.getHours()) + ':' + pad2(target.getMinutes());
  const days = calendarDayDiff(now, target);
  if (days === 0) return 'today ' + clock;
  if (days === 1) return 'tomorrow ' + clock;
  if (days < 7) return DAY_FULL[target.getDay()] + ' ' + clock;
  if (days < 60) return 'in ' + days + ' days';

  const months = Math.round(days / 30.44);
  if (months < 24) return 'in ' + months + ' months';
  return 'in ' + Math.round(days / 365.25) + ' years';
}

function stampOf(d) {
  return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate()) +
    ' ' + pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ' ' + DAY_ABBR[d.getDay()].charAt(0) +
    DAY_ABBR[d.getDay()].slice(1).toLowerCase();
}

function countdown(ms) {
  if (ms < 0) ms = 0;
  const total = Math.floor(ms / 1000);
  const days = Math.floor(total / 86400);
  const h = Math.floor((total % 86400) / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const clock = pad2(h) + ':' + pad2(m) + ':' + pad2(s);
  return days > 0 ? days + 'd ' + clock : clock;
}

/* ---------- DOM ---------- */

const PRESETS = [
  { label: 'Every Minute', expr: '* * * * *' },
  { label: 'Every 5 Minutes', expr: '*/5 * * * *' },
  { label: 'Hourly', expr: '0 * * * *' },
  { label: 'Daily 03:00', expr: '0 3 * * *' },
  { label: 'Weekdays 09:00', expr: '0 9 * * MON-FRI' },
  { label: 'Sunday 04:00', expr: '0 4 * * SUN' },
  { label: 'First Of The Month', expr: '0 0 1 * *' }
];

const STORE_KEY = 'cron.expr';
const FIELD_LABELS = ['min', 'hour', 'dom', 'mon', 'dow'];

function initApp() {
  const el = {
    expr: document.getElementById('expr'),
    presets: document.getElementById('presets'),
    status: document.getElementById('status'),
    sentence: document.getElementById('sentence'),
    fieldsBody: document.getElementById('fieldsBody'),
    orNote: document.getElementById('orNote'),
    runsBody: document.getElementById('runsBody'),
    runsNote: document.getElementById('runsNote'),
    countdown: document.getElementById('countdown'),
    tz: document.getElementById('tz')
  };

  let current = null;   // last successful parse
  let nextTimes = [];   // Date[]

  el.tz.textContent = tzLabel();

  PRESETS.forEach(function (p) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn preset';
    b.textContent = p.label;
    b.setAttribute('aria-pressed', 'false');
    b.addEventListener('click', function () {
      el.expr.value = p.expr;
      render();
      el.expr.focus();
    });
    b.dataset.expr = p.expr;
    el.presets.appendChild(b);
  });

  el.expr.value = initialExpression();
  el.expr.addEventListener('input', render);
  render();

  setInterval(tick, 1000);

  function initialExpression() {
    const fromHash = location.hash ? safeDecode(location.hash.slice(1)) : '';
    if (fromHash) return fromHash;
    try {
      const saved = localStorage.getItem(STORE_KEY);
      if (saved) return saved;
    } catch (e) { /* private mode */ }
    return '*/15 * * * *';
  }

  function render() {
    const text = el.expr.value;
    persist(text);
    markPresets(text.trim());

    const parsed = parseExpression(text);

    if (!parsed.ok) {
      current = null;
      nextTimes = [];
      el.status.textContent = parsed.error;
      el.status.className = 'status bad';
      el.sentence.textContent = '';
      el.sentence.classList.add('empty-sentence');
      el.fieldsBody.innerHTML = '<tr><td colspan="3" class="empty">Not parsed.</td></tr>';
      el.orNote.textContent = '';
      el.runsBody.innerHTML = '<tr><td colspan="2" class="empty">Not parsed.</td></tr>';
      el.runsNote.textContent = '';
      el.countdown.textContent = '—';
      return;
    }

    current = parsed;
    el.status.className = 'status ok';
    el.status.textContent = parsed.alias
      ? 'Valid. ' + parsed.alias + ' expands to ' + parsed.normalized + '.'
      : 'Valid.';

    el.sentence.textContent = describe(parsed);
    el.sentence.classList.remove('empty-sentence');

    renderFields(parsed);
    compute();
  }

  function renderFields(parsed) {
    const rows = parsed.fields.map(function (f, i) {
      return '<tr><td class="fkey mono">' + FIELD_LABELS[i] + '</td>' +
        '<td class="fraw mono">' + esc(f.raw) + '</td>' +
        '<td class="fres">' + esc(resolveField(f)) + '</td></tr>';
    });
    el.fieldsBody.innerHTML = rows.join('');

    const domSet = !parsed.dom.wildcard && !parsed.dom.full;
    const dowSet = !parsed.dow.wildcard && !parsed.dow.full;
    // Only worth saying when it changes the answer: the OR rule.
    el.orNote.textContent = domSet && dowSet
      ? 'dom and dow are both restricted, so cron fires when either matches — not only when both do.'
      : '';
  }

  function compute() {
    if (!current) return;
    const result = nextFireTimes(current, new Date(), 10, HORIZON_YEARS);
    nextTimes = result.times;

    if (nextTimes.length === 0) {
      el.runsBody.innerHTML = '<tr><td colspan="2" class="empty">Never fires. No matching date within 100 years.</td></tr>';
      el.runsNote.textContent = 'That combination of day-of-month and month never occurs.';
      el.countdown.textContent = 'never';
      return;
    }

    const now = new Date();
    el.runsBody.innerHTML = nextTimes.map(function (d) {
      return '<tr><td class="mono stamp">' + stampOf(d) + '</td>' +
        '<td class="rel">' + esc(relative(d, now)) + '</td></tr>';
    }).join('');

    let note = '';
    if (result.skippedByDst > 0) {
      note = result.skippedByDst + ' wall-clock time' + (result.skippedByDst === 1 ? '' : 's') +
        ' in this window do not exist locally (daylight saving jump) and were dropped.';
    }
    if (result.exhausted && nextTimes.length > 0) {
      note = (note ? note + ' ' : '') + 'Only ' + nextTimes.length + ' occurrence' +
        (nextTimes.length === 1 ? '' : 's') + ' found within ' + HORIZON_YEARS + ' years.';
    }
    el.runsNote.textContent = note;
    updateCountdown(now);
  }

  function tick() {
    if (!current || nextTimes.length === 0) return;
    const now = new Date();
    if (now.getTime() >= nextTimes[0].getTime()) {
      compute();
      return;
    }
    updateCountdown(now);
  }

  function updateCountdown(now) {
    el.countdown.textContent = countdown(nextTimes[0].getTime() - now.getTime());
  }

  function markPresets(text) {
    const buttons = el.presets.querySelectorAll('button');
    for (let i = 0; i < buttons.length; i++) {
      const on = buttons[i].dataset.expr === text;
      buttons[i].setAttribute('aria-pressed', on ? 'true' : 'false');
      buttons[i].className = on ? 'btn preset primary' : 'btn preset';
    }
  }

  function persist(text) {
    try { localStorage.setItem(STORE_KEY, text); } catch (e) { /* private mode */ }
    try {
      history.replaceState(null, '', text.trim() ? '#' + encodeURIComponent(text.trim()) : location.pathname);
    } catch (e) { /* file:// */ }
  }
}

function safeDecode(s) {
  try { return decodeURIComponent(s); } catch (e) { return ''; }
}

function tzLabel() {
  let zone = '';
  try { zone = Intl.DateTimeFormat().resolvedOptions().timeZone || ''; } catch (e) { zone = ''; }
  const offset = -new Date().getTimezoneOffset();
  const sign = offset < 0 ? '-' : '+';
  const abs = Math.abs(offset);
  const utc = 'UTC' + sign + pad2(Math.floor(abs / 60)) + ':' + pad2(abs % 60);
  return zone ? zone + ' ' + utc : utc;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c];
  });
}

if (typeof document !== 'undefined') {
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', initApp);
  else initApp();
}

if (typeof module === 'object' && module.exports) {
  module.exports = {
    parseExpression: parseExpression,
    describe: describe,
    nextFireTimes: nextFireTimes,
    resolveField: resolveField,
    relative: relative,
    stampOf: stampOf,
    dayMatches: dayMatches
  };
}
