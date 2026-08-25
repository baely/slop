/* tc — total comp calculator
 *
 * An award is composed, not typed. Three orthogonal parts:
 *   value    — where the number comes from (a fixed amount, or a percent of
 *              one or more groups in the same year)
 *   schedule — how it lands across years (one-off, recurring with optional
 *              growth, or a total spread over N years with a vest split)
 *   group    — a user-defined category, which is what the view toggles switch
 *
 * "Award types" are just saved templates of that composition.
 */

const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];

const CFG_KEY = 'tc-calc-v2';
const OLD_KEY = 'tc-calc-v1';
const FX_KEY = 'tc-calc-fx-v1';
const FX_MAX_AGE = 12 * 60 * 60 * 1000;

const CURRENCIES = ['AUD', 'USD', 'EUR', 'GBP', 'NZD', 'SGD', 'CAD', 'CHF', 'HKD', 'JPY', 'INR'];

const VALUE_MODES = [
  { key: 'fixed', label: 'Fixed Amount' },
  { key: 'percent', label: 'Percent Of' },
];
const SCHEDULES = [
  { key: 'once', label: 'One-Off' },
  { key: 'recurring', label: 'Recurring' },
  { key: 'spread', label: 'Spread' },
];

/* ---------- state ---------- */

let cfg = null;
let fx = { rates: { AUD: 1 }, source: null, fetched: null, date: null, error: null };
let seq = 1;
const uid = p => `${p}-${seq++}-${Math.round(performance.now() * 1000) % 100000}`;

function defaultConfig() {
  const y = new Date().getFullYear();
  const G = { base: 'g-base', bonus: 'g-bonus', equity: 'g-equity', cash: 'g-cash' };

  const groups = [
    { id: G.base, name: 'Base', superDefault: true },
    { id: G.bonus, name: 'Bonus', superDefault: true },
    { id: G.equity, name: 'Equity', superDefault: false },
    { id: G.cash, name: 'Cash', superDefault: true },
  ];

  const awards = [
    {
      id: 'a-1', label: 'Base Salary', groupId: G.base, enabled: true, superApplies: true,
      value: { mode: 'fixed', amount: 185000, currency: 'AUD', pct: 0, ofGroups: [] },
      schedule: { type: 'recurring', startYear: y, years: 0, growth: 0, split: '' },
    },
    {
      id: 'a-2', label: 'Target Bonus', groupId: G.bonus, enabled: true, superApplies: true,
      value: { mode: 'percent', amount: 0, currency: 'AUD', pct: 15, ofGroups: [G.base] },
      schedule: { type: 'recurring', startYear: y, years: 0, growth: 0, split: '' },
    },
    {
      id: 'a-3', label: 'Equity — Initial Grant', groupId: G.equity, enabled: true, superApplies: false,
      value: { mode: 'fixed', amount: 160000, currency: 'USD', pct: 0, ofGroups: [] },
      schedule: { type: 'spread', startYear: y - 1, years: 4, growth: 0, split: '' },
    },
    {
      id: 'a-4', label: 'Equity — Refresh', groupId: G.equity, enabled: true, superApplies: false,
      value: { mode: 'fixed', amount: 90000, currency: 'USD', pct: 0, ofGroups: [] },
      schedule: { type: 'spread', startYear: y, years: 4, growth: 0, split: '' },
    },
    {
      id: 'a-5', label: 'Cash Grant — Sign-On', groupId: G.cash, enabled: true, superApplies: true,
      value: { mode: 'fixed', amount: 60000, currency: 'USD', pct: 0, ofGroups: [] },
      schedule: { type: 'spread', startYear: y, years: 2, growth: 0, split: '60/40' },
    },
  ];

  const show = { super: true };
  groups.forEach(g => { show[g.id] = true; });

  return {
    startYear: y, horizon: 4,
    superRate: 12, superCapOn: false, superCapBase: 250000,
    display: 'AUD', selYear: y,
    groups, awards, show,
    types: defaultTypes(G),
    views: [
      { id: 'v-cash', name: 'Cash Only', show: { [G.base]: true, [G.bonus]: true, [G.cash]: true, super: false } },
      { id: 'v-guar', name: 'Guaranteed', show: { [G.base]: true, super: true } },
    ],
    overrides: {},
  };
}

function defaultTypes(G) {
  const mk = (id, name, groupId, value, schedule, superApplies) =>
    ({ id, name, spec: { groupId, superApplies, value, schedule } });
  return [
    mk('t-base', 'Base Salary', G.base,
      { mode: 'fixed', amount: 0, currency: 'AUD', pct: 0, ofGroups: [] },
      { type: 'recurring', startYear: null, years: 0, growth: 0, split: '' }, true),
    mk('t-bonus', 'Target Bonus (% Of Base)', G.bonus,
      { mode: 'percent', amount: 0, currency: 'AUD', pct: 15, ofGroups: [G.base] },
      { type: 'recurring', startYear: null, years: 0, growth: 0, split: '' }, true),
    mk('t-equity', 'Equity Grant (4-Year Even)', G.equity,
      { mode: 'fixed', amount: 0, currency: 'USD', pct: 0, ofGroups: [] },
      { type: 'spread', startYear: null, years: 4, growth: 0, split: '' }, false),
    mk('t-equity-cliff', 'Equity Grant (1-Year Cliff)', G.equity,
      { mode: 'fixed', amount: 0, currency: 'USD', pct: 0, ofGroups: [] },
      { type: 'spread', startYear: null, years: 4, growth: 0, split: '0/33/33/34' }, false),
    mk('t-cash', 'Cash Grant (Multi-Year)', G.cash,
      { mode: 'fixed', amount: 0, currency: 'USD', pct: 0, ofGroups: [] },
      { type: 'spread', startYear: null, years: 2, growth: 0, split: '' }, true),
    mk('t-oneoff', 'One-Off Payment', G.cash,
      { mode: 'fixed', amount: 0, currency: 'AUD', pct: 0, ofGroups: [] },
      { type: 'once', startYear: null, years: 1, growth: 0, split: '' }, true),
    mk('t-allow', 'Recurring Allowance', G.cash,
      { mode: 'fixed', amount: 0, currency: 'AUD', pct: 0, ofGroups: [] },
      { type: 'recurring', startYear: null, years: 0, growth: 0, split: '' }, false),
  ];
}

/* ---------- load / migrate / save ---------- */

function normalise(c) {
  const d = defaultConfig();
  if (!Array.isArray(c.groups) || !c.groups.length) c.groups = d.groups;
  if (!Array.isArray(c.awards)) c.awards = [];
  if (!Array.isArray(c.types) || !c.types.length) c.types = d.types;
  if (!Array.isArray(c.views)) c.views = [];
  c.show = c.show || {};
  if (typeof c.show.super !== 'boolean') c.show.super = true;
  c.groups.forEach(g => { if (typeof c.show[g.id] !== 'boolean') c.show[g.id] = true; });
  c.overrides = c.overrides || {};

  const gids = new Set(c.groups.map(g => g.id));
  c.awards.forEach(a => {
    a.value = a.value || { mode: 'fixed', amount: 0, currency: 'AUD', pct: 0, ofGroups: [] };
    a.schedule = a.schedule || { type: 'recurring', startYear: c.startYear, years: 0, growth: 0, split: '' };
    if (!Array.isArray(a.value.ofGroups)) a.value.ofGroups = [];
    a.value.ofGroups = a.value.ofGroups.filter(id => gids.has(id));
    if (!gids.has(a.groupId)) a.groupId = c.groups[0].id;
  });
  return c;
}

/** v1 stored a fixed `kind` per award; map each onto the composed model. */
function migrateV1(old) {
  const d = defaultConfig();
  const byKind = { base: 'g-base', bonus: 'g-bonus', equity: 'g-equity', cash: 'g-cash' };
  const groups = d.groups.slice();
  if ((old.awards || []).some(a => a.kind === 'other')) {
    groups.push({ id: 'g-other', name: 'Other', superDefault: false });
    byKind.other = 'g-other';
  }

  const awards = (old.awards || []).map((a, i) => {
    const gid = byKind[a.kind] || groups[0].id;
    const spread = a.mode === 'total' && a.kind !== 'bonus';
    return {
      id: 'a-m' + (i + 1),
      label: a.label || 'Award',
      groupId: gid,
      enabled: a.enabled !== false,
      superApplies: !!a.superEligible,
      value: a.kind === 'bonus'
        ? { mode: 'percent', amount: 0, currency: 'AUD', pct: a.pct || 0, ofGroups: ['g-base'] }
        : { mode: 'fixed', amount: a.amount || 0, currency: a.currency || 'AUD', pct: 0, ofGroups: [] },
      schedule: {
        type: spread ? 'spread' : 'recurring',
        startYear: a.startYear ?? old.startYear ?? d.startYear,
        years: a.years || (spread ? 4 : 0),
        growth: 0,
        split: a.split || '',
      },
    };
  });

  const show = { super: old.show ? old.show.super !== false : true };
  groups.forEach(g => { show[g.id] = true; });
  if (old.show) {
    for (const k in byKind) if (k in old.show) show[byKind[k]] = !!old.show[k];
  }

  return normalise(Object.assign(d, {
    startYear: old.startYear ?? d.startYear,
    horizon: old.horizon ?? d.horizon,
    superRate: old.superRate ?? d.superRate,
    superCapOn: !!old.superCapOn,
    superCapBase: old.superCapBase ?? d.superCapBase,
    display: old.display || 'AUD',
    selYear: old.selYear ?? d.selYear,
    overrides: old.overrides || {},
    groups, awards, show,
  }));
}

function loadConfig() {
  try {
    const raw = localStorage.getItem(CFG_KEY);
    if (raw) { cfg = normalise(Object.assign(defaultConfig(), JSON.parse(raw))); return; }
    const legacy = localStorage.getItem(OLD_KEY);
    if (legacy) {
      cfg = migrateV1(JSON.parse(legacy));
      localStorage.removeItem(OLD_KEY);
      saveConfig();
      return;
    }
  } catch (e) { /* fall through to defaults */ }
  cfg = defaultConfig();
}

function saveConfig() {
  try { localStorage.setItem(CFG_KEY, JSON.stringify(cfg)); } catch (e) { /* private mode */ }
}

const groupById = id => cfg.groups.find(g => g.id === id);
const groupName = id => (groupById(id) || {}).name || '—';

/* ---------- fx ---------- */

function audPerUnit(cur) {
  if (cfg.overrides[cur] > 0) return cfg.overrides[cur];
  if (cur === 'AUD') return 1;
  const r = fx.rates[cur];
  return r > 0 ? 1 / r : 1;
}
const toAUD = (amt, cur) => (amt || 0) * audPerUnit(cur || 'AUD');
const fromAUD = (aud, cur) => (aud || 0) / audPerUnit(cur || 'AUD');

async function fetchRates() {
  const sources = [
    ['frankfurter', 'https://api.frankfurter.dev/v1/latest?base=AUD', j => ({ rates: j.rates, date: j.date })],
    ['exchangerate-api', 'https://open.er-api.com/v6/latest/AUD', j => ({ rates: j.rates, date: (j.time_last_update_utc || '').slice(5, 16) })],
  ];
  for (const [source, url, pick] of sources) {
    try {
      const res = await fetch(url, { cache: 'no-store' });
      if (!res.ok) continue;
      const { rates, date } = pick(await res.json());
      if (!rates || !rates.USD) continue;
      fx = { rates: Object.assign({}, rates, { AUD: 1 }), source, fetched: Date.now(), date: date || null, error: null };
      try { localStorage.setItem(FX_KEY, JSON.stringify(fx)); } catch (e) { /* ignore */ }
      return true;
    } catch (e) { /* try next source */ }
  }
  fx.error = 'no rates';
  return false;
}

function loadCachedFx() {
  try {
    const c = JSON.parse(localStorage.getItem(FX_KEY) || 'null');
    if (!c || !c.rates || !c.rates.USD) return false;
    fx = c;
    return Date.now() - (c.fetched || 0) < FX_MAX_AGE;
  } catch (e) { return false; }
}

function ago(ts) {
  if (!ts) return 'never';
  const s = Math.round((Date.now() - ts) / 1000);
  if (s < 90) return 'just now';
  const m = Math.round(s / 60);
  if (m < 90) return m + ' minutes ago';
  const h = Math.round(m / 60);
  if (h < 36) return h + ' hours ago';
  return Math.round(h / 24) + ' days ago';
}

function renderFxLine() {
  const el = $('#fxLine');
  el.classList.remove('stale', 'bad');
  if (fx.error && !fx.rates.USD) {
    el.textContent = 'fx unavailable — set rates manually';
    el.classList.add('bad');
    return;
  }
  const manual = Object.keys(cfg.overrides).some(k => cfg.overrides[k] > 0);
  el.textContent = `1 USD = ${audPerUnit('USD').toFixed(4)} AUD · ${manual ? 'manual override active' : fx.source} · ${ago(fx.fetched)}`;
  if (Date.now() - (fx.fetched || 0) > FX_MAX_AGE) el.classList.add('stale');
}

/* ---------- schedule ---------- */

const yearList = () => Array.from({ length: cfg.horizon }, (_, i) => cfg.startYear + i);

function scheduleSpan(a) {
  const s = a.schedule;
  if (s.type === 'once') return 1;
  if (s.type === 'recurring' && !(s.years > 0)) {
    return Math.max(1, cfg.startYear + cfg.horizon - s.startYear);
  }
  return Math.max(1, Math.round(s.years || 1));
}

function splitWeights(a, n) {
  const raw = String(a.schedule.split || '').split(/[/,\s]+/).map(parseFloat).filter(v => !isNaN(v));
  if (!raw.length) return { w: Array(n).fill(1 / n), custom: false, bad: false };
  if (raw.length !== n) return { w: Array(n).fill(1 / n), custom: false, bad: true };
  const sum = raw.reduce((x, y) => x + y, 0);
  if (sum <= 0) return { w: Array(n).fill(1 / n), custom: false, bad: true };
  return { w: raw.map(v => v / sum), custom: true, bad: false };
}

/** Multiplier applied to the award's value in year `y`. */
function scheduleWeight(a, y) {
  const s = a.schedule;
  const i = y - s.startYear;
  if (s.type === 'once') return i === 0 ? 1 : 0;
  const n = scheduleSpan(a);
  if (i < 0 || i >= n) return 0;
  if (s.type === 'recurring') return Math.pow(1 + (s.growth || 0) / 100, i);
  return splitWeights(a, n).w[i];
}

/* ---------- compute ---------- */

/** One year, resolving percent-of references with cycle protection. */
function computeYear(y) {
  const awardMemo = {};
  const groupMemo = {};
  const visitingGroups = new Set();
  const visitingAwards = new Set();
  const cycles = new Set();

  function groupTotal(gid) {
    if (gid in groupMemo) return groupMemo[gid];
    if (visitingGroups.has(gid)) { cycles.add(gid); return 0; }
    visitingGroups.add(gid);
    let sum = 0;
    for (const a of cfg.awards) if (a.groupId === gid) sum += awardValue(a);
    visitingGroups.delete(gid);
    return (groupMemo[gid] = sum);
  }

  function awardValue(a) {
    if (a.id in awardMemo) return awardMemo[a.id];
    // re-entry means a percent-of chain leads back here; break it and report,
    // rather than memoising a provisional 0 that hides the cycle
    if (visitingAwards.has(a.id)) { cycles.add(a.groupId); return 0; }
    visitingAwards.add(a.id);
    let v = 0;
    if (a.enabled) {
      const w = scheduleWeight(a, y);
      if (w) {
        const basis = a.value.mode === 'percent'
          ? a.value.ofGroups.reduce((s, gid) => s + groupTotal(gid), 0) * ((a.value.pct || 0) / 100)
          : toAUD(a.value.amount, a.value.currency);
        v = basis * w;
      }
    }
    visitingAwards.delete(a.id);
    return (awardMemo[a.id] = v);
  }

  const perAward = {};
  for (const a of cfg.awards) perAward[a.id] = awardValue(a);

  const byGroup = {};
  cfg.groups.forEach(g => { byGroup[g.id] = 0; });
  for (const a of cfg.awards) byGroup[a.groupId] = (byGroup[a.groupId] || 0) + perAward[a.id];

  let superBase = 0;
  for (const a of cfg.awards) {
    if (a.superApplies && cfg.show[a.groupId]) superBase += perAward[a.id];
  }
  const capped = cfg.superCapOn ? Math.min(superBase, cfg.superCapBase || 0) : superBase;
  const sup = capped * (cfg.superRate || 0) / 100;

  let total = 0;
  cfg.groups.forEach(g => { if (cfg.show[g.id]) total += byGroup[g.id]; });
  if (cfg.show.super) total += sup;

  return { year: y, perAward, byGroup, super: sup, superBase, total, cycles: [...cycles] };
}

/* A year's figures are needed by the tables and again by each award's inline
   schedule line (which can reach outside the display window), so memoise per
   render pass rather than recomputing per award. */
let yearCache = {};
const invalidate = () => { yearCache = {}; };
function computeYearCached(y) {
  if (!(y in yearCache)) yearCache[y] = computeYear(y);
  return yearCache[y];
}

function computeAll() {
  const rows = yearList().map(computeYearCached);
  const sum = { byGroup: {}, super: 0, total: 0, perAward: {} };
  cfg.groups.forEach(g => { sum.byGroup[g.id] = 0; });
  for (const r of rows) {
    cfg.groups.forEach(g => { sum.byGroup[g.id] += r.byGroup[g.id] || 0; });
    sum.super += r.super;
    sum.total += r.total;
    for (const id in r.perAward) sum.perAward[id] = (sum.perAward[id] || 0) + r.perAward[id];
  }
  const cycles = [...new Set(rows.flatMap(r => r.cycles))];
  return { rows, sum, cycles };
}

/* ---------- views ---------- */

function builtinViews() {
  const all = { super: true };
  cfg.groups.forEach(g => { all[g.id] = true; });
  return [
    { id: '__all', name: 'Everything', builtin: true, show: all },
    { id: '__ex', name: 'Ex-Super', builtin: true, show: Object.assign({}, all, { super: false }) },
  ];
}
const allViews = () => builtinViews().concat(cfg.views);

function viewMatches(v) {
  if (!!v.show.super !== !!cfg.show.super) return false;
  return cfg.groups.every(g => !!v.show[g.id] === !!cfg.show[g.id]);
}
const activeView = () => allViews().find(viewMatches) || null;

function applyView(v) {
  cfg.groups.forEach(g => { cfg.show[g.id] = !!v.show[g.id]; });
  cfg.show.super = !!v.show.super;
}

/* ---------- formatting ---------- */

const money = aud => moneyIn(fromAUD(aud, cfg.display), cfg.display);
const moneyIn = (amt, cur) => new Intl.NumberFormat('en-AU', {
  style: 'currency', currency: cur, maximumFractionDigits: 0,
}).format(amt || 0);

const pct = (part, whole) => (whole ? (part / whole * 100).toFixed(1) + '%' : '—');

const esc = s => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');

function specSummary(spec) {
  const v = spec.value, s = spec.schedule;
  const parts = [];
  parts.push(v.mode === 'percent'
    ? `${v.pct}% of ${(v.ofGroups || []).map(groupName).join(' + ') || '—'}`
    : `fixed ${v.currency}`);
  if (s.type === 'once') parts.push('one-off');
  else if (s.type === 'recurring') parts.push(s.years > 0 ? `recurring ${s.years}y` : 'recurring, ongoing');
  else parts.push(`spread ${s.years}y ${s.split ? s.split : 'even'}`);
  if (s.growth) parts.push(`+${s.growth}%/yr`);
  parts.push(spec.superApplies ? 'super' : 'no super');
  return parts.join(' · ');
}

/* ---------- render: awards ---------- */

function groupOptions(selected) {
  return cfg.groups.map(g =>
    `<option value="${g.id}" data-gopt="${g.id}"${g.id === selected ? ' selected' : ''}>${esc(g.name)}</option>`
  ).join('');
}

function renderAwards() {
  const host = $('#awardList');
  if (!cfg.awards.length) {
    host.innerHTML = '<p class="empty">0 awards. Add one above.</p>';
    return;
  }

  host.innerHTML = cfg.awards.map(a => {
    const v = a.value, s = a.schedule;
    const num = (path, f, label, opts = {}) => {
      const obj = path === 'value' ? v : s;
      const val = obj[f];
      return `<div class="field">
        <label for="f-${a.id}-${path}-${f}">${label}</label>
        <input type="number" id="f-${a.id}-${path}-${f}" data-id="${a.id}" data-path="${path}" data-f="${f}"
          value="${val === 0 && opts.zeroBlank ? '' : (val ?? '')}"
          step="${opts.step || 'any'}" placeholder="${opts.ph || ''}"></div>`;
    };

    return `<div class="award${a.enabled ? '' : ' off'}" data-id="${a.id}">
      <div class="award-head">
        <label class="toggle"><input type="checkbox" data-id="${a.id}" data-path="root" data-f="enabled" ${a.enabled ? 'checked' : ''}><span>Include</span></label>
        <input class="label-input" type="text" data-id="${a.id}" data-path="root" data-f="label" value="${esc(a.label)}" aria-label="Award Name">
        <select class="group-select" data-id="${a.id}" data-path="root" data-f="groupId" aria-label="Group">${groupOptions(a.groupId)}</select>
        <button class="btn tiny" type="button" data-savetype="${a.id}">Save As Type</button>
        <button class="btn tiny" type="button" data-remove="${a.id}">Remove</button>
      </div>

      <div class="award-row">
        <div class="award-grid">
          <div class="field">
            <label for="f-${a.id}-value-mode">Value</label>
            <select id="f-${a.id}-value-mode" data-id="${a.id}" data-path="value" data-f="mode">
              ${VALUE_MODES.map(m => `<option value="${m.key}"${m.key === v.mode ? ' selected' : ''}>${m.label}</option>`).join('')}
            </select>
          </div>
          ${v.mode === 'percent'
            ? num('value', 'pct', 'Percent', { step: '0.5' })
            : num('value', 'amount', 'Amount', { step: '1000' }) + `<div class="field">
                <label for="f-${a.id}-value-currency">Currency</label>
                <select id="f-${a.id}-value-currency" data-id="${a.id}" data-path="value" data-f="currency">
                  ${CURRENCIES.map(c => `<option value="${c}"${c === v.currency ? ' selected' : ''}>${c}</option>`).join('')}
                </select></div>`}
        </div>
        ${v.mode === 'percent' ? `<div class="of-groups">
          <span class="stat-label">Of</span>
          <div class="chips">${cfg.groups.map(g => `<label class="toggle">
            <input type="checkbox" data-id="${a.id}" data-ofgroup="${g.id}" ${v.ofGroups.includes(g.id) ? 'checked' : ''}>
            <span data-gopt="${g.id}">${esc(g.name)}</span></label>`).join('')}</div>
        </div>` : ''}
      </div>

      <div class="award-grid">
        <div class="field">
          <label for="f-${a.id}-schedule-type">Schedule</label>
          <select id="f-${a.id}-schedule-type" data-id="${a.id}" data-path="schedule" data-f="type">
            ${SCHEDULES.map(m => `<option value="${m.key}"${m.key === s.type ? ' selected' : ''}>${m.label}</option>`).join('')}
          </select>
        </div>
        ${num('schedule', 'startYear', s.type === 'once' ? 'Year' : 'First Year', { step: '1' })}
        ${s.type !== 'once' ? num('schedule', 'years', 'Years', { step: '1', zeroBlank: true, ph: s.type === 'recurring' ? 'Ongoing' : '4' }) : ''}
        ${s.type === 'recurring' ? num('schedule', 'growth', 'Growth %/Yr', { step: '0.5', zeroBlank: true, ph: '0' }) : ''}
        ${s.type === 'spread' ? `<div class="field wide">
          <label for="f-${a.id}-schedule-split">Vest Split %</label>
          <input type="text" id="f-${a.id}-schedule-split" data-id="${a.id}" data-path="schedule" data-f="split"
            value="${esc(s.split)}" placeholder="Even — or 25/25/25/25"></div>` : ''}
      </div>

      <div class="award-foot">
        <label class="toggle"><input type="checkbox" data-id="${a.id}" data-path="root" data-f="superApplies" ${a.superApplies ? 'checked' : ''}><span>Super Applies</span></label>
        <span class="award-sched" id="sched-${a.id}"></span>
      </div>
    </div>`;
  }).join('');
}

function renderAwardScheds() {
  for (const a of cfg.awards) {
    const el = $('#sched-' + a.id);
    if (!el) continue;
    const n = scheduleSpan(a);
    const bad = a.schedule.type === 'spread' && splitWeights(a, n).bad;
    const parts = [];
    for (let i = 0; i < n; i++) {
      const y = a.schedule.startYear + i;
      parts.push(`${y} ${money(valueOfAwardInYear(a, y))}`);
    }
    const warn = bad ? `<span class="warn">split ignored — needs ${n} numbers</span> · ` : '';
    const empty = a.value.mode === 'percent' && !a.value.ofGroups.length
      ? '<span class="warn">pick at least one group</span>' : '';
    el.innerHTML = empty || (warn + (parts.length ? parts.join(' · ') : 'no years'));
  }
}

/** Single-award lookup for the inline schedule line. */
function valueOfAwardInYear(a, y) {
  return computeYearCached(y).perAward[a.id] || 0;
}

/* ---------- render: chips ---------- */

function renderChips() {
  $('#yearChips').innerHTML = yearList()
    .map(y => `<button class="chip-btn" type="button" data-year="${y}">${y}</button>`).join('')
    + '<button class="chip-btn" type="button" data-year="avg">Average</button>'
    + `<button class="chip-btn" type="button" data-year="total">${cfg.horizon}-Year Total</button>`;

  $('#views').innerHTML = allViews()
    .map(v => `<button class="chip-btn" type="button" data-view="${v.id}">${esc(v.name)}</button>`).join('');

  $('#toggles').innerHTML = cfg.groups
    .map(g => `<label class="toggle"><input type="checkbox" data-show="${g.id}"><span data-gopt="${g.id}">${esc(g.name)}</span></label>`).join('')
    + '<label class="toggle"><input type="checkbox" data-show="super"><span>Super</span></label>';
}

function syncChips() {
  const sel = String(cfg.selYear);
  $$('[data-year]').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.year === sel)));
  const av = activeView();
  $$('[data-view]').forEach(b => b.setAttribute('aria-pressed', String(!!av && b.dataset.view === av.id)));
  $$('[data-show]').forEach(c => { c.checked = !!cfg.show[c.dataset.show]; });
  const del = $('#deleteView');
  del.hidden = !(av && !av.builtin);
}

/* ---------- render: groups & types ---------- */

function renderGroups() {
  $('#groupList').innerHTML = cfg.groups.map(g => {
    const used = cfg.awards.filter(a => a.groupId === g.id).length;
    const refs = cfg.awards.filter(a => a.value.mode === 'percent' && a.value.ofGroups.includes(g.id)).length;
    const last = cfg.groups.length < 2;
    const usage = [
      `${used} award${used === 1 ? '' : 's'}`,
      refs ? `${refs} reference${refs === 1 ? '' : 's'}` : '',
    ].filter(Boolean).join(' · ');
    return `<div class="row-item">
      <input type="text" class="row-name" data-gname="${g.id}" value="${esc(g.name)}" aria-label="Group Name">
      <label class="toggle"><input type="checkbox" data-gsuper="${g.id}" ${g.superDefault ? 'checked' : ''}><span>Super By Default</span></label>
      <span class="mono row-tag grow">${usage}</span>
      ${used || refs || last
        ? `<span class="mono row-tag">${last ? 'last group' : 'in use'}</span>`
        : `<button class="btn tiny" type="button" data-gdel="${g.id}">Delete</button>`}
    </div>`;
  }).join('');
}

function renderTypes() {
  $('#typeList').innerHTML = cfg.types.length ? cfg.types.map(t => `<div class="row-item">
    <input type="text" class="row-name" data-tname="${t.id}" value="${esc(t.name)}" aria-label="Type Name">
    <span class="mono row-tag grow">${esc(specSummary(t.spec))}</span>
    <button class="btn tiny" type="button" data-tdel="${t.id}">Delete</button>
  </div>`).join('') : '<p class="empty">0 types. Save an award as a type to start one.</p>';

  $('#typeSelect').innerHTML = cfg.types.length
    ? cfg.types.map(t => `<option value="${t.id}">${esc(t.name)}</option>`).join('')
    : '<option value="">— no types —</option>';
}

/* ---------- render: outputs ---------- */

function selection(all) {
  const rowsFor = () => {
    if (cfg.selYear === 'total') return { label: `${cfg.horizon}-Year Total`, byGroup: all.sum.byGroup, sup: all.sum.super, total: all.sum.total };
    if (cfg.selYear === 'avg') {
      const n = Math.max(1, all.rows.length);
      const byGroup = {};
      cfg.groups.forEach(g => { byGroup[g.id] = all.sum.byGroup[g.id] / n; });
      return { label: 'Average Year', byGroup, sup: all.sum.super / n, total: all.sum.total / n };
    }
    const r = all.rows.find(x => x.year === Number(cfg.selYear)) || all.rows[0];
    if (!r) return { label: '—', byGroup: {}, sup: 0, total: 0 };
    return { label: String(r.year), byGroup: r.byGroup, sup: r.super, total: r.total };
  };
  return rowsFor();
}

function renderOutputs() {
  invalidate();
  const all = computeAll();
  const sel = selection(all);
  const av = activeView();
  const alt = cfg.display === 'AUD' ? 'USD' : 'AUD';

  $('#tcLabel').textContent = cfg.selYear === 'total' ? 'Total Compensation, Cumulative' : 'Total Compensation';
  $('#tcValue').textContent = money(sel.total);
  $('#tcContext').textContent = `${sel.label} · ${av ? av.name : 'Custom View'} · ${moneyIn(fromAUD(sel.total, alt), alt)}`;

  // breakdown, one row per group plus super
  const rows = cfg.groups.map(g => ({ key: g.id, name: g.name, v: sel.byGroup[g.id] || 0 }));
  rows.push({ key: 'super', name: 'Super', v: sel.sup });
  $('#breakdownBody').innerHTML = rows.map(r => {
    const on = cfg.show[r.key];
    if (!on && !r.v) return '';
    return `<tr class="${on ? '' : 'off'}">
      <td>${esc(r.name)}${on ? '' : ' <span class="mono">(excluded)</span>'}</td>
      <td class="num">${money(r.v)}</td>
      <td class="num">${on ? pct(r.v, sel.total) : '—'}</td></tr>`;
  }).join('') + `<tr><td class="total">Total</td><td class="num total">${money(sel.total)}</td><td class="num total">${sel.total ? '100%' : '—'}</td></tr>`;

  // by year
  const cols = cfg.groups.map(g => ({ key: g.id, name: g.name })).concat([{ key: 'super', name: 'Super' }]);
  const cell = (r, c) => (c.key === 'super' ? r.super : r.byGroup[c.key] || 0);
  $('#yearHead').innerHTML = '<th>Year</th>'
    + cols.map(c => `<th class="num${cfg.show[c.key] ? '' : ' off'}">${esc(c.name)}</th>`).join('')
    + '<th class="num total">TC</th>';
  $('#yearBody').innerHTML = all.rows.map(r => `<tr class="${String(r.year) === String(cfg.selYear) ? 'selected' : ''}">
    <td class="num">${r.year}</td>
    ${cols.map(c => `<td class="num${cfg.show[c.key] ? '' : ' off'}">${money(cell(r, c))}</td>`).join('')}
    <td class="num total">${money(r.total)}</td></tr>`).join('');
  $('#yearFoot').innerHTML = '<td>Total</td>'
    + cols.map(c => `<td class="num${cfg.show[c.key] ? '' : ' off'}">${money(c.key === 'super' ? all.sum.super : all.sum.byGroup[c.key] || 0)}</td>`).join('')
    + `<td class="num total">${money(all.sum.total)}</td>`;

  // schedule
  const years = yearList();
  const byYear = {};
  all.rows.forEach(r => { byYear[r.year] = r; });
  $('#schedHead').innerHTML = '<th>Award</th>' + years.map(y => `<th class="num">${y}</th>`).join('') + '<th class="num total">In Window</th>';
  $('#schedBody').innerHTML = cfg.awards.length
    ? cfg.awards.map(a => {
        const on = cfg.show[a.groupId] && a.enabled;
        const cur = a.value.mode === 'fixed' && a.value.currency !== 'AUD' ? ' · ' + a.value.currency : '';
        return `<tr class="${on ? '' : 'off'}">
          <td>${esc(a.label)} <span class="mono row-tag">· ${esc(groupName(a.groupId))}${cur}</span></td>
          ${years.map(y => `<td class="num">${money(byYear[y].perAward[a.id] || 0)}</td>`).join('')}
          <td class="num total">${money(all.sum.perAward[a.id] || 0)}</td></tr>`;
      }).join('')
    : `<tr><td colspan="${years.length + 2}" class="empty">0 awards.</td></tr>`;

  if (all.cycles.length) {
    $('#schedBody').insertAdjacentHTML('beforeend',
      `<tr><td colspan="${years.length + 2}" class="warn">Circular percent-of reference in ${all.cycles.map(groupName).map(esc).join(', ')} — treated as 0.</td></tr>`);
  }

  renderAwardScheds();
  syncChips();
  renderFxLine();
  updateRateSources();
  saveConfig();
}

/* ---------- render: rates ---------- */

function usedCurrencies() {
  const set = new Set(['USD', cfg.display]);
  cfg.awards.forEach(a => { if (a.value.mode === 'fixed') set.add(a.value.currency || 'AUD'); });
  return [...set].filter(c => c !== 'AUD').sort();
}

function renderRates() {
  $('#rateBody').innerHTML = usedCurrencies().map(c => `<tr>
    <td>${c}</td>
    <td class="num"><input class="rate-input" type="number" step="0.0001" data-rate="${c}"
      value="${cfg.overrides[c] > 0 ? cfg.overrides[c] : ''}" placeholder="${audPerUnit(c).toFixed(4)}" aria-label="${c} Rate"></td>
    <td class="mono" id="rate-src-${c}"></td></tr>`).join('')
    || '<tr><td colspan="3" class="empty">Everything is in AUD.</td></tr>';
  updateRateSources();
}

function updateRateSources() {
  usedCurrencies().forEach(c => {
    const el = $('#rate-src-' + c);
    if (!el) return;
    el.textContent = cfg.overrides[c] > 0 ? 'manual' : (fx.rates[c] ? (fx.source || 'cached') : 'unavailable');
  });
}

/* ---------- group rename propagation ---------- */

function renameGroup(gid, name) {
  const g = groupById(gid);
  if (!g) return;
  g.name = name;
  $$(`[data-gopt="${gid}"]`).forEach(el => { el.textContent = name; });
  renderOutputs();
}

/* ---------- events ---------- */

function bindEvents() {
  const dc = $('#displayCur');
  dc.innerHTML = CURRENCIES.map(c => `<option value="${c}">${c}</option>`).join('');
  dc.addEventListener('change', () => { cfg.display = dc.value; renderRates(); renderOutputs(); });

  $('#fxRefresh').addEventListener('click', async () => {
    $('#fxLine').textContent = 'fx refreshing…';
    await fetchRates();
    renderRates();
    renderOutputs();
  });

  $('#yearChips').addEventListener('click', e => {
    const b = e.target.closest('[data-year]');
    if (!b) return;
    const v = b.dataset.year;
    cfg.selYear = (v === 'avg' || v === 'total') ? v : Number(v);
    renderOutputs();
  });

  $('#views').addEventListener('click', e => {
    const b = e.target.closest('[data-view]');
    if (!b) return;
    const v = allViews().find(x => x.id === b.dataset.view);
    if (v) { applyView(v); renderOutputs(); }
  });

  $('#toggles').addEventListener('change', e => {
    const c = e.target.closest('[data-show]');
    if (!c) return;
    cfg.show[c.dataset.show] = c.checked;
    renderOutputs();
  });

  $('#saveView').addEventListener('click', () => {
    const input = $('#newViewName');
    const name = input.value.trim();
    if (!name) { input.focus(); return; }
    const show = { super: !!cfg.show.super };
    cfg.groups.forEach(g => { show[g.id] = !!cfg.show[g.id]; });
    cfg.views.push({ id: uid('v'), name, show });
    input.value = '';
    renderChips();
    renderOutputs();
  });

  $('#deleteView').addEventListener('click', () => {
    const av = activeView();
    if (!av || av.builtin) return;
    cfg.views = cfg.views.filter(v => v.id !== av.id);
    renderChips();
    renderOutputs();
  });

  // awards
  const list = $('#awardList');
  list.addEventListener('input', e => onAwardField(e, false));
  list.addEventListener('change', e => onAwardField(e, true));
  list.addEventListener('click', e => {
    const rm = e.target.closest('[data-remove]');
    if (rm) {
      cfg.awards = cfg.awards.filter(a => a.id !== rm.dataset.remove);
      renderAwards(); renderGroups(); renderRates(); renderOutputs();
      return;
    }
    const st = e.target.closest('[data-savetype]');
    if (st) saveAwardAsType(st.dataset.savetype);
  });

  $('#addAward').addEventListener('click', () => addAwardFromType($('#typeSelect').value));

  // groups
  const gl = $('#groupList');
  gl.addEventListener('input', e => {
    const n = e.target.closest('[data-gname]');
    if (n) renameGroup(n.dataset.gname, n.value);
  });
  gl.addEventListener('change', e => {
    const s = e.target.closest('[data-gsuper]');
    if (!s) return;
    const g = groupById(s.dataset.gsuper);
    if (g) { g.superDefault = s.checked; saveConfig(); }
  });
  gl.addEventListener('click', e => {
    const d = e.target.closest('[data-gdel]');
    if (!d) return;
    cfg.groups = cfg.groups.filter(g => g.id !== d.dataset.gdel);
    delete cfg.show[d.dataset.gdel];
    cfg.views.forEach(v => delete v.show[d.dataset.gdel]);
    renderGroups(); renderAwards(); renderChips(); renderOutputs();
  });

  $('#addGroup').addEventListener('click', () => {
    const input = $('#newGroupName');
    const name = input.value.trim();
    if (!name) { input.focus(); return; }
    const g = { id: uid('g'), name, superDefault: false };
    cfg.groups.push(g);
    cfg.show[g.id] = true;
    input.value = '';
    renderGroups(); renderAwards(); renderChips(); renderOutputs();
  });

  // types
  const tl = $('#typeList');
  tl.addEventListener('input', e => {
    const n = e.target.closest('[data-tname]');
    if (!n) return;
    const t = cfg.types.find(x => x.id === n.dataset.tname);
    if (!t) return;
    t.name = n.value;
    const opt = $(`#typeSelect option[value="${t.id}"]`);
    if (opt) opt.textContent = n.value;
    saveConfig();
  });
  tl.addEventListener('click', e => {
    const d = e.target.closest('[data-tdel]');
    if (!d) return;
    cfg.types = cfg.types.filter(t => t.id !== d.dataset.tdel);
    renderTypes(); saveConfig();
  });

  // settings
  bindNum('#setStart', v => { cfg.startYear = Math.round(v) || cfg.startYear; renderChips(); });
  bindNum('#setHorizon', v => { cfg.horizon = Math.min(12, Math.max(1, Math.round(v) || 1)); renderChips(); });
  bindNum('#setSuperRate', v => { cfg.superRate = Math.max(0, v || 0); });
  bindNum('#setCapBase', v => { cfg.superCapBase = Math.max(0, v || 0); });
  $('#setCapOn').addEventListener('change', e => { cfg.superCapOn = e.target.checked; renderOutputs(); });

  $('#rateBody').addEventListener('input', e => {
    const i = e.target.closest('[data-rate]');
    if (!i) return;
    const v = parseFloat(i.value);
    if (v > 0) cfg.overrides[i.dataset.rate] = v;
    else delete cfg.overrides[i.dataset.rate];
    renderOutputs();
  });

  $('#resetBtn').addEventListener('click', () => {
    const keep = { display: cfg.display, overrides: cfg.overrides };
    cfg = Object.assign(defaultConfig(), keep);
    fullRender();
  });
  $('#clearBtn').addEventListener('click', () => {
    cfg.awards = [];
    renderAwards(); renderGroups(); renderRates(); renderOutputs();
  });
}

function bindNum(sel, apply) {
  const el = $(sel);
  el.addEventListener('input', () => { apply(parseFloat(el.value)); renderOutputs(); });
}

function onAwardField(e, isChange) {
  // percent-of group multi-select
  const og = e.target.closest('[data-ofgroup]');
  if (og) {
    if (!isChange) return;
    const a = cfg.awards.find(x => x.id === og.dataset.id);
    if (!a) return;
    const gid = og.dataset.ofgroup;
    a.value.ofGroups = og.checked
      ? [...new Set([...a.value.ofGroups, gid])]
      : a.value.ofGroups.filter(x => x !== gid);
    renderOutputs();
    return;
  }

  const el = e.target.closest('[data-id][data-f]');
  if (!el) return;

  // selects and checkboxes act on change; text and number act live on input
  const discrete = el.tagName === 'SELECT' || el.type === 'checkbox';
  if (discrete !== isChange) return;

  const a = cfg.awards.find(x => x.id === el.dataset.id);
  if (!a) return;
  const path = el.dataset.path;
  const f = el.dataset.f;
  const target = path === 'root' ? a : a[path];

  if (el.type === 'checkbox') {
    target[f] = el.checked;
    if (f === 'enabled') el.closest('.award').classList.toggle('off', !el.checked);
    renderOutputs();
    return;
  }

  if (el.tagName === 'SELECT') {
    target[f] = el.value;
    if (f === 'mode' && el.value === 'percent' && !a.value.ofGroups.length) {
      // default a new percent award at the first group that isn't its own
      const other = cfg.groups.find(g => g.id !== a.groupId) || cfg.groups[0];
      if (other) a.value.ofGroups = [other.id];
    }
    if (f === 'groupId') {
      const g = groupById(el.value);
      if (g) a.superApplies = g.superDefault;
      renderGroups();
    }
    // mode/type/group changes swap which fields exist
    renderAwards(); renderRates(); renderOutputs();
    return;
  }

  if (el.type === 'text') { target[f] = el.value; renderOutputs(); return; }

  const v = parseFloat(el.value);
  target[f] = isNaN(v) ? 0 : v;
  renderOutputs();
}

function addAwardFromType(typeId) {
  const t = cfg.types.find(x => x.id === typeId);
  const spec = t ? JSON.parse(JSON.stringify(t.spec)) : {
    groupId: cfg.groups[0] && cfg.groups[0].id,
    superApplies: false,
    value: { mode: 'fixed', amount: 0, currency: 'AUD', pct: 0, ofGroups: [] },
    schedule: { type: 'recurring', startYear: null, years: 0, growth: 0, split: '' },
  };
  if (!cfg.groups.some(g => g.id === spec.groupId)) spec.groupId = cfg.groups[0].id;
  spec.value.ofGroups = (spec.value.ofGroups || []).filter(id => cfg.groups.some(g => g.id === id));
  if (spec.schedule.startYear == null) spec.schedule.startYear = cfg.startYear;

  const a = {
    id: uid('a'),
    label: t ? t.name : 'New Award',
    groupId: spec.groupId,
    enabled: true,
    superApplies: !!spec.superApplies,
    value: spec.value,
    schedule: spec.schedule,
  };
  cfg.awards.push(a);
  renderAwards(); renderGroups(); renderRates(); renderOutputs();
  const first = $(`#f-${a.id}-value-${a.value.mode === 'percent' ? 'pct' : 'amount'}`);
  if (first) { first.focus(); first.select(); }
}

function saveAwardAsType(awardId) {
  const a = cfg.awards.find(x => x.id === awardId);
  if (!a) return;
  const spec = JSON.parse(JSON.stringify({
    groupId: a.groupId,
    superApplies: a.superApplies,
    value: a.value,
    schedule: a.schedule,
  }));
  spec.value.amount = 0;              // a type carries the shape, not the money
  spec.schedule.startYear = null;     // and starts wherever it's dropped
  const existing = cfg.types.find(t => t.name === a.label);
  if (existing) existing.spec = spec;
  else cfg.types.push({ id: uid('t'), name: a.label, spec });
  renderTypes();
  saveConfig();
  $('#typeSelect').value = (existing || cfg.types[cfg.types.length - 1]).id;
}

/* ---------- boot ---------- */

function fullRender() {
  $('#displayCur').value = cfg.display;
  $('#setStart').value = cfg.startYear;
  $('#setHorizon').value = cfg.horizon;
  $('#setSuperRate').value = cfg.superRate;
  $('#setCapBase').value = cfg.superCapBase;
  $('#setCapOn').checked = !!cfg.superCapOn;
  renderAwards();
  renderChips();
  renderGroups();
  renderTypes();
  renderRates();
  renderOutputs();
}

async function init() {
  loadConfig();
  const fresh = loadCachedFx();
  bindEvents();
  fullRender();
  if (!fresh) {
    await fetchRates();
    renderRates();
    renderOutputs();
  }
}

init();
