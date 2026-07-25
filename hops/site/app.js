/* hops — traceroute / mtr / tracert reader. No network, no deps. */

/* ==== parser:start ====
   Everything between these markers is pure logic with no DOM access, so the
   node test harness can slice it out of this file and exercise the shipped
   code rather than a copy of it. */

const TIME_TOKEN = /^\d+(?:\.\d+)?$/;

function isIPv4(s) {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s);
  if (!m) return false;
  for (let i = 1; i <= 4; i++) if (Number(m[i]) > 255) return false;
  return true;
}

function isIPv6(s) {
  const base = String(s).split('%')[0];
  if (!/^[0-9a-fA-F:.]+$/.test(base)) return false;
  // Every valid v6 literal has at least two colons ("::" being the shortest).
  return (base.match(/:/g) || []).length >= 2;
}

function isAddress(s) {
  return isIPv4(s) || isIPv6(s);
}

function scope(key, label) {
  return { key: key, label: label };
}

function classifyV4(a) {
  const o = a.split('.').map(Number);
  if (o[0] === 127) return scope('loopback', 'Loopback');
  if (o[0] === 10) return scope('private', 'Private (RFC1918)');
  if (o[0] === 172 && o[1] >= 16 && o[1] <= 31) return scope('private', 'Private (RFC1918)');
  if (o[0] === 192 && o[1] === 168) return scope('private', 'Private (RFC1918)');
  if (o[0] === 100 && o[1] >= 64 && o[1] <= 127) return scope('cgnat', 'CGNAT (RFC6598)');
  if (o[0] === 169 && o[1] === 254) return scope('linklocal', 'Link-local');
  if (o[0] === 0) return scope('reserved', 'Unspecified');
  if (o[0] >= 224 && o[0] <= 239) return scope('multicast', 'Multicast');
  return scope('public', 'Public');
}

function classifyV6(a) {
  if (a === '::1') return scope('loopback', 'Loopback');
  if (a === '::') return scope('reserved', 'Unspecified');
  const head = a.split(':')[0];
  const g = head === '' ? 0 : parseInt(head, 16);
  if (!isFinite(g)) return scope('unknown', 'Unknown');
  if (g >= 0xfe80 && g <= 0xfebf) return scope('linklocal', 'Link-local');
  if (g >= 0xfc00 && g <= 0xfdff) return scope('ula', 'Unique local (ULA)');
  if (g >= 0xff00) return scope('multicast', 'Multicast');
  if (g >= 0x2000 && g <= 0x3fff) return scope('public', 'Public');
  return scope('reserved', 'Reserved');
}

function classifyAddress(addr) {
  if (!addr) return scope('unknown', 'Unknown');
  const a = String(addr).split('%')[0].toLowerCase();
  if (isIPv4(a)) return classifyV4(a);
  if (!isIPv6(a)) return scope('unknown', 'Unknown');
  // ::ffff:1.2.3.4 is an IPv4 host wearing a v6 hat; classify by the v4 part.
  const mapped = /^::ffff:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})$/.exec(a);
  if (mapped && isIPv4(mapped[1])) return classifyV4(mapped[1]);
  return classifyV6(a);
}

function newHop(num) {
  return {
    num: num,
    hosts: [],
    probes: [],
    annotations: [],
    note: null,
    sent: null,
    last: null,
    statsFromFormat: false,
    stats: { best: null, avg: null, worst: null, loss: null }
  };
}

function tidyHosts(hop) {
  const out = [];
  const seen = Object.create(null);
  for (const e of hop.hosts) {
    let host = e.host;
    let addr = e.addr;
    if (addr == null && host != null && isAddress(host)) { addr = host; host = null; }
    if (host != null && addr != null && host === addr) host = null;
    if (host == null && addr == null) continue;
    const key = (host || '') + '|' + (addr || '');
    if (seen[key]) continue;
    seen[key] = true;
    out.push({ host: host, addr: addr });
  }
  hop.hosts = out;
}

/* Stats from individual probes. Missing probes (`*`) are excluded from
   best/avg/worst entirely — averaging them in as 0 would understate the hop
   and averaging them as "worst" would invent a number nothing measured. */
function computeStats(hop) {
  if (hop.statsFromFormat) return;
  const got = hop.probes.filter((p) => p.ms != null).map((p) => p.ms);
  if (got.length) {
    let sum = 0;
    let best = Infinity;
    let worst = -Infinity;
    for (const v of got) {
      sum += v;
      if (v < best) best = v;
      if (v > worst) worst = v;
    }
    hop.stats.best = best;
    hop.stats.worst = worst;
    hop.stats.avg = sum / got.length;
  }
  if (hop.probes.length) {
    hop.stats.loss = ((hop.probes.length - got.length) / hop.probes.length) * 100;
    hop.sent = hop.probes.length;
  }
}

function finishHop(hop) {
  tidyHosts(hop);
  computeStats(hop);
  hop.replied = hop.stats.avg != null;
  hop.timeout = !hop.replied;
  hop.addr = hop.hosts.length ? hop.hosts[0].addr : null;
  hop.host = hop.hosts.length ? hop.hosts[0].host : null;
  hop.scope = classifyAddress(hop.addr);
  return hop;
}

/* ---------- traceroute (BSD / macOS / Linux) ---------- */

function tracerouteHopBody(hop, rest) {
  const toks = rest.trim().split(/\s+/).filter(Boolean);
  let i = 0;
  let pending = null;
  while (i < toks.length) {
    const t = toks[i];
    if (t === '*') {
      hop.probes.push({ ms: null, label: 'No reply' });
      i++;
      continue;
    }
    if (/^![\w<>.]*$/.test(t)) {
      hop.annotations.push(t);
      i++;
      continue;
    }
    // traceroute -A / mtr -z tack the origin AS on as [AS1221]; it is an
    // annotation, not another router that answered.
    if (/^\[[^\]]*\]$/.test(t)) {
      hop.annotations.push(t);
      i++;
      continue;
    }
    if (TIME_TOKEN.test(t) && /^ms$/i.test(toks[i + 1] || '')) {
      hop.probes.push({ ms: parseFloat(t), label: t + ' ms' });
      pending = null;
      i += 2;
      continue;
    }
    const glued = /^(\d+(?:\.\d+)?)ms$/i.exec(t);
    if (glued) {
      hop.probes.push({ ms: parseFloat(glued[1]), label: glued[1] + ' ms' });
      pending = null;
      i++;
      continue;
    }
    const paren = /^\(([^)]*)\)$/.exec(t);
    if (paren) {
      if (pending && pending.addr == null) pending.addr = paren[1];
      else hop.hosts.push({ host: null, addr: paren[1] });
      pending = null;
      i++;
      continue;
    }
    const entry = { host: t.replace(/,$/, ''), addr: null };
    hop.hosts.push(entry);
    pending = entry;
    i++;
  }
}

function parseTraceroute(text) {
  const hops = [];
  let current = null;
  for (const raw of text.split(/\r?\n/)) {
    if (!raw.trim()) continue;
    const head = /^\s*(\d{1,3})\s+(.*)$/.exec(raw);
    if (head) {
      current = newHop(Number(head[1]));
      tracerouteHopBody(current, head[2]);
      hops.push(current);
      continue;
    }
    // Continuation line: an extra host/probe for the hop above, indented and
    // carrying no hop number of its own.
    if (current && /^\s+\S/.test(raw) && (/\d\s*ms\b/i.test(raw) || /^\s*\*/.test(raw))) {
      tracerouteHopBody(current, raw);
    }
  }
  return hops.map(finishHop);
}

/* ---------- Windows tracert ---------- */

const TRACERT_PROBE = /^\s*(?:(\*)|(<)?\s*(\d+(?:\.\d+)?)\s*ms)(?=\s|$)/;

function parseTracert(text) {
  const hops = [];
  for (const raw of text.split(/\r?\n/)) {
    const head = /^\s*(\d{1,3})\s+(.*\S)\s*$/.exec(raw);
    if (!head) continue;
    let rest = head[2];
    const hop = newHop(Number(head[1]));
    let m;
    while (hop.probes.length < 12 && (m = TRACERT_PROBE.exec(rest))) {
      if (m[1]) hop.probes.push({ ms: null, label: 'No reply' });
      else if (m[2]) {
        // tracert prints "<1 ms" for anything sub-millisecond; 0.5 is the
        // midpoint of the bucket it actually reported.
        hop.probes.push({ ms: 0.5, label: '<1 ms' });
      } else {
        hop.probes.push({ ms: parseFloat(m[3]), label: m[3] + ' ms' });
      }
      rest = rest.slice(m[0].length);
    }
    if (!hop.probes.length) continue;
    rest = rest.trim();
    if (rest) {
      const reports = /^(.*?)\s+reports:\s*(.*)$/i.exec(rest);
      if (reports) {
        hop.note = reports[2];
        rest = reports[1].trim();
      }
      const bracket = /^(.+?)\s+\[([^\]]+)\]$/.exec(rest);
      if (bracket) hop.hosts.push({ host: bracket[1], addr: bracket[2] });
      else if (isAddress(rest)) hop.hosts.push({ host: null, addr: rest });
      else if (/^[A-Za-z0-9_](?:[A-Za-z0-9_.-]*)$/.test(rest)) hop.hosts.push({ host: rest, addr: null });
      else if (!hop.note) hop.note = rest.replace(/\.$/, '') + '.';
    }
    hops.push(hop);
  }
  return hops.map(finishHop);
}

/* ---------- mtr --report ---------- */

const MTR_HOP = /^\s*(\d{1,3})\.\|--\s+(.+?)\s+(\d+(?:\.\d+)?)%\s+(.*)$/;

function parseMtr(text) {
  const hops = [];
  for (const raw of text.split(/\r?\n/)) {
    const m = MTR_HOP.exec(raw);
    if (!m) continue;
    const hop = newHop(Number(m[1]));
    const loss = parseFloat(m[3]);
    const nums = m[4].trim().split(/\s+/).filter((t) => /^-?\d+(?:\.\d+)?$/.test(t)).map(Number);

    const hostField = m[2].trim();
    if (hostField !== '???' && hostField !== '?') {
      const pair = /^(\S+)\s+\(([^)]+)\)$/.exec(hostField);
      if (pair) hop.hosts.push({ host: pair[1], addr: pair[2] });
      else hop.hosts.push({ host: hostField, addr: null });
    }

    hop.statsFromFormat = true;
    hop.sent = nums.length > 0 ? nums[0] : null;
    // mtr prints 0.0 across the timing columns for a hop that never answered;
    // those zeros are absence, not a sub-millisecond hop.
    const answered = loss < 100 && nums.length >= 5;
    hop.last = answered ? nums[1] : null;
    hop.stats.avg = answered ? nums[2] : null;
    hop.stats.best = answered ? nums[3] : null;
    hop.stats.worst = answered ? nums[4] : null;
    hop.stdev = answered && nums.length >= 6 ? nums[5] : null;
    hop.stats.loss = loss;
    if (!answered) hop.note = 'No reply';
    hops.push(hop);
  }
  return hops.map(finishHop);
}

/* ---------- format detection ---------- */

function tracerouteFlavor(text) {
  if (/(^|\s)_gateway(\s|$)/m.test(text)) return 'Linux';
  const hdr = /,\s*(\d+)\s+hops max,\s*(\d+)\s+byte packets/.exec(text);
  if (hdr) {
    const maxHops = Number(hdr[1]);
    const bytes = Number(hdr[2]);
    if (bytes === 52 || bytes === 12) return 'BSD/macOS';
    if (bytes === 60 || bytes === 80) return 'Linux';
    if (maxHops === 64) return 'BSD/macOS';
    if (maxHops === 30) return 'Linux';
  }
  if (/^traceroute6 to /m.test(text)) return 'BSD/macOS';
  return 'Unix';
}

function detectFormat(text) {
  if (!text || !text.trim()) return { kind: 'empty', label: 'none' };

  let mtrLines = 0;
  let tracertLines = 0;
  let tracerouteLines = 0;

  for (const raw of text.split(/\r?\n/)) {
    if (MTR_HOP.test(raw)) { mtrLines++; continue; }
    const head = /^\s*(\d{1,3})\s+(.*\S)\s*$/.exec(raw);
    if (!head) continue;
    const rest = head[2];
    // The discriminator is what follows the hop number: tracert leads with the
    // probe times, traceroute leads with the host.
    if (TRACERT_PROBE.test(rest) && !/^\s*\*/.test(rest)) tracertLines++;
    else if (/\d\s*ms\b/i.test(rest) || /^\*(\s+\*)*\s*$/.test(rest.trim())) tracerouteLines++;
  }

  let score = { mtr: mtrLines * 3, tracert: tracertLines * 3, traceroute: tracerouteLines * 3 };
  if (/Loss%\s+Snt/i.test(text)) score.mtr += 6;
  if (/^\s*Start:/m.test(text)) score.mtr += 2;
  if (/^Tracing route to /im.test(text)) score.tracert += 6;
  if (/Request timed out\./i.test(text)) score.tracert += 4;
  if (/over a maximum of \d+ hops/i.test(text)) score.tracert += 4;
  if (/^traceroute6? to /m.test(text)) score.traceroute += 6;

  const best = Object.keys(score).reduce((a, b) => (score[b] > score[a] ? b : a), 'traceroute');
  if (score[best] === 0) return { kind: 'unknown', label: 'unrecognized' };
  if (best === 'mtr') return { kind: 'mtr', label: 'mtr --report' };
  if (best === 'tracert') return { kind: 'tracert', label: 'tracert (Windows)' };
  return { kind: 'traceroute', label: 'traceroute (' + tracerouteFlavor(text) + ')' };
}

function parseTrace(text) {
  const fmt = detectFormat(text);
  let hops = [];
  if (fmt.kind === 'mtr') hops = parseMtr(text);
  else if (fmt.kind === 'tracert') hops = parseTracert(text);
  else if (fmt.kind === 'traceroute') hops = parseTraceroute(text);
  return { format: fmt, hops: hops };
}

/* ---------- derived callouts ---------- */

function analyze(hops) {
  const replying = hops.filter((h) => h.stats.avg != null);
  const maxAvg = replying.reduce((m, h) => Math.max(m, h.stats.avg), 0);

  // Compare each responding hop with the previous responding one, so a
  // timeout in the middle doesn't hide the jump on either side of it.
  let jump = null;
  for (let i = 1; i < replying.length; i++) {
    const delta = replying[i].stats.avg - replying[i - 1].stats.avg;
    if (delta > 0 && (!jump || delta > jump.delta)) {
      jump = { from: replying[i - 1].num, to: replying[i].num, delta: delta };
    }
  }

  const lossy = hops
    .filter((h) => h.stats.loss != null && h.stats.loss > 0)
    .map((h) => ({ num: h.num, loss: h.stats.loss }));

  let firstPublic = null;
  for (const h of hops) {
    if (h.scope.key === 'public') { firstPublic = h.num; break; }
  }

  return {
    count: hops.length,
    silent: hops.filter((h) => h.timeout).length,
    maxAvg: maxAvg,
    jump: jump,
    lossy: lossy,
    firstPublic: firstPublic
  };
}

/* ==== parser:end ==== */

/* ---------- samples ---------- */

const SAMPLES = {
  bsd: [
    'traceroute to one.one.one.one (1.1.1.1), 64 hops max, 52 byte packets',
    ' 1  router.lan (192.168.0.1)  1.234 ms  1.111 ms  1.222 ms',
    ' 2  100.82.0.1 (100.82.0.1)  8.451 ms  7.998 ms  8.220 ms',
    ' 3  * * *',
    ' 4  bundle-ether1.exi-edge901.melbourne.telstra.net (203.50.11.98)  12.114 ms  11.902 ms  12.401 ms',
    ' 5  bundle-ether12.exi-core10.melbourne.telstra.net (203.50.11.122)  13.002 ms  12.884 ms  13.551 ms',
    ' 6  203.50.13.98 (203.50.13.98)  194.221 ms  193.887 ms  195.043 ms',
    ' 7  i-92.sgpl-core02.telstraglobal.net (202.84.247.18)  196.554 ms * 197.221 ms',
    ' 8  one.one.one.one (1.1.1.1)  196.101 ms  195.774 ms  196.332 ms'
  ].join('\n'),

  v6: [
    'traceroute to ipv6.google.com (2404:6800:4006:80e::200e), 30 hops max, 80 byte packets',
    ' 1  fe80::1%eth0 (fe80::1%eth0)  0.612 ms  0.588 ms  0.541 ms',
    ' 2  fd7a:115c:a1e0::1 (fd7a:115c:a1e0::1)  1.902 ms  1.844 ms  1.877 ms',
    ' 3  2001:44b8:3168:1::1 (2001:44b8:3168:1::1)  9.331 ms  9.102 ms  9.554 ms',
    ' 4  * * *',
    ' 5  2001:4860:1:1::1 (2001:4860:1:1::1)  11.208 ms  11.114 ms  11.402 ms',
    ' 6  2404:6800:8000::1  12.881 ms  12.744 ms  13.010 ms',
    ' 7  syd15s16-in-x0e.1e100.net (2404:6800:4006:80e::200e)  12.451 ms  12.388 ms  12.502 ms'
  ].join('\n'),

  mtr: [
    'Start: 2026-07-25T09:41:02+1000',
    'HOST: nas.lan                                          Loss%   Snt   Last   Avg  Best  Wrst StDev',
    '  1.|-- router.lan (192.168.0.1)                        0.0%    10    1.1   1.3   1.0   2.4   0.4',
    '  2.|-- 100.82.0.1                                      0.0%    10    8.4   8.6   8.1   9.9   0.5',
    '  3.|-- ???                                           100.0%    10    0.0   0.0   0.0   0.0   0.0',
    '  4.|-- exi-edge901.melbourne (203.50.11.98)            0.0%    10   12.2  12.4  11.9  14.1   0.6',
    '  5.|-- 203.50.13.98                                   10.0%    10  194.3 194.7 193.6 199.2   1.7',
    '  6.|-- one.one.one.one (1.1.1.1)                       0.0%    10  196.0 196.2 195.5 198.8   0.9'
  ].join('\n'),

  tracert: [
    'Tracing route to one.one.one.one [1.1.1.1]',
    'over a maximum of 30 hops:',
    '',
    '  1    <1 ms    <1 ms    <1 ms  router.lan [192.168.0.1]',
    '  2     8 ms     8 ms     9 ms  100.82.0.1',
    '  3     *        *        *     Request timed out.',
    '  4    12 ms    12 ms    13 ms  bundle-ether1.exi-edge901.melbourne.telstra.net [203.50.11.98]',
    '  5   194 ms   194 ms   195 ms  203.50.13.98',
    '  6     *      196 ms   196 ms  one.one.one.one [1.1.1.1]',
    '',
    'Trace complete.'
  ].join('\n')
};

/* ---------- formatting ---------- */

/* One decimal from 10 ms up, two below it: three probes 196.554 / 196.887 /
   197.221 ms have to stay distinguishable, and a 0.5 ms LAN hop needs the
   second digit to mean anything. */
function fmtNum(v) {
  if (v == null || !isFinite(v)) return '—';
  return v >= 10 ? v.toFixed(1) : v.toFixed(2);
}

function fmtLoss(v) {
  if (v == null) return '—';
  return (Math.round(v * 10) / 10).toFixed(1) + '%';
}

/* ---------- DOM ---------- */

const els = {
  src: document.getElementById('src'),
  detect: document.getElementById('detect'),
  callouts: document.getElementById('callouts'),
  tbody: document.getElementById('hop-rows'),
  table: document.getElementById('hop-table'),
  empty: document.getElementById('empty'),
  clear: document.getElementById('btn-clear')
};

const STORE_KEY = 'hops.input.v1';

function save(text) {
  try {
    localStorage.setItem(STORE_KEY, text);
  } catch (e) {
    /* private mode: state just doesn't survive the tab */
  }
}

function load() {
  try {
    return localStorage.getItem(STORE_KEY);
  } catch (e) {
    return null;
  }
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}

function cell(label, cls, children) {
  const td = el('td', cls);
  td.setAttribute('data-label', label);
  // Everything lives in one wrapper so the narrow-screen `td { display: grid }`
  // sees exactly two children: the ::before label and this body.
  const body = el('div', 'cell-body');
  for (const c of children || []) {
    if (c == null) continue;
    body.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  td.appendChild(body);
  return td;
}

function hostCell(hop) {
  const kids = [];
  const primary = hop.hosts[0] || null;
  if (primary && primary.host) kids.push(el('div', 'host-name', primary.host));
  else if (!primary) kids.push(el('div', 'host-name muted-line', hop.note || 'No reply'));
  if (primary && primary.addr) kids.push(el('div', 'host-addr num', primary.addr));
  // Scope comes from the address, so a hostname-only hop (plain mtr --report)
  // gets no tag rather than a row of "Unknown".
  if (primary && primary.addr) {
    const wrap = el('div', 'scope-wrap');
    wrap.appendChild(el('span', 'scope-tag', hop.scope.label));
    kids.push(wrap);
  }
  // Extra hosts show up when the probes for one hop come back from different routers.
  for (let i = 1; i < hop.hosts.length; i++) {
    const alt = hop.hosts[i];
    kids.push(el('div', 'host-alt num', 'also ' + (alt.host || alt.addr) + (alt.host && alt.addr ? ' (' + alt.addr + ')' : '')));
  }
  if (hop.annotations.length) kids.push(el('div', 'host-alt num', hop.annotations.join(' ')));
  if (primary && hop.note) kids.push(el('div', 'host-alt', hop.note));
  return cell('Host', null, kids);
}

function probeCell(hop) {
  const kids = [];
  if (hop.statsFromFormat) {
    kids.push(el('div', 'probe num', hop.last != null ? 'Last ' + fmtNum(hop.last) + ' ms' : 'No reply'));
    if (hop.sent != null) kids.push(el('div', 'probe-sent num', hop.sent + ' sent'));
  } else if (!hop.probes.length) {
    kids.push(el('div', 'muted-line', 'No probes'));
  } else {
    for (const p of hop.probes) {
      kids.push(el('div', p.ms == null ? 'probe probe-lost' : 'probe num', p.label));
    }
  }
  return cell('Probes', 'probes', kids);
}

function lossCell(hop) {
  const v = hop.stats.loss;
  if (v == null) return cell('Loss', 'num', ['\u2014']);
  if (v <= 0) return cell('Loss', 'num', [el('span', 'loss-none', '0.0%')]);
  // Color never carries the meaning on its own — the word "lost" rides along.
  return cell('Loss', 'num', [el('span', v >= 100 ? 'loss-bad' : 'loss-warn', fmtLoss(v) + ' lost')]);
}

function barCell(hop, maxAvg) {
  if (hop.stats.avg == null) return cell('Latency', 'bar-cell', [el('div', 'muted-line', 'No reply')]);
  const pct = maxAvg > 0 ? (hop.stats.avg / maxAvg) * 100 : 0;
  const track = el('div', 'bar');
  track.setAttribute('aria-hidden', 'true'); // the number is already in the Avg column
  const fill = el('div', 'bar-fill');
  fill.style.width = Math.max(1.5, Math.min(100, pct)).toFixed(2) + '%';
  track.appendChild(fill);
  return cell('Latency', 'bar-cell', [track]);
}

function renderRows(hops, maxAvg) {
  els.tbody.textContent = '';
  for (const hop of hops) {
    const tr = el('tr', hop.timeout ? 'row-silent' : null);
    tr.appendChild(cell('Hop', 'num hop-num', [String(hop.num)]));
    tr.appendChild(hostCell(hop));
    tr.appendChild(probeCell(hop));
    tr.appendChild(cell('Best (ms)', 'num', [fmtNum(hop.stats.best)]));
    tr.appendChild(cell('Avg (ms)', 'num strong', [fmtNum(hop.stats.avg)]));
    tr.appendChild(cell('Worst (ms)', 'num', [fmtNum(hop.stats.worst)]));
    tr.appendChild(lossCell(hop));
    tr.appendChild(barCell(hop, maxAvg));
    els.tbody.appendChild(tr);
  }
}

function line(parts) {
  const li = document.createElement('li');
  for (const p of parts) {
    if (typeof p === 'string') li.appendChild(document.createTextNode(p));
    else li.appendChild(el('span', p.cls || 'num', p.text));
  }
  return li;
}

function renderCallouts(hops, info) {
  els.callouts.textContent = '';

  const hopLine = [{ text: String(info.count) }, info.count === 1 ? ' hop parsed. ' : ' hops parsed. '];
  if (info.silent === 0) hopLine.push('All replied.');
  else hopLine.push({ text: String(info.silent) }, ' returned no reply.');
  els.callouts.appendChild(line(hopLine));

  if (info.jump) {
    els.callouts.appendChild(line([
      'Largest jump: hop ',
      { text: String(info.jump.from) },
      ' → ',
      { text: String(info.jump.to) },
      ', ',
      { text: '+' + info.jump.delta.toFixed(1) + ' ms' },
      '.'
    ]));
  } else {
    els.callouts.appendChild(line(['No latency increase between consecutive responding hops.']));
  }

  if (info.lossy.length) {
    const parts = ['Packet loss: '];
    info.lossy.forEach((h, i) => {
      if (i) parts.push(', ');
      parts.push('hop ', { text: String(h.num) }, ' (', { text: fmtLoss(h.loss) }, ')');
    });
    parts.push('.');
    els.callouts.appendChild(line(parts));
  } else {
    els.callouts.appendChild(line(['No packet loss on any hop.']));
  }

  if (info.firstPublic != null) {
    els.callouts.appendChild(line(['First public address at hop ', { text: String(info.firstPublic) }, '.']));
  } else {
    els.callouts.appendChild(line(['No public address in this path.']));
  }

  if (info.maxAvg > 0) {
    els.callouts.appendChild(line(['Bars scaled to the slowest hop, ', { text: fmtNum(info.maxAvg) + ' ms' }, '.']));
  }
}

function showEmpty(message) {
  els.empty.textContent = message;
  els.empty.hidden = false;
  els.table.hidden = true;
  els.callouts.textContent = '';
  els.tbody.textContent = '';
}

function render() {
  const text = els.src.value;
  save(text);

  if (!text.trim()) {
    els.detect.textContent = 'Nothing pasted yet.';
    showEmpty('Nothing pasted yet.');
    return;
  }

  const result = parseTrace(text);

  if (result.format.kind === 'unknown') {
    els.detect.textContent = 'Unrecognized format. Expected traceroute, mtr --report or Windows tracert output.';
    showEmpty('0 hops.');
    return;
  }

  if (!result.hops.length) {
    els.detect.textContent = 'Detected: ' + result.format.label + '. No hop lines found.';
    showEmpty('0 hops.');
    return;
  }

  const info = analyze(result.hops);
  els.detect.textContent = 'Detected: ' + result.format.label + '. ' + info.count + (info.count === 1 ? ' hop.' : ' hops.');
  els.empty.hidden = true;
  els.table.hidden = false;
  renderCallouts(result.hops, info);
  renderRows(result.hops, info.maxAvg);
}

function setInput(text) {
  els.src.value = text;
  render();
}

document.querySelectorAll('[data-sample]').forEach((btn) => {
  btn.addEventListener('click', () => setInput(SAMPLES[btn.getAttribute('data-sample')]));
});

els.clear.addEventListener('click', () => {
  setInput('');
  els.src.focus();
});

els.src.addEventListener('input', render);

const stored = load();
if (stored != null) {
  els.src.value = stored;
} else {
  // First visit: land on something readable rather than a blank grid.
  els.src.value = SAMPLES.bsd;
}
render();
