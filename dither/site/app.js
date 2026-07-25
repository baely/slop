'use strict';

/* dither — drop a photo, get a 1-bit image fit for an e-ink panel or a zine.
   Everything is computed in this tab; nothing is uploaded. */

const $ = (id) => document.getElementById(id);

const MAX_DIM = 4096;          // hard cap on either output dimension
const MAX_PIXELS = 6e6;        // and on total pixels, so "Original" on a 60 MP scan can't hang
const PREVIEW_BYTES = 512;     // bytes rendered into the <pre>; Copy always copies all of them
const TONE_STEPS = 2048;       // resolution of the 1-D tone curve LUT
const STORE_KEY = 'dither.v1';

/* ---------- colour ----------

   sRGB is a gamma-encoded signal. The Rec.709 luma coefficients 0.2126/0.7152/0.0722
   are defined against *linear* light, so the only correct place to apply them is after
   undoing the sRGB transfer function. We do that, then re-encode the resulting
   luminance back through the sRGB curve and run tone controls + dithering in that
   perceptual space.

   Why not diffuse error in linear light? Doing so preserves mean luminance exactly,
   which is the right model for an emissive display. E-ink is reflective: white
   reflects ~35-40%, black ~5%, and the ink spreads. That linear model doesn't hold,
   and diffusing in linear light drags midtones far darker than every reference
   implementation (and than the source looks). Perceptual space is the useful target
   here; the Gamma control exists for anyone who disagrees. */

const LINEAR = new Float32Array(256);
for (let i = 0; i < 256; i++) {
  const c = i / 255;
  LINEAR[i] = c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function linearToSrgb(c) {
  if (c <= 0) return 0;
  if (c >= 1) return 1;
  return c <= 0.0031308 ? c * 12.92 : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
}

/** ImageData -> Float32Array of perceptual grey in 0..1. */
function toGrey(imageData) {
  const d = imageData.data;
  const n = imageData.width * imageData.height;
  const out = new Float32Array(n);
  for (let i = 0, p = 0; i < n; i++, p += 4) {
    const y = 0.2126 * LINEAR[d[p]] + 0.7152 * LINEAR[d[p + 1]] + 0.0722 * LINEAR[d[p + 2]];
    out[i] = linearToSrgb(y);
  }
  return out;
}

/* ---------- tone ---------- */

/* Gamma first (a curve on the raw luminance), then contrast about mid-grey, then
   brightness as a plain offset. It's a pure 1-D function, so bake it into a LUT
   instead of calling Math.pow a few million times per slider tick. */
function toneLUT(brightness, contrast, gamma) {
  const lut = new Float32Array(TONE_STEPS + 1);
  const inv = 1 / gamma;
  for (let i = 0; i <= TONE_STEPS; i++) {
    let v = Math.pow(i / TONE_STEPS, inv);
    v = (v - 0.5) * contrast + 0.5 + brightness;
    lut[i] = v < 0 ? 0 : v > 1 ? 1 : v;
  }
  return lut;
}

function applyTone(grey, lut, out) {
  const dst = out || new Float32Array(grey.length);
  for (let i = 0; i < grey.length; i++) {
    dst[i] = lut[Math.round(grey[i] * TONE_STEPS)];
  }
  return dst;
}

/* ---------- dither ----------
   Every algorithm returns a Uint8Array where 1 = ink (black) and 0 = paper (white). */

const KERNELS = {
  //        X 7/16          Floyd–Steinberg: all 16/16 of the error is passed on.
  // 3/16 5/16 1/16
  fs: { div: 16, taps: [[1, 0, 7], [-1, 1, 3], [0, 1, 5], [1, 1, 1]] },

  //        X 1/8 1/8       Atkinson: only 6/8 of the error is passed on. The missing
  // 1/8 1/8 1/8            2/8 is thrown away on purpose — that lost error is what
  //     1/8                blows out highlights and shadows and gives it the look.
  atkinson: { div: 8, taps: [[1, 0, 1], [2, 0, 1], [-1, 1, 1], [0, 1, 1], [1, 1, 1], [0, 2, 1]] }
};

/* `work` and `out` are optional scratch buffers — at 1872x1404 the allocation churn
   costs more than the arithmetic, and slider drags re-enter this every frame. */
function diffuse(v, w, h, kernel, thr, work, out) {
  const ink = out || new Uint8Array(w * h);
  const buf = work || new Float32Array(w * h);
  buf.set(v);
  const taps = kernel.taps;
  const div = kernel.div;
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x;
      const old = buf[i];
      const white = old >= thr ? 1 : 0;
      ink[i] = white ^ 1;
      const err = old - white;
      if (err === 0) continue;
      for (let t = 0; t < taps.length; t++) {
        const nx = x + taps[t][0];
        const ny = y + taps[t][1];
        // Error that falls off the edge of the raster is discarded, as in the
        // original implementations.
        if (nx < 0 || nx >= w || ny >= h) continue;
        buf[ny * w + nx] += err * taps[t][2] / div;
      }
    }
  }
  return ink;
}

/** Recursive Bayer construction: M(2n) = [[4M, 4M+2], [4M+3, 4M+1]] from [[0,2],[3,1]]. */
function bayer(n) {
  let m = [[0, 2], [3, 1]];
  while (m.length < n) {
    const s = m.length;
    const next = [];
    for (let y = 0; y < s * 2; y++) next.push(new Array(s * 2).fill(0));
    for (let y = 0; y < s; y++) {
      for (let x = 0; x < s; x++) {
        const v = m[y][x] * 4;
        next[y][x] = v;
        next[y][x + s] = v + 2;
        next[y + s][x] = v + 3;
        next[y + s][x + s] = v + 1;
      }
    }
    m = next;
  }
  return m;
}

const BAYER = { bayer4: bayer(4), bayer8: bayer(8) };

function ordered(v, w, h, map, thr, out) {
  const n = map.length;
  const n2 = n * n;
  const ink = out || new Uint8Array(w * h);
  for (let y = 0; y < h; y++) {
    const row = map[y % n];
    for (let x = 0; x < w; x++) {
      // (M + 0.5) / n² spreads the map evenly over 0..1. Threshold shifts the whole
      // map, so thr = 0.5 reduces to classic ordered dithering.
      const t = thr + (row[x % n] + 0.5) / n2 - 0.5;
      ink[y * w + x] = v[y * w + x] >= t ? 0 : 1;
    }
  }
  return ink;
}

function threshold(v, w, h, thr, out) {
  const ink = out || new Uint8Array(w * h);
  for (let i = 0; i < ink.length; i++) ink[i] = v[i] >= thr ? 0 : 1;
  return ink;
}

function dither(v, w, h, algo, thr, work, out) {
  if (algo === 'atkinson') return diffuse(v, w, h, KERNELS.atkinson, thr, work, out);
  if (algo === 'bayer4') return ordered(v, w, h, BAYER.bayer4, thr, out);
  if (algo === 'bayer8') return ordered(v, w, h, BAYER.bayer8, thr, out);
  if (algo === 'threshold') return threshold(v, w, h, thr, out);
  return diffuse(v, w, h, KERNELS.fs, thr, work, out);
}

/* ---------- packing ----------
   1 bit per pixel, MSB first, row-major, each row padded out to a whole byte.
   That is framebuf.MONO_HLSB, and what most e-ink panel drivers want. */

function pack(ink, w, h) {
  const stride = (w + 7) >> 3;
  const out = new Uint8Array(stride * h);
  for (let y = 0; y < h; y++) {
    const rowBase = y * stride;
    const inBase = y * w;
    for (let x = 0; x < w; x++) {
      if (ink[inBase + x]) out[rowBase + (x >> 3)] |= 0x80 >> (x & 7);
    }
  }
  return out;
}

const hex = (b) => '0x' + b.toString(16).padStart(2, '0');
const esc = (b) => '\\x' + b.toString(16).padStart(2, '0');

function formatC(bytes, w, h, count) {
  const stride = (w + 7) >> 3;
  const lines = [
    '/* ' + w + 'x' + h + ', 1bpp MSB-first, row-major, ' + stride + ' bytes/row */',
    '#define DITHER_W ' + w,
    '#define DITHER_H ' + h,
    'static const uint8_t dither_data[' + bytes.length + '] = {'
  ];
  const n = Math.min(count, bytes.length);
  for (let i = 0; i < n; i += 12) {
    const chunk = [];
    for (let j = i; j < Math.min(i + 12, n); j++) chunk.push(hex(bytes[j]));
    lines.push('  ' + chunk.join(', ') + (i + 12 < n ? ',' : n < bytes.length ? ',' : ''));
  }
  if (n < bytes.length) lines.push('  /* … */');
  lines.push('};');
  return lines.join('\n');
}

function formatPy(bytes, w, h, count) {
  const stride = (w + 7) >> 3;
  const lines = [
    '# ' + w + 'x' + h + ', 1bpp MSB-first, row-major, ' + stride + ' bytes/row',
    'import framebuf',
    'WIDTH, HEIGHT = ' + w + ', ' + h,
    'DATA = bytearray('
  ];
  const n = Math.min(count, bytes.length);
  for (let i = 0; i < n; i += 16) {
    let s = '';
    for (let j = i; j < Math.min(i + 16, n); j++) s += esc(bytes[j]);
    lines.push("    b'" + s + "'");
  }
  if (n < bytes.length) lines.push('    # …');
  lines.push(')');
  lines.push('fb = framebuf.FrameBuffer(DATA, WIDTH, HEIGHT, framebuf.MONO_HLSB)');
  return lines.join('\n');
}

function formatBytes(bytes, w, h, fmt, count) {
  return fmt === 'py' ? formatPy(bytes, w, h, count) : formatC(bytes, w, h, count);
}

/* ---------- resampling ---------- */

/** Halve repeatedly before the final draw — one big downscale step drops detail badly. */
function resample(img, w, h) {
  let src = img;
  let cw = img.naturalWidth;
  let ch = img.naturalHeight;
  while (cw > w * 2 && ch > h * 2) {
    const nw = Math.max(w, cw >> 1);
    const nh = Math.max(h, ch >> 1);
    const step = document.createElement('canvas');
    step.width = nw;
    step.height = nh;
    const sx = step.getContext('2d');
    sx.imageSmoothingEnabled = true;
    sx.imageSmoothingQuality = 'high';
    sx.drawImage(src, 0, 0, nw, nh);
    src = step;
    cw = nw;
    ch = nh;
  }
  const out = document.createElement('canvas');
  out.width = w;
  out.height = h;
  const ctx = out.getContext('2d', { willReadFrequently: true });
  ctx.fillStyle = '#ffffff';          // flatten any alpha onto white paper
  ctx.fillRect(0, 0, w, h);
  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(src, 0, 0, w, h);
  return ctx.getImageData(0, 0, w, h);
}

function clampTarget(reqW, srcW, srcH) {
  const ratio = srcH / srcW;
  let w = Math.round(reqW);
  if (!isFinite(w) || w < 8) w = 8;
  let capped = false;
  if (w > MAX_DIM) { w = MAX_DIM; capped = true; }
  let h = Math.max(1, Math.round(w * ratio));
  if (h > MAX_DIM) { h = MAX_DIM; w = Math.max(8, Math.round(h / ratio)); capped = true; }
  if (w * h > MAX_PIXELS) {
    const s = Math.sqrt(MAX_PIXELS / (w * h));
    w = Math.max(8, Math.floor(w * s));
    h = Math.max(1, Math.round(w * ratio));
    capped = true;
  }
  return { w: w, h: h, capped: capped };
}

/* ---------- state ---------- */

const state = {
  algo: 'fs',
  widthMode: '800',
  width: 800,
  brightness: 0,
  contrast: 1,
  gamma: 1,
  threshold: 0.5,
  invert: false,
  fmt: 'c',
  zoom: 'one'
};

const ALGO_NOTES = {
  fs: 'Floyd–Steinberg diffuses all 16/16 of the error to 4 neighbours.',
  atkinson: 'Atkinson diffuses only 6/8 of the error to 6 neighbours — the discarded 2/8 is what blows out the extremes.',
  bayer4: 'Ordered dither against a 4×4 recursive Bayer matrix. Coarse, obvious crosshatch.',
  bayer8: 'Ordered dither against an 8×8 recursive Bayer matrix. 64 levels, finer texture.',
  threshold: 'Hard cut at the threshold. No dithering at all.'
};

let img = null;          // the decoded source
let srcName = '';
let grey = null;         // Float32Array of the downscaled source, perceptual grey
let greyW = 0;
let greyH = 0;
let greyForWidth = -1;   // width `grey` was built at
let packed = null;       // Uint8Array of the current 1-bit output
let lastCapped = false;

/* Scratch buffers, re-cut only when the raster size changes. */
const scratch = { n: -1, toned: null, work: null, ink: null, pixels: null };
function scratchFor(n) {
  if (scratch.n !== n) {
    scratch.n = n;
    scratch.toned = new Float32Array(n);
    scratch.work = new Float32Array(n);
    scratch.ink = new Uint8Array(n);
    scratch.pixels = null;   // ImageData is cut against the canvas, below
  }
  return scratch;
}

const canvas = $('canvas');
const ctx = canvas.getContext('2d');

/* ---------- persistence ---------- */

function save() {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(state));
  } catch (e) { /* private mode, quota — the app works fine without it */ }
}

function load() {
  try {
    const raw = localStorage.getItem(STORE_KEY);
    if (!raw) return;
    const saved = JSON.parse(raw);
    for (const k of Object.keys(state)) {
      if (typeof saved[k] === typeof state[k]) state[k] = saved[k];
    }
  } catch (e) { /* corrupt or unavailable — fall back to defaults */ }
  sanitize();
}

/* Restored values are untrusted: an older build, a hand-edited key or a NaN would
   otherwise reach the render path. Snap everything back into range. */
function sanitize() {
  const num = (v, lo, hi, dflt) =>
    (typeof v === 'number' && isFinite(v)) ? Math.min(hi, Math.max(lo, v)) : dflt;
  if (!ALGO_NOTES[state.algo]) state.algo = 'fs';
  if (state.fmt !== 'c' && state.fmt !== 'py') state.fmt = 'c';
  if (state.zoom !== 'one' && state.zoom !== 'fit') state.zoom = 'one';
  if (!/^(800|400|1872|orig|custom)$/.test(state.widthMode)) state.widthMode = '800';
  state.width = Math.round(num(state.width, 8, MAX_DIM, 800));
  // A numeric preset owns the width outright; 'orig' is resolved once an image lands.
  if (/^(800|400|1872)$/.test(state.widthMode)) state.width = parseInt(state.widthMode, 10);
  state.brightness = num(state.brightness, -0.5, 0.5, 0);
  state.contrast = num(state.contrast, 0.2, 3, 1);
  state.gamma = num(state.gamma, 0.4, 2.5, 1);
  state.threshold = num(state.threshold, 0.05, 0.95, 0.5);
  state.invert = !!state.invert;
}

/* ---------- ui plumbing ---------- */

function setStatus(msg, bad) {
  const el = $('status');
  el.textContent = msg;
  el.classList.toggle('bad', !!bad);
}

function setPressed(container, attr, value) {
  for (const b of container.querySelectorAll('button')) {
    b.setAttribute('aria-pressed', String(b.dataset[attr] === value));
  }
}

function syncControls() {
  setPressed($('algo'), 'algo', state.algo);
  setPressed($('preset'), 'w', state.widthMode);
  setPressed($('fmt'), 'fmt', state.fmt);
  setPressed($('zoom'), 'zoom', state.zoom);
  $('algoNote').textContent = ALGO_NOTES[state.algo];
  $('invert').setAttribute('aria-pressed', String(state.invert));
  $('width').value = String(state.width);
  $('brightness').value = String(state.brightness);
  $('contrast').value = String(state.contrast);
  $('gamma').value = String(state.gamma);
  $('threshold').value = String(state.threshold);
  $('brightnessVal').textContent = (state.brightness >= 0 ? '+' : '') + state.brightness.toFixed(2);
  $('contrastVal').textContent = state.contrast.toFixed(2) + '×';
  $('gammaVal').textContent = state.gamma.toFixed(2);
  $('thresholdVal').textContent = state.threshold.toFixed(2);
  $('canvasWrap').classList.toggle('fit', state.zoom === 'fit');
}

function targetWidth() {
  if (!img) return state.width;
  return state.widthMode === 'orig' ? img.naturalWidth : state.width;
}

let queued = false;
function schedule(rebuild) {
  if (rebuild) greyForWidth = -1;
  if (queued) return;
  queued = true;
  requestAnimationFrame(() => { queued = false; render(); });
}

/* ---------- render ---------- */

function render() {
  if (!img) return;
  const t = clampTarget(targetWidth(), img.naturalWidth, img.naturalHeight);

  if (greyForWidth !== t.w || greyH !== t.h) {
    const data = resample(img, t.w, t.h);
    grey = toGrey(data);
    greyW = t.w;
    greyH = t.h;
    greyForWidth = t.w;
  }

  const s = scratchFor(greyW * greyH);
  applyTone(grey, toneLUT(state.brightness, state.contrast, state.gamma), s.toned);
  const ink = dither(s.toned, greyW, greyH, state.algo, state.threshold, s.work, s.ink);
  // Invert flips the finished bitmap, so the preview and the packed bytes always agree.
  if (state.invert) {
    for (let i = 0; i < ink.length; i++) ink[i] ^= 1;
  }

  if (canvas.width !== greyW || canvas.height !== greyH) {
    canvas.width = greyW;
    canvas.height = greyH;
    s.pixels = null;
  }
  canvas.hidden = false;
  if (!s.pixels) s.pixels = ctx.createImageData(greyW, greyH);
  const d = s.pixels.data;
  for (let i = 0, p = 0; i < ink.length; i++, p += 4) {
    const c = ink[i] ? 0 : 255;
    d[p] = c; d[p + 1] = c; d[p + 2] = c; d[p + 3] = 255;
  }
  ctx.putImageData(s.pixels, 0, 0);
  $('empty').hidden = true;

  packed = pack(ink, greyW, greyH);
  const stride = (greyW + 7) >> 3;

  $('dims').textContent =
    'source ' + img.naturalWidth + '×' + img.naturalHeight +
    ' → output ' + greyW + '×' + greyH +
    ' · ' + (greyW * greyH).toLocaleString('en-US') + ' px' +
    (t.capped ? ' · capped' : '');

  $('bytesMeta').textContent =
    packed.length.toLocaleString('en-US') + ' bytes · ' + stride + ' bytes/row · ' + greyH + ' rows' +
    (packed.length > PREVIEW_BYTES
      ? ' · showing first ' + PREVIEW_BYTES
      : '');

  $('bytes').textContent = formatBytes(packed, greyW, greyH, state.fmt, PREVIEW_BYTES);

  // Only speak up when the cap first bites, so slider ticks don't spam the live region.
  if (t.capped && !lastCapped) {
    setStatus('Downscaled to ' + greyW + '×' + greyH + ' to stay under the working limit.');
  }
  lastCapped = t.capped;
}

/* ---------- loading ---------- */

function adopt(next, name) {
  img = next;
  srcName = name;
  greyForWidth = -1;
  if (state.widthMode === 'orig') {
    state.width = clampTarget(img.naturalWidth, img.naturalWidth, img.naturalHeight).w;
  }
  $('srcName').textContent = name;
  $('srcDims').textContent = img.naturalWidth + '×' + img.naturalHeight;
  setStatus('Loaded ' + img.naturalWidth + '×' + img.naturalHeight + '.');
  syncControls();
  render();
}

function loadFile(file) {
  if (!file) return;
  const type = file.type || '';
  if (!type.startsWith('image/')) {
    setStatus('Not an image: ' + file.name + ' (' + (type || 'unknown type') + ').', true);
    return;
  }
  const url = URL.createObjectURL(file);
  const next = new Image();
  next.onload = () => {
    URL.revokeObjectURL(url);
    if (!next.naturalWidth || !next.naturalHeight) {
      setStatus('No intrinsic size in ' + file.name + '. Vector files need a width and height.', true);
      return;
    }
    adopt(next, file.name);
  };
  next.onerror = () => {
    URL.revokeObjectURL(url);
    setStatus('Could not decode ' + file.name + '. This browser may not support ' + (type || 'that format') + '.', true);
  };
  next.src = url;
}

/* ---------- exports ---------- */

function baseName() {
  const n = srcName.replace(/\.[^.]+$/, '').replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-|-$/g, '');
  return n || 'dither';
}

function downloadPng() {
  if (!img) { setStatus('Nothing loaded yet.', true); return; }
  let url;
  try {
    url = canvas.toDataURL('image/png');
  } catch (e) {
    setStatus('Could not encode the PNG.', true);
    return;
  }
  const a = document.createElement('a');
  a.href = url;
  a.download = baseName() + '-' + state.algo + '-' + greyW + 'x' + greyH + '.png';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setStatus('Saved ' + a.download + '.');
}

function copyText(text, onDone) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => onDone(true), () => fallback());
  } else {
    fallback();
  }
  function fallback() {
    try {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      onDone(ok);
    } catch (e) {
      onDone(false);
    }
  }
}

function copyBytes() {
  if (!packed) { setStatus('Nothing loaded yet.', true); return; }
  // Formatted lazily: the full text for a 1872-wide panel is a few hundred KB.
  const text = formatBytes(packed, greyW, greyH, state.fmt, packed.length);
  copyText(text, (ok) => {
    setStatus(ok
      ? 'Copied ' + packed.length.toLocaleString('en-US') + ' bytes as ' + (state.fmt === 'py' ? 'MicroPython' : 'a C array') + '.'
      : 'Copy failed. Select the text below and copy it manually.', !ok);
  });
}

/* ---------- wiring ---------- */

load();
syncControls();

$('pick').addEventListener('click', () => $('file').click());
$('file').addEventListener('change', (e) => {
  loadFile(e.target.files && e.target.files[0]);
  e.target.value = '';
});

$('algo').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.algo = b.dataset.algo;
  syncControls();
  save();
  schedule(false);
});

$('preset').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.widthMode = b.dataset.w;
  if (state.widthMode === 'orig') {
    state.width = img ? clampTarget(img.naturalWidth, img.naturalWidth, img.naturalHeight).w : state.width;
  } else {
    state.width = parseInt(state.widthMode, 10);
  }
  syncControls();
  save();
  schedule(true);
});

$('width').addEventListener('change', (e) => {
  const v = parseInt(e.target.value, 10);
  if (!isFinite(v)) {
    setStatus('Width must be a whole number between 8 and ' + MAX_DIM + '.', true);
    syncControls();
    return;
  }
  const clamped = Math.min(MAX_DIM, Math.max(8, v));
  if (clamped !== v) setStatus('Width clamped to ' + clamped + ' px.');
  state.width = clamped;
  state.widthMode = String(clamped) === '800' || String(clamped) === '400' || String(clamped) === '1872'
    ? String(clamped) : 'custom';
  syncControls();
  save();
  schedule(true);
});

for (const id of ['brightness', 'contrast', 'gamma', 'threshold']) {
  $(id).addEventListener('input', (e) => {
    const v = parseFloat(e.target.value);
    if (!isFinite(v)) return;
    state[id] = v;
    syncControls();
    schedule(false);
  });
  $(id).addEventListener('change', save);
}

$('invert').addEventListener('click', () => {
  state.invert = !state.invert;
  syncControls();
  save();
  schedule(false);
});

$('reset').addEventListener('click', () => {
  state.brightness = 0;
  state.contrast = 1;
  state.gamma = 1;
  state.threshold = 0.5;
  state.invert = false;
  syncControls();
  save();
  schedule(false);
  setStatus('Tone reset.');
});

$('zoom').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.zoom = b.dataset.zoom;
  syncControls();
  save();
});

$('fmt').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.fmt = b.dataset.fmt;
  syncControls();
  save();
  if (packed) $('bytes').textContent = formatBytes(packed, greyW, greyH, state.fmt, PREVIEW_BYTES);
});

$('png').addEventListener('click', downloadPng);
$('copy').addEventListener('click', copyBytes);

/* drag and drop over the whole page */
let dragDepth = 0;
window.addEventListener('dragenter', (e) => {
  e.preventDefault();
  dragDepth++;
  document.body.classList.add('dragging');
});
window.addEventListener('dragover', (e) => { e.preventDefault(); });
window.addEventListener('dragleave', () => {
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) document.body.classList.remove('dragging');
});
window.addEventListener('drop', (e) => {
  e.preventDefault();
  dragDepth = 0;
  document.body.classList.remove('dragging');
  const dt = e.dataTransfer;
  if (!dt) return;
  const file = dt.files && dt.files[0];
  if (!file) { setStatus('That drop carried no file.', true); return; }
  loadFile(file);
});

/* paste from clipboard */
window.addEventListener('paste', (e) => {
  // Don't hijack a paste aimed at the width box.
  const t = e.target;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  const items = e.clipboardData && e.clipboardData.items;
  if (!items) return;
  for (let i = 0; i < items.length; i++) {
    if (items[i].kind === 'file') {
      const file = items[i].getAsFile();
      if (file) { e.preventDefault(); loadFile(file); return; }
    }
  }
  setStatus('No image on the clipboard.', true);
});
