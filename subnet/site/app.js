'use strict';

/* ------------------------------------------------------------------ *
 * Address math. Everything is BigInt: IPv6 needs it, and IPv4 host
 * counts share the same code paths when they use it too.
 * ------------------------------------------------------------------ */

var BITS = { 4: 32, 6: 128 };
var FULL = { 4: (1n << 32n) - 1n, 6: (1n << 128n) - 1n };
var SPLIT_CAP = 256;

function parseV4Addr(text) {
  var parts = text.split('.');
  if (parts.length !== 4) {
    return { err: 'needs 4 dot-separated octets, got ' + parts.length };
  }
  var v = 0n;
  for (var i = 0; i < 4; i++) {
    var p = parts[i];
    if (!/^[0-9]{1,3}$/.test(p)) {
      return { err: 'octet ' + (i + 1) + ' ("' + (p || '') + '") is not a number' };
    }
    if (p.length > 1 && p[0] === '0') {
      return { err: 'octet ' + (i + 1) + ' has a leading zero' };
    }
    var n = Number(p);
    if (n > 255) return { err: 'octet ' + (i + 1) + ' is ' + n + ', above 255' };
    v = (v << 8n) | BigInt(n);
  }
  return { v: v };
}

function parseV6Addr(text) {
  var str = text;

  // A dotted tail means an embedded IPv4 form (::ffff:192.168.0.1).
  // Rewrite it into two hex groups so the rest of the parser is uniform.
  if (str.indexOf('.') !== -1) {
    var cut = str.lastIndexOf(':');
    if (cut === -1) return { err: 'looks like IPv4 but has no colon' };
    var tail = parseV4Addr(str.slice(cut + 1));
    if (tail.err) return { err: 'embedded IPv4 tail: ' + tail.err };
    str = str.slice(0, cut + 1) +
      (tail.v >> 16n).toString(16) + ':' + (tail.v & 0xffffn).toString(16);
  }

  var halves = str.split('::');
  if (halves.length > 2) return { err: 'more than one "::"' };

  var head, mid;
  if (halves.length === 2) {
    head = halves[0] === '' ? [] : halves[0].split(':');
    mid = halves[1] === '' ? [] : halves[1].split(':');
    if (head.length + mid.length > 7) {
      return { err: '"::" must stand in for at least one group' };
    }
  } else {
    head = str.split(':');
    mid = [];
    if (head.length !== 8) {
      return { err: 'needs 8 groups, got ' + head.length + ' (use "::" to elide zeros)' };
    }
  }

  var groups = head.slice();
  var fill = 8 - head.length - mid.length;
  for (var f = 0; f < fill; f++) groups.push('0');
  groups = groups.concat(mid);

  var v = 0n;
  for (var i = 0; i < 8; i++) {
    var g = groups[i];
    if (!/^[0-9a-fA-F]{1,4}$/.test(g)) {
      return { err: 'group ' + (i + 1) + ' ("' + g + '") is not 1-4 hex digits' };
    }
    v = (v << 16n) | BigInt(parseInt(g, 16));
  }
  return { v: v };
}

function parseInput(raw) {
  var text = String(raw == null ? '' : raw).replace(/\s+/g, '');
  if (!text) return { ok: false, empty: true, error: 'Nothing entered.' };

  var bits = text.split('/');
  if (bits.length > 2) return { ok: false, error: 'Too many slashes: ' + text };

  var addrText = bits[0];
  var lenText = bits.length === 2 ? bits[1] : null;
  if (!addrText) return { ok: false, error: 'No address before the slash.' };

  var version = addrText.indexOf(':') !== -1 ? 6 : 4;
  var width = BITS[version];
  var got = version === 4 ? parseV4Addr(addrText) : parseV6Addr(addrText);
  if (got.err) return { ok: false, error: 'Not an address: ' + addrText + ' (' + got.err + ').' };

  var prefix;
  if (lenText === null) {
    prefix = width;
  } else {
    if (!/^[0-9]{1,3}$/.test(lenText)) {
      return { ok: false, error: 'Not a prefix length: /' + lenText + '.' };
    }
    prefix = Number(lenText);
    if (prefix > width) {
      return {
        ok: false,
        error: 'Prefix length out of range: /' + prefix + '. IPv' + version +
          ' goes /0 to /' + width + '.'
      };
    }
  }

  var mask = maskFor(version, prefix);
  return {
    ok: true,
    version: version,
    width: width,
    addr: got.v,
    prefix: prefix,
    assumed: lenText === null,
    mask: mask,
    network: got.v & mask,
    text: addrText + '/' + prefix
  };
}

function maskFor(version, prefix) {
  if (prefix === 0) return 0n;
  return (FULL[version] << BigInt(BITS[version] - prefix)) & FULL[version];
}

function fmtV4(v) {
  return [24n, 16n, 8n, 0n].map(function (s) {
    return ((v >> s) & 255n).toString();
  }).join('.');
}

function groupsOf(v) {
  var out = [];
  for (var i = 7; i >= 0; i--) out.push(Number((v >> BigInt(i * 16)) & 0xffffn));
  return out;
}

function expandV6(v) {
  return groupsOf(v).map(function (g) {
    return g.toString(16).padStart(4, '0');
  }).join(':');
}

// RFC 5952: elide the longest run of zero groups (>= 2), leftmost on a tie.
function compressV6(v) {
  var g = groupsOf(v);
  var best = -1, bestLen = 0, start = -1, len = 0;
  for (var i = 0; i < 8; i++) {
    if (g[i] === 0) {
      if (start === -1) { start = i; len = 0; }
      len++;
      if (len > bestLen) { bestLen = len; best = start; }
    } else {
      start = -1; len = 0;
    }
  }
  var hex = g.map(function (x) { return x.toString(16); });
  if (bestLen < 2) return hex.join(':');
  return hex.slice(0, best).join(':') + '::' + hex.slice(best + bestLen).join(':');
}

function fmtAddr(version, v) {
  return version === 4 ? fmtV4(v) : compressV6(v);
}

function fmtCidr(version, v, prefix) {
  return fmtAddr(version, v) + '/' + prefix;
}

function group(n) {
  return n.toLocaleString('en-US');
}

/* Numbers past ~15 digits stop being readable; show the power of two and
   keep the exact digits for the title attribute. */
function countLabel(n) {
  return n.toString().length <= 15 ? group(n) : '2^' + exp2(n);
}

function exp2(n) {
  var e = 0;
  while (n > 1n) { n >>= 1n; e++; }
  return e;
}

/* ---------- scope classification ---------- */

var SCOPES_V4 = [
  ['0.0.0.0/8', 'This network'],
  ['10.0.0.0/8', 'Private (RFC 1918)'],
  ['100.64.0.0/10', 'CGNAT (RFC 6598)'],
  ['127.0.0.0/8', 'Loopback'],
  ['169.254.0.0/16', 'Link-local'],
  ['172.16.0.0/12', 'Private (RFC 1918)'],
  ['192.0.0.0/24', 'IETF protocol assignments'],
  ['192.0.2.0/24', 'Documentation (TEST-NET-1)'],
  ['192.88.99.0/24', '6to4 relay anycast'],
  ['192.168.0.0/16', 'Private (RFC 1918)'],
  ['198.18.0.0/15', 'Benchmarking'],
  ['198.51.100.0/24', 'Documentation (TEST-NET-2)'],
  ['203.0.113.0/24', 'Documentation (TEST-NET-3)'],
  ['224.0.0.0/4', 'Multicast'],
  ['240.0.0.0/4', 'Reserved'],
  ['255.255.255.255/32', 'Limited broadcast']
];

var SCOPES_V6 = [
  ['::/128', 'Unspecified'],
  ['::1/128', 'Loopback'],
  ['::ffff:0:0/96', 'IPv4-mapped'],
  ['64:ff9b::/96', 'NAT64 well-known'],
  ['100::/64', 'Discard-only'],
  ['2000::/3', 'Global unicast'],
  ['2001::/32', 'Teredo'],
  ['2001:db8::/32', 'Documentation'],
  ['2002::/16', '6to4'],
  ['fc00::/7', 'Unique local (ULA)'],
  ['fe80::/10', 'Link-local'],
  ['ff00::/8', 'Multicast']
];

var SCOPE_TABLE = null;

function scopeTable() {
  if (SCOPE_TABLE) return SCOPE_TABLE;
  SCOPE_TABLE = { 4: [], 6: [] };
  [[4, SCOPES_V4], [6, SCOPES_V6]].forEach(function (pair) {
    pair[1].forEach(function (row) {
      var p = parseInput(row[0]);
      SCOPE_TABLE[pair[0]].push({ network: p.network, prefix: p.prefix, label: row[1] });
    });
  });
  return SCOPE_TABLE;
}

// The most specific well-known range that fully contains the prefix wins.
function classify(version, network, prefix) {
  var rows = scopeTable()[version];
  var best = null;
  for (var i = 0; i < rows.length; i++) {
    var r = rows[i];
    if (prefix >= r.prefix && (network & maskFor(version, r.prefix)) === r.network) {
      if (!best || r.prefix > best.prefix) best = r;
    }
  }
  if (best) return best.label;
  return version === 4 ? 'Public unicast' : 'Unassigned / reserved';
}

/* ---------- facts ---------- */

function facts(p) {
  var total = 1n << BigInt(p.width - p.prefix);
  var network = p.network;
  var last = network + total - 1n;
  var out = {
    version: p.version,
    prefix: p.prefix,
    network: network,
    last: last,
    total: total,
    hostBits: p.width - p.prefix,
    scope: classify(p.version, network, p.prefix)
  };

  if (p.version === 4) {
    out.netmask = fmtV4(p.mask);
    out.wildcard = fmtV4(FULL[4] ^ p.mask);
    if (p.prefix === 32) {
      out.broadcast = null;
      out.broadcastNote = 'None. A /32 is a single host.';
      out.first = network;
      out.lastUsable = network;
      out.usable = 1n;
      out.usableNote = '1 address, and it is the host.';
      out.usableShort = 'The address is the host';
    } else if (p.prefix === 31) {
      out.broadcast = null;
      out.broadcastNote = 'None. A /31 has no broadcast address (RFC 3021).';
      out.first = network;
      out.lastUsable = last;
      out.usable = 0n;
      out.usableNote = '0 by the classic network+broadcast rule; RFC 3021 makes both addresses usable on a point-to-point link.';
      out.usableShort = 'RFC 3021 makes both usable';
    } else {
      out.broadcast = last;
      out.first = network + 1n;
      out.lastUsable = last - 1n;
      out.usable = total - 2n;
      out.usableShort = 'Network and broadcast excluded';
    }
  } else {
    out.expanded = expandV6(network);
    out.expandedLast = expandV6(last);
    out.first = network;
    out.lastUsable = last;
    if (p.prefix <= 64) {
      out.sixtyFours = 1n << BigInt(64 - p.prefix);
    } else {
      out.sixtyFours = 0n;
      out.sixtyFourNote = 'None. A /' + p.prefix + ' sits inside a single /64.';
    }
  }
  return out;
}

/* ---------- split ---------- */

function splitPrefix(p, newLen, cap) {
  if (newLen <= p.prefix || newLen > p.width) return null;
  var count = 1n << BigInt(newLen - p.prefix);
  var step = 1n << BigInt(p.width - newLen);
  var limit = BigInt(cap);
  var shown = count < limit ? count : limit;
  var rows = [];
  for (var i = 0n; i < shown; i++) {
    var start = p.network + i * step;
    rows.push({ i: i, network: start, last: start + step - 1n });
  }
  return { count: count, size: step, rows: rows, shown: shown };
}

/* ---------- containment ---------- */

function containment(p, q) {
  if (q.version !== p.version) {
    return {
      inside: false,
      line: 'No. ' + q.text + ' is IPv' + q.version + '; the prefix is IPv' + p.version + '.',
      why: 'The two address families never overlap.'
    };
  }
  var baseMask = maskFor(p.version, p.prefix);
  var landsOn = q.network & baseMask;
  var bitsMatch = landsOn === p.network;
  var label = q.prefix === q.width ? fmtAddr(q.version, q.addr) : fmtCidr(q.version, q.network, q.prefix);
  var here = fmtCidr(p.version, p.network, p.prefix);

  if (bitsMatch && q.prefix < p.prefix) {
    return {
      inside: false,
      line: 'No. ' + label + ' is larger than ' + here + '.',
      why: 'A /' + q.prefix + ' contains the /' + p.prefix + ', not the other way around.'
    };
  }
  if (!bitsMatch) {
    return {
      inside: false,
      line: 'No. ' + label + ' is not inside ' + here + '.',
      why: 'Masked to /' + p.prefix + ' it lands on ' + fmtAddr(p.version, landsOn) +
        ', not ' + fmtAddr(p.version, p.network) + '.'
    };
  }
  var offset = q.network - p.network;
  var total = 1n << BigInt(p.width - p.prefix);
  var why;
  if (q.prefix === q.width) {
    why = 'The first ' + p.prefix + ' bits match. It is address ' + countLabel(offset) +
      ' of ' + countLabel(total) + ' (0-indexed).';
  } else {
    var qSize = 1n << BigInt(q.width - q.prefix);
    why = 'The first ' + p.prefix + ' bits match. It covers ' + countLabel(qSize) +
      ' of ' + countLabel(total) + ' addresses, starting at offset ' + countLabel(offset) + '.';
  }
  return { inside: true, line: 'Yes. ' + label + ' is inside ' + here + '.', why: why };
}

/* ---------- position ladder ---------- */

function ladder(p) {
  var step = p.version === 4 ? 8 : 16;
  var levels = [];
  var L = p.prefix;
  while (L > 0 && levels.length < 4) {
    L = Math.max(0, L - step);
    levels.push(L);
  }
  levels.reverse();
  levels.push(p.prefix);

  var rows = [];
  for (var i = 0; i < levels.length - 1; i++) {
    var parentLen = levels[i];
    var childLen = levels[i + 1];
    var parentNet = p.network & maskFor(p.version, parentLen);
    var childNet = p.network & maskFor(p.version, childLen);
    var parentSize = 1n << BigInt(p.width - parentLen);
    var childSize = 1n << BigInt(p.width - childLen);
    var offset = childNet - parentNet;
    rows.push({
      parent: fmtCidr(p.version, parentNet, parentLen),
      childLen: childLen,
      index: offset / childSize,
      count: parentSize / childSize,
      left: pct(offset, parentSize),
      width: pct(childSize, parentSize)
    });
  }
  return rows;
}

function pct(part, whole) {
  return Number((part * 1000000n) / whole) / 10000;
}

/* ------------------------------------------------------------------ *
 * UI
 * ------------------------------------------------------------------ */

var PRESETS = [
  '192.168.0.0/24',
  '10.0.0.0/8',
  '172.16.0.0/12',
  '192.168.0.0/16',
  '100.64.0.0/10',
  '169.254.0.0/16',
  '2406:da1c::/56',
  'fc00::/7',
  'fe80::/10'
];

var STORE = 'subnet.v1';
var el = {};
var state = { splitLen: null, parsed: null };

function $(id) { return document.getElementById(id); }

function load() {
  try {
    var raw = localStorage.getItem(STORE);
    return raw ? JSON.parse(raw) : null;
  } catch (e) {
    return null;
  }
}

function save() {
  try {
    localStorage.setItem(STORE, JSON.stringify({
      cidr: el.cidr.value,
      query: el.query.value
    }));
  } catch (e) { /* private mode: state just doesn't persist */ }
}

function init() {
  el.cidr = $('cidr');
  el.query = $('query');
  el.parseStatus = $('parse-status');
  el.presets = $('presets');
  el.headline = $('headline');
  el.headlineSub = $('headline-sub');
  el.stats = $('stats');
  el.facts = $('facts-body');
  el.ladder = $('ladder');
  el.queryStatus = $('query-status');
  el.queryWhy = $('query-why');
  el.splitLen = $('split-len');
  el.splitNote = $('split-note');
  el.splitBody = $('split-body');
  el.splitCol = $('split-col-count');

  PRESETS.forEach(function (v) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn preset mono';
    b.textContent = v;
    b.setAttribute('aria-pressed', 'false');
    b.addEventListener('click', function () {
      el.cidr.value = v;
      state.splitLen = null;
      render();
      save();
    });
    el.presets.appendChild(b);
  });

  var saved = load() || {};
  el.cidr.value = typeof saved.cidr === 'string' ? saved.cidr : '192.168.0.0/24';
  el.query.value = typeof saved.query === 'string' ? saved.query : '192.168.0.42';

  el.cidr.addEventListener('input', function () {
    state.splitLen = null;
    render();
    save();
  });
  el.query.addEventListener('input', function () {
    renderQuery();
    save();
  });
  el.splitLen.addEventListener('change', function () {
    state.splitLen = Number(el.splitLen.value);
    renderSplit();
  });
  el.splitBody.addEventListener('click', function (ev) {
    var b = ev.target.closest('button[data-cidr]');
    if (!b) return;
    el.cidr.value = b.getAttribute('data-cidr');
    state.splitLen = null;
    render();
    save();
    window.scrollTo({ top: 0 });
  });

  render();
}

function render() {
  var p = parseInput(el.cidr.value);
  state.parsed = p.ok ? p : null;

  Array.prototype.forEach.call(el.presets.children, function (b) {
    var on = p.ok && b.textContent === fmtCidr(p.version, p.network, p.prefix);
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
  });

  if (!p.ok) {
    el.parseStatus.textContent = p.error;
    el.parseStatus.className = 'status ' + (p.empty ? 'muted' : 'bad');
    blank();
    renderQuery();
    return;
  }

  var notes = [];
  if (p.assumed) notes.push('No prefix length given, assumed /' + p.prefix + '.');
  if (p.addr !== p.network) {
    notes.push('Host bits are set: ' + fmtAddr(p.version, p.addr) +
      ' is a host inside ' + fmtCidr(p.version, p.network, p.prefix) + '.');
  }
  el.parseStatus.textContent = notes.length
    ? notes.join(' ')
    : 'Parsed ' + fmtCidr(p.version, p.network, p.prefix) + '.';
  el.parseStatus.className = 'status ' + (notes.length ? 'note' : 'ok');

  var f = facts(p);
  el.headline.textContent = fmtCidr(p.version, p.network, p.prefix);
  el.headlineSub.textContent = 'IPv' + p.version + ' · ' + f.scope + ' · ' +
    f.hostBits + ' host bits';

  renderStats(p, f);
  renderFacts(p, f);
  renderLadder(p);
  renderSplitOptions(p);
  renderSplit();
  renderQuery();
}

function blank() {
  el.headline.textContent = '—';
  el.headlineSub.textContent = 'Nothing parsed yet.';
  el.stats.innerHTML = '';
  el.facts.innerHTML = '<tr><td colspan="2" class="empty">0 rows.</td></tr>';
  el.ladder.innerHTML = '<p class="empty">No prefix to place.</p>';
  el.splitLen.innerHTML = '';
  el.splitLen.disabled = true;
  el.splitNote.textContent = 'Nothing to split.';
  el.splitBody.innerHTML = '<tr><td colspan="4" class="empty">0 rows.</td></tr>';
}

function statCard(label, value, context, title) {
  var d = document.createElement('div');
  d.className = 'stat';
  var l = document.createElement('div');
  l.className = 'stat-label';
  l.textContent = label;
  var v = document.createElement('div');
  v.className = 'stat-value mono';
  v.textContent = value;
  if (title) v.title = title;
  d.appendChild(l);
  d.appendChild(v);
  if (context) {
    var c = document.createElement('div');
    c.className = 'stat-context';
    c.textContent = context;
    d.appendChild(c);
  }
  return d;
}

function renderStats(p, f) {
  el.stats.innerHTML = '';
  el.stats.appendChild(statCard(
    'Total Addresses',
    countLabel(f.total),
    '2^' + f.hostBits,
    f.total.toString()
  ));
  if (p.version === 4) {
    el.stats.appendChild(statCard(
      'Usable Hosts',
      countLabel(f.usable),
      f.usableShort,
      f.usable.toString()
    ));
    el.stats.appendChild(statCard('Netmask', f.netmask, 'Wildcard ' + f.wildcard));
  } else {
    el.stats.appendChild(statCard(
      '/64 Subnets',
      f.sixtyFours === 0n ? 'None' : countLabel(f.sixtyFours),
      f.sixtyFours === 0n ? 'Prefix is longer than /64' : '2^' + (64 - p.prefix),
      f.sixtyFours.toString()
    ));
    el.stats.appendChild(statCard('Prefix Length', '/' + p.prefix, f.hostBits + ' bits left over'));
  }
}

function row(label, value, note, title) {
  var tr = document.createElement('tr');
  var th = document.createElement('th');
  th.scope = 'row';
  th.textContent = label;
  var td = document.createElement('td');
  if (value !== null && value !== undefined) {
    var span = document.createElement('span');
    span.className = 'mono val';
    span.textContent = value;
    if (title) span.title = title;
    td.appendChild(span);
  }
  if (note) {
    var n = document.createElement('div');
    n.className = 'cell-note';
    n.textContent = note;
    td.appendChild(n);
  }
  tr.appendChild(th);
  tr.appendChild(td);
  return tr;
}

function renderFacts(p, f) {
  var v = p.version;
  el.facts.innerHTML = '';
  var add = function (a, b, c, d) { el.facts.appendChild(row(a, b, c, d)); };

  if (v === 4) {
    add('Network Address', fmtV4(f.network));
    add('Broadcast', f.broadcast === null ? null : fmtV4(f.broadcast), f.broadcastNote);
    add('First Usable', fmtV4(f.first), p.prefix === 31 ? 'Under RFC 3021 both /31 addresses are usable.' : null);
    add('Last Usable', fmtV4(f.lastUsable));
    add('Usable Hosts', countLabel(f.usable), f.usableNote, f.usable.toString());
    add('Total Addresses', countLabel(f.total), null, f.total.toString());
    add('Netmask', f.netmask);
    add('Wildcard', f.wildcard);
    add('Range', fmtV4(f.network) + ' → ' + fmtV4(f.last));
    add('Host Bits', String(f.hostBits));
    add('Type', f.scope);
  } else {
    add('Network', compressV6(f.network));
    add('Expanded', f.expanded);
    add('First Address', compressV6(f.first), null, f.expanded);
    add('Last Address', compressV6(f.lastUsable), null, f.expandedLast);
    add('Total Addresses', countLabel(f.total), '2^' + f.hostBits, f.total.toString());
    add('/64 Subnets', f.sixtyFours === 0n ? null : countLabel(f.sixtyFours),
      f.sixtyFourNote, f.sixtyFours.toString());
    add('Host Bits', String(f.hostBits));
    add('Type', f.scope);
    add('Broadcast', null, 'IPv6 has no broadcast address. Multicast covers that job.');
  }
}

function renderLadder(p) {
  var rows = ladder(p);
  el.ladder.innerHTML = '';
  if (!rows.length) {
    var only = document.createElement('p');
    only.className = 'ladder-only mono';
    only.textContent = fmtCidr(p.version, p.network, p.prefix) + ' is the entire IPv' +
      p.version + ' space.';
    el.ladder.appendChild(only);
    return;
  }
  rows.forEach(function (r) {
    var wrap = document.createElement('div');
    wrap.className = 'ladder-row';

    var head = document.createElement('div');
    head.className = 'ladder-head';
    var a = document.createElement('span');
    a.className = 'mono';
    a.textContent = r.parent + ' → /' + r.childLen;
    var b = document.createElement('span');
    b.className = 'mono ladder-pos';
    b.textContent = '#' + group(r.index + 1n) + ' of ' + group(r.count);
    head.appendChild(a);
    head.appendChild(b);

    var track = document.createElement('div');
    track.className = 'ladder-track';
    var fill = document.createElement('div');
    fill.className = 'ladder-fill';
    fill.style.left = 'min(' + r.left + '%, 100% - 3px)';
    fill.style.width = r.width + '%';
    track.appendChild(fill);

    wrap.appendChild(head);
    wrap.appendChild(track);
    el.ladder.appendChild(wrap);
  });
}

function defaultSplitLen(p) {
  if (p.prefix >= p.width) return null;
  if (p.version === 6 && p.prefix < 64) return 64;
  return Math.min(p.width, p.prefix + 2);
}

function renderSplitOptions(p) {
  el.splitLen.innerHTML = '';
  if (p.prefix >= p.width) {
    el.splitLen.disabled = true;
    return;
  }
  el.splitLen.disabled = false;
  var want = state.splitLen;
  if (want === null || want <= p.prefix || want > p.width) want = defaultSplitLen(p);
  state.splitLen = want;

  for (var L = p.prefix + 1; L <= p.width; L++) {
    var o = document.createElement('option');
    o.value = String(L);
    o.textContent = '/' + L + ' — ' + countLabel(1n << BigInt(L - p.prefix)) + ' subnets';
    if (L === want) o.selected = true;
    el.splitLen.appendChild(o);
  }
}

function renderSplit() {
  var p = state.parsed;
  el.splitBody.innerHTML = '';
  if (!p) return;
  el.splitCol.textContent = p.version === 4 ? 'Usable' : 'Addresses';
  if (p.prefix >= p.width) {
    el.splitNote.textContent = 'A /' + p.width + ' is a single address. Nothing to split.';
    el.splitBody.innerHTML = '<tr><td colspan="4" class="empty">0 rows.</td></tr>';
    return;
  }
  var res = splitPrefix(p, state.splitLen, SPLIT_CAP);
  if (!res) {
    el.splitNote.textContent = 'Pick a longer prefix length.';
    return;
  }
  var per = p.version === 4 ? usableIn(p.version, state.splitLen, res.size) : res.size;
  el.splitNote.textContent = res.shown < res.count
    ? 'Showing ' + group(res.shown) + ' of ' + countLabel(res.count) + '.'
    : countLabel(res.count) + ' subnets.';

  var frag = document.createDocumentFragment();
  res.rows.forEach(function (r) {
    var tr = document.createElement('tr');
    tr.appendChild(cell(group(r.i + 1n), 'num'));

    var td = document.createElement('td');
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'linkish mono';
    btn.setAttribute('data-cidr', fmtCidr(p.version, r.network, state.splitLen));
    btn.textContent = fmtCidr(p.version, r.network, state.splitLen);
    td.appendChild(btn);
    tr.appendChild(td);

    tr.appendChild(cell(fmtAddr(p.version, r.network) + ' → ' + fmtAddr(p.version, r.last), 'mono range'));
    tr.appendChild(cell(countLabel(per), 'num'));
    frag.appendChild(tr);
  });
  el.splitBody.appendChild(frag);
}

function usableIn(version, prefix, size) {
  if (version !== 4) return size;
  if (prefix === 32) return 1n;
  if (prefix === 31) return 0n;
  return size - 2n;
}

function cell(text, cls) {
  var td = document.createElement('td');
  if (cls) td.className = cls;
  td.textContent = text;
  return td;
}

function renderQuery() {
  var p = state.parsed;
  if (!p) {
    el.queryStatus.textContent = 'No prefix to check against.';
    el.queryStatus.className = 'answer muted';
    el.queryWhy.textContent = '';
    return;
  }
  var q = parseInput(el.query.value);
  if (!q.ok) {
    el.queryStatus.textContent = q.empty ? 'Nothing to check.' : q.error;
    el.queryStatus.className = 'answer ' + (q.empty ? 'muted' : 'bad');
    el.queryWhy.textContent = '';
    return;
  }
  var r = containment(p, q);
  el.queryStatus.textContent = r.line;
  el.queryStatus.className = 'answer ' + (r.inside ? 'yes' : 'no');
  el.queryWhy.textContent = r.why;
}

if (typeof document !== 'undefined') {
  document.addEventListener('DOMContentLoaded', init);
} else if (typeof module !== 'undefined') {
  module.exports = {
    parseInput: parseInput, facts: facts, splitPrefix: splitPrefix,
    containment: containment, ladder: ladder, expandV6: expandV6,
    compressV6: compressV6, fmtV4: fmtV4, maskFor: maskFor,
    classify: classify, countLabel: countLabel, usableIn: usableIn
  };
}
