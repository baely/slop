'use strict';

/* sheet — drop a CSV, understand it in ten seconds.
   Everything above the "DOM" banner is pure and runs under node; the test
   harness requires this file directly. */

/* ============================================================
   CSV — RFC 4180 with lenient recovery
   ============================================================ */

var DELIMS = [
  { ch: ',', name: 'comma' },
  { ch: '\t', name: 'tab' },
  { ch: ';', name: 'semicolon' },
  { ch: '|', name: 'pipe' }
];

function stripBOM(t) {
  return t.charCodeAt(0) === 0xFEFF ? t.slice(1) : t;
}

/* State machine rather than split(): only a machine can carry a delimiter or a
   newline through the inside of a quoted field. Returns physical line numbers
   per row so ragged rows can be reported against the file the user has open. */
function parseDelimited(text, delim) {
  text = stripBOM(String(text));
  var rows = [], lines = [], warnings = [];
  var row = [], field = '';
  var inQ = false, quoted = false, started = false;
  var line = 1, rowLine = 1;
  var i = 0, n = text.length, c;

  while (i < n) {
    c = text.charAt(i);

    if (inQ) {
      if (c === '"') {
        if (text.charAt(i + 1) === '"') { field += '"'; i += 2; continue; }
        inQ = false; i++; continue;
      }
      if (c === '\r') {
        if (text.charAt(i + 1) === '\n') { field += '\r\n'; i += 2; }
        else { field += '\r'; i++; }
        line++; continue;
      }
      if (c === '\n') { field += '\n'; line++; i++; continue; }
      field += c; i++; continue;
    }

    if (c === '"' && field === '' && !quoted) {
      inQ = true; quoted = true; started = true; i++; continue;
    }
    if (c === delim) {
      row.push(field); field = ''; quoted = false; started = true; i++; continue;
    }
    if (c === '\r' || c === '\n') {
      if (c === '\r' && text.charAt(i + 1) === '\n') i++;
      row.push(field); rows.push(row); lines.push(rowLine);
      row = []; field = ''; quoted = false; started = false;
      i++; line++; rowLine = line; continue;
    }
    field += c; started = true; i++;
  }

  if (inQ) warnings.push({ line: rowLine, msg: 'Unterminated quoted field.' });
  if (started || row.length || field !== '') {
    row.push(field); rows.push(row); lines.push(rowLine);
  }
  return { rows: rows, lines: lines, warnings: warnings };
}

/* Score each candidate on how consistently it produces the same field count
   across the first rows. A delimiter that never splits anything scores zero. */
function detectDelimiter(text) {
  var sample = text.length > 65536 ? text.slice(0, 65536) : text;
  var truncated = sample.length < text.length;
  var best = null;

  for (var i = 0; i < DELIMS.length; i++) {
    var d = DELIMS[i];
    var rows = parseDelimited(sample, d.ch).rows;
    if (truncated && rows.length > 1) rows = rows.slice(0, rows.length - 1);
    rows = rows.slice(0, 25);
    if (!rows.length) continue;

    var freq = {}, j;
    for (j = 0; j < rows.length; j++) {
      var len = rows[j].length;
      freq[len] = (freq[len] || 0) + 1;
    }
    var mode = 0, modeCount = 0;
    for (var k in freq) {
      var kn = Number(k);
      if (freq[k] > modeCount || (freq[k] === modeCount && kn > mode)) { mode = kn; modeCount = freq[k]; }
    }
    if (mode < 2) continue;
    var score = (modeCount / rows.length) * 100 + Math.min(mode, 30);
    if (!best || score > best.score) best = { delim: d, score: score, fields: mode };
  }

  if (!best) return { delim: DELIMS[0], found: false, fields: 1 };
  return { delim: best.delim, found: true, fields: best.fields };
}

function normaliseHeader(cells) {
  var seen = {}, out = [];
  for (var i = 0; i < cells.length; i++) {
    var name = String(cells[i]).trim();
    if (!name) name = 'Column ' + (i + 1);
    if (Object.prototype.hasOwnProperty.call(seen, name)) {
      seen[name]++;
      name = name + ' (' + seen[name] + ')';
    } else {
      seen[name] = 1;
    }
    out.push(name);
  }
  return out;
}

/* First row is the header. Rows whose width differs are reported by line number
   and then padded/truncated so the rest of the file stays usable. */
function buildTable(parsed) {
  var rows = parsed.rows, lines = parsed.lines;
  var issues = [], blankLines = 0;
  var clean = [], cleanLines = [], i;

  for (i = 0; i < rows.length; i++) {
    if (rows[i].length === 1 && rows[i][0] === '') { blankLines++; continue; }
    clean.push(rows[i]); cleanLines.push(lines[i]);
  }
  if (!clean.length) {
    return { header: [], rows: [], issues: issues, blankLines: blankLines, empty: true };
  }

  var header = normaliseHeader(clean[0]);
  var width = header.length;
  var out = [];
  for (i = 1; i < clean.length; i++) {
    var r = clean[i];
    if (r.length !== width) {
      issues.push({ line: cleanLines[i], got: r.length, want: width });
      if (r.length < width) { r = r.slice(); while (r.length < width) r.push(''); }
      else r = r.slice(0, width);
    }
    out.push(r);
  }
  return { header: header, rows: out, issues: issues, blankLines: blankLines, empty: false };
}

/* ============================================================
   Type inference
   ============================================================ */

/* Number rule, decided deliberately:
   - grouped thousands are numbers only when grouped correctly: 1,234.50 -> yes,
     12,34 -> no.
   - a leading zero on a multi-digit integer means identifier, not quantity:
     007, 0400, 00612 stay text. 0, 0.5, 0.00 are numbers.
   - one leading currency symbol is allowed ($1,234.50), because bank exports.
   - scientific notation is allowed. Percentages and (123) negatives are not. */
var RE_NUM_BODY = /^(\d{1,3}(,\d{3})+|\d+)(\.\d+)?$/;
var RE_NUM_FRAC = /^\.\d+$/;
var RE_NUM_SCI = /^\d+(\.\d+)?[eE][+-]?\d+$/;

function parseNumber(raw) {
  if (raw === null || raw === undefined) return null;
  var s = String(raw).trim();
  if (!s) return null;
  var neg = false;
  if (s.charAt(0) === '+') s = s.slice(1);
  else if (s.charAt(0) === '-') { neg = true; s = s.slice(1); }
  if (s && '$€£¥'.indexOf(s.charAt(0)) >= 0) {
    s = s.slice(1).trim();
    if (s.charAt(0) === '-') { neg = true; s = s.slice(1); }
  }
  if (!s) return null;

  var v;
  if (RE_NUM_SCI.test(s)) {
    v = Number(s);
  } else if (RE_NUM_FRAC.test(s)) {
    v = Number(s);
  } else if (RE_NUM_BODY.test(s)) {
    var intPart = s.split('.')[0].replace(/,/g, '');
    if (intPart.length > 1 && intPart.charAt(0) === '0') return null;
    v = Number(s.replace(/,/g, ''));
  } else {
    return null;
  }
  if (!isFinite(v)) return null;
  return neg ? -v : v;
}

function decimalsOf(raw) {
  var s = String(raw).trim();
  var dot = s.indexOf('.');
  if (dot < 0) return 0;
  var tail = s.slice(dot + 1).replace(/[^\d].*$/, '');
  return Math.min(tail.length, 6);
}

var MONTH_NAMES = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

var RE_ISO = /^(\d{4})-(\d{1,2})-(\d{1,2})(?:[T ](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?\s*(?:Z|[+-]\d{2}:?\d{2})?)?$/;
var RE_YMD = /^(\d{4})\/(\d{1,2})\/(\d{1,2})$/;
var RE_AMB = /^(\d{1,2})[\/-](\d{1,2})[\/-](\d{2}|\d{4})$/;
var RE_DMONY = /^(\d{1,2})[ -]([A-Za-z]{3,9})\.?[ -](\d{2}|\d{4})$/;
var RE_MONDY = /^([A-Za-z]{3,9})\.?[ -](\d{1,2}),?[ -]?(\d{2}|\d{4})$/;

function monthIndex(word) {
  var w = String(word).toLowerCase().slice(0, 3);
  var i = MONTH_NAMES.indexOf(w);
  return i < 0 ? 0 : i + 1;
}

function expandYear(y) {
  var n = Number(y);
  if (String(y).length <= 2) return n < 70 ? 2000 + n : 1900 + n;
  return n;
}

function daysInMonth(y, m) {
  return [31, (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 29 : 28,
    31, 30, 31, 30, 31, 31, 30, 31, 30, 31][m - 1];
}

function validYMD(y, m, d) {
  return m >= 1 && m <= 12 && d >= 1 && d <= daysInMonth(y, m) && y >= 1 && y <= 9999;
}

function utc(y, m, d, hh, mm, ss) {
  return Date.UTC(y, m - 1, d, hh || 0, mm || 0, ss || 0);
}

/* Returns either a resolved date, or an "ambiguous" record whose day/month
   order the column as a whole has to decide. */
function probeDate(raw) {
  var s = String(raw === null || raw === undefined ? '' : raw).trim();
  if (!s) return null;
  var m;

  m = RE_ISO.exec(s);
  if (m) {
    var y = +m[1], mo = +m[2], d = +m[3];
    if (!validYMD(y, mo, d)) return null;
    return { ms: utc(y, mo, d, +(m[4] || 0), +(m[5] || 0), +(m[6] || 0)), hasTime: m[4] !== undefined };
  }
  m = RE_YMD.exec(s);
  if (m) {
    if (!validYMD(+m[1], +m[2], +m[3])) return null;
    return { ms: utc(+m[1], +m[2], +m[3]), hasTime: false };
  }
  m = RE_DMONY.exec(s);
  if (m) {
    var mi = monthIndex(m[2]);
    if (!mi) return null;
    var yy = expandYear(m[3]);
    if (!validYMD(yy, mi, +m[1])) return null;
    return { ms: utc(yy, mi, +m[1]), hasTime: false };
  }
  m = RE_MONDY.exec(s);
  if (m) {
    var mi2 = monthIndex(m[1]);
    if (!mi2) return null;
    var yy2 = expandYear(m[3]);
    if (!validYMD(yy2, mi2, +m[2])) return null;
    return { ms: utc(yy2, mi2, +m[2]), hasTime: false };
  }
  m = RE_AMB.exec(s);
  if (m) {
    var p1 = +m[1], p2 = +m[2], year = expandYear(m[3]);
    var dmy = validYMD(year, p2, p1);
    var mdy = validYMD(year, p1, p2);
    if (!dmy && !mdy) return null;
    return { ambiguous: true, p1: p1, p2: p2, year: year, dmy: dmy, mdy: mdy };
  }
  return null;
}

function dateFromProbe(p, order) {
  if (!p) return null;
  if (!p.ambiguous) return p.ms;
  if (order === 'mdy') return p.mdy ? utc(p.year, p.p1, p.p2) : null;
  return p.dmy ? utc(p.year, p.p2, p.p1) : null;
}

var BOOL_SETS = [['true', 'false'], ['yes', 'no'], ['y', 'n'], ['t', 'f']];

function isBooleanColumn(nonEmpty) {
  var distinct = {}, count = 0, i;
  for (i = 0; i < nonEmpty.length; i++) {
    var v = nonEmpty[i].toLowerCase();
    if (!Object.prototype.hasOwnProperty.call(distinct, v)) { distinct[v] = 1; count++; }
    if (count > 2) return false;
  }
  var keys = Object.keys(distinct);
  for (i = 0; i < BOOL_SETS.length; i++) {
    var ok = true;
    for (var j = 0; j < keys.length; j++) if (BOOL_SETS[i].indexOf(keys[j]) < 0) { ok = false; break; }
    if (ok) return true;
  }
  return false;
}

function parseBool(raw) {
  var s = String(raw).trim().toLowerCase();
  if (s === 'true' || s === 'yes' || s === 'y' || s === 't') return true;
  if (s === 'false' || s === 'no' || s === 'n' || s === 'f') return false;
  return null;
}

var TYPE_THRESHOLD = 0.9;

/* One column of raw strings in, a type plus everything needed to re-parse it
   out. dateOrder is resolved from the column, never from a single cell. */
function inferColumn(values) {
  var nonEmpty = [], i;
  for (i = 0; i < values.length; i++) {
    var v = values[i];
    if (v === null || v === undefined) continue;
    var s = String(v).trim();
    if (s !== '') nonEmpty.push(s);
  }
  var meta = {
    type: 'text', blanks: values.length - nonEmpty.length, filled: nonEmpty.length,
    dateOrder: 'dmy', dateAmbiguous: false, dateConflict: false, dateSlashCount: 0, dec: 0
  };
  if (!nonEmpty.length) return meta;

  if (isBooleanColumn(nonEmpty)) { meta.type = 'boolean'; return meta; }

  var numOK = 0, dec = 0;
  for (i = 0; i < nonEmpty.length; i++) {
    var n = parseNumber(nonEmpty[i]);
    if (n !== null) { numOK++; dec = Math.max(dec, decimalsOf(nonEmpty[i])); }
  }

  var probes = [], needDmy = 0, needMdy = 0, ambCount = 0, definite = 0;
  for (i = 0; i < nonEmpty.length; i++) {
    var p = probeDate(nonEmpty[i]);
    probes.push(p);
    if (!p) continue;
    if (p.ambiguous) {
      ambCount++;
      if (!p.mdy && p.dmy) needDmy++;
      else if (!p.dmy && p.mdy) needMdy++;
      else if (p.p1 > 12) needDmy++;
      else if (p.p2 > 12) needMdy++;
    } else {
      definite++;
    }
  }
  var order = 'dmy', ambiguousFlag = false, conflictFlag = false;
  if (needDmy && !needMdy) order = 'dmy';
  else if (needMdy && !needDmy) order = 'mdy';
  else if (needDmy && needMdy) { order = 'dmy'; conflictFlag = true; }
  else if (ambCount) { order = 'dmy'; ambiguousFlag = true; }

  /* Shape decides the type, the resolved order decides the values. A column
     whose source mixes 25/07 and 07/25 is still a date column; the cells that
     don't fit the chosen order are reported as unparsed rather than silently
     read under the other order. */
  meta.dateSlashCount = ambCount;
  var dateShaped = definite + ambCount;
  var dateOK = 0;
  for (i = 0; i < probes.length; i++) if (dateFromProbe(probes[i], order) !== null) dateOK++;

  var numFrac = numOK / nonEmpty.length;
  var dateFrac = dateShaped / nonEmpty.length;

  if (numFrac >= TYPE_THRESHOLD && numFrac >= dateFrac) {
    meta.type = 'number'; meta.dec = dec;
  } else if (dateFrac >= TYPE_THRESHOLD) {
    meta.type = 'date';
    meta.dateOrder = order;
    meta.dateAmbiguous = ambiguousFlag && ambCount > 0;
    meta.dateConflict = conflictFlag;
    meta.dateUnparsed = dateShaped - dateOK;
  }
  return meta;
}

/* Re-run date-order detection when the user forces a column to Date. */
function dateOrderFor(values, forced) {
  var m = inferColumn(values);
  if (forced === 'dmy' || forced === 'mdy') {
    return { order: forced, ambiguous: false, conflict: false, slashCount: m.dateSlashCount };
  }
  return { order: m.dateOrder, ambiguous: m.dateAmbiguous, conflict: m.dateConflict, slashCount: m.dateSlashCount };
}

function parseColumn(values, meta) {
  var out = new Array(values.length), i;
  if (meta.type === 'number') {
    for (i = 0; i < values.length; i++) out[i] = parseNumber(values[i]);
  } else if (meta.type === 'date') {
    for (i = 0; i < values.length; i++) out[i] = dateFromProbe(probeDate(values[i]), meta.dateOrder);
  } else if (meta.type === 'boolean') {
    for (i = 0; i < values.length; i++) out[i] = parseBool(values[i]);
  } else {
    for (i = 0; i < values.length; i++) out[i] = null;
  }
  return out;
}

/* ============================================================
   Stats
   ============================================================ */

function median(sorted) {
  var n = sorted.length;
  if (!n) return null;
  var mid = n >> 1;
  return n % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

function summarise(values, parsed, meta) {
  var i, s = { type: meta.type, blanks: meta.blanks, total: values.length };

  if (meta.type === 'number') {
    var nums = [];
    for (i = 0; i < parsed.length; i++) if (parsed[i] !== null) nums.push(parsed[i]);
    s.count = nums.length;
    if (!nums.length) return s;
    var sum = 0;
    for (i = 0; i < nums.length; i++) sum += nums[i];
    var sortedNums = nums.slice().sort(function (a, b) { return a - b; });
    s.min = sortedNums[0];
    s.max = sortedNums[sortedNums.length - 1];
    s.sum = sum;
    s.mean = sum / nums.length;
    s.median = median(sortedNums);
    s.dec = meta.dec;
    return s;
  }

  if (meta.type === 'date') {
    var lo = null, hi = null, c = 0;
    for (i = 0; i < parsed.length; i++) {
      var v = parsed[i];
      if (v === null) continue;
      c++;
      if (lo === null || v < lo) lo = v;
      if (hi === null || v > hi) hi = v;
    }
    s.count = c;
    if (c) { s.min = lo; s.max = hi; s.spanDays = Math.round((hi - lo) / 86400000); }
    return s;
  }

  if (meta.type === 'boolean') {
    var t = 0, f = 0;
    for (i = 0; i < parsed.length; i++) {
      if (parsed[i] === true) t++;
      else if (parsed[i] === false) f++;
    }
    s.count = t + f; s.trueCount = t; s.falseCount = f;
    return s;
  }

  var counts = Object.create(null), filled = 0;
  for (i = 0; i < values.length; i++) {
    var str = String(values[i] === null || values[i] === undefined ? '' : values[i]).trim();
    if (!str) continue;
    filled++;
    counts[str] = (counts[str] || 0) + 1;
  }
  var keys = Object.keys(counts);
  keys.sort(function (a, b) { return counts[b] - counts[a] || (a < b ? -1 : 1); });
  s.count = filled;
  s.distinct = keys.length;
  s.top = keys.slice(0, 5).map(function (k) { return { value: k, n: counts[k] }; });
  return s;
}

/* ============================================================
   Formatting
   ============================================================ */

function fmtNumber(v, dec) {
  if (v === null || v === undefined || !isFinite(v)) return '—';
  var d = dec === undefined ? (Number.isInteger(v) ? 0 : 2) : dec;
  d = Math.max(0, Math.min(6, d));
  var abs = Math.abs(v);
  if (abs >= 1e15 || (abs > 0 && abs < 1e-6)) return v.toExponential(3);
  return v.toLocaleString('en-AU', { minimumFractionDigits: d, maximumFractionDigits: d });
}

function pad2(n) { return n < 10 ? '0' + n : '' + n; }

function fmtDate(ms, withTime) {
  if (ms === null || ms === undefined) return '—';
  var d = new Date(ms);
  var s = d.getUTCFullYear() + '-' + pad2(d.getUTCMonth() + 1) + '-' + pad2(d.getUTCDate());
  if (withTime) s += ' ' + pad2(d.getUTCHours()) + ':' + pad2(d.getUTCMinutes());
  return s;
}

function fmtSpan(days) {
  if (days === null || days === undefined) return '—';
  if (days < 1) return 'same day';
  if (days < 90) return days.toLocaleString('en-AU') + ' days';
  var years = days / 365.25;
  if (years < 1) return (days / 30.44).toFixed(1) + ' months';
  return years.toFixed(1) + ' years';
}

function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1048576).toFixed(1) + ' MB';
}

function plural(n, one, many) {
  return n.toLocaleString('en-AU') + ' ' + (n === 1 ? one : (many || one + 's'));
}

/* ============================================================
   Chart scales
   ============================================================ */

function niceStep(range, round) {
  if (range <= 0) return 1;
  var exp = Math.floor(Math.log10(range));
  var f = range / Math.pow(10, exp);
  var nf;
  if (round) nf = f < 1.5 ? 1 : f < 3 ? 2 : f < 7 ? 5 : 10;
  else nf = f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10;
  return nf * Math.pow(10, exp);
}

function niceScale(min, max, ticks) {
  if (!isFinite(min) || !isFinite(max)) return { lo: 0, hi: 1, step: 1, dec: 0 };
  if (min === max) {
    if (min === 0) return { lo: 0, hi: 1, step: 0.5, dec: 1 };
    min = Math.min(0, min); max = Math.max(0, max);
    if (min === max) max = min + 1;
  }
  var step = niceStep(niceStep(max - min, false) / Math.max(1, ticks - 1), true);
  var lo = Math.floor(min / step) * step;
  var hi = Math.ceil(max / step) * step;
  // ticks are labelled to the precision of the step, not of the source data
  var dec = step >= 1 ? 0 : Math.min(6, Math.ceil(-Math.log10(step)));
  return { lo: lo, hi: hi, step: step, dec: dec };
}

/* ============================================================
   Exports for the node harness
   ============================================================ */

if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    parseDelimited: parseDelimited, detectDelimiter: detectDelimiter,
    buildTable: buildTable, normaliseHeader: normaliseHeader,
    parseNumber: parseNumber, probeDate: probeDate, dateFromProbe: dateFromProbe,
    inferColumn: inferColumn, parseColumn: parseColumn, summarise: summarise,
    median: median, niceScale: niceScale, fmtNumber: fmtNumber, fmtDate: fmtDate,
    decimalsOf: decimalsOf
  };
}

/* ============================================================
   DOM — everything below this line needs a browser
   ============================================================ */

if (typeof document !== 'undefined') (function () {

  var $ = function (id) { return document.getElementById(id); };

  var el = {
    loader: $('loader'), drop: $('drop'), file: $('file'), btnChoose: $('btnChoose'),
    btnSample: $('btnSample'), pasteBox: $('pasteBox'), btnParse: $('btnParse'),
    fileBar: $('fileBar'), fileName: $('fileName'), fileMeta: $('fileMeta'),
    btnAnother: $('btnAnother'), btnClear: $('btnClear'),
    status: $('status'), issues: $('issues'), result: $('result'),
    cards: $('cards'),
    chartX: $('chartX'), chartY: $('chartY'), chartAgg: $('chartAgg'),
    kindBar: $('kindBar'), kindLine: $('kindLine'), chartNote: $('chartNote'),
    chartWrap: $('chartWrap'), chartHost: $('chartHost'), tip: $('tip'),
    chartData: $('chartData'),
    filter: $('filter'), tableCount: $('tableCount'),
    gridHead: $('gridHead'), gridBody: $('gridBody'),
    btnMore: $('btnMore'), capNote: $('capNote')
  };

  var PAGE = 500;
  var MAX_BYTES = 25 * 1024 * 1024;
  var LS_KEY = 'sheet.v1';

  var state = {
    name: '', size: 0, delimName: '',
    header: [], rows: [], cols: [], search: [],
    sortCol: -1, sortDir: 0, filter: '', shown: PAGE, view: [],
    chart: { x: -1, y: -1, agg: 'sum', kind: 'bar' },
    series: null
  };

  /* ---------- storage ---------- */

  function lsSet(obj) {
    try { localStorage.setItem(LS_KEY, JSON.stringify(obj)); } catch (e) { /* private mode */ }
  }
  function lsGet() {
    try {
      var raw = localStorage.getItem(LS_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (e) { return null; }
  }
  function lsClear() {
    try { localStorage.removeItem(LS_KEY); } catch (e) { /* ignore */ }
  }
  function persist(text) {
    var prev = lsGet();
    var payload = {
      name: state.name,
      overrides: state.cols.map(function (c) { return c.override || null; }),
      chart: state.chart
    };
    if (text !== undefined && text !== null) {
      // only small files ride in localStorage; bigger ones just aren't restored
      if (text.length <= 400000) payload.text = text;
    } else if (prev && prev.name === state.name && prev.text) {
      payload.text = prev.text;
    }
    lsSet(payload);
  }

  /* ---------- helpers ---------- */

  function esc(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return c === '&' ? '&amp;' : c === '<' ? '&lt;' : c === '>' ? '&gt;' : '&quot;';
    });
  }

  // an ISO column has no day/month question to answer, so it doesn't claim one
  function typeLabel(c) {
    if (c.meta.type !== 'date') return c.meta.type;
    if (!c.meta.dateSlashCount) return 'date';
    return c.meta.dateOrder === 'mdy' ? 'date m/d/y' : 'date d/m/y';
  }

  function say(msg) { el.status.textContent = msg; }

  /* ---------- load ---------- */

  function loadText(text, name, size) {
    if (typeof text !== 'string') text = String(text);
    if (!text.trim()) {
      reset();
      say('0 bytes of content. Nothing to parse.');
      return;
    }
    if (/[\u0000\u0001\u0002\u0003\u0004\u0005\u0006\u0007\u0008\u000b\u000c\u000e\u000f]/.test(text.slice(0, 8192))) {
      reset();
      say('Not text — this file contains binary control bytes. Load a CSV.');
      return;
    }

    var det = detectDelimiter(text);
    var parsed = parseDelimited(text, det.delim.ch);
    var table = buildTable(parsed);

    state.name = name || 'pasted text';
    state.size = size === undefined ? text.length : size;
    state.delimName = det.delim.name;
    state.header = table.header;
    state.rows = table.rows;
    state.sortCol = -1; state.sortDir = 0; state.filter = ''; state.shown = PAGE;
    el.filter.value = '';

    if (table.empty) {
      reset();
      say('No rows found. The file parsed to nothing.');
      return;
    }

    buildColumns();
    buildSearchIndex();

    var restored = lsGet();
    if (restored && restored.name === state.name && restored.overrides) {
      for (var i = 0; i < state.cols.length && i < restored.overrides.length; i++) {
        if (restored.overrides[i]) applyOverride(i, restored.overrides[i], true);
      }
    }

    // unhide first: the chart measures its container, which is 0px while hidden
    el.result.hidden = false;
    el.loader.hidden = true;
    el.fileBar.hidden = false;

    renderIssues(det, parsed, table, text);
    renderFileBar();
    renderCards();
    renderHead();
    setupChartControls(restored && restored.name === state.name ? restored.chart : null);
    applyView();

    var counts = plural(state.rows.length, 'row') + ' · ' + plural(state.header.length, 'column') + '.';
    say(state.rows.length ? counts : '0 rows · ' + plural(state.header.length, 'column') + '. Header only.');

    persist(text);
  }

  function buildColumns() {
    var cols = [], i, r;
    for (i = 0; i < state.header.length; i++) {
      var values = new Array(state.rows.length);
      for (r = 0; r < state.rows.length; r++) values[r] = state.rows[r][i];
      var meta = inferColumn(values);
      cols.push({
        name: state.header[i], values: values, meta: meta, inferred: meta.type,
        parsed: parseColumn(values, meta), override: null,
        stats: null
      });
      cols[i].stats = summarise(values, cols[i].parsed, meta);
    }
    state.cols = cols;
  }

  function buildSearchIndex() {
    // joined on a control char so a filter cannot match across a cell boundary
    var idx = new Array(state.rows.length);
    for (var r = 0; r < state.rows.length; r++) idx[r] = state.rows[r].join('\u0001').toLowerCase();
    state.search = idx;
  }

  function applyOverride(i, kind, quiet) {
    var col = state.cols[i];
    col.override = kind;
    var meta;
    if (kind === 'number') {
      var dec = 0;
      for (var k = 0; k < col.values.length; k++) {
        if (parseNumber(col.values[k]) !== null) dec = Math.max(dec, decimalsOf(col.values[k]));
      }
      meta = { type: 'number', blanks: col.meta.blanks, dec: dec, dateOrder: 'dmy' };
    } else if (kind === 'date-dmy' || kind === 'date-mdy') {
      var res = dateOrderFor(col.values, kind === 'date-mdy' ? 'mdy' : 'dmy');
      meta = {
        type: 'date', blanks: col.meta.blanks, dateOrder: res.order,
        dateAmbiguous: false, dateConflict: false, dateSlashCount: res.slashCount, dec: 0
      };
    } else if (kind === 'boolean') {
      meta = { type: 'boolean', blanks: col.meta.blanks, dateOrder: 'dmy', dec: 0 };
    } else if (kind === 'auto') {
      meta = inferColumn(col.values);
      col.override = null;
    } else {
      meta = { type: 'text', blanks: col.meta.blanks, dateOrder: 'dmy', dec: 0 };
    }
    col.meta = meta;
    col.parsed = parseColumn(col.values, meta);
    col.stats = summarise(col.values, col.parsed, meta);
    if (!quiet) {
      renderCards();
      renderHead();
      setupChartControls(state.chart);
      applyView();
      persist();
    }
  }

  function reset() {
    state.header = []; state.rows = []; state.cols = []; state.search = [];
    state.sortCol = -1; state.sortDir = 0; state.filter = ''; state.shown = PAGE;
    state.series = null;
    el.result.hidden = true;
    el.loader.hidden = false;
    el.fileBar.hidden = true;
    el.issues.hidden = true;
    el.issues.innerHTML = '';
    el.gridBody.innerHTML = '';
    el.gridHead.innerHTML = '';
    el.cards.innerHTML = '';
  }

  /* ---------- issues ---------- */

  function renderIssues(det, parsed, table, text) {
    var msgs = [], i;

    if (!det.found) {
      var head = text.slice(0, 400).trim();
      if (head.charAt(0) === '{' || head.charAt(0) === '[') {
        msgs.push('No delimiter found. This looks like JSON, not CSV — parsed as one column.');
      } else if (head.charAt(0) === '<') {
        msgs.push('No delimiter found. This looks like markup, not CSV — parsed as one column.');
      } else {
        msgs.push('No delimiter found. Parsed as a single column.');
      }
    }
    for (i = 0; i < parsed.warnings.length; i++) {
      msgs.push('Line ' + parsed.warnings[i].line + ': ' + parsed.warnings[i].msg + ' Parsed to end of file.');
    }
    if (table.issues.length) {
      var shown = table.issues.slice(0, 5).map(function (x) {
        return 'Line ' + x.line + ': ' + x.got + ' fields, expected ' + x.want + '.';
      });
      msgs.push(plural(table.issues.length, 'ragged row') + ' — padded or trimmed to the header width. ' + shown.join(' ') +
        (table.issues.length > 5 ? ' (+' + (table.issues.length - 5) + ' more)' : ''));
    }
    if (table.blankLines) msgs.push(plural(table.blankLines, 'blank line') + ' skipped.');
    if (state.rows.length === 0 && state.header.length) msgs.push('Header only — no data rows.');

    if (!msgs.length) { el.issues.hidden = true; el.issues.innerHTML = ''; return; }
    el.issues.innerHTML = msgs.map(function (m) { return '<p>' + esc(m) + '</p>'; }).join('');
    el.issues.hidden = false;
  }

  function renderFileBar() {
    el.fileName.textContent = state.name;
    el.fileMeta.textContent = fmtBytes(state.size) + ' · ' + state.delimName + '-delimited';
  }

  /* ---------- column cards ---------- */

  function kv(label, value, mono) {
    return '<div class="kv-row"><dt>' + esc(label) + '</dt><dd' + (mono === false ? '' : ' class="mono"') + '>' + esc(value) + '</dd></div>';
  }

  function renderCards() {
    var html = [], i;
    for (i = 0; i < state.cols.length; i++) {
      var c = state.cols[i], s = c.stats, body = '';

      if (s.type === 'number') {
        if (!s.count) {
          body = '<p class="empty">No numeric values.</p>';
        } else {
          body = '<dl class="kv">' +
            kv('Count', fmtNumber(s.count, 0)) +
            kv('Min', fmtNumber(s.min, s.dec)) +
            kv('Max', fmtNumber(s.max, s.dec)) +
            kv('Mean', fmtNumber(s.mean, Math.max(s.dec, 2))) +
            kv('Median', fmtNumber(s.median, Math.max(s.dec, 2))) +
            kv('Sum', fmtNumber(s.sum, s.dec)) +
            '</dl>';
        }
      } else if (s.type === 'date') {
        if (!s.count) {
          body = '<p class="empty">No parseable dates.</p>';
        } else {
          body = '<dl class="kv">' +
            kv('Count', fmtNumber(s.count, 0)) +
            kv('Earliest', fmtDate(s.min)) +
            kv('Latest', fmtDate(s.max)) +
            kv('Span', fmtSpan(s.spanDays)) +
            '</dl>';
        }
      } else if (s.type === 'boolean') {
        body = '<dl class="kv">' +
          kv('True', fmtNumber(s.trueCount, 0)) +
          kv('False', fmtNumber(s.falseCount, 0)) +
          '</dl>';
      } else {
        var top = '';
        if (s.distinct === s.count && s.count > 1) {
          top = '<p class="col-note">All ' + fmtNumber(s.count, 0) + ' values distinct.</p>';
        } else if (s.top && s.top.length) {
          top = '<dl class="kv top">' + s.top.map(function (t) {
            return '<div class="kv-row"><dt class="tv">' + esc(t.value.length > 34 ? t.value.slice(0, 33) + '…' : t.value) +
              '</dt><dd class="mono">' + fmtNumber(t.n, 0) + '</dd></div>';
          }).join('') + '</dl>';
        }
        body = '<dl class="kv">' +
          kv('Values', fmtNumber(s.count, 0)) +
          kv('Distinct', fmtNumber(s.distinct, 0)) +
          '</dl>' + top;
      }

      var notes = [];
      if (s.blanks) notes.push(plural(s.blanks, 'blank') + '.');
      if (c.meta.dateAmbiguous) notes.push('Day-first assumed — every value fits either order.');
      if (c.meta.dateConflict) notes.push('Mixed day/month order in the source — day-first used.');
      if (c.meta.dateUnparsed) notes.push(plural(c.meta.dateUnparsed, 'value') + ' did not parse under this order.');
      if (c.override) notes.push('Type set by hand.');

      html.push(
        '<div class="card col-card">' +
        '<div class="col-head">' +
        '<span class="col-name">' + esc(c.name) + '</span>' +
        '<span class="chip">' + esc(typeLabel(c)) + '</span>' +
        '</div>' +
        body +
        (notes.length ? '<p class="col-note">' + esc(notes.join(' ')) + '</p>' : '') +
        '<div class="col-set">' +
        '<label for="type' + i + '">Type</label>' +
        '<select id="type' + i + '" class="type-sel" data-i="' + i + '">' +
        '<option value="auto"' + (c.override ? '' : ' selected') + '>Auto (' + esc(c.inferred) + ')</option>' +
        '<option value="number"' + (c.override === 'number' ? ' selected' : '') + '>Number</option>' +
        '<option value="date-dmy"' + (c.override === 'date-dmy' ? ' selected' : '') + '>Date D/M/Y</option>' +
        '<option value="date-mdy"' + (c.override === 'date-mdy' ? ' selected' : '') + '>Date M/D/Y</option>' +
        '<option value="boolean"' + (c.override === 'boolean' ? ' selected' : '') + '>Boolean</option>' +
        '<option value="text"' + (c.override === 'text' ? ' selected' : '') + '>Text</option>' +
        '</select>' +
        '</div>' +
        '</div>'
      );
    }
    el.cards.innerHTML = html.join('');
  }

  el.cards.addEventListener('change', function (e) {
    var t = e.target;
    if (!t.classList.contains('type-sel')) return;
    applyOverride(Number(t.getAttribute('data-i')), t.value, false);
  });

  /* ---------- table ---------- */

  function renderHead() {
    var html = '<tr>';
    for (var i = 0; i < state.cols.length; i++) {
      var c = state.cols[i];
      var sorted = state.sortCol === i && state.sortDir !== 0;
      var dir = sorted ? (state.sortDir === 1 ? 'ascending' : 'descending') : 'none';
      var sub = typeLabel(c) + (sorted ? ' · ' + (state.sortDir === 1 ? 'asc' : 'desc') : '');
      html += '<th scope="col" aria-sort="' + dir + '" class="' + (c.meta.type === 'number' ? 'num' : '') + '">' +
        '<button type="button" class="th-btn' + (sorted ? ' sorted' : '') + '" data-i="' + i + '">' +
        '<span class="th-name">' + esc(c.name) + '</span>' +
        '<span class="th-type">' + esc(sub) + '</span>' +
        '</button></th>';
    }
    el.gridHead.innerHTML = html + '</tr>';
  }

  el.gridHead.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('.th-btn') : null;
    if (!btn) return;
    var i = Number(btn.getAttribute('data-i'));
    if (state.sortCol === i) {
      state.sortDir = state.sortDir === 1 ? -1 : state.sortDir === -1 ? 0 : 1;
      if (state.sortDir === 0) state.sortCol = -1;
    } else {
      state.sortCol = i; state.sortDir = 1;
    }
    state.shown = PAGE;
    renderHead();
    applyView();
  });

  function applyView() {
    var idx = [], i;
    var q = state.filter.trim().toLowerCase();
    if (q) {
      for (i = 0; i < state.rows.length; i++) if (state.search[i].indexOf(q) >= 0) idx.push(i);
    } else {
      for (i = 0; i < state.rows.length; i++) idx.push(i);
    }

    if (state.sortCol >= 0 && state.sortDir !== 0) {
      var col = state.cols[state.sortCol], dir = state.sortDir;
      var t = col.meta.type;
      if (t === 'number' || t === 'date') {
        idx.sort(function (a, b) {
          var va = col.parsed[a], vb = col.parsed[b];
          if (va === null && vb === null) return a - b;
          if (va === null) return 1;
          if (vb === null) return -1;
          return va === vb ? a - b : (va < vb ? -dir : dir);
        });
      } else if (t === 'boolean') {
        idx.sort(function (a, b) {
          var va = col.parsed[a], vb = col.parsed[b];
          if (va === null && vb === null) return a - b;
          if (va === null) return 1;
          if (vb === null) return -1;
          return va === vb ? a - b : (va < vb ? -dir : dir);
        });
      } else {
        idx.sort(function (a, b) {
          var va = String(col.values[a]).trim(), vb = String(col.values[b]).trim();
          if (!va && !vb) return a - b;
          if (!va) return 1;
          if (!vb) return -1;
          var c = va.localeCompare(vb, 'en', { sensitivity: 'base', numeric: true });
          return c === 0 ? a - b : c * dir;
        });
      }
    }

    state.view = idx;
    renderBody();
    renderChart();
  }

  function renderBody() {
    var idx = state.view;
    var limit = Math.min(state.shown, idx.length);
    var cols = state.cols;
    var parts = [], r, i;

    for (r = 0; r < limit; r++) {
      var row = state.rows[idx[r]];
      var tds = '<tr>';
      for (i = 0; i < cols.length; i++) {
        var t = cols[i].meta.type;
        var cls = t === 'number' ? ' class="num"' : (t === 'date' || t === 'boolean' ? ' class="mono"' : '');
        tds += '<td' + cls + '>' + esc(row[i] === undefined ? '' : row[i]) + '</td>';
      }
      parts.push(tds + '</tr>');
    }
    el.gridBody.innerHTML = parts.join('');

    var total = state.rows.length;
    if (!total) {
      el.tableCount.textContent = '0 rows.';
    } else if (state.filter.trim()) {
      el.tableCount.textContent = fmtNumber(idx.length, 0) + ' of ' + fmtNumber(total, 0) + ' rows match.';
    } else {
      el.tableCount.textContent = plural(total, 'row') + ' · ' + plural(state.header.length, 'column') + '.';
    }

    if (idx.length > limit) {
      el.btnMore.hidden = false;
      el.capNote.textContent = 'Showing the first ' + fmtNumber(limit, 0) + ' of ' + fmtNumber(idx.length, 0) +
        ' rows. Rendering is capped so large files stay responsive.';
    } else {
      el.btnMore.hidden = true;
      el.capNote.textContent = idx.length > PAGE ? 'All ' + fmtNumber(idx.length, 0) + ' rows rendered.' : '';
    }
  }

  el.btnMore.addEventListener('click', function () {
    state.shown += PAGE;
    renderBody();
  });

  var filterTimer = null;
  el.filter.addEventListener('input', function () {
    if (filterTimer) clearTimeout(filterTimer);
    var run = function () {
      state.filter = el.filter.value;
      state.shown = PAGE;
      applyView();
    };
    if (state.rows.length > 5000) filterTimer = setTimeout(run, 120);
    else run();
  });

  /* ---------- chart ---------- */

  /* A column of four-digit integers in a plausible year range is a label, not a
     measure — it shouldn't be the default thing we add up. */
  function yearLike(col) {
    var p = col.parsed, seen = 0, i;
    for (i = 0; i < p.length; i++) {
      if (p[i] === null) continue;
      if (p[i] !== Math.round(p[i]) || p[i] < 1500 || p[i] > 2200) return false;
      seen++;
    }
    return seen > 0;
  }

  function setupChartControls(saved) {
    var i, opts = '';
    for (i = 0; i < state.cols.length; i++) opts += '<option value="' + i + '">' + esc(state.cols[i].name) + '</option>';
    el.chartX.innerHTML = opts;

    var yOpts = '';
    for (i = 0; i < state.cols.length; i++) {
      if (state.cols[i].meta.type === 'number') yOpts += '<option value="' + i + '">' + esc(state.cols[i].name) + '</option>';
    }
    el.chartY.innerHTML = yOpts || '<option value="-1">No numeric column</option>';

    var x = -1, y = -1;
    if (saved && typeof saved.x === 'number' && saved.x < state.cols.length) x = saved.x;
    if (saved && typeof saved.y === 'number' && saved.y < state.cols.length) y = saved.y;

    if (x < 0) {
      for (i = 0; i < state.cols.length; i++) if (state.cols[i].meta.type === 'date') { x = i; break; }
    }
    if (x < 0) {
      for (i = 0; i < state.cols.length; i++) {
        var s = state.cols[i].stats;
        if (state.cols[i].meta.type === 'text' && s.distinct > 1 && s.distinct <= 60) { x = i; break; }
      }
    }
    if (x < 0) x = 0;

    if (y < 0 || state.cols[y].meta.type !== 'number') {
      y = -1;
      var firstNum = -1;
      for (i = 0; i < state.cols.length; i++) {
        if (state.cols[i].meta.type !== 'number') continue;
        if (firstNum < 0) firstNum = i;
        if (y < 0 && !yearLike(state.cols[i])) y = i;
      }
      if (y < 0) y = firstNum;
    }

    state.chart.x = x;
    state.chart.y = y;
    // summing a column of years says nothing; count the rows instead
    var measurable = y >= 0 && !yearLike(state.cols[y]);
    state.chart.agg = (saved && saved.agg) || (measurable ? 'sum' : 'count');
    state.chart.kind = (saved && saved.kind) || (state.cols[x] && state.cols[x].meta.type === 'date' ? 'line' : 'bar');

    el.chartX.value = String(x);
    if (y >= 0) el.chartY.value = String(y);
    el.chartY.disabled = y < 0 || state.chart.agg === 'count';
    el.chartAgg.value = state.chart.agg;
    setKind(state.chart.kind, true);
  }

  function setKind(kind, quiet) {
    state.chart.kind = kind;
    el.kindBar.setAttribute('aria-pressed', kind === 'bar' ? 'true' : 'false');
    el.kindLine.setAttribute('aria-pressed', kind === 'line' ? 'true' : 'false');
    if (!quiet) { renderChart(); persist(); }
  }

  el.kindBar.addEventListener('click', function () { setKind('bar'); });
  el.kindLine.addEventListener('click', function () { setKind('line'); });
  el.chartX.addEventListener('change', function () { state.chart.x = Number(el.chartX.value); renderChart(); persist(); });
  el.chartY.addEventListener('change', function () { state.chart.y = Number(el.chartY.value); renderChart(); persist(); });
  el.chartAgg.addEventListener('change', function () {
    state.chart.agg = el.chartAgg.value;
    el.chartY.disabled = state.chart.agg === 'count' || state.chart.y < 0;
    renderChart(); persist();
  });

  function bucketDate(ms, unit) {
    var d = new Date(ms);
    if (unit === 'year') return Date.UTC(d.getUTCFullYear(), 0, 1);
    if (unit === 'month') return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1);
    return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  }

  function labelDate(ms, unit) {
    var d = new Date(ms);
    if (unit === 'year') return String(d.getUTCFullYear());
    if (unit === 'month') return d.getUTCFullYear() + '-' + pad2(d.getUTCMonth() + 1);
    return fmtDate(ms);
  }

  /* Aggregate the filtered rows into at most a few hundred plottable points,
     and record in words whatever reduction was applied. */
  function buildSeries() {
    var xi = state.chart.x, yi = state.chart.y, agg = state.chart.agg;
    if (xi < 0 || !state.cols[xi]) return null;
    var xc = state.cols[xi];
    var yc = yi >= 0 ? state.cols[yi] : null;
    if (agg !== 'count' && !yc) {
      return { points: [], notes: [], problem: 'No numeric column to take the ' + agg + ' of. Switch Aggregate to Count.' };
    }

    var idx = state.view, notes = [];
    if (state.filter.trim()) notes.push('Filtered to ' + fmtNumber(idx.length, 0) + ' of ' + fmtNumber(state.rows.length, 0) + ' rows.');

    var groups = Object.create(null), order = [], i, r, key;
    var xType = xc.meta.type;
    var unit = null, bins = null;

    if (xType === 'date') {
      var distinctDays = Object.create(null), dayCount = 0;
      for (i = 0; i < idx.length; i++) {
        var v = xc.parsed[idx[i]];
        if (v === null) continue;
        var dk = bucketDate(v, 'day');
        if (!distinctDays[dk]) { distinctDays[dk] = 1; dayCount++; }
      }
      unit = 'day';
      if (dayCount > 60) {
        var months = Object.create(null), mCount = 0;
        for (key in distinctDays) { var mk = bucketDate(Number(key), 'month'); if (!months[mk]) { months[mk] = 1; mCount++; } }
        unit = mCount > 60 ? 'year' : 'month';
      }
      if (unit !== 'day') notes.push('Bucketed by ' + unit + '.');
    } else if (xType === 'number') {
      var distinctVals = Object.create(null), dCount = 0, lo = Infinity, hi = -Infinity;
      for (i = 0; i < idx.length; i++) {
        var nv = xc.parsed[idx[i]];
        if (nv === null) continue;
        if (!distinctVals[nv]) { distinctVals[nv] = 1; dCount++; }
        if (nv < lo) lo = nv;
        if (nv > hi) hi = nv;
      }
      if (dCount > 200 && hi > lo) {
        bins = { lo: lo, hi: hi, n: 40, w: (hi - lo) / 40 };
        notes.push('Binned into 40 equal ranges over ' + fmtNumber(lo, xc.meta.dec) + ' to ' + fmtNumber(hi, xc.meta.dec) + '.');
      }
    }

    for (i = 0; i < idx.length; i++) {
      r = idx[i];
      var raw = xc.values[r];
      var sortKey, label;

      if (xType === 'date') {
        var dv = xc.parsed[r];
        if (dv === null) continue;
        sortKey = bucketDate(dv, unit);
        label = labelDate(sortKey, unit);
      } else if (xType === 'number') {
        var xv = xc.parsed[r];
        if (xv === null) continue;
        if (bins) {
          var b = Math.min(bins.n - 1, Math.floor((xv - bins.lo) / bins.w));
          sortKey = bins.lo + b * bins.w;
          label = fmtNumber(sortKey, xc.meta.dec) + '–' + fmtNumber(sortKey + bins.w, xc.meta.dec);
        } else {
          sortKey = xv;
          label = fmtNumber(xv, xc.meta.dec);
        }
      } else {
        label = String(raw === undefined || raw === null ? '' : raw).trim();
        if (!label) label = '(blank)';
        sortKey = label;
      }

      var g = groups[sortKey];
      if (!g) { g = groups[sortKey] = { key: sortKey, label: label, sum: 0, n: 0 }; order.push(g); }
      g.n++;
      if (yc) {
        var yv = yc.parsed[r];
        if (yv !== null) g.sum += yv;
      }
    }

    if (!order.length) return { points: [], notes: notes, xType: xType, yLabel: '' };

    var points = order.map(function (g) {
      var value = agg === 'count' ? g.n : (agg === 'mean' ? (g.n ? g.sum / g.n : 0) : g.sum);
      return { key: g.key, label: g.label, value: value, n: g.n };
    });

    var categorical = xType !== 'date' && xType !== 'number';
    if (categorical || (xType === 'number' && !bins && points.length > 200)) {
      points.sort(function (a, b) { return b.value - a.value || (a.label < b.label ? -1 : 1); });
      if (points.length > 20) {
        notes.push('Top 20 of ' + fmtNumber(points.length, 0) + ' categories by ' + aggWord() + '.');
        points = points.slice(0, 20);
      }
    } else {
      points.sort(function (a, b) { return a.key - b.key; });
      if (points.length > 400) {
        notes.push('Showing the first 400 of ' + fmtNumber(points.length, 0) + ' points.');
        points = points.slice(0, 400);
      }
    }

    var yLabel = agg === 'count' ? 'Row count' :
      (agg === 'mean' ? 'Mean of ' : 'Sum of ') + (yc ? yc.name : '');

    var dec = agg === 'count' ? 0 : (yc ? Math.max(yc.meta.dec, agg === 'mean' ? 2 : 0) : 0);
    return { points: points, notes: notes, xType: xType, yLabel: yLabel, dec: dec, xName: xc.name };
  }

  function aggWord() {
    return state.chart.agg === 'count' ? 'row count' : state.chart.agg;
  }

  function fmtAxis(v, dec) {
    var a = Math.abs(v);
    if (a >= 1e9) return (v / 1e9).toFixed(a >= 1e10 ? 0 : 1) + 'B';
    if (a >= 1e6) return (v / 1e6).toFixed(a >= 1e7 ? 0 : 1) + 'M';
    if (a >= 1e4) return (v / 1e3).toFixed(a >= 1e5 ? 0 : 1) + 'k';
    return fmtNumber(v, dec);
  }

  function truncLabel(s, max) {
    return s.length > max ? s.slice(0, max - 1) + '…' : s;
  }

  var chartGeom = null;

  function renderChart() {
    if (!state.cols.length) return;
    var series = buildSeries();
    state.series = series;
    chartGeom = null;
    el.tip.hidden = true;

    if (!series || !series.points.length) {
      el.chartHost.innerHTML = '';
      el.chartNote.textContent = !series ? 'Pick an X column.' :
        (series.problem || 'Nothing to plot from these columns.');
      el.chartData.innerHTML = '';
      return;
    }

    var pts = series.points;
    var W = Math.max(320, el.chartHost.clientWidth || 700);
    var H = W < 520 ? 240 : 320;
    var rotate = series.xType !== 'date' && series.xType !== 'number';
    var padL = 62, padR = 14, padT = 14, padB = rotate ? 78 : 44;
    var plotW = W - padL - padR;
    var plotH = H - padT - padB;

    var lo = Infinity, hi = -Infinity, i;
    for (i = 0; i < pts.length; i++) {
      if (pts[i].value < lo) lo = pts[i].value;
      if (pts[i].value > hi) hi = pts[i].value;
    }
    if (state.chart.kind === 'bar') { lo = Math.min(0, lo); hi = Math.max(0, hi); }
    else if (lo > 0 && lo / (hi || 1) > 0.35) { /* keep a zoomed baseline for lines */ }
    else { lo = Math.min(0, lo); hi = Math.max(0, hi); }

    var sc = niceScale(lo, hi, 5);
    var yOf = function (v) { return padT + plotH - ((v - sc.lo) / (sc.hi - sc.lo)) * plotH; };
    var band = plotW / pts.length;
    var xOf = function (i2) { return padL + band * (i2 + 0.5); };

    var svg = [];
    svg.push('<svg viewBox="0 0 ' + W + ' ' + H + '" width="' + W + '" height="' + H + '" role="img" aria-label="' +
      esc(series.yLabel + ' by ' + series.xName + ', ' + pts.length + ' points') + '">');

    for (var t = sc.lo; t <= sc.hi + sc.step / 2; t += sc.step) {
      var y = yOf(t);
      svg.push('<line class="grid" x1="' + padL + '" y1="' + y.toFixed(1) + '" x2="' + (W - padR) + '" y2="' + y.toFixed(1) + '"/>');
      svg.push('<text class="ytick" x="' + (padL - 8) + '" y="' + (y + 4).toFixed(1) + '" text-anchor="end">' + esc(fmtAxis(t, sc.dec)) + '</text>');
    }

    if (sc.lo < 0 && sc.hi > 0) {
      svg.push('<line class="zero" x1="' + padL + '" y1="' + yOf(0).toFixed(1) + '" x2="' + (W - padR) + '" y2="' + yOf(0).toFixed(1) + '"/>');
    }

    if (state.chart.kind === 'bar') {
      var bw = Math.max(1, Math.min(band * 0.72, 46));
      var base = yOf(Math.max(sc.lo, Math.min(0, sc.hi)));
      for (i = 0; i < pts.length; i++) {
        var vy = yOf(pts[i].value);
        var top = Math.min(vy, base), h = Math.max(1, Math.abs(base - vy));
        svg.push('<rect class="bar" x="' + (xOf(i) - bw / 2).toFixed(1) + '" y="' + top.toFixed(1) +
          '" width="' + bw.toFixed(1) + '" height="' + h.toFixed(1) + '"/>');
      }
    } else {
      var d = '';
      for (i = 0; i < pts.length; i++) d += (i ? 'L' : 'M') + xOf(i).toFixed(1) + ' ' + yOf(pts[i].value).toFixed(1) + ' ';
      svg.push('<path class="line" d="' + d.trim() + '"/>');
      if (pts.length <= 60) {
        for (i = 0; i < pts.length; i++) {
          svg.push('<circle class="dot" cx="' + xOf(i).toFixed(1) + '" cy="' + yOf(pts[i].value).toFixed(1) + '" r="2.5"/>');
        }
      }
    }

    var maxLabels = Math.max(2, Math.floor(plotW / (rotate ? 26 : 74)));
    var every = Math.ceil(pts.length / maxLabels);
    for (i = 0; i < pts.length; i++) {
      if (i % every !== 0 && i !== pts.length - 1) continue;
      var lx = xOf(i);
      if (rotate) {
        svg.push('<text class="xtick" x="' + lx.toFixed(1) + '" y="' + (padT + plotH + 12) +
          '" text-anchor="end" transform="rotate(-40 ' + lx.toFixed(1) + ' ' + (padT + plotH + 12) + ')">' +
          esc(truncLabel(pts[i].label, 16)) + '</text>');
      } else {
        svg.push('<text class="xtick" x="' + lx.toFixed(1) + '" y="' + (padT + plotH + 18) + '" text-anchor="middle">' +
          esc(truncLabel(pts[i].label, 12)) + '</text>');
      }
    }

    svg.push('<line class="guide" id="guide" x1="0" y1="' + padT + '" x2="0" y2="' + (padT + plotH) + '" style="display:none"/>');
    svg.push('<rect id="hit" x="' + padL + '" y="' + padT + '" width="' + plotW + '" height="' + plotH + '" fill="transparent"/>');
    svg.push('</svg>');

    el.chartHost.innerHTML = svg.join('');
    chartGeom = { padL: padL, padT: padT, plotW: plotW, plotH: plotH, band: band, n: pts.length, yOf: yOf, xOf: xOf, dec: series.dec };

    el.chartNote.textContent = (series.yLabel + ' by ' + series.xName + '. ' + series.notes.join(' ')).trim();
    renderChartTable(series);
  }

  function renderChartTable(series) {
    var rows = series.points.map(function (p) {
      return '<tr><td>' + esc(p.label) + '</td><td class="num">' + esc(fmtNumber(p.value, series.dec)) +
        '</td><td class="num">' + esc(fmtNumber(p.n, 0)) + '</td></tr>';
    }).join('');
    el.chartData.innerHTML = '<table><thead><tr><th scope="col">' + esc(series.xName) +
      '</th><th scope="col" class="num">' + esc(series.yLabel) + '</th><th scope="col" class="num">Rows</th></tr></thead><tbody>' +
      rows + '</tbody></table>';
  }

  function moveTip(e) {
    if (!chartGeom || !state.series || !state.series.points.length) return;
    var svg = el.chartHost.firstChild;
    if (!svg) return;
    var rect = svg.getBoundingClientRect();
    var scale = rect.width ? (svg.viewBox.baseVal.width / rect.width) : 1;
    var x = (e.clientX - rect.left) * scale;
    var i = Math.floor((x - chartGeom.padL) / chartGeom.band);
    if (i < 0 || i >= chartGeom.n) { hideTip(); return; }
    var p = state.series.points[i];
    var gx = chartGeom.xOf(i);

    var guide = document.getElementById('guide');
    if (guide) {
      guide.setAttribute('x1', gx.toFixed(1));
      guide.setAttribute('x2', gx.toFixed(1));
      guide.style.display = '';
    }

    el.tip.innerHTML = '<span class="tip-label">' + esc(p.label) + '</span>' +
      '<span class="tip-value mono">' + esc(fmtNumber(p.value, chartGeom.dec)) + '</span>' +
      '<span class="tip-sub">' + esc(plural(p.n, 'row')) + '</span>';
    el.tip.hidden = false;

    var wrapRect = el.chartWrap.getBoundingClientRect();
    var px = rect.left - wrapRect.left + gx / scale;
    var tipW = el.tip.offsetWidth;
    var left = Math.max(4, Math.min(wrapRect.width - tipW - 4, px - tipW / 2));
    el.tip.style.left = left + 'px';
    el.tip.style.top = Math.max(4, (e.clientY - wrapRect.top) - el.tip.offsetHeight - 14) + 'px';
  }

  function hideTip() {
    el.tip.hidden = true;
    var guide = document.getElementById('guide');
    if (guide) guide.style.display = 'none';
  }

  el.chartHost.addEventListener('pointermove', moveTip);
  el.chartHost.addEventListener('pointerdown', moveTip);
  el.chartHost.addEventListener('pointerleave', hideTip);

  var resizeTimer = null;
  window.addEventListener('resize', function () {
    if (!state.cols.length) return;
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(renderChart, 150);
  });

  /* ---------- input plumbing ---------- */

  function readFile(f) {
    if (!f) return;
    if (f.size > MAX_BYTES) {
      say('File is ' + fmtBytes(f.size) + '. The limit is ' + fmtBytes(MAX_BYTES) + '.');
      return;
    }
    say('Reading ' + f.name + '…');
    var reader = new FileReader();
    reader.onerror = function () { say('Could not read that file.'); };
    reader.onload = function () {
      try {
        loadText(String(reader.result), f.name, f.size);
      } catch (err) {
        say('Parse failed: ' + (err && err.message ? err.message : 'unknown error') + '.');
      }
    };
    reader.readAsText(f, 'utf-8');
  }

  el.btnChoose.addEventListener('click', function () { el.file.click(); });
  el.file.addEventListener('change', function () { readFile(el.file.files && el.file.files[0]); el.file.value = ''; });

  el.btnParse.addEventListener('click', function () {
    var t = el.pasteBox.value;
    if (!t.trim()) { say('Nothing pasted.'); return; }
    loadText(t, 'pasted text', t.length);
  });

  el.btnAnother.addEventListener('click', function () {
    el.loader.hidden = false;
    el.fileBar.hidden = true;
    el.drop.scrollIntoView({ block: 'nearest' });
  });

  el.btnClear.addEventListener('click', function () {
    lsClear();
    state.name = '';
    reset();
    el.pasteBox.value = '';
    say('Nothing loaded yet.');
  });

  var dragDepth = 0;
  function dragOff() {
    dragDepth = 0;
    el.drop.classList.remove('over');
    document.body.classList.remove('dragging');
  }
  window.addEventListener('dragenter', function (e) {
    e.preventDefault();
    dragDepth++;
    el.drop.classList.add('over');
    document.body.classList.add('dragging');
  });
  window.addEventListener('dragover', function (e) { e.preventDefault(); });
  window.addEventListener('dragleave', function () {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) dragOff();
  });
  window.addEventListener('drop', function (e) {
    e.preventDefault();
    dragOff();
    var dt = e.dataTransfer;
    if (!dt) return;
    if (dt.files && dt.files.length) { readFile(dt.files[0]); return; }
    var txt = dt.getData ? dt.getData('text/plain') : '';
    if (txt) loadText(txt, 'dropped text', txt.length);
  });

  document.addEventListener('paste', function (e) {
    var t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
    var txt = e.clipboardData && e.clipboardData.getData('text/plain');
    if (!txt || !txt.trim()) return;
    e.preventDefault();
    loadText(txt, 'pasted text', txt.length);
  });

  var SAMPLE = 'Date,Title,Year,Rating,Rewatch\n' +
    '2026-01-04,"Dune: Part Two",2024,4.5,No\n' +
    '2026-01-11,Perfect Days,2023,5,No\n' +
    '2026-02-02,"The Zone of Interest",2023,4,No\n' +
    '2026-02-14,Paddington 2,2017,5,Yes\n' +
    '2026-03-03,Anatomy of a Fall,2023,4.5,No\n' +
    '2026-03-21,"Killers of the Flower Moon",2023,3.5,No\n' +
    '2026-04-08,Past Lives,2023,4.5,Yes\n' +
    '2026-05-19,"Poor Things",2023,4,No\n' +
    '2026-06-02,La Chimera,2023,3.5,No\n' +
    '2026-07-01,"Hundreds of Beavers",2024,4.5,No\n';

  el.btnSample.addEventListener('click', function () { loadText(SAMPLE, 'sample.csv', SAMPLE.length); });

  /* ---------- boot ---------- */

  (function boot() {
    var saved = lsGet();
    if (saved && saved.text) {
      try {
        loadText(saved.text, saved.name || 'restored.csv', saved.text.length);
        return;
      } catch (e) { lsClear(); }
    }
    say('Nothing loaded yet.');
  }());

}());
