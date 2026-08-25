/* tc — total comp calculator */

const $ = (s, r = document) => r.querySelector(s);
const CFG_KEY = 'tc-calc-v1';
const FX_KEY = 'tc-calc-fx-v1';
const FX_MAX_AGE = 12 * 60 * 60 * 1000;

const CURRENCIES = ['AUD', 'USD', 'EUR', 'GBP', 'NZD', 'SGD', 'CAD', 'CHF', 'HKD', 'JPY', 'INR'];

const KINDS = {
  base:   { label: 'Base',         tag: 'Base',   defSuper: true,  defMode: 'annual' },
  bonus:  { label: 'Bonus',        tag: 'Bonus',  defSuper: true,  defMode: 'annual' },
  cash:   { label: 'Cash Grant',   tag: 'Cash',   defSuper: true,  defMode: 'total' },
  equity: { label: 'Equity Grant', tag: 'Equity', defSuper: false, defMode: 'total' },
  other:  { label: 'Other',        tag: 'Other',  defSuper: false, defMode: 'annual' },
};
const CAT_ORDER = ['base', 'bonus', 'super', 'cash', 'equity', 'other'];
const CAT_LABEL = { base: 'Base', bonus: 'Bonus', super: 'Super', cash: 'Cash', equity: 'Equity', other: 'Other' };

const PRESETS = [
  { key: 'everything', label: 'Everything', show: { base: 1, bonus: 1, super: 1, cash: 1, equity: 1, other: 1 } },
  { key: 'exsuper',    label: 'Ex-Super',   show: { base: 1, bonus: 1, super: 0, cash: 1, equity: 1, other: 1 } },
  { key: 'cash',       label: 'Cash Only',  show: { base: 1, bonus: 1, super: 0, cash: 1, equity: 0, other: 0 } },
  { key: 'guaranteed', label: 'Guaranteed', show: { base: 1, bonus: 0, super: 1, cash: 0, equity: 0, other: 0 } },
];

/* ---------- state ---------- */

let cfg = null;
let fx = { rates: { AUD: 1 }, source: null, fetched: null, date: null, error: null };
let nextId = 1;

function defaultConfig() {
  const y = new Date().getFullYear();
  return {
    startYear: y,
    horizon: 4,
    superRate: 12,
    superCapOn: false,
    superCapBase: 250000,
    display: 'AUD',
    selYear: y,
    show: { base: true, bonus: true, super: true, cash: true, equity: true, other: true },
    overrides: {},
    awards: [
      { id: 1, label: 'Base Salary', kind: 'base', currency: 'AUD', amount: 185000, mode: 'annual', startYear: y, years: 0, split: '', superEligible: true, enabled: true },
      { id: 2, label: 'Target Bonus', kind: 'bonus', currency: 'AUD', pct: 15, amount: 0, mode: 'annual', startYear: y, years: 0, split: '', superEligible: true, enabled: true },
      { id: 3, label: 'Equity — Initial Grant', kind: 'equity', currency: 'USD', amount: 160000, mode: 'total', startYear: y - 1, years: 4, split: '', superEligible: false, enabled: true },
      { id: 4, label: 'Equity — Refresh', kind: 'equity', currency: 'USD', amount: 90000, mode: 'total', startYear: y, years: 4, split: '', superEligible: false, enabled: true },
      { id: 5, label: 'Cash Grant — Sign-On', kind: 'cash', currency: 'USD', amount: 60000, mode: 'total', startYear: y, years: 2, split: '60/40', superEligible: true, enabled: true },
    ],
  };
}

function loadConfig() {
  try {
    const raw = localStorage.getItem(CFG_KEY);
    if (raw) {
      const saved = JSON.parse(raw);
      cfg = Object.assign(defaultConfig(), saved);
      cfg.show = Object.assign({ base: true, bonus: true, super: true, cash: true, equity: true, other: true }, saved.show || {});
      cfg.overrides = saved.overrides || {};
      if (!Array.isArray(cfg.awards)) cfg.awards = defaultConfig().awards;
    } else {
      cfg = defaultConfig();
    }
  } catch (e) {
    cfg = defaultConfig();
  }
  nextId = cfg.awards.reduce((m, a) => Math.max(m, a.id || 0), 0) + 1;
}

function saveConfig() {
  try { localStorage.setItem(CFG_KEY, JSON.stringify(cfg)); } catch (e) { /* private mode */ }
}

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
    const raw = localStorage.getItem(FX_KEY);
    if (!raw) return false;
    const c = JSON.parse(raw);
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

/* ---------- model helpers ---------- */

const yearList = () => Array.from({ length: cfg.horizon }, (_, i) => cfg.startYear + i);

function awardSpan(a) {
  if (a.mode === 'annual' && !(a.years > 0)) {
    return Math.max(1, cfg.startYear + cfg.horizon - a.startYear);
  }
  return Math.max(1, Math.round(a.years || 1));
}

function splitWeights(a, n) {
  const raw = String(a.split || '').split(/[/,\s]+/).map(parseFloat).filter(v => !isNaN(v));
  if (!raw.length) return { w: Array(n).fill(1 / n), custom: false, bad: false };
  if (raw.length !== n) return { w: Array(n).fill(1 / n), custom: false, bad: true };
  const sum = raw.reduce((x, y) => x + y, 0);
  if (sum <= 0) return { w: Array(n).fill(1 / n), custom: false, bad: true };
  return { w: raw.map(v => v / sum), custom: true, bad: false };
}

/** Base pay (AUD) in a given year — drives bonus, regardless of view toggles. */
function baseForYear(y) {
  return cfg.awards.reduce((sum, a) => {
    if (a.kind !== 'base' || !a.enabled) return sum;
    return sum + rawValue(a, y);
  }, 0);
}

/** AUD value of one award in one year, ignoring visibility toggles. */
function rawValue(a, y) {
  if (!a.enabled) return 0;
  const n = awardSpan(a);
  const idx = y - a.startYear;
  if (idx < 0 || idx >= n) return 0;
  if (a.kind === 'bonus') return ((a.pct || 0) / 100) * baseForYear(y);
  if (a.mode === 'annual') return toAUD(a.amount, a.currency);
  return toAUD((a.amount || 0) * splitWeights(a, n).w[idx], a.currency);
}

function computeYear(y) {
  const cats = { base: 0, bonus: 0, cash: 0, equity: 0, other: 0 };
  const perAward = {};
  let superBase = 0;

  for (const a of cfg.awards) {
    const v = rawValue(a, y);
    perAward[a.id] = v;
    cats[a.kind] = (cats[a.kind] || 0) + v;
    if (v && a.superEligible && cfg.show[a.kind]) superBase += v;
  }

  const capped = cfg.superCapOn ? Math.min(superBase, cfg.superCapBase || 0) : superBase;
  const sup = capped * (cfg.superRate || 0) / 100;

  let total = 0;
  for (const k of ['base', 'bonus', 'cash', 'equity', 'other']) {
    if (cfg.show[k]) total += cats[k];
  }
  if (cfg.show.super) total += sup;

  return { year: y, cats: Object.assign(cats, { super: sup }), perAward, superBase, total };
}

function computeAll() {
  const rows = yearList().map(computeYear);
  const sum = { cats: {}, total: 0, perAward: {} };
  CAT_ORDER.forEach(k => { sum.cats[k] = 0; });
  for (const r of rows) {
    CAT_ORDER.forEach(k => { sum.cats[k] += r.cats[k] || 0; });
    sum.total += r.total;
    for (const id in r.perAward) sum.perAward[id] = (sum.perAward[id] || 0) + r.perAward[id];
  }
  return { rows, sum };
}

function activePresetKey() {
  const hit = PRESETS.find(p => CAT_ORDER.every(k => !!p.show[k] === !!cfg.show[k]));
  return hit ? hit.key : null;
}

/* ---------- formatting ---------- */

/** Format an AUD amount in the current display currency. */
function money(aud) { return moneyIn(fromAUD(aud, cfg.display), cfg.display); }

/** Format an amount already denominated in `cur`. */
function moneyIn(amt, cur) {
  return new Intl.NumberFormat('en-AU', {
    style: 'currency', currency: cur, maximumFractionDigits: 0,
  }).format(amt || 0);
}

function pct(part, whole) {
  if (!whole) return '—';
  return (part / whole * 100).toFixed(1) + '%';
}

/* ---------- render: awards ---------- */

function renderAwards() {
  const host = $('#awardList');
  if (!cfg.awards.length) {
    host.innerHTML = '<p class="empty">0 awards. Add one above.</p>';
    return;
  }
  host.innerHTML = cfg.awards.map(a => {
    const k = KINDS[a.kind] || KINDS.other;
    const isBonus = a.kind === 'bonus';
    const isTotal = a.mode === 'total';
    const num = (f, label, opts = {}) => `<div class="field${opts.wide ? ' wide' : ''}">
        <label for="f-${a.id}-${f}">${label}</label>
        <input type="number" id="f-${a.id}-${f}" data-id="${a.id}" data-f="${f}"
          value="${a[f] === 0 && opts.zeroBlank ? '' : (a[f] ?? '')}"
          step="${opts.step || 'any'}" placeholder="${opts.ph || ''}"></div>`;

    return `<div class="award${a.enabled ? '' : ' off'}" data-id="${a.id}">
      <div class="award-head">
        <label class="toggle"><input type="checkbox" data-id="${a.id}" data-f="enabled" ${a.enabled ? 'checked' : ''}><span>Include</span></label>
        <input class="label-input" type="text" data-id="${a.id}" data-f="label" value="${escapeAttr(a.label)}" aria-label="Award Name">
        <span class="kind-tag">${k.tag}</span>
        <button class="btn tiny" type="button" data-remove="${a.id}">Remove</button>
      </div>
      <div class="award-grid">
        ${isBonus ? num('pct', 'Percent Of Base', { step: '0.5' }) : num('amount', 'Amount', { step: '1000' })}
        ${isBonus ? '' : `<div class="field">
          <label for="f-${a.id}-currency">Currency</label>
          <select id="f-${a.id}-currency" data-id="${a.id}" data-f="currency">
            ${CURRENCIES.map(c => `<option value="${c}"${c === a.currency ? ' selected' : ''}>${c}</option>`).join('')}
          </select></div>`}
        ${isBonus ? '' : `<div class="field">
          <label for="f-${a.id}-mode">Basis</label>
          <select id="f-${a.id}-mode" data-id="${a.id}" data-f="mode">
            <option value="annual"${a.mode === 'annual' ? ' selected' : ''}>Per Year</option>
            <option value="total"${a.mode === 'total' ? ' selected' : ''}>Multi-Year</option>
          </select></div>`}
        ${num('startYear', 'First Year', { step: '1' })}
        ${num('years', 'Years', { step: '1', zeroBlank: true, ph: a.mode === 'annual' ? 'Ongoing' : '4' })}
        ${isTotal && !isBonus ? `<div class="field wide">
          <label for="f-${a.id}-split">Vest Split %</label>
          <input type="text" id="f-${a.id}-split" data-id="${a.id}" data-f="split" value="${escapeAttr(a.split)}" placeholder="Even — or 25/25/25/25"></div>` : ''}
      </div>
      <div class="award-foot">
        <label class="toggle"><input type="checkbox" data-id="${a.id}" data-f="superEligible" ${a.superEligible ? 'checked' : ''}><span>Super Applies</span></label>
        <span class="award-sched" id="sched-${a.id}"></span>
      </div>
    </div>`;
  }).join('');
}

function escapeAttr(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
}

function renderAwardScheds() {
  for (const a of cfg.awards) {
    const el = $('#sched-' + a.id);
    if (!el) continue;
    const n = awardSpan(a);
    const sw = splitWeights(a, n);
    const parts = [];
    for (let i = 0; i < n; i++) {
      const y = a.startYear + i;
      parts.push(`${y} ${money(rawValue(a, y))}`);
    }
    const warn = sw.bad ? '<span class="warn">split ignored — needs ' + n + ' numbers</span> · ' : '';
    el.innerHTML = warn + (parts.length ? parts.join(' · ') : 'no vesting years');
  }
}

/* ---------- render: chips ---------- */

function renderChips() {
  $('#yearChips').innerHTML = yearList()
    .map(y => `<button class="chip-btn" type="button" data-year="${y}">${y}</button>`)
    .join('') +
    `<button class="chip-btn" type="button" data-year="avg">Average</button>` +
    `<button class="chip-btn" type="button" data-year="total">${cfg.horizon}-Year Total</button>`;

  $('#presets').innerHTML = PRESETS
    .map(p => `<button class="chip-btn" type="button" data-preset="${p.key}">${p.label}</button>`)
    .join('');

  $('#toggles').innerHTML = CAT_ORDER
    .map(k => `<label class="toggle"><input type="checkbox" data-show="${k}"><span>${CAT_LABEL[k]}</span></label>`)
    .join('');
}

function syncChips() {
  const sel = String(cfg.selYear);
  document.querySelectorAll('[data-year]').forEach(b => {
    b.setAttribute('aria-pressed', String(b.dataset.year === sel));
  });
  const ap = activePresetKey();
  document.querySelectorAll('[data-preset]').forEach(b => {
    b.setAttribute('aria-pressed', String(b.dataset.preset === ap));
  });
  document.querySelectorAll('[data-show]').forEach(c => {
    c.checked = !!cfg.show[c.dataset.show];
  });
}

/* ---------- render: outputs ---------- */

function selection(all) {
  if (cfg.selYear === 'total') {
    return { label: `${cfg.horizon}-Year Total`, cats: all.sum.cats, total: all.sum.total };
  }
  if (cfg.selYear === 'avg') {
    const n = Math.max(1, all.rows.length);
    const cats = {};
    CAT_ORDER.forEach(k => { cats[k] = all.sum.cats[k] / n; });
    return { label: 'Average Year', cats, total: all.sum.total / n };
  }
  const row = all.rows.find(r => r.year === Number(cfg.selYear)) || all.rows[0];
  if (!row) return { label: '—', cats: {}, total: 0 };
  return { label: String(row.year), cats: row.cats, total: row.total };
}

function renderOutputs() {
  const all = computeAll();
  const sel = selection(all);
  const presetLabel = (PRESETS.find(p => p.key === activePresetKey()) || {}).label || 'Custom View';
  const alt = cfg.display === 'AUD' ? 'USD' : 'AUD';

  $('#tcLabel').textContent = cfg.selYear === 'total' ? 'Total Compensation, Cumulative' : 'Total Compensation';
  $('#tcValue').textContent = money(sel.total);
  $('#tcContext').textContent = `${sel.label} · ${presetLabel} · ${moneyIn(fromAUD(sel.total, alt), alt)}`;

  // breakdown
  $('#breakdownBody').innerHTML = CAT_ORDER.map(k => {
    const v = sel.cats[k] || 0;
    const has = k === 'super' ? true : cfg.awards.some(a => a.kind === k);
    if (!has && !v) return '';
    const on = cfg.show[k];
    return `<tr class="${on ? '' : 'off'}">
      <td>${CAT_LABEL[k]}${on ? '' : ' <span class="mono">(excluded)</span>'}</td>
      <td class="num">${money(v)}</td>
      <td class="num">${on ? pct(v, sel.total) : '—'}</td>
    </tr>`;
  }).join('') + `<tr><td class="total">Total</td><td class="num total">${money(sel.total)}</td><td class="num total">${sel.total ? '100%' : '—'}</td></tr>`;

  // by year
  const cats = CAT_ORDER.filter(k => k === 'super' || cfg.awards.some(a => a.kind === k));
  $('#yearHead').innerHTML = '<th>Year</th>' +
    cats.map(k => `<th class="num${cfg.show[k] ? '' : ' off'}">${CAT_LABEL[k]}</th>`).join('') +
    '<th class="num total">TC</th>';
  $('#yearBody').innerHTML = all.rows.map(r => `<tr class="${String(r.year) === String(cfg.selYear) ? 'selected' : ''}">
    <td class="num">${r.year}</td>
    ${cats.map(k => `<td class="num${cfg.show[k] ? '' : ' off'}">${money(r.cats[k] || 0)}</td>`).join('')}
    <td class="num total">${money(r.total)}</td></tr>`).join('');
  $('#yearFoot').innerHTML = '<td>Total</td>' +
    cats.map(k => `<td class="num${cfg.show[k] ? '' : ' off'}">${money(all.sum.cats[k] || 0)}</td>`).join('') +
    `<td class="num total">${money(all.sum.total)}</td>`;

  // vesting schedule
  const years = yearList();
  $('#schedHead').innerHTML = '<th>Award</th>' +
    years.map(y => `<th class="num">${y}</th>`).join('') +
    '<th class="num total">In Window</th>';
  $('#schedBody').innerHTML = cfg.awards.length
    ? cfg.awards.map(a => {
        const on = cfg.show[a.kind] && a.enabled;
        return `<tr class="${on ? '' : 'off'}">
          <td>${escapeAttr(a.label)} <span class="mono row-tag">· ${KINDS[a.kind].tag}${a.currency !== 'AUD' && a.kind !== 'bonus' ? ' · ' + a.currency : ''}</span></td>
          ${years.map(y => `<td class="num">${money(all.rows.find(r => r.year === y).perAward[a.id] || 0)}</td>`).join('')}
          <td class="num total">${money(all.sum.perAward[a.id] || 0)}</td></tr>`;
      }).join('')
    : `<tr><td colspan="${years.length + 2}" class="empty">0 awards.</td></tr>`;

  renderAwardScheds();
  syncChips();
  renderFxLine();
  updateRateSources();
  saveConfig();
}

/* ---------- render: rates ---------- */

function usedCurrencies() {
  const set = new Set(['USD', cfg.display, 'AUD']);
  cfg.awards.forEach(a => { if (a.kind !== 'bonus') set.add(a.currency || 'AUD'); });
  return [...set].filter(c => c !== 'AUD').sort();
}

function renderRates() {
  $('#rateBody').innerHTML = usedCurrencies().map(c => `<tr>
    <td>${c}</td>
    <td class="num"><input class="rate-input" type="number" step="0.0001" data-rate="${c}"
      value="${cfg.overrides[c] > 0 ? cfg.overrides[c] : ''}" placeholder="${audPerUnit(c).toFixed(4)}"></td>
    <td class="mono" id="rate-src-${c}"></td></tr>`).join('') ||
    '<tr><td colspan="3" class="empty">Everything is in AUD.</td></tr>';
  updateRateSources();
}

function updateRateSources() {
  usedCurrencies().forEach(c => {
    const el = $('#rate-src-' + c);
    if (!el) return;
    el.textContent = cfg.overrides[c] > 0 ? 'manual' : (fx.rates[c] ? (fx.source || 'cached') : 'unavailable');
  });
}

/* ---------- events ---------- */

function bindEvents() {
  // display currency
  const dc = $('#displayCur');
  dc.innerHTML = CURRENCIES.map(c => `<option value="${c}">${c}</option>`).join('');
  dc.value = cfg.display;
  dc.addEventListener('change', () => { cfg.display = dc.value; renderRates(); renderOutputs(); });

  $('#fxRefresh').addEventListener('click', async () => {
    $('#fxLine').textContent = 'fx refreshing…';
    await fetchRates();
    renderRates();
    renderOutputs();
  });

  // year / preset / toggle chips
  $('#yearChips').addEventListener('click', e => {
    const b = e.target.closest('[data-year]');
    if (!b) return;
    const v = b.dataset.year;
    cfg.selYear = (v === 'avg' || v === 'total') ? v : Number(v);
    renderOutputs();
  });
  $('#presets').addEventListener('click', e => {
    const b = e.target.closest('[data-preset]');
    if (!b) return;
    const p = PRESETS.find(x => x.key === b.dataset.preset);
    CAT_ORDER.forEach(k => { cfg.show[k] = !!p.show[k]; });
    renderOutputs();
  });
  $('#toggles').addEventListener('change', e => {
    const c = e.target.closest('[data-show]');
    if (!c) return;
    cfg.show[c.dataset.show] = c.checked;
    renderOutputs();
  });

  // award edits
  const list = $('#awardList');
  list.addEventListener('input', e => onAwardField(e, false));
  list.addEventListener('change', e => onAwardField(e, true));
  list.addEventListener('click', e => {
    const rm = e.target.closest('[data-remove]');
    if (!rm) return;
    cfg.awards = cfg.awards.filter(a => a.id !== Number(rm.dataset.remove));
    renderAwards(); renderRates(); renderOutputs();
  });

  // add award
  document.querySelectorAll('[data-add]').forEach(b => {
    b.addEventListener('click', () => addAward(b.dataset.add));
  });

  // settings
  bindNum('#setStart', 'startYear', v => { cfg.startYear = Math.round(v) || cfg.startYear; renderChips(); });
  bindNum('#setHorizon', 'horizon', v => { cfg.horizon = Math.min(10, Math.max(1, Math.round(v) || 1)); renderChips(); });
  bindNum('#setSuperRate', 'superRate', v => { cfg.superRate = Math.max(0, v || 0); });
  bindNum('#setCapBase', 'superCapBase', v => { cfg.superCapBase = Math.max(0, v || 0); });
  $('#setCapOn').addEventListener('change', e => { cfg.superCapOn = e.target.checked; renderOutputs(); });

  // rate overrides
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
    nextId = cfg.awards.length + 1;
    fullRender();
  });
  $('#clearBtn').addEventListener('click', () => {
    cfg.awards = [];
    renderAwards(); renderRates(); renderOutputs();
  });
}

function bindNum(sel, key, apply) {
  const el = $(sel);
  el.value = cfg[key];
  el.addEventListener('input', () => {
    apply(parseFloat(el.value));
    renderOutputs();
  });
}

function onAwardField(e, isChange) {
  const el = e.target.closest('[data-id][data-f]');
  if (!el) return;

  // Selects and checkboxes fire both input and change; text/number fields are
  // handled live on input. Taking each from one event only avoids double work
  // and stops a rebuild from re-entering on the detached element.
  const discrete = el.tagName === 'SELECT' || el.type === 'checkbox';
  if (discrete !== isChange) return;

  const a = cfg.awards.find(x => x.id === Number(el.dataset.id));
  if (!a) return;
  const f = el.dataset.f;

  if (el.type === 'checkbox') {
    a[f] = el.checked;
    if (f === 'enabled') el.closest('.award').classList.toggle('off', !el.checked);
    renderOutputs();
    return;
  }
  if (f === 'currency') { a.currency = el.value; renderRates(); renderOutputs(); return; }
  if (f === 'mode') { a.mode = el.value; renderAwards(); renderOutputs(); return; }
  if (f === 'label' || f === 'split') { a[f] = el.value; renderOutputs(); return; }

  const v = parseFloat(el.value);
  a[f] = isNaN(v) ? 0 : v;
  renderOutputs();
}

function addAward(kind) {
  const k = KINDS[kind];
  const a = {
    id: nextId++,
    label: k.label,
    kind,
    currency: kind === 'equity' || kind === 'cash' ? 'USD' : 'AUD',
    amount: kind === 'bonus' ? 0 : 0,
    pct: kind === 'bonus' ? 10 : 0,
    mode: k.defMode,
    startYear: cfg.startYear,
    years: k.defMode === 'total' ? 4 : 0,
    split: '',
    superEligible: k.defSuper,
    enabled: true,
  };
  cfg.awards.push(a);
  renderAwards(); renderRates(); renderOutputs();
  const first = $(`#f-${a.id}-${kind === 'bonus' ? 'pct' : 'amount'}`);
  if (first) first.focus();
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
