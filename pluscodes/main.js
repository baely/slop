/* Plus Codes / Open Location Code grid visualisation */

const ALPHABET = '23456789CFGHJMPQRVWX';
const ALPHABET_INDEX = Object.fromEntries([...ALPHABET].map((c, i) => [c, i]));
const SEPARATOR = '+';
const PADDING = '0';

/* Shared runtime state. Updated by the grid layer on each redraw. */
const state = { primaryLevel: 6 };

/* Precision levels we visualise.
 * Pair phase (5 pairs, 10 chars): each pair divides previous cell by 20×20.
 * Refinement: chars 11+ use a 4×5 grid (4 rows lat × 5 cols lng) of each cell.
 */
const LEVELS = [2, 4, 6, 8, 10, 11, 12];

const CELL = {
  2:  { lat: 20,            lng: 20            },
  4:  { lat: 1,             lng: 1             },
  6:  { lat: 0.05,          lng: 0.05          },
  8:  { lat: 0.0025,        lng: 0.0025        },
  10: { lat: 0.000125,      lng: 0.000125      },
  11: { lat: 0.00003125,    lng: 0.000025      },
  12: { lat: 0.0000078125,  lng: 0.000005      },
};

function encode(lat, lng, length = 11) {
  lat = Math.max(-90, Math.min(90, lat));
  if (lat === 90) lat = 90 - 1e-10;
  lng = ((lng + 180) % 360 + 360) % 360 - 180;

  let latRem = lat + 90;
  let lngRem = lng + 180;
  let code = '';
  let step = 20;

  for (let p = 0; p < 5; p++) {
    const latDigit = Math.min(19, Math.floor(latRem / step));
    const lngDigit = Math.min(19, Math.floor(lngRem / step));
    code += ALPHABET[latDigit] + ALPHABET[lngDigit];
    latRem -= latDigit * step;
    lngRem -= lngDigit * step;
    step /= 20;
    if (code.length === length) break;
  }

  if (code.length < length) {
    let latRes = 0.000125;
    let lngRes = 0.000125;
    while (code.length < length) {
      latRes /= 4;
      lngRes /= 5;
      const row = Math.min(3, Math.floor(latRem / latRes));
      const col = Math.min(4, Math.floor(lngRem / lngRes));
      code += ALPHABET[row * 5 + col];
      latRem -= row * latRes;
      lngRem -= col * lngRes;
    }
  }

  if (code.length < 8) code = code.padEnd(8, PADDING);
  return code.slice(0, 8) + SEPARATOR + code.slice(8);
}

/* Opacity per level. Tuned so finer levels don't appear as tiny "noise" cells
 * inside a parent — they're only considered once they're big enough to be the
 * new primary themselves. The grid renderer only ever paints one level (the
 * most opaque), so this is mainly used to pick which level that is. */
function levelOpacity(pxSize) {
  if (pxSize < 40) return 0;
  if (pxSize < 100) return (pxSize - 40) / 60;
  if (pxSize < 1100) return 1;
  if (pxSize < 1700) return 1 - (pxSize - 1100) / 600;
  return 0;
}

function levelWeight(pxSize) {
  if (pxSize < 140) return 0.8;
  if (pxSize < 500) return 1.1;
  return 1.4;
}

/* Custom Leaflet layer that draws all grid levels onto one canvas. */
const GridLayer = L.Layer.extend({
  options: { padding: 0.4 },

  onAdd(map) {
    this._map = map;
    this._canvas = L.DomUtil.create('canvas', 'pc-grid-canvas');
    this._canvas.style.position = 'absolute';
    this._canvas.style.pointerEvents = 'none';
    this._canvas.style.willChange = 'transform';
    map.getPanes().overlayPane.appendChild(this._canvas);
    this._reset();
  },

  onRemove() {
    L.DomUtil.remove(this._canvas);
  },

  getEvents() {
    return {
      viewreset: this._reset,
      moveend: this._reset,
      zoomend: this._reset,
      resize: this._reset,
      zoomanim: this._animateZoom,
    };
  },

  _animateZoom(e) {
    const scale = this._map.getZoomScale(e.zoom, this._zoom);
    const offset = this._map._latLngBoundsToNewLayerBounds(
      this._bounds, e.zoom, e.center
    ).min;
    L.DomUtil.setTransform(this._canvas, offset, scale);
  },

  _reset() {
    const map = this._map;
    const size = map.getSize();
    const pad = this.options.padding;
    const padded = size.multiplyBy(pad);
    const min = map.containerPointToLayerPoint(padded.multiplyBy(-1)).round();
    const max = map.containerPointToLayerPoint(size.add(padded)).round();
    const bounds = L.bounds(min, max);
    const totalSize = bounds.getSize();

    L.DomUtil.setTransform(this._canvas, bounds.min, 1);

    const dpr = window.devicePixelRatio || 1;
    this._canvas.width = totalSize.x * dpr;
    this._canvas.height = totalSize.y * dpr;
    this._canvas.style.width = totalSize.x + 'px';
    this._canvas.style.height = totalSize.y + 'px';

    this._bounds = L.latLngBounds(
      map.layerPointToLatLng(bounds.min),
      map.layerPointToLatLng(bounds.max)
    );
    this._origin = bounds.min;
    this._zoom = map.getZoom();
    this._dpr = dpr;

    this._draw();
  },

  /* Convert lat/lng to a canvas (drawing) coordinate. */
  _project(lat, lng) {
    const layerPoint = this._map.latLngToLayerPoint([lat, lng]);
    return {
      x: (layerPoint.x - this._origin.x) * this._dpr,
      y: (layerPoint.y - this._origin.y) * this._dpr,
    };
  },

  _draw() {
    const ctx = this._canvas.getContext('2d');
    const w = this._canvas.width;
    const h = this._canvas.height;
    ctx.clearRect(0, 0, w, h);

    const map = this._map;
    const zoom = map.getZoom();
    const bounds = this._bounds;
    const drawBounds = {
      south: bounds.getSouth(),
      north: bounds.getNorth(),
      west: bounds.getWest(),
      east: bounds.getEast(),
    };

    // Per-degree pixel scale, in canvas drawing units (dpr-multiplied)
    const center = map.getCenter();
    const dx = map.latLngToLayerPoint([center.lat, center.lng + 0.1]).x
             - map.latLngToLayerPoint([center.lat, center.lng]).x;
    const pxPerDegLng = (dx / 0.1) * this._dpr;

    // Track levels we will actually paint so we can pick the primary
    const drawn = [];
    for (const level of LEVELS) {
      const { lat: cellLat, lng: cellLng } = CELL[level];
      const pxSize = cellLng * pxPerDegLng / this._dpr; // CSS px
      const opacity = levelOpacity(pxSize);
      if (opacity <= 0.01) continue;
      drawn.push({ level, cellLat, cellLng, pxSize, opacity });
    }

    // Pick the single primary level — the one the user most likely sees as "the
    // grid". Most opaque wins; tie-break to the finer level.
    const primary = drawn.length
      ? drawn.reduce((a, b) => {
          if (b.opacity > a.opacity + 0.05) return b;
          if (a.opacity > b.opacity + 0.05) return a;
          return b.level > a.level ? b : a;
        })
      : { level: 2, cellLat: 20, cellLng: 20, pxSize: 0, opacity: 0 };
    this._primary = primary;
    state.primaryLevel = primary.level;

    updateLegend(drawn);

    // Only draw the primary level. Boost opacity in transition zones so the
    // grid stays visible. Other levels are skipped entirely — one layer at a time.
    if (drawn.length) {
      const drawOpacity = Math.max(primary.opacity, 0.75);
      this._drawLevelLines(ctx, { ...primary, opacity: drawOpacity }, drawBounds);
      this._drawLevelLabels(ctx, { ...primary, opacity: drawOpacity, tier: 'primary' }, drawBounds);
    }
  },

  _drawLevelLines(ctx, { level, cellLat, cellLng, pxSize, opacity }, b) {
    const south = Math.floor((b.south + 90) / cellLat) * cellLat - 90;
    const north = Math.min(90, Math.ceil((b.north + 90) / cellLat) * cellLat - 90);
    const west = Math.floor((b.west + 180) / cellLng) * cellLng - 180;
    const east = Math.ceil((b.east + 180) / cellLng) * cellLng - 180;

    const cols = Math.round((east - west) / cellLng);
    const rows = Math.round((north - south) / cellLat);
    if (cols * rows > 60000) return; // safety guard

    const weight = levelWeight(pxSize);
    ctx.lineWidth = weight * this._dpr;
    ctx.strokeStyle = `rgba(255, 220, 140, ${opacity * 0.85})`;
    ctx.lineCap = 'butt';

    ctx.beginPath();
    // Vertical lines (constant lng)
    for (let c = 0; c <= cols; c++) {
      const lng = west + c * cellLng;
      const top = this._project(north, lng);
      const bot = this._project(south, lng);
      ctx.moveTo(top.x, top.y);
      ctx.lineTo(bot.x, bot.y);
    }
    // Horizontal lines (constant lat) — at Mercator, these are horizontal so just project endpoints
    for (let r = 0; r <= rows; r++) {
      const lat = south + r * cellLat;
      const left = this._project(lat, west);
      const right = this._project(lat, east);
      ctx.moveTo(left.x, left.y);
      ctx.lineTo(right.x, right.y);
    }
    ctx.stroke();
  },

  _drawLevelLabels(ctx, { level, cellLat, cellLng, pxSize, opacity, tier }, b) {
    const south = Math.floor((b.south + 90) / cellLat) * cellLat - 90;
    const north = Math.min(90, Math.ceil((b.north + 90) / cellLat) * cellLat - 90);
    const west = Math.floor((b.west + 180) / cellLng) * cellLng - 180;
    const east = Math.ceil((b.east + 180) / cellLng) * cellLng - 180;

    let fontSize, ctxColor, ctxContext;
    if (tier === 'primary') {
      fontSize = Math.min(15, Math.max(11, pxSize / 6));
      ctxColor = `rgba(255, 225, 160, ${Math.min(1, opacity + 0.15)})`;
      ctxContext = `rgba(255, 225, 160, 0.32)`;
    } else if (tier === 'parent') {
      fontSize = Math.min(48, Math.max(18, pxSize / 18));
      ctxColor = `rgba(255, 225, 160, 0.55)`;
      ctxContext = null;
    } else {
      fontSize = 10;
      ctxColor = `rgba(255, 225, 160, ${opacity * 0.6})`;
      ctxContext = null;
    }

    ctx.font = `500 ${fontSize * this._dpr}px ui-monospace, "JetBrains Mono", monospace`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.shadowColor = 'rgba(0, 0, 0, 0.95)';
    ctx.shadowBlur = 4 * this._dpr;

    const cols = Math.round((east - west) / cellLng);
    const rows = Math.round((north - south) / cellLat);
    if (cols * rows > 3500) {
      ctx.shadowBlur = 0;
      return;
    }
    const compact = tier !== 'parent' && pxSize < 95;

    for (let r = 0; r < rows; r++) {
      const lat = south + (r + 0.5) * cellLat;
      if (lat > 88 || lat < -88) continue;
      for (let c = 0; c < cols; c++) {
        const lng = west + (c + 0.5) * cellLng;
        if (lng < -180 || lng >= 180) continue;
        const code = encode(lat, lng, level);
        const { newPart, ctxPart } = splitCode(code, level);
        const pt = this._project(lat, lng);

        if (tier === 'parent' || compact || !ctxPart) {
          ctx.textAlign = 'center';
          ctx.fillStyle = ctxColor;
          ctx.fillText(newPart, pt.x, pt.y);
        } else {
          const ctxW = ctx.measureText(ctxPart).width;
          const newW = ctx.measureText(newPart).width;
          const startX = pt.x - (ctxW + newW) / 2;
          ctx.textAlign = 'left';
          ctx.fillStyle = ctxContext;
          ctx.fillText(ctxPart, startX, pt.y);
          ctx.fillStyle = ctxColor;
          ctx.fillText(newPart, startX + ctxW, pt.y);
        }
      }
    }
    ctx.shadowBlur = 0;
    ctx.textAlign = 'center';
  },
});

/* Split code into (context, new) parts for visual emphasis.
 * The '+' separator always belongs with the context (so the "new" portion
 * never starts with a separator). */
function splitCode(code, level) {
  const parentLen = parentLevel(level);
  function sliceByDigits(n) {
    if (n < 8) return code.slice(0, n);
    if (n === 8) return code.slice(0, 8) + '+';
    return code.slice(0, 8) + '+' + code.slice(9, 9 + (n - 8));
  }
  const fullVisible = sliceByDigits(level);
  let ctxVisible = parentLen ? sliceByDigits(parentLen) : '';
  let newPart = fullVisible.slice(ctxVisible.length);
  // Move a leading '+' from newPart into ctxVisible
  if (newPart.startsWith('+')) {
    ctxVisible += '+';
    newPart = newPart.slice(1);
  }
  return { ctxPart: ctxVisible, newPart };
}

function parentLevel(level) {
  const idx = LEVELS.indexOf(level);
  if (idx <= 0) return 0;
  return LEVELS[idx - 1];
}

/* Legend: highlight active levels */
function buildLegend() {
  const list = document.getElementById('legend-list');
  const sizes = {
    2:  '20°',
    4:  '1°',
    6:  '5.6 km',
    8:  '275 m',
    10: '14 m',
    11: '3 m',
    12: '0.6 m',
  };
  list.innerHTML = '';
  for (const level of LEVELS) {
    const li = document.createElement('li');
    li.dataset.level = level;
    li.innerHTML = `
      <span class="swatch"></span>
      <span class="label">${level}-digit</span>
      <span class="size">${sizes[level]}</span>
    `;
    list.appendChild(li);
  }
}
function updateLegend(drawn) {
  const activeSet = new Set(drawn.filter(d => d.opacity > 0.4).map(d => d.level));
  for (const li of document.querySelectorAll('#legend-list li')) {
    const level = Number(li.dataset.level);
    li.classList.toggle('active', activeSet.has(level));
  }
}

/* ------------------------ Map setup ------------------------ */
const map = L.map('map', {
  zoomSnap: 0.25,
  zoomDelta: 0.5,
  wheelDebounceTime: 8,
  wheelPxPerZoomLevel: 90,
  worldCopyJump: false,
  preferCanvas: true,
  attributionControl: true,
  zoomControl: false,
  maxZoom: 22,
}).setView([37.7793, -122.4193], 12);

L.control.zoom({ position: 'bottomright' }).addTo(map);

L.tileLayer(
  'https://cartodb-basemaps-a.global.ssl.fastly.net/dark_nolabels/{z}/{x}/{y}{r}.png',
  {
    attribution:
      '&copy; <a href="https://www.openstreetmap.org/copyright">OSM</a> &middot; ' +
      '<a href="https://carto.com/attributions">CARTO</a> &middot; ' +
      '<a href="https://maps.google.com/pluscodes/">Plus Codes</a>',
    maxZoom: 22,
    maxNativeZoom: 20,
    detectRetina: true,
    subdomains: 'abcd',
  }
).addTo(map);

L.tileLayer(
  'https://cartodb-basemaps-a.global.ssl.fastly.net/dark_only_labels/{z}/{x}/{y}{r}.png',
  {
    maxZoom: 22,
    maxNativeZoom: 20,
    opacity: 0.45,
    detectRetina: true,
    subdomains: 'abcd',
  }
).addTo(map);

new GridLayer().addTo(map);

buildLegend();

/* ------------------------ Cursor readout ------------------------ */
const elCode = document.getElementById('r-code');
const elCoords = document.getElementById('r-coords');
const elCell = document.getElementById('r-cell');

function formatCodeHTML(code) {
  // Highlight the '+' separator
  return code.replace('+', '<span class="sep">+</span>');
}

function cellSizeLabel(level) {
  const { lat, lng } = CELL[level];
  // Approximate meters at given lat (use 40.075km / 360deg for lng at equator scaled by cos(lat))
  const center = map.getCenter();
  const mPerDegLat = 111320;
  const mPerDegLng = 111320 * Math.cos(center.lat * Math.PI / 180);
  const wm = lng * mPerDegLng;
  const hm = lat * mPerDegLat;
  function fmt(m) {
    if (m >= 1000) return `${(m/1000).toFixed(m >= 10000 ? 0 : 1)} km`;
    if (m >= 1) return `${m.toFixed(0)} m`;
    return `${(m*100).toFixed(0)} cm`;
  }
  return `${fmt(wm)} × ${fmt(hm)}`;
}

function updateReadout(lat, lng) {
  const level = state.primaryLevel;
  const code = encode(lat, lng, level);
  elCode.innerHTML = formatCodeHTML(code);
  elCoords.textContent = `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
  elCell.textContent = cellSizeLabel(level);
}

let lastMouseLatLng = null;
map.on('mousemove', e => {
  lastMouseLatLng = e.latlng;
  updateReadout(e.latlng.lat, e.latlng.lng);
});
map.on('zoomend moveend', () => {
  if (lastMouseLatLng) {
    updateReadout(lastMouseLatLng.lat, lastMouseLatLng.lng);
  } else {
    const c = map.getCenter();
    updateReadout(c.lat, c.lng);
  }
});

// Initial
const c = map.getCenter();
updateReadout(c.lat, c.lng);

/* ------------------------ Touch hint ------------------------ */
// On touch devices, since there's no mouse, follow center on every move
if (matchMedia('(pointer: coarse)').matches) {
  map.on('move', () => {
    const center = map.getCenter();
    updateReadout(center.lat, center.lng);
  });
}

/* ------------------------ Click to copy ------------------------ */
const toastEl = document.getElementById('toast');

function showToast(text) {
  if (!toastEl) return;
  toastEl.textContent = text;
  toastEl.classList.add('visible');
  clearTimeout(showToast._t);
  showToast._t = setTimeout(() => toastEl.classList.remove('visible'), 1800);
}

async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch (e) {
    /* fall through */
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch (e) { /* ignore */ }
  document.body.removeChild(ta);
  return ok;
}

map.on('click', async e => {
  const code = encode(e.latlng.lat, e.latlng.lng, state.primaryLevel);
  const ok = await copyText(code);
  showToast(ok ? `Copied  ${code}` : code);
});
