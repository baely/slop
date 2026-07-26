// parity — RAID price optimiser.
// The layouts are capacity archetypes, not brand names: RAID 5 and RAIDZ1 have
// identical geometry (N−1 usable, survives 1), so they are one row, labelled
// with both names. Numbers are geometry only — filesystems take their own cut.

const CONFIGS = [
  {
    id: 'none', label: 'No Redundancy', sub: 'RAID 0 / JBOD',
    min: 1, even: false,
    usable: (n) => n, tol: () => 0, tolLabel: () => 'nothing',
  },
  {
    id: 'mirror', label: 'Mirror', sub: 'RAID 1',
    min: 2, even: false,
    usable: () => 1, tol: (n) => n - 1, tolLabel: (n) => `${n - 1} of ${n} drives`,
  },
  {
    id: 'stripedmirror', label: 'Striped Mirrors', sub: 'RAID 10 / mirror vdevs',
    min: 4, even: true,
    usable: (n) => n / 2, tol: () => 1, tolLabel: () => '1 per pair, guaranteed 1',
  },
  {
    id: 'p1', label: 'Single Parity', sub: 'RAID 5 / RAIDZ1',
    min: 3, even: false,
    usable: (n) => n - 1, tol: () => 1, tolLabel: () => 'any 1 drive',
  },
  {
    id: 'p2', label: 'Double Parity', sub: 'RAID 6 / RAIDZ2',
    min: 4, even: false,
    usable: (n) => n - 2, tol: () => 2, tolLabel: () => 'any 2 drives',
  },
  {
    id: 'p3', label: 'Triple Parity', sub: 'RAIDZ3',
    min: 5, even: false,
    usable: (n) => n - 3, tol: () => 3, tolLabel: () => 'any 3 drives',
  },
];

// Enumerate every drive × count × layout combo that reaches the target with
// the constraints, cheapest first.
function optimize(drives, opts) {
  const { targetTB, maxBays, minTol, enabled, sortBy } = opts;
  const rows = [];
  let evaluated = 0;

  for (const cfg of CONFIGS) {
    if (!enabled.has(cfg.id)) continue;
    for (const d of drives) {
      if (!(d.tb > 0) || !(d.price > 0)) continue;
      for (let n = cfg.min; n <= maxBays; n++) {
        if (cfg.even && n % 2 !== 0) continue;
        evaluated++;
        if (cfg.tol(n) < minTol) continue;
        const usable = cfg.usable(n) * d.tb;
        if (usable < targetTB) continue;
        const total = n * d.price;
        rows.push({
          cfg,
          n,
          tb: d.tb,
          usable,
          raw: n * d.tb,
          total,
          perTB: total / usable,
          tol: cfg.tol(n),
          tolLabel: cfg.tolLabel(n),
          over: usable - targetTB,
        });
      }
    }
  }

  rows.sort((a, b) =>
    sortBy === 'perTB'
      ? a.perTB - b.perTB || a.total - b.total
      : a.total - b.total || a.perTB - b.perTB);

  // Dedupe: same layout + drive size at a higher count than an already-listed
  // row is never interesting (more money, same everything else scaled).
  const seen = new Set();
  const out = [];
  for (const r of rows) {
    const key = `${r.cfg.id}:${r.tb}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(r);
  }
  return { rows: out, all: rows.length, evaluated };
}

if (typeof module !== 'undefined') {
  module.exports = { CONFIGS, optimize };
} else {
  // ---------- browser ----------
  const $ = (id) => document.getElementById(id);
  const STORE_KEY = 'parity.v1';

  const DEFAULT_DRIVES = [
    { tb: 4, price: 129 },
    { tb: 8, price: 239 },
    { tb: 12, price: 329 },
    { tb: 16, price: 399 },
    { tb: 20, price: 479 },
    { tb: 24, price: 599 },
  ];

  const state = Object.assign(
    {
      targetTB: 40,
      maxBays: 8,
      minTol: 1,
      enabled: ['mirror', 'stripedmirror', 'p1', 'p2', 'p3'],
      sortBy: 'total',
      drives: DEFAULT_DRIVES,
    },
    load()
  );

  function load() {
    try { return JSON.parse(localStorage.getItem(STORE_KEY)) || {}; } catch (e) { return {}; }
  }
  function save() {
    try { localStorage.setItem(STORE_KEY, JSON.stringify(state)); } catch (e) { /* private mode */ }
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  const money = (x) => '$' + (Math.round(x * 100) / 100).toLocaleString(undefined, { maximumFractionDigits: 0 });
  const tb = (x) => (Math.round(x * 100) / 100).toLocaleString(undefined, { maximumFractionDigits: 2 });

  // ---------- drives table ----------

  function renderDrives() {
    const body = $('drives').tBodies[0];
    body.replaceChildren();
    state.drives.forEach((d, i) => {
      const tr = el('tr');
      const tdTb = el('td', 'num');
      const inTb = el('input');
      inTb.type = 'number'; inTb.min = '1'; inTb.step = '1'; inTb.value = d.tb;
      inTb.setAttribute('aria-label', 'Capacity in TB');
      inTb.addEventListener('input', () => { d.tb = parseFloat(inTb.value) || 0; recompute(); });
      tdTb.append(inTb);
      const tdPrice = el('td', 'num');
      const inPrice = el('input');
      inPrice.type = 'number'; inPrice.min = '1'; inPrice.step = '1'; inPrice.value = d.price;
      inPrice.setAttribute('aria-label', 'Price');
      inPrice.addEventListener('input', () => { d.price = parseFloat(inPrice.value) || 0; recompute(); });
      tdPrice.append(inPrice);
      const tdPer = el('td', 'num mono', d.tb > 0 && d.price > 0 ? money(d.price / d.tb) : '—');
      const tdDel = el('td', 'num');
      const del = el('button', 'btn btn-sm', 'Remove');
      del.type = 'button';
      del.addEventListener('click', () => { state.drives.splice(i, 1); renderDrives(); recompute(); });
      tdDel.append(del);
      tr.append(tdTb, tdPrice, tdPer, tdDel);
      body.append(tr);
    });
    if (!state.drives.length) {
      const tr = el('tr');
      const td = el('td', null, '0 drives.');
      td.colSpan = 4;
      td.className = 'empty';
      tr.append(td);
      body.append(tr);
    }
  }

  // ---------- results ----------

  function recompute() {
    state.targetTB = parseFloat($('target').value) || 0;
    state.maxBays = Math.max(1, Math.min(24, parseInt($('bays').value, 10) || 1));
    state.minTol = parseInt($('tol').value, 10);
    save();

    const body = $('results').tBodies[0];
    body.replaceChildren();
    const summary = $('summary');

    if (!(state.targetTB > 0)) {
      summary.textContent = 'Set a usable target.';
      $('best').replaceChildren();
      return;
    }
    const opts = {
      targetTB: state.targetTB,
      maxBays: state.maxBays,
      minTol: state.minTol,
      enabled: new Set(state.enabled),
      sortBy: state.sortBy,
    };
    const { rows, all, evaluated } = optimize(state.drives, opts);

    // The headline is always the cheapest total — the table sort only reorders
    // the table. "Cheapest" must never label a per-TB winner.
    const cheapest = state.sortBy === 'total'
      ? rows[0]
      : optimize(state.drives, { ...opts, sortBy: 'total' }).rows[0];
    renderBest(cheapest);

    if (!rows.length) {
      summary.textContent =
        `Nothing reaches ${tb(state.targetTB)} TB usable within ${state.maxBays} bays at that tolerance. ` +
        `Raise the bay count, lower the tolerance, or add bigger drives.`;
      return;
    }
    summary.textContent =
      `${all} of ${evaluated} combinations meet the target; best per layout and size shown, ` +
      (state.sortBy === 'perTB' ? 'cheapest per usable TB first.' : 'cheapest total first.');

    rows.slice(0, 14).forEach((r, i) => {
      const tr = el('tr');
      if (i === 0) tr.className = 'selected';
      const layout = el('td');
      layout.append(el('div', 'layout-name', r.cfg.label));
      layout.append(el('div', 'layout-sub', r.cfg.sub));
      tr.append(layout);
      tr.append(el('td', 'num', `${r.n} × ${tb(r.tb)} TB`));
      const us = el('td', 'num', `${tb(r.usable)} TB`);
      us.title = `${tb(r.raw)} TB raw, +${tb(r.over)} TB over target`;
      tr.append(us);
      tr.append(el('td', 'num', money(r.total)));
      tr.append(el('td', 'num', money(r.perTB)));
      tr.append(el('td', 'num', r.tolLabel));
      body.append(tr);
    });
  }

  function renderBest(r) {
    const best = $('best');
    best.replaceChildren();
    if (!r) return;
    const items = [
      ['Cheapest', money(r.total), `${r.cfg.label} · ${r.n} × ${tb(r.tb)} TB`],
      ['Usable', `${tb(r.usable)} TB`, `${tb(r.over)} TB over target`],
      ['Per Usable TB', money(r.perTB), `${tb(r.raw)} TB raw`],
      ['Survives', String(r.tol), r.tolLabel === 'nothing' ? 'no failures' : r.tolLabel],
    ];
    for (const [label, value, context] of items) {
      const s = el('div', 'stat');
      s.append(el('div', 'stat-label', label));
      s.append(el('div', 'stat-value mono', value));
      s.append(el('div', 'stat-context', context));
      best.append(s);
    }
  }

  // ---------- wiring ----------

  $('target').value = state.targetTB;
  $('bays').value = state.maxBays;
  $('tol').value = String(state.minTol);
  for (const inp of ['target', 'bays']) $(inp).addEventListener('input', recompute);
  $('tol').addEventListener('change', recompute);

  const configsWrap = $('configs');
  for (const cfg of CONFIGS) {
    const b = el('button', 'preset', '');
    b.type = 'button';
    b.append(el('span', null, cfg.label));
    b.append(el('span', 'preset-sub', cfg.sub));
    b.setAttribute('aria-pressed', String(state.enabled.includes(cfg.id)));
    b.addEventListener('click', () => {
      const on = state.enabled.includes(cfg.id);
      state.enabled = on ? state.enabled.filter((x) => x !== cfg.id) : [...state.enabled, cfg.id];
      b.setAttribute('aria-pressed', String(!on));
      recompute();
    });
    configsWrap.append(b);
  }

  for (const b of document.querySelectorAll('#sort .preset')) {
    b.addEventListener('click', () => {
      state.sortBy = b.dataset.sort;
      for (const x of document.querySelectorAll('#sort .preset')) {
        x.setAttribute('aria-pressed', String(x === b));
      }
      recompute();
    });
    b.setAttribute('aria-pressed', String(b.dataset.sort === state.sortBy));
  }

  $('add-drive').addEventListener('click', () => {
    state.drives.push({ tb: 0, price: 0 });
    renderDrives();
    recompute();
  });
  $('reset-drives').addEventListener('click', () => {
    state.drives = DEFAULT_DRIVES.map((d) => ({ ...d }));
    renderDrives();
    recompute();
  });

  renderDrives();
  recompute();
}
