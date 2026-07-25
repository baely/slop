(function () {
  const $ = (id) => document.getElementById(id);
  const STORE_KEY = 'stops.v1';

  // ---------- scales (third-stop steps, nominal values as marked on lenses) ----------

  const APERTURES = [1, 1.1, 1.2, 1.4, 1.6, 1.8, 2, 2.2, 2.5, 2.8, 3.2, 3.5, 4, 4.5, 5,
    5.6, 6.3, 7.1, 8, 9, 10, 11, 13, 14, 16, 18, 20, 22, 25, 29, 32, 36, 45];

  const SHUTTERS = (() => {
    const fractions = [8000, 6400, 5000, 4000, 3200, 2500, 2000, 1600, 1250, 1000, 800,
      640, 500, 400, 320, 250, 200, 160, 125, 100, 80, 60, 50, 40, 30, 25, 20, 15, 13,
      10, 8, 6, 5, 4, 3, 2.5, 2, 1.6];
    const longs = [1, 1.3, 1.6, 2, 2.5, 3, 4, 5, 6, 8, 10, 13, 15, 20, 25, 30, 40, 50,
      60, 80, 100, 120, 160, 200, 240];
    return [
      ...fractions.map((d) => ({ s: 1 / d, label: `1/${d % 1 ? d.toFixed(1) : d}` })),
      ...longs.map((s) => ({ s, label: s < 60 ? `${s}s` : `${Math.floor(s / 60)}m${s % 60 ? ' ' + (s % 60) + 's' : ''}` })),
    ];
  })();

  const ISOS = [12, 16, 25, 32, 50, 64, 80, 100, 125, 160, 200, 250, 320, 400, 500, 640,
    800, 1000, 1250, 1600, 2000, 2500, 3200, 4000, 5000, 6400];

  const SCENES = [
    { name: 'Sand Or Snow', ev: 16, note: 'Glare off water, snow, white sand.' },
    { name: 'Bright Sun', ev: 15, note: 'Distinct shadows. This is Sunny 16.' },
    { name: 'Hazy Sun', ev: 14, note: 'Soft-edged shadows.' },
    { name: 'Cloudy Bright', ev: 13, note: 'No real shadows, still bright.' },
    { name: 'Overcast', ev: 12, note: 'Flat grey light.' },
    { name: 'Open Shade', ev: 11, note: 'Shadow side of a building. Blue hour opens here.' },
    { name: 'Bright Interior', ev: 9, note: 'Lit shopfronts, neon, a well-lit room.' },
    { name: 'Home At Night', ev: 7, note: 'Domestic lamps.' },
    { name: 'Candlelight', ev: 4, note: 'One flame, close.' },
  ];

  // Reciprocity: exponent models use the Schwarzschild form t_corrected = t^p.
  const STOCKS = [
    { name: 'Ilford HP5+ / FP4+ / Delta', p: 1.31 },
    { name: 'Kodak Tri-X 400', p: 1.28 },
    { name: 'Kodak T-Max 100 / 400', p: 1.06 },
    { name: 'Fomapan 100 / 400', p: 1.62 },
    { name: 'Fujifilm Acros II', flat: true, note: 'None to 2m' },
    { name: 'Kodak Portra 400', portra: true },
  ];

  // ---------- state ----------

  const state = Object.assign(
    { sceneEV: 15, ap: APERTURES.indexOf(16), sh: SHUTTERS.findIndex((x) => x.label === '1/125'), iso: ISOS.indexOf(100), comp: 'shutter' },
    load()
  );

  function load() {
    try { return JSON.parse(localStorage.getItem(STORE_KEY)) || {}; } catch (e) { return {}; }
  }
  function save() {
    try { localStorage.setItem(STORE_KEY, JSON.stringify(state)); } catch (e) { /* private mode */ }
  }

  // ---------- exposure math ----------
  // EV100 = log2(N^2 / t) - log2(ISO / 100)

  const N = () => APERTURES[state.ap];
  const T = () => SHUTTERS[state.sh].s;
  const S = () => ISOS[state.iso];

  const evOf = (n, t, iso) => Math.log2((n * n) / t) - Math.log2(iso / 100);
  const evNow = () => evOf(N(), T(), S());

  // Nearest index on a scale, measured in log space so a stop is a stop.
  function nearest(values, target, get = (v) => v) {
    let best = 0;
    let bestErr = Infinity;
    for (let i = 0; i < values.length; i++) {
      const err = Math.abs(Math.log2(get(values[i])) - Math.log2(target));
      if (err < bestErr) { bestErr = err; best = i; }
    }
    return best;
  }

  // Move the compensating dial so the settings expose the scene correctly.
  // Returns {clamped, wanted} describing what the exact answer would have been.
  function recompute() {
    const k = Math.pow(2, state.sceneEV) * (S() / 100); // N^2 / t
    if (state.comp === 'shutter') {
      const wanted = (N() * N()) / (Math.pow(2, state.sceneEV) * (S() / 100));
      state.sh = nearest(SHUTTERS, wanted, (x) => x.s);
      return { wanted, kind: 'shutter', clamped: wanted > SHUTTERS[SHUTTERS.length - 1].s || wanted < SHUTTERS[0].s };
    }
    if (state.comp === 'aperture') {
      const wanted = Math.sqrt(k * T());
      state.ap = nearest(APERTURES, wanted);
      return { wanted, kind: 'aperture', clamped: wanted > APERTURES[APERTURES.length - 1] || wanted < APERTURES[0] };
    }
    const wanted = (100 * N() * N()) / (T() * Math.pow(2, state.sceneEV));
    state.iso = nearest(ISOS, wanted);
    return { wanted, kind: 'iso', clamped: wanted > ISOS[ISOS.length - 1] || wanted < ISOS[0] };
  }

  // ---------- formatting ----------

  function fmtStops(x) {
    const thirds = Math.round(x * 3);
    if (thirds === 0) return '0';
    const sign = thirds < 0 ? '-' : '+';
    const whole = Math.floor(Math.abs(thirds) / 3);
    const rem = Math.abs(thirds) % 3;
    const frac = rem === 1 ? '1/3' : rem === 2 ? '2/3' : '';
    return sign + [whole || '', frac].filter(Boolean).join(' ');
  }

  function fmtEV(x) {
    const r = Math.round(x * 10) / 10;
    return Number.isInteger(r) ? String(r) : r.toFixed(1);
  }

  function fmtSeconds(s) {
    if (s < 10) return `${s.toFixed(1)}s`;
    if (s < 60) return `${Math.round(s)}s`;
    const m = Math.floor(s / 60);
    const rem = Math.round(s % 60);
    return rem ? `${m}m ${rem}s` : `${m}m`;
  }

  // Under/over relative to the scene: positive means the settings are stopped
  // down past what the light supports, i.e. underexposed.
  // Anything inside half a scale step is exact as far as a camera is concerned —
  // the rest is nominal f-number drift (f/2.8 is really 2√2), not a real error.
  function deviationLabel(err, long) {
    if (Math.abs(err) < 1 / 6) return 'Exact';
    const amount = fmtStops(Math.abs(err)).replace('+', '');
    const unit = long ? (Math.abs(err) < 1.17 ? ' stop' : ' stops') : '';
    return `${amount}${unit} ${err > 0 ? 'under' : 'over'}`;
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  // ---------- render ----------

  function render(info) {
    const ev = evNow();
    const err = ev - state.sceneEV;

    $('scene-name').textContent = `EV ${fmtEV(state.sceneEV)}`;
    const scene = SCENES.find((s) => s.ev === state.sceneEV);
    $('scene-sub').textContent = scene
      ? scene.note
      : 'Between presets — metered by hand.';
    $('nav-ev').textContent = `f/${N()} · ${SHUTTERS[state.sh].label} · ISO ${S()}`;

    $('value-aperture').textContent = `f/${N()}`;
    $('value-shutter').textContent = SHUTTERS[state.sh].label;
    $('value-iso').textContent = String(S());

    for (const dial of document.querySelectorAll('.dial')) {
      dial.classList.toggle('compensating', dial.dataset.key === state.comp);
    }
    for (const b of document.querySelectorAll('#presets .preset')) {
      b.setAttribute('aria-pressed', String(Number(b.dataset.ev) === state.sceneEV));
    }
    for (const b of document.querySelectorAll('#compensators .preset')) {
      b.setAttribute('aria-pressed', String(b.dataset.comp === state.comp));
    }

    const compName = { shutter: 'Shutter', aperture: 'Aperture', iso: 'Film Speed' }[state.comp];
    $('comp-hint').textContent =
      `${compName} follows the other two. Move ${compName.toLowerCase()} itself and the scene is re-metered instead.`;

    // Warning line: only when the settings can't hit the scene exactly.
    let warning = '';
    if (info && info.clamped) {
      const want = info.kind === 'shutter' ? fmtSeconds(info.wanted)
        : info.kind === 'aperture' ? `f/${info.wanted.toFixed(1)}`
        : `ISO ${Math.round(info.wanted)}`;
      warning = `Out of range: needs ${want}. Clamped — ${deviationLabel(err, true)}.`;
    } else if (Math.abs(err) >= 1 / 6) {
      warning = `Nearest setting is ${deviationLabel(err, true)}.`;
    }
    $('warning').textContent = warning;

    renderEquivalents();
    renderReciprocity();
    save();
  }

  function renderEquivalents() {
    const body = $('equiv').tBodies[0];
    body.replaceChildren();
    const fullStops = [1.4, 2, 2.8, 4, 5.6, 8, 11, 16, 22];
    $('equiv-sub').textContent =
      `Same exposure at ISO ${S()}, EV ${fmtEV(state.sceneEV)}. Pick a row to use it.`;

    for (const n of fullStops) {
      const wanted = (n * n) / (Math.pow(2, state.sceneEV) * (S() / 100));
      const si = nearest(SHUTTERS, wanted, (x) => x.s);
      const err = evOf(n, SHUTTERS[si].s, S()) - state.sceneEV;
      const outOfRange = wanted > SHUTTERS[SHUTTERS.length - 1].s || wanted < SHUTTERS[0].s;

      const tr = el('tr');
      if (n === N() && si === state.sh) tr.className = 'selected';

      const cells = [
        [`f/${n}`, ''],
        [SHUTTERS[si].label, 'num'],
        [outOfRange ? 'Out of range' : deviationLabel(err), 'num'],
      ];
      cells.forEach(([text, cls], i) => {
        const td = el('td', cls);
        const btn = el('button', 'cell', text);
        btn.type = 'button';
        // One tab stop per row; the other cells are click targets only.
        if (i > 0) btn.tabIndex = -1;
        btn.addEventListener('click', () => {
          state.ap = APERTURES.indexOf(n);
          state.sh = si;
          render();
        });
        td.append(btn);
        tr.append(td);
      });
      body.append(tr);
    }
  }

  function renderReciprocity() {
    const t = T();
    const section = $('recip-section');
    if (t < 1) { section.hidden = true; return; }
    section.hidden = false;
    $('recip-sub').textContent =
      `Metered ${SHUTTERS[state.sh].label}. Past a second, film stops keeping up — these are the corrected times.`;

    const body = $('recip').tBodies[0];
    body.replaceChildren();
    for (const stock of STOCKS) {
      let corrected = null;
      let note = '';
      if (stock.flat) {
        corrected = t;
        note = t <= 120 ? stock.note : 'Beyond published range';
      } else if (stock.portra) {
        // Kodak publishes no correction to 1s and about +1/2 stop at 10s.
        if (t <= 10) {
          corrected = t * Math.pow(2, 0.5 * Math.log10(t));
        } else {
          note = 'Not published beyond 10s';
        }
      } else {
        corrected = Math.pow(t, stock.p);
      }

      const tr = el('tr');
      tr.append(el('td', null, stock.name));
      tr.append(el('td', 'num', corrected === null ? '—' : fmtSeconds(corrected)));
      tr.append(el('td', 'num', corrected === null ? note : (note || fmtStops(Math.log2(corrected / t)))));
      body.append(tr);
    }
  }

  // ---------- wiring ----------

  const presets = $('presets');
  for (const scene of SCENES) {
    const b = el('button', 'preset', scene.name);
    b.type = 'button';
    b.dataset.ev = String(scene.ev);
    b.setAttribute('aria-pressed', 'false');
    b.addEventListener('click', () => {
      state.sceneEV = scene.ev;
      render(recompute());
    });
    presets.append(b);
  }

  for (const btn of document.querySelectorAll('.dial-btns .btn')) {
    btn.addEventListener('click', () => {
      const key = btn.dataset.key;
      const dir = Number(btn.dataset.dir);
      const idxKey = { aperture: 'ap', shutter: 'sh', iso: 'iso' }[key];
      const len = { aperture: APERTURES.length, shutter: SHUTTERS.length, iso: ISOS.length }[key];
      const next = state[idxKey] + dir;
      if (next < 0 || next >= len) return;
      state[idxKey] = next;

      if (key === state.comp) {
        // Moving the compensator itself means you're metering a different light.
        // Snap to thirds so nominal f-numbers don't leave EV 14.94 lying around.
        state.sceneEV = Math.round(evNow() * 3) / 3;
        render();
      } else {
        render(recompute());
      }
    });
  }

  for (const btn of document.querySelectorAll('#compensators .preset')) {
    btn.addEventListener('click', () => {
      state.comp = btn.dataset.comp;
      render(recompute());
    });
  }

  render(recompute());
})();
