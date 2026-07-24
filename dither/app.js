// dither — downscale + dither video, fully client-side.

const $ = (id) => document.getElementById(id);

const video = $('video');
const display = $('display');
const dctx = display.getContext('2d');
const buffer = document.createElement('canvas');
const bctx = buffer.getContext('2d', { willReadFrequently: true });

const stage = $('stage');
const editor = $('editor');
const dropzone = $('dropzone');
const fileInput = $('file');

const playBtn = $('playBtn');
const scrub = $('scrub');
const timeEl = $('time');
const exportBtn = $('exportBtn');
const specsEl = $('specs');

const params = {
  width: 160,
  algo: 'fs',
  palette: 'bw',
  bright: 0,
  contrast: 0,
};

// palettes are dark→light luminance ramps; dithering happens in luminance,
// the ramp supplies the output colors
const PALETTES = {
  bw:      [[0, 0, 0], [255, 255, 255]],
  gray4:   [[0, 0, 0], [85, 85, 85], [170, 170, 170], [255, 255, 255]],
  gameboy: [[15, 56, 15], [48, 98, 48], [139, 172, 15], [155, 188, 15]],
  amber:   [[26, 15, 0], [255, 176, 0]],
};

const BAYER4 = [
  [0, 8, 2, 10],
  [12, 4, 14, 6],
  [3, 11, 1, 9],
  [15, 7, 13, 5],
];
const BAYER8 = [
  [0, 32, 8, 40, 2, 34, 10, 42],
  [48, 16, 56, 24, 50, 18, 58, 26],
  [12, 44, 4, 36, 14, 46, 6, 38],
  [60, 28, 52, 20, 62, 30, 54, 22],
  [3, 35, 11, 43, 1, 33, 9, 41],
  [51, 19, 59, 27, 49, 17, 57, 25],
  [15, 47, 7, 39, 13, 45, 5, 37],
  [63, 31, 55, 23, 61, 29, 53, 21],
];

// x offset, y offset, weight
const KERNELS = {
  fs: [[1, 0, 7 / 16], [-1, 1, 3 / 16], [0, 1, 5 / 16], [1, 1, 1 / 16]],
  atkinson: [[1, 0, 1 / 8], [2, 0, 1 / 8], [-1, 1, 1 / 8], [0, 1, 1 / 8], [1, 1, 1 / 8], [0, 2, 1 / 8]],
};

let fileName = 'clip';
let objectUrl = null;
let needsRender = false;

// ---------- pipeline ----------

function luminance(data, bright, contrast) {
  const n = data.length / 4;
  const lum = new Float32Array(n);
  const f = (259 * (contrast + 255)) / (255 * (259 - contrast));
  const b = bright * 1.28;
  for (let i = 0; i < n; i++) {
    const j = i * 4;
    const l = 0.2126 * data[j] + 0.7152 * data[j + 1] + 0.0722 * data[j + 2];
    lum[i] = (l - 128) * f + 128 + b;
  }
  return lum;
}

function ditherDiffusion(lum, w, h, levels, kernel) {
  const step = 255 / (levels - 1);
  const out = new Uint8Array(w * h);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x;
      let q = Math.round(lum[i] / step);
      if (q < 0) q = 0; else if (q > levels - 1) q = levels - 1;
      out[i] = q;
      const err = lum[i] - q * step;
      for (const [dx, dy, wt] of kernel) {
        const nx = x + dx, ny = y + dy;
        if (nx >= 0 && nx < w && ny < h) lum[ny * w + nx] += err * wt;
      }
    }
  }
  return out;
}

function ditherOrdered(lum, w, h, levels, matrix) {
  const n = matrix.length;
  const step = 255 / (levels - 1);
  const out = new Uint8Array(w * h);
  for (let y = 0; y < h; y++) {
    const row = matrix[y % n];
    for (let x = 0; x < w; x++) {
      const i = y * w + x;
      const v = lum[i] + (row[x % n] / (n * n) - 0.5) * step;
      let q = Math.round(v / step);
      if (q < 0) q = 0; else if (q > levels - 1) q = levels - 1;
      out[i] = q;
    }
  }
  return out;
}

function posterize(lum, w, h, levels) {
  const step = 255 / (levels - 1);
  const out = new Uint8Array(w * h);
  for (let i = 0; i < w * h; i++) {
    let q = Math.round(lum[i] / step);
    if (q < 0) q = 0; else if (q > levels - 1) q = levels - 1;
    out[i] = q;
  }
  return out;
}

function render() {
  if (video.readyState < 2 || !video.videoWidth) return;

  const w = params.width;
  const h = Math.max(1, Math.round((w * video.videoHeight) / video.videoWidth));
  if (buffer.width !== w || buffer.height !== h) {
    buffer.width = w;
    buffer.height = h;
  }
  bctx.drawImage(video, 0, 0, w, h);

  const img = bctx.getImageData(0, 0, w, h);
  const pal = PALETTES[params.palette];
  const levels = pal.length;
  const lum = luminance(img.data, params.bright, params.contrast);

  let idx;
  if (params.algo === 'fs' || params.algo === 'atkinson') {
    idx = ditherDiffusion(lum, w, h, levels, KERNELS[params.algo]);
  } else if (params.algo === 'bayer4') {
    idx = ditherOrdered(lum, w, h, levels, BAYER4);
  } else if (params.algo === 'bayer8') {
    idx = ditherOrdered(lum, w, h, levels, BAYER8);
  } else {
    idx = posterize(lum, w, h, levels);
  }

  const data = img.data;
  for (let i = 0; i < idx.length; i++) {
    const c = pal[idx[i]];
    const j = i * 4;
    data[j] = c[0];
    data[j + 1] = c[1];
    data[j + 2] = c[2];
    data[j + 3] = 255;
  }
  bctx.putImageData(img, 0, 0);

  // integer upscale for crisp pixels, but never beyond the source's own
  // size on either axis — the export records this canvas verbatim
  const scale = Math.max(1, Math.min(
    Math.floor(Math.min(960, video.videoWidth) / w),
    Math.floor(Math.min(720, video.videoHeight) / h),
  ));
  const dw = w * scale, dh = h * scale;
  if (display.width !== dw || display.height !== dh) {
    display.width = dw;
    display.height = dh;
  }
  dctx.imageSmoothingEnabled = false;
  dctx.drawImage(buffer, 0, 0, dw, dh);

  specsEl.textContent = `${fileName} · ${video.videoWidth}×${video.videoHeight} → ${w}×${h} · ×${scale}`;
}

function tick() {
  if ((!video.paused && !video.ended) || needsRender) {
    needsRender = false;
    render();
    updateTransport();
  }
  // browsers may pause a gestureless playback mid-export; keep it rolling
  if (recorder && recorder.state === 'recording' && video.paused && !video.seeking && !video.ended) {
    video.play();
  }
  requestAnimationFrame(tick);
}
requestAnimationFrame(tick);

// ---------- transport ----------

function fmt(t) {
  if (!isFinite(t)) t = 0;
  const m = Math.floor(t / 60);
  const s = Math.floor(t % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

function updateTransport() {
  if (!scrubbing) scrub.value = video.currentTime;
  timeEl.textContent = `${fmt(video.currentTime)} / ${fmt(video.duration)}`;
}

let scrubbing = false;
scrub.addEventListener('pointerdown', () => { scrubbing = true; });
scrub.addEventListener('pointerup', () => { scrubbing = false; });
scrub.addEventListener('input', () => {
  video.currentTime = parseFloat(scrub.value);
});

function togglePlay() {
  if (recorder) { stopExport(); return; }
  if (video.paused || video.ended) video.play(); else video.pause();
}
playBtn.addEventListener('click', togglePlay);
display.addEventListener('click', togglePlay);
video.addEventListener('play', () => { playBtn.textContent = 'Pause'; });
video.addEventListener('pause', () => { playBtn.textContent = 'Play'; });
video.addEventListener('seeked', () => { needsRender = true; });

// ---------- controls ----------

function bindRange(id, outId, key, suffix = '') {
  const el = $(id), out = $(outId);
  el.addEventListener('input', () => {
    params[key] = parseInt(el.value, 10);
    out.textContent = `${el.value}${suffix}`;
    needsRender = true;
  });
}
bindRange('width', 'widthOut', 'width', ' px');
bindRange('bright', 'brightOut', 'bright');
bindRange('contrast', 'contrastOut', 'contrast');

$('algo').addEventListener('change', (e) => { params.algo = e.target.value; needsRender = true; });
$('palette').addEventListener('change', (e) => { params.palette = e.target.value; needsRender = true; });

// ---------- file loading ----------

function loadFile(file) {
  if (!file || !file.type.startsWith('video/')) return;
  if (objectUrl) URL.revokeObjectURL(objectUrl);
  objectUrl = URL.createObjectURL(file);
  fileName = file.name.replace(/\.[^.]+$/, '');
  video.src = objectUrl;
  video.loop = true;
  video.load();
}

video.addEventListener('loadedmetadata', () => {
  scrub.max = video.duration;
  stage.hidden = true;
  editor.hidden = false;
  // some browsers won't paint frame 0 to a canvas until a play or a real
  // seek kicks the decoder — nudge it
  video.currentTime = 0.01;
});
video.addEventListener('loadeddata', () => { needsRender = true; });

$('chooseBtn').addEventListener('click', () => fileInput.click());
$('changeBtn').addEventListener('click', () => fileInput.click());
fileInput.addEventListener('change', () => loadFile(fileInput.files[0]));

window.addEventListener('dragover', (e) => {
  e.preventDefault();
  dropzone.classList.add('over');
});
window.addEventListener('dragleave', (e) => {
  if (!e.relatedTarget) dropzone.classList.remove('over');
});
window.addEventListener('drop', (e) => {
  e.preventDefault();
  dropzone.classList.remove('over');
  loadFile(e.dataTransfer.files[0]);
});

// ---------- export ----------

let recorder = null;
let chunks = [];

function pickMime() {
  const types = ['video/webm;codecs=vp9', 'video/webm;codecs=vp8', 'video/webm', 'video/mp4'];
  return types.find((t) => MediaRecorder.isTypeSupported(t)) || '';
}

exportBtn.addEventListener('click', () => {
  if (recorder) { stopExport(); return; }
  const mime = pickMime();
  const opts = { videoBitsPerSecond: 12_000_000 };
  if (mime) opts.mimeType = mime;
  recorder = new MediaRecorder(display.captureStream(), opts);
  chunks = [];
  recorder.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
  recorder.onstop = saveExport;

  video.pause();
  video.loop = false;
  video.addEventListener('ended', stopExport, { once: true });
  video.addEventListener('seeked', () => {
    video.play();
    recorder.start();
  }, { once: true });
  video.currentTime = 0;
  exportBtn.textContent = 'Stop Export';
});

function stopExport() {
  if (!recorder || recorder.state === 'inactive') return;
  recorder.stop();
}

function saveExport() {
  const type = recorder.mimeType || 'video/webm';
  const ext = type.includes('mp4') ? 'mp4' : 'webm';
  const blob = new Blob(chunks, { type });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${fileName}-dither-${params.width}px.${ext}`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 10_000);

  recorder = null;
  chunks = [];
  video.removeEventListener('ended', stopExport);
  video.pause();
  video.loop = true;
  exportBtn.textContent = 'Export WebM';
}
