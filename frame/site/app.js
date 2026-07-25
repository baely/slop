/* frame — source shape vs target display, drawn to scale.
   All maths is exact rational-ish arithmetic on the ratio; pixel figures are
   rounded only at print time. */

/* ---------- pure maths (also loaded by the node verification script) ---------- */

function gcd(a, b) {
  a = Math.abs(a);
  b = Math.abs(b);
  while (b) { const t = a % b; a = b; b = t; }
  return a;
}

// Reduce a w:h pair to whole numbers. Decimals are lifted by the smallest power
// of ten that clears the fraction, so 2.39:1 becomes 239:100 before the GCD.
function reduceRatio(w, h) {
  const whole = (n) => Math.abs(n - Math.round(n)) < 1e-9;
  let p = 0;
  while (p < 6 && !(whole(w) && whole(h))) { w *= 10; h *= 10; p++; }
  w = Math.round(w);
  h = Math.round(h);
  const g = gcd(w, h) || 1;
  return [w / g, h / g];
}

// Loose parse: "2.39", "16:9", "16 9", "1920x1080", "3/2", "1.85 : 1", "4 by 5".
function parseSpec(str) {
  const raw = String(str == null ? '' : str).trim();
  if (!raw) return { ok: false, error: 'Nothing typed.' };

  const cleaned = raw.toLowerCase()
    .replace(/px\b/g, ' ')                      // "1920px x 1080px"
    .replace(/[×x*:\/,]|\bby\b|\s+/g, ' ')      // every separator people actually type
    .trim();
  const tokens = cleaned.split(' ').filter(Boolean);

  if (tokens.length > 2) {
    return { ok: false, error: 'Too many numbers (' + tokens.length + '). Give one or two.' };
  }
  for (const t of tokens) {
    if (!/^\d*\.?\d+$/.test(t)) {
      return { ok: false, error: 'Could not read "' + raw + '". Try 2.39, 16:9 or 1920x1080.' };
    }
  }

  const w = parseFloat(tokens[0]);
  const h = tokens.length === 2 ? parseFloat(tokens[1]) : 1;
  if (!isFinite(w) || !isFinite(h) || w <= 0 || h <= 0) {
    return { ok: false, error: 'Both numbers must be greater than zero.' };
  }

  const ar = w / h;
  if (ar < 0.05 || ar > 20) {
    return { ok: false, error: 'Ratio ' + ar.toFixed(3) + ' is outside the 0.05–20 range.' };
  }

  // Two whole numbers this large read as a pixel size, not a ratio.
  const isPixels = tokens.length === 2 &&
    Number.isInteger(w) && Number.isInteger(h) && w >= 100 && h >= 100;

  return { ok: true, w: w, h: h, ar: ar, isPixels: isPixels };
}

/* Geometry of a source of aspect sAR placed on a tW x tH panel.
   fit  = contain, bars appear on one axis, nothing is lost.
   fill = cover, the panel is full and the overflow is cut. */
function computeGeometry(sAR, tW, tH, mode) {
  const tAR = tW / tH;
  let iW, iH;

  if (mode === 'fill' ? sAR >= tAR : sAR <= tAR) {
    // height is the binding edge
    iH = tH;
    iW = tH * sAR;
  } else {
    iW = tW;
    iH = tW / sAR;
  }

  const barX = (tW - iW) / 2;   // negative in fill mode = overflow per side
  const barY = (tH - iH) / 2;

  return {
    tAR: tAR,
    imageW: iW,
    imageH: iH,
    barX: Math.max(0, barX),
    barY: Math.max(0, barY),
    // fraction of the panel actually carrying picture
    used: mode === 'fill' ? 1 : (iW * iH) / (tW * tH),
    // fraction of the source frame lost when covering (same either mode:
    // it is what fill would cut, which fit mode still reports)
    cropFrac: sAR >= tAR ? 1 - tAR / sAR : 1 - sAR / tAR,
    cropAxis: Math.abs(sAR - tAR) < 1e-12 ? 'none' : (sAR >= tAR ? 'width' : 'height'),
    barAxis: Math.abs(sAR - tAR) < 1e-12 ? 'none' : (sAR >= tAR ? 'height' : 'width')
  };
}

/* Display size for the target rectangle so that the rectangle — plus, in fill
   mode, the cropped overflow drawn outside it — fits the stage. */
function fitBox(availW, availH, tAR, sAR, mode) {
  let bw = tAR, bh = 1;
  let uw = bw, uh = bh;
  if (mode === 'fill') {
    uw = Math.max(bw, bh * sAR);
    uh = Math.max(bh, bw / sAR);
  }
  const k = Math.min(availW / uw, availH / uh);
  return { w: bw * k, h: bh * k };
}

/* ---------- data ---------- */

const SOURCES = {
  silent:    { label: 'Silent aperture 1.33:1', w: 4, h: 3, note: '1.33:1 — the full silent aperture, before the optical sound track took a strip off the side.' },
  academy:   { label: 'Academy 1.37:1', w: 137, h: 100, note: '1.37:1 — the Academy standard set in 1932; the silent frame masked back to square-ish after sound.' },
  imax:      { label: 'IMAX 1.43:1', w: 143, h: 100, note: '1.43:1 — 15-perf 70mm run horizontally. Only a true IMAX screen shows the whole negative.' },
  flat:      { label: 'Widescreen flat 1.85:1', w: 185, h: 100, note: '1.85:1 — the US widescreen default since 1953; a spherical negative masked down in projection.' },
  univisium: { label: 'Univisium 2.00:1', w: 2, h: 1, note: '2.00:1 — Storaro’s compromise between flat and scope; now common on streaming originals.' },
  seventy:   { label: '70mm 2.20:1', w: 220, h: 100, note: '2.20:1 — 65mm negative, 70mm print. Todd-AO from 1955, still the roadshow shape.' },
  scope:     { label: 'Scope 2.39:1', w: 239, h: 100, note: '2.39:1 — anamorphic, squeezed 2x onto the negative. Standardised at 2.39 in 1970; 2.35 before that.' },

  halfframe: { label: 'Half-frame 18×24', w: 3, h: 4, note: '18×24mm, portrait — two frames per 35mm still frame, 72 exposures on a roll of 36.' },
  m645:      { label: '6×4.5', w: 4, h: 3, note: '56×42mm on 120 roll film, 16 frames a roll. The cheapest way into medium format.' },
  m66:       { label: '6×6 square', w: 1, h: 1, note: '56×56mm — square, so the camera never needs turning.' },
  m67:       { label: '6×7', w: 5, h: 4, note: '56×70mm — the “ideal format”: enlarges to 8×10 with nothing trimmed.' },
  lf45:      { label: '4×5 large format', w: 5, h: 4, note: '4×5in sheet film, about 95×120mm of negative per shot.' },
  s35:       { label: '35mm 3:2', w: 3, h: 2, note: '36×24mm — two cine frames laid sideways, which is where 3:2 came from.' },

  d43:  { label: '4:3', w: 4, h: 3, note: '4:3 — every television made before roughly 2005.' },
  d11:  { label: '1:1 square', w: 1, h: 1, note: '1:1 — square, as the feed prefers it.' },
  d169: { label: '16:9', w: 16, h: 9, note: '16:9 — chosen as the geometric mean of 4:3 and 2.35, so both lose about the same.' },
  d916: { label: '9:16 vertical', w: 9, h: 16, note: '9:16 — 16:9 stood upright. Native to the phone, hostile to everything else.' }
};

const TARGETS = {
  t169:    { label: '16:9 TV', w: 1920, h: 1080, note: '1920×1080 — the standard panel.' },
  t219:    { label: '21:9 Ultrawide', w: 2560, h: 1080, note: '2560×1080 — reduces to 64:27, not 21:9. The name is rounded.' },
  t43:     { label: '4:3', w: 1600, h: 1200, note: '1600×1200 — the old monitor, and the projector in the meeting room.' },
  tphone:  { label: 'Phone 19.5:9', w: 1170, h: 2532, note: '1170×2532 — 19.5:9 nominal, 195:422 exactly.' },
  tcustom: { label: 'Custom', w: 1920, h: 1200, note: 'Whatever you type.' }
};

/* ---------- app ---------- */

function boot() {
  const $ = (id) => document.getElementById(id);

  const el = {
    navPair: $('navPair'),
    stage: $('stage'),
    frameBox: $('frameBox'),
    ghost: $('ghost'),
    imgBox: $('imgBox'),
    imgLabel: $('imgLabel'),
    legend: $('legend'),
    keyCrop: $('keyCrop'),
    sourceSel: $('sourceSel'),
    sourceNote: $('sourceNote'),
    targetSeg: $('targetSeg'),
    customRow: $('customRow'),
    customW: $('customW'),
    customH: $('customH'),
    targetNote: $('targetNote'),
    modeSeg: $('modeSeg'),
    modeNote: $('modeNote'),
    freeIn: $('freeIn'),
    useSource: $('useSource'),
    useTarget: $('useTarget'),
    parseNote: $('parseNote'),
    rSource: $('rSource'),
    rTarget: $('rTarget'),
    rTargetPx: $('rTargetPx'),
    rImagePx: $('rImagePx'),
    rUsed: $('rUsed'),
    rBars: $('rBars'),
    rCrop: $('rCrop')
  };

  const KEY = 'frame.v1';
  const state = {
    source: 'scope',
    customSource: null,      // { w, h } from typed input
    target: 't169',
    customTarget: { w: 1920, h: 1200 },
    mode: 'fit',
    free: ''
  };

  function load() {
    try {
      const saved = JSON.parse(localStorage.getItem(KEY) || 'null');
      if (!saved || typeof saved !== 'object') return;
      if (SOURCES[saved.source] || saved.source === 'custom-source') state.source = saved.source;
      if (saved.customSource && saved.customSource.w > 0 && saved.customSource.h > 0) {
        state.customSource = { w: +saved.customSource.w, h: +saved.customSource.h };
      }
      if (TARGETS[saved.target]) state.target = saved.target;
      if (saved.customTarget && saved.customTarget.w > 0 && saved.customTarget.h > 0) {
        state.customTarget = { w: +saved.customTarget.w, h: +saved.customTarget.h };
      }
      if (saved.mode === 'fit' || saved.mode === 'fill') state.mode = saved.mode;
      if (typeof saved.free === 'string') state.free = saved.free.slice(0, 40);
      if (state.source === 'custom-source' && !state.customSource) state.source = 'scope';
    } catch (e) { /* private mode, or corrupt value: defaults are fine */ }
  }

  function save() {
    try { localStorage.setItem(KEY, JSON.stringify(state)); } catch (e) { /* ignore */ }
  }

  /* ---- formatting ---- */

  const px = (n) => String(Math.round(n));
  const pct = (n, d) => (n * 100).toFixed(d === undefined ? 1 : d) + '%';
  const dec = (ar) => ar >= 1 ? ar.toFixed(2) : ar.toFixed(3);
  const ratioText = (w, h) => { const r = reduceRatio(w, h); return r[0] + ':' + r[1]; };

  /* ---- current selection ---- */

  function currentSource() {
    if (state.source === 'custom-source') {
      const c = state.customSource || { w: 239, h: 100 };
      return { label: 'Custom ' + ratioText(c.w, c.h), w: c.w, h: c.h, note: 'Typed: ' + dec(c.w / c.h) + ':1.' };
    }
    return SOURCES[state.source] || SOURCES.scope;
  }

  function currentTarget() {
    if (state.target === 'tcustom') {
      const c = state.customTarget;
      return { label: 'Custom', w: c.w, h: c.h, note: TARGETS.tcustom.note };
    }
    return TARGETS[state.target];
  }

  /* ---- draw ---- */

  function draw() {
    const src = currentSource();
    const tgt = currentTarget();
    const sAR = src.w / src.h;
    const geo = computeGeometry(sAR, tgt.w, tgt.h, state.mode);

    // stage box, minus a little breathing room
    const availW = Math.max(40, el.stage.clientWidth - 8);
    const availH = Math.max(40, el.stage.clientHeight - 8);
    const box = fitBox(availW, availH, geo.tAR, sAR, state.mode);

    el.frameBox.style.width = box.w + 'px';
    el.frameBox.style.height = box.h + 'px';

    // scale the panel-space rectangle into display space
    const k = box.w / tgt.w;
    const dw = geo.imageW * k;
    const dh = geo.imageH * k;
    const dl = (box.w - dw) / 2;
    const dt = (box.h - dh) / 2;

    const place = (node) => {
      node.style.left = dl + 'px';
      node.style.top = dt + 'px';
      node.style.width = dw + 'px';
      node.style.height = dh + 'px';
    };
    place(el.imgBox);
    place(el.ghost);

    const cropping = state.mode === 'fill' && geo.cropAxis !== 'none';
    el.ghost.hidden = !cropping;
    el.keyCrop.hidden = !cropping;

    // label only when the *visible* slab can hold it (fill mode clips the image)
    const visW = Math.min(dw, box.w);
    const visH = Math.min(dh, box.h);
    el.imgLabel.textContent = (visW >= 96 && visH >= 24)
      ? px(Math.min(geo.imageW, tgt.w)) + '×' + px(Math.min(geo.imageH, tgt.h))
      : '';

    report(src, tgt, sAR, geo);
  }

  function report(src, tgt, sAR, geo) {
    const barsHoriz = geo.barAxis === 'height';   // bars top and bottom
    const cropPxEach = geo.cropAxis === 'width'
      ? (geo.imageW - tgt.w) / 2
      : (geo.imageH - tgt.h) / 2;

    el.navPair.textContent = dec(sAR) + ' → ' + dec(geo.tAR);

    el.rSource.textContent = dec(sAR) + '  ·  ' + ratioText(src.w, src.h);
    el.rTarget.textContent = dec(geo.tAR) + '  ·  ' + ratioText(tgt.w, tgt.h);
    el.rTargetPx.textContent = px(tgt.w) + ' × ' + px(tgt.h);

    if (state.mode === 'fit') {
      el.rImagePx.textContent = px(geo.imageW) + ' × ' + px(geo.imageH);
      el.rUsed.textContent = pct(geo.used);
      if (geo.barAxis === 'none') {
        el.rBars.textContent = 'None — exact match';
      } else if (barsHoriz) {
        el.rBars.textContent = px(geo.barY) + ' px top, ' + px(geo.barY) + ' px bottom  ·  '
          + px(geo.barY * 2) + ' px  ·  ' + pct((geo.barY * 2) / tgt.h) + ' of height';
      } else {
        el.rBars.textContent = px(geo.barX) + ' px left, ' + px(geo.barX) + ' px right  ·  '
          + px(geo.barX * 2) + ' px  ·  ' + pct((geo.barX * 2) / tgt.w) + ' of width';
      }
      el.rCrop.textContent = geo.cropAxis === 'none'
        ? 'None'
        : 'Nothing — Fill would cut ' + pct(geo.cropFrac) + ' of ' + geo.cropAxis;
      el.legend.textContent = px(tgt.w) + '×' + px(tgt.h) + ' panel · '
        + px(geo.imageW) + '×' + px(geo.imageH) + ' image · '
        + (geo.barAxis === 'none'
            ? 'no bars'
            : (barsHoriz ? px(geo.barY) + 'px bars top and bottom' : px(geo.barX) + 'px bars left and right'));
    } else {
      el.rImagePx.textContent = px(geo.imageW) + ' × ' + px(geo.imageH)
        + '  (' + px(tgt.w) + ' × ' + px(tgt.h) + ' shown)';
      el.rUsed.textContent = '100.0%';
      el.rBars.textContent = 'None — the panel is full';
      el.rCrop.textContent = geo.cropAxis === 'none'
        ? 'None — exact match'
        : pct(geo.cropFrac) + ' of ' + geo.cropAxis + '  ·  ' + px(cropPxEach) + ' px each side  ·  '
          + px(cropPxEach * 2) + ' px';
      el.legend.textContent = px(tgt.w) + '×' + px(tgt.h) + ' panel · '
        + px(geo.imageW) + '×' + px(geo.imageH) + ' image · '
        + (geo.cropAxis === 'none' ? 'nothing cut' : px(cropPxEach) + 'px cut off each side');
    }

    el.sourceNote.textContent = src.note;
    el.targetNote.textContent = tgt.note;
    el.modeNote.textContent = state.mode === 'fit'
      ? 'Fit letterboxes: nothing is lost, the panel is not filled.'
      : 'Fill crops: the panel is full, the edges are gone.';
  }

  /* ---- ui wiring ---- */

  function setSourceOptionLabel() {
    const opt = el.sourceSel.querySelector('option[value="custom-source"]');
    if (!opt) return;
    opt.textContent = state.customSource
      ? 'Custom — ' + ratioText(state.customSource.w, state.customSource.h)
      : 'Custom — type below';
  }

  function syncControls() {
    setSourceOptionLabel();
    el.sourceSel.value = state.source;
    el.targetSeg.querySelectorAll('button').forEach((b) => {
      b.setAttribute('aria-pressed', String(b.dataset.target === state.target));
    });
    el.modeSeg.querySelectorAll('button').forEach((b) => {
      b.setAttribute('aria-pressed', String(b.dataset.mode === state.mode));
    });
    el.customRow.hidden = state.target !== 'tcustom';
    el.customW.value = String(state.customTarget.w);
    el.customH.value = String(state.customTarget.h);
  }

  function apply() { syncControls(); draw(); save(); }

  function setParseNote(text, bad) {
    el.parseNote.textContent = text;
    el.parseNote.classList.toggle('bad', !!bad);
  }

  el.sourceSel.addEventListener('change', () => {
    state.source = el.sourceSel.value;
    if (state.source === 'custom-source' && !state.customSource) {
      const prev = SOURCES.scope;
      state.customSource = { w: prev.w, h: prev.h };
      setParseNote('Custom source seeded at 2.39:1. Type a ratio below and press Use As Source.', false);
      el.freeIn.focus();
    }
    apply();
  });

  el.targetSeg.addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-target]');
    if (!btn) return;
    state.target = btn.dataset.target;
    apply();
    if (state.target === 'tcustom') el.customW.focus();
  });

  el.modeSeg.addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-mode]');
    if (!btn) return;
    state.mode = btn.dataset.mode;
    apply();
  });

  function readCustomTarget() {
    const w = parseInt(el.customW.value, 10);
    const h = parseInt(el.customH.value, 10);
    if (!isFinite(w) || !isFinite(h) || w < 1 || h < 1) {
      setParseNote('Width and height must be whole numbers of 1 or more. Still showing '
        + state.customTarget.w + '×' + state.customTarget.h + '.', true);
      return;
    }
    if (w > 100000 || h > 100000) {
      setParseNote('Cap is 100000 px per side. Still showing '
        + state.customTarget.w + '×' + state.customTarget.h + '.', true);
      return;
    }
    state.customTarget = { w: w, h: h };
    setParseNote('Target set to ' + w + '×' + h + '  ·  ' + ratioText(w, h) + '.', false);
    draw();
    save();
  }
  el.customW.addEventListener('input', readCustomTarget);
  el.customH.addEventListener('input', readCustomTarget);

  el.freeIn.addEventListener('input', () => { state.free = el.freeIn.value; });
  el.freeIn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); el.useSource.click(); }
  });

  el.useSource.addEventListener('click', () => {
    const r = parseSpec(el.freeIn.value);
    if (!r.ok) { setParseNote(r.error, true); return; }
    state.customSource = { w: r.w, h: r.h };
    state.source = 'custom-source';
    setParseNote('Source set to ' + dec(r.ar) + '  ·  ' + ratioText(r.w, r.h)
      + (r.isPixels ? '  ·  read as pixels, kept as a ratio.' : '.'), false);
    apply();
  });

  el.useTarget.addEventListener('click', () => {
    const r = parseSpec(el.freeIn.value);
    if (!r.ok) { setParseNote(r.error, true); return; }
    // A bare ratio has no pixel size, so scale it to a 1080-tall panel.
    let w = r.w, h = r.h;
    if (!r.isPixels) {
      h = 1080;
      w = Math.round(1080 * r.ar);
    }
    state.customTarget = { w: Math.round(w), h: Math.round(h) };
    state.target = 'tcustom';
    setParseNote('Target set to ' + state.customTarget.w + '×' + state.customTarget.h
      + '  ·  ' + ratioText(state.customTarget.w, state.customTarget.h)
      + (r.isPixels ? '.' : '  ·  ratio scaled to 1080 tall.'), false);
    apply();
  });

  let raf = 0;
  window.addEventListener('resize', () => {
    if (raf) return;
    raf = requestAnimationFrame(() => { raf = 0; draw(); });
  });

  load();
  el.freeIn.value = state.free;
  apply();
}

/* test hook: the verification script loads this file in node, where there is no document */
if (typeof module === 'object' && module.exports) {
  module.exports = { gcd, reduceRatio, parseSpec, computeGeometry, fitBox, SOURCES, TARGETS };
}
if (typeof document !== 'undefined') boot();
