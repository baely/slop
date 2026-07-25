'use strict';

/* plot — seeded generative 1-bit pattern maker.
   Part 1 is pure (no DOM): PRNG, noise, four generators, SVG builder.
   Part 2 wires it to the page. The node checks load part 1 only. */

/* ---------- caps ---------- */

var MAX_SEGMENTS = 150000;   // line segments a flow field may emit
var MAX_PARTICLES = 60000;   // flow field seeds attempted, emitted or not
var MAX_SUBPATHS = 100000;   // emitted subpaths, any generator
var MAX_CELLS = 250000;      // contour field grid vertices
var MAX_CELL_TESTS = 4000000; // grid cells x contour levels
var MAX_TILES = 30000;       // truchet tiles
var MAX_HATCH_LINES = 12000; // hatch lines across all passes
var BATCH = 500;             // subpaths per <path> element

/* ---------- defaults (single source of truth; the UI seeds itself here) ---------- */

var DEFAULTS = {
  gen: 'flow',
  seed: 'A3F19C2B',
  width: 1404, height: 1872,
  weight: 1.4, density: 1, margin: 90, invert: false,
  flowParticles: 1400, flowSteps: 240, flowStep: 4, flowScale: 2,
  flowOctaves: 3, flowCurl: 1, flowSep: 4,
  ctrRes: 220, ctrLevels: 22, ctrScale: 2, ctrOctaves: 4,
  truTile: 90, truLines: 3, truFill: 100, truVariant: 'arcs',
  hatAngle: 45, hatSpacing: 9, hatJitter: 25, hatPasses: 2,
  hatCross: 90, hatMask: 0, hatScale: 3
};

/* ---------- PRNG: mulberry32 ---------- */

function mulberry32(seed) {
  var a = seed >>> 0;
  return function () {
    a = (a + 0x6D2B79F5) >>> 0;
    var t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/* FNV-1a, so any seed string maps to a stable 32-bit state. */
function hashString(str) {
  var h = 0x811c9dc5;
  for (var i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/* ---------- gradient noise, seeded from the same PRNG ---------- */

function makeNoise(rand) {
  var p = new Uint8Array(256);
  var i;
  for (i = 0; i < 256; i++) p[i] = i;
  for (i = 255; i > 0; i--) {           // Fisher-Yates off the seeded stream
    var j = Math.floor(rand() * (i + 1));
    var t = p[i]; p[i] = p[j]; p[j] = t;
  }
  var perm = new Uint16Array(512);
  for (i = 0; i < 512; i++) perm[i] = p[i & 255];

  var gx = new Float64Array(256), gy = new Float64Array(256);
  for (i = 0; i < 256; i++) {
    var ang = rand() * Math.PI * 2;
    gx[i] = Math.cos(ang); gy[i] = Math.sin(ang);
  }

  // quintic fade: C2 continuous, so contours of the field stay smooth
  function fade(t) { return t * t * t * (t * (t * 6 - 15) + 10); }

  function noise2(x, y) {
    var X = Math.floor(x), Y = Math.floor(y);
    var xf = x - X, yf = y - Y;
    var xi = X & 255, yi = Y & 255;
    var xi1 = (xi + 1) & 255, yi1 = (yi + 1) & 255;
    var aa = perm[perm[xi] + yi], ba = perm[perm[xi1] + yi];
    var ab = perm[perm[xi] + yi1], bb = perm[perm[xi1] + yi1];
    var n00 = gx[aa] * xf + gy[aa] * yf;
    var n10 = gx[ba] * (xf - 1) + gy[ba] * yf;
    var n01 = gx[ab] * xf + gy[ab] * (yf - 1);
    var n11 = gx[bb] * (xf - 1) + gy[bb] * (yf - 1);
    var u = fade(xf), v = fade(yf);
    var a1 = n00 + u * (n10 - n00);
    var b1 = n01 + u * (n11 - n01);
    // *1.4142 lifts the theoretical +/-0.7071 span to roughly +/-1
    return (a1 + v * (b1 - a1)) * 1.41421356;
  }

  function fbm(x, y, octaves) {
    var sum = 0, norm = 0, amp = 1, freq = 1;
    for (var o = 0; o < octaves; o++) {
      sum += amp * noise2(x * freq, y * freq);
      norm += amp;
      amp *= 0.5; freq *= 2;
    }
    return sum / norm;
  }

  return { noise2: noise2, fbm: fbm };
}

/* ---------- geometry / formatting helpers ---------- */

function fmt(v) {
  var r = Math.round(v * 100) / 100;
  if (r === 0) r = 0;                  // fold -0 so path data is stable
  return String(r);
}

function int(v) { return Math.round(v).toLocaleString('en-US'); }

function polyPath(pts) {
  var n = pts.length;
  var closed = false;
  if (n > 3 &&
      Math.abs(pts[0][0] - pts[n - 1][0]) < 0.01 &&
      Math.abs(pts[0][1] - pts[n - 1][1]) < 0.01) {
    closed = true; n -= 1;
  }
  var d = 'M' + fmt(pts[0][0]) + ' ' + fmt(pts[0][1]);
  for (var i = 1; i < n; i++) d += 'L' + fmt(pts[i][0]) + ' ' + fmt(pts[i][1]);
  return closed ? d + 'Z' : d;
}

function linePath(x1, y1, x2, y2) {
  return 'M' + fmt(x1) + ' ' + fmt(y1) + 'L' + fmt(x2) + ' ' + fmt(y2);
}

function arcPath(x1, y1, x2, y2, r, sweep) {
  return 'M' + fmt(x1) + ' ' + fmt(y1) + 'A' + fmt(r) + ' ' + fmt(r) +
    ' 0 0 ' + sweep + ' ' + fmt(x2) + ' ' + fmt(y2);
}

/* Liang-Barsky clip of an infinite-ish segment to the drawing rect. */
function clipToRect(x1, y1, x2, y2, xmin, ymin, xmax, ymax) {
  var t0 = 0, t1 = 1, dx = x2 - x1, dy = y2 - y1;
  var p = [-dx, dx, -dy, dy];
  var q = [x1 - xmin, xmax - x1, y1 - ymin, ymax - y1];
  for (var i = 0; i < 4; i++) {
    if (p[i] === 0) { if (q[i] < 0) return null; continue; }
    var r = q[i] / p[i];
    if (p[i] < 0) { if (r > t1) return null; if (r > t0) t0 = r; }
    else { if (r < t0) return null; if (r < t1) t1 = r; }
  }
  return [x1 + t0 * dx, y1 + t0 * dy, x1 + t1 * dx, y1 + t1 * dy];
}

/* ---------- generator 1: flow field ---------- */

function genFlow(ctx) {
  var a = ctx.area, s = ctx.s, rand = ctx.rand, fbm = ctx.noise.fbm, notes = ctx.notes;
  var particles = Math.max(1, Math.round(s.flowParticles * ctx.density));
  var steps = Math.max(2, Math.round(s.flowSteps));
  var stepLen = s.flowStep;
  var scale = s.flowScale / 1000;      // slider units -> cycles per pixel
  var oct = Math.round(s.flowOctaves);
  var curl = s.flowCurl;
  var sep = Math.round(s.flowSep);

  if (particles > MAX_PARTICLES) {
    notes.push('Clamped: particles ' + int(particles) + ' → ' + int(MAX_PARTICLES) +
      ' (particle cap).');
    particles = MAX_PARTICLES;
  }

  // occupancy grid keeps lines from piling into the same channel
  var grid = null, gw = 0, gs = sep;
  if (sep > 0) {
    gw = Math.ceil(a.w / gs) + 1;
    grid = new Uint8Array(gw * (Math.ceil(a.h / gs) + 1));
  }

  var out = [];
  var drawn = 0;                       // real emitted segments, the thing that costs
  for (var i = 0; i < particles; i++) {
    if (drawn >= MAX_SEGMENTS) {
      notes.push('Stopped at ' + int(MAX_SEGMENTS) + ' segments after ' + int(i) +
        ' of ' + int(particles) + ' particles (segment cap).');
      break;
    }
    var x = a.x0 + rand() * a.w, y = a.y0 + rand() * a.h;
    var last = -1;
    if (grid) {
      last = Math.floor((y - a.y0) / gs) * gw + Math.floor((x - a.x0) / gs);
      if (grid[last]) continue;
      grid[last] = 1;
    }
    var pts = [[x, y]];
    for (var k = 0; k < steps; k++) {
      var ang = fbm(x * scale, y * scale, oct) * Math.PI * 2 * curl;
      x += Math.cos(ang) * stepLen;
      y += Math.sin(ang) * stepLen;
      if (x < a.x0 || x > a.x1 || y < a.y0 || y > a.y1) break;
      if (grid) {
        var gi = Math.floor((y - a.y0) / gs) * gw + Math.floor((x - a.x0) / gs);
        if (gi !== last) {
          if (grid[gi]) break;
          grid[gi] = 1;
          last = gi;
        }
      }
      pts.push([x, y]);
    }
    if (pts.length > 1) { out.push(polyPath(pts)); drawn += pts.length - 1; }
  }
  return out;
}

/* ---------- generator 2: contours (marching squares) ---------- */

/* edge ids: 0 top, 1 right, 2 bottom, 3 left. Index bits: 8 TL, 4 TR, 2 BR, 1 BL. */
var MS_EDGES = [
  [], [3, 2], [2, 1], [3, 1], [0, 1], [0, 1, 3, 2], [0, 2], [3, 0],
  [3, 0], [0, 2], [3, 0, 2, 1], [0, 1], [3, 1], [2, 1], [3, 2], []
];

function marchingSquares(f, nx, ny, W1, t, ox, oy, cw, ch) {
  var segs = [];                       // flat, stride 4
  var ex = [0, 0, 0, 0], ey = [0, 0, 0, 0];
  for (var j = 0; j < ny; j++) {
    var row = j * W1, row1 = (j + 1) * W1;
    var y = oy + j * ch, y1 = y + ch;
    for (var i = 0; i < nx; i++) {
      var v0 = f[row + i], v1 = f[row + i + 1], v2 = f[row1 + i + 1], v3 = f[row1 + i];
      var idx = 0;
      if (v0 > t) idx |= 8;
      if (v1 > t) idx |= 4;
      if (v2 > t) idx |= 2;
      if (v3 > t) idx |= 1;
      if (idx === 0 || idx === 15) continue;
      var e = MS_EDGES[idx];
      if (idx === 5 || idx === 10) {   // saddle: let the cell average decide
        var mid = (v0 + v1 + v2 + v3) * 0.25;
        if (mid > t) e = (idx === 5) ? MS_EDGES[10] : MS_EDGES[5];
      }
      var x = ox + i * cw, x1 = x + cw;
      ex[0] = x + cw * (t - v0) / (v1 - v0); ey[0] = y;
      ex[1] = x1;                            ey[1] = y + ch * (t - v1) / (v2 - v1);
      ex[2] = x + cw * (t - v3) / (v2 - v3); ey[2] = y1;
      ex[3] = x;                             ey[3] = y + ch * (t - v0) / (v3 - v0);
      for (var k = 0; k < e.length; k += 2) {
        segs.push(ex[e[k]], ey[e[k]], ex[e[k + 1]], ey[e[k + 1]]);
      }
    }
  }
  return segs;
}

/* Join loose segments into polylines so one contour is one subpath. */
function stitch(segs) {
  var n = segs.length / 4;
  var map = new Map();
  var i, k, arr;
  function key(x, y) { return Math.round(x * 64) + ',' + Math.round(y * 64); }
  function add(k2, i2) { arr = map.get(k2); if (!arr) { arr = []; map.set(k2, arr); } arr.push(i2); }
  for (i = 0; i < n; i++) {
    add(key(segs[i * 4], segs[i * 4 + 1]), i);
    add(key(segs[i * 4 + 2], segs[i * 4 + 3]), i);
  }
  var used = new Uint8Array(n);
  function walk(pts) {
    for (;;) {
      var p = pts[pts.length - 1];
      var pk = key(p[0], p[1]);
      var list = map.get(pk);
      if (!list) return;
      var next = -1;
      for (var q = 0; q < list.length; q++) { if (!used[list[q]]) { next = list[q]; break; } }
      if (next < 0) return;
      used[next] = 1;
      var o = next * 4;
      var np = (key(segs[o], segs[o + 1]) === pk) ? [segs[o + 2], segs[o + 3]] : [segs[o], segs[o + 1]];
      pts.push(np);
    }
  }
  var polys = [];
  for (i = 0; i < n; i++) {
    if (used[i]) continue;
    used[i] = 1;
    var o2 = i * 4;
    var fwd = [[segs[o2], segs[o2 + 1]], [segs[o2 + 2], segs[o2 + 3]]];
    walk(fwd);
    var bwd = [[segs[o2 + 2], segs[o2 + 3]], [segs[o2], segs[o2 + 1]]];
    walk(bwd);
    var poly = [];
    for (k = bwd.length - 1; k >= 2; k--) poly.push(bwd[k]);
    for (k = 0; k < fwd.length; k++) poly.push(fwd[k]);
    polys.push(poly);
  }
  return polys;
}

function genContours(ctx) {
  var a = ctx.area, s = ctx.s, fbm = ctx.noise.fbm, notes = ctx.notes;
  var res = Math.max(8, Math.round(s.ctrRes * ctx.density));
  var levels = Math.max(1, Math.round(s.ctrLevels));
  var scale = s.ctrScale / 1000;
  var oct = Math.round(s.ctrOctaves);

  var nx = res, ny = Math.max(2, Math.round(res * a.h / a.w));
  if ((nx + 1) * (ny + 1) > MAX_CELLS) {
    var f1 = Math.sqrt(MAX_CELLS / ((nx + 1) * (ny + 1)));
    var nr = Math.max(8, Math.floor(res * f1));
    notes.push('Clamped: resolution ' + int(res) + ' → ' + int(nr) +
      ' cells (grid cap ' + int(MAX_CELLS) + ' vertices).');
    res = nr; nx = res; ny = Math.max(2, Math.round(res * a.h / a.w));
  }
  if (nx * ny * levels > MAX_CELL_TESTS) {
    var nl = Math.max(1, Math.floor(MAX_CELL_TESTS / (nx * ny)));
    notes.push('Clamped: levels ' + int(levels) + ' → ' + int(nl) +
      ' (cell-test cap ' + int(MAX_CELL_TESTS) + ').');
    levels = nl;
  }

  var W1 = nx + 1, cw = a.w / nx, ch = a.h / ny;
  var field = new Float64Array(W1 * (ny + 1));
  var mn = Infinity, mx = -Infinity;
  for (var j = 0; j <= ny; j++) {
    for (var i = 0; i <= nx; i++) {
      var v = fbm((a.x0 + i * cw) * scale, (a.y0 + j * ch) * scale, oct);
      field[j * W1 + i] = v;
      if (v < mn) mn = v;
      if (v > mx) mx = v;
    }
  }

  var out = [];
  for (var L = 0; L < levels; L++) {
    // spread thresholds across the field's real range so every level draws
    var t = mn + (mx - mn) * (L + 1) / (levels + 1);
    var polys = stitch(marchingSquares(field, nx, ny, W1, t, a.x0, a.y0, cw, ch));
    for (var q = 0; q < polys.length; q++) {
      if (polys[q].length > 1) out.push(polyPath(polys[q]));
      if (out.length >= MAX_SUBPATHS) {
        notes.push('Truncated at ' + int(MAX_SUBPATHS) + ' subpaths (output cap).');
        return out;
      }
    }
  }
  return out;
}

/* ---------- generator 3: truchet tiles ---------- */

/* Each tile carries n quarter-arcs (or their chords) around two opposite
   corners; radii r_k = s(k+1)/(n+1) so lines meet across tile edges. */
function truchetTile(x, y, s2, n, flip, asArc, out) {
  for (var k = 0; k < n; k++) {
    var r = s2 * (k + 1) / (n + 1);
    var ax, ay, bx, by, cx, cy, dx2, dy2, sw1, sw2;
    if (!flip) {
      ax = x;          ay = y + r;      bx = x + r;      by = y;          sw1 = 0;
      cx = x + s2 - r; cy = y + s2;     dx2 = x + s2;    dy2 = y + s2 - r; sw2 = 1;
    } else {
      ax = x + s2 - r; ay = y;          bx = x + s2;     by = y + r;      sw1 = 0;
      cx = x;          cy = y + s2 - r; dx2 = x + r;     dy2 = y + s2;    sw2 = 1;
    }
    if (asArc) {
      out.push(arcPath(ax, ay, bx, by, r, sw1));
      out.push(arcPath(cx, cy, dx2, dy2, r, sw2));
    } else {
      out.push(linePath(ax, ay, bx, by));
      out.push(linePath(cx, cy, dx2, dy2));
    }
  }
}

function genTruchet(ctx) {
  var a = ctx.area, s = ctx.s, rand = ctx.rand, notes = ctx.notes;
  var tile = Math.max(4, s.truTile / ctx.density);
  var lines = Math.max(1, Math.round(s.truLines));
  var fill = s.truFill / 100;
  var variant = s.truVariant;

  var cols = Math.max(1, Math.floor(a.w / tile));
  var rows = Math.max(1, Math.floor(a.h / tile));
  if (cols * rows > MAX_TILES) {
    var grow = Math.sqrt((cols * rows) / MAX_TILES);
    var nt = tile * grow;
    notes.push('Clamped: tile size ' + int(tile) + ' → ' + int(nt) +
      ' px (tile cap ' + int(MAX_TILES) + ').');
    tile = nt;
    cols = Math.max(1, Math.floor(a.w / tile));
    rows = Math.max(1, Math.floor(a.h / tile));
  }
  var ox = a.x0 + (a.w - cols * tile) / 2;
  var oy = a.y0 + (a.h - rows * tile) / 2;

  var out = [];
  for (var j = 0; j < rows; j++) {
    for (var i = 0; i < cols; i++) {
      var rFill = rand(), rFlip = rand(), rKind = rand();
      if (rFill > fill) continue;
      var asArc = variant === 'arcs' ? true : (variant === 'chords' ? false : rKind < 0.5);
      truchetTile(ox + i * tile, oy + j * tile, tile, lines, rFlip < 0.5, asArc, out);
    }
  }
  return out;
}

/* ---------- generator 4: hatching ---------- */

function genHatch(ctx) {
  var a = ctx.area, s = ctx.s, rand = ctx.rand, fbm = ctx.noise.fbm, notes = ctx.notes;
  var spacing = Math.max(0.5, s.hatSpacing / ctx.density);
  var passes = Math.max(1, Math.round(s.hatPasses));
  var jitter = s.hatJitter / 100;
  var cross = s.hatCross;
  var mask = s.hatMask / 100;
  var mscale = s.hatScale / 1000;
  var oct = 3;
  var diag = Math.sqrt(a.w * a.w + a.h * a.h);

  var est = passes * Math.ceil(diag / spacing);
  if (est > MAX_HATCH_LINES) {
    var ns = spacing * (est / MAX_HATCH_LINES);
    notes.push('Clamped: spacing ' + fmt(spacing) + ' → ' + fmt(ns) +
      ' px (line cap ' + int(MAX_HATCH_LINES) + ').');
    spacing = ns;
  }
  var thresh = mask * 2 - 1;
  var step = Math.max(1.5, spacing * 0.5);
  var out = [];

  for (var pi = 0; pi < passes; pi++) {
    var ang = (s.hatAngle + pi * cross) * Math.PI / 180;
    var dx = Math.cos(ang), dy = Math.sin(ang);
    var nxu = -dy, nyu = dx;
    var c = [[a.x0, a.y0], [a.x1, a.y0], [a.x1, a.y1], [a.x0, a.y1]];
    var pmin = Infinity, pmax = -Infinity;
    for (var ci = 0; ci < 4; ci++) {
      var pr = c[ci][0] * nxu + c[ci][1] * nyu;
      if (pr < pmin) pmin = pr;
      if (pr > pmax) pmax = pr;
    }
    var k0 = Math.ceil(pmin / spacing), k1 = Math.floor(pmax / spacing);
    for (var k = k0; k <= k1; k++) {
      var off = k * spacing + (rand() - 0.5) * jitter * spacing;
      var bx = nxu * off, by = nyu * off;
      var seg = clipToRect(bx - dx * diag * 2, by - dy * diag * 2,
                           bx + dx * diag * 2, by + dy * diag * 2,
                           a.x0, a.y0, a.x1, a.y1);
      if (!seg) continue;
      var len = Math.sqrt((seg[2] - seg[0]) * (seg[2] - seg[0]) + (seg[3] - seg[1]) * (seg[3] - seg[1]));
      var trimA = rand() * jitter * spacing * 2;
      var trimB = rand() * jitter * spacing * 2;
      var t0 = trimA, t1 = len - trimB;
      if (t1 - t0 < 0.5) continue;
      var sx = seg[0], sy = seg[1];

      if (mask <= 0) {
        out.push(linePath(sx + dx * t0, sy + dy * t0, sx + dx * t1, sy + dy * t1));
      } else {
        var runStart = -1;
        for (var t = t0; t <= t1 + step; t += step) {
          var tc = t > t1 ? t1 : t;
          var px = sx + dx * tc, py = sy + dy * tc;
          var on = fbm(px * mscale, py * mscale, oct) > thresh && t <= t1;
          if (on && runStart < 0) runStart = tc;
          else if (!on && runStart >= 0) {
            if (tc - runStart > 0.5) {
              out.push(linePath(sx + dx * runStart, sy + dy * runStart, sx + dx * tc, sy + dy * tc));
            }
            runStart = -1;
          }
        }
        if (runStart >= 0 && t1 - runStart > 0.5) {
          out.push(linePath(sx + dx * runStart, sy + dy * runStart, sx + dx * t1, sy + dy * t1));
        }
      }
      if (out.length >= MAX_SUBPATHS) {
        notes.push('Truncated at ' + int(MAX_SUBPATHS) + ' subpaths (output cap).');
        return out;
      }
    }
  }
  return out;
}

/* ---------- assembly ---------- */

var GENERATORS = {
  flow: { label: 'Flow Field', run: genFlow },
  contours: { label: 'Contours', run: genContours },
  truchet: { label: 'Truchet', run: genTruchet },
  hatch: { label: 'Hatching', run: genHatch }
};

function generate(state) {
  var seedInt = hashString(String(state.seed));
  var rand = mulberry32(seedInt);
  // noise runs on its own stream so tweaking counts doesn't reshuffle the field
  var noise = makeNoise(mulberry32((seedInt ^ 0x9e3779b9) >>> 0));
  var notes = [];

  var W = state.width, H = state.height;
  var maxMargin = Math.floor(Math.min(W, H) / 2) - 8;
  var m = state.margin;
  if (m > maxMargin) {
    notes.push('Clamped: margin ' + int(m) + ' → ' + int(maxMargin) + ' px (canvas too small).');
    m = maxMargin;
  }
  if (m < 0) m = 0;

  var area = { x0: m, y0: m, x1: W - m, y1: H - m, w: W - 2 * m, h: H - 2 * m };
  var ctx = { rand: rand, noise: noise, area: area, s: state, notes: notes, density: state.density };
  var gen = GENERATORS[state.gen] || GENERATORS.flow;
  var subpaths = gen.run(ctx);
  return { subpaths: subpaths, notes: notes, area: area };
}

function buildSVG(subpaths, state) {
  var bg = state.invert ? '#000000' : '#ffffff';
  var fg = state.invert ? '#ffffff' : '#000000';
  var W = state.width, H = state.height;
  var parts = [];
  for (var i = 0; i < subpaths.length; i += BATCH) {
    parts.push('<path d="' + subpaths.slice(i, i + BATCH).join('') + '"/>');
  }
  var svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' + W + '" height="' + H +
    '" viewBox="0 0 ' + W + ' ' + H + '">' +
    '<rect width="' + W + '" height="' + H + '" fill="' + bg + '"/>' +
    '<g fill="none" stroke="' + fg + '" stroke-width="' + state.weight +
    '" stroke-linecap="round" stroke-linejoin="round">' +
    parts.join('') + '</g></svg>';
  return { svg: svg, elements: parts.length };
}

if (typeof module === 'object' && module.exports) {
  module.exports = {
    mulberry32: mulberry32, hashString: hashString, makeNoise: makeNoise,
    generate: generate, buildSVG: buildSVG, GENERATORS: GENERATORS,
    stitch: stitch, clipToRect: clipToRect, DEFAULTS: DEFAULTS
  };
}

/* ---------- 2. page wiring ---------- */

var FORMAT = {
  weight: function (v) { return v.toFixed(2) + ' px'; },
  density: function (v) { return v.toFixed(2) + '×'; },
  margin: function (v) { return int(v) + ' px'; },
  flowParticles: int,
  flowSteps: int,
  flowStep: function (v) { return v.toFixed(1) + ' px'; },
  flowScale: function (v) { return v.toFixed(1); },
  flowOctaves: int,
  flowCurl: function (v) { return v.toFixed(2); },
  flowSep: function (v) { return v > 0 ? int(v) + ' px' : 'off'; },
  ctrRes: function (v) { return int(v) + ' cells'; },
  ctrLevels: int,
  ctrScale: function (v) { return v.toFixed(1); },
  ctrOctaves: int,
  truTile: function (v) { return int(v) + ' px'; },
  truLines: int,
  truFill: function (v) { return int(v) + '%'; },
  hatAngle: function (v) { return int(v) + '°'; },
  hatSpacing: function (v) { return v.toFixed(1) + ' px'; },
  hatJitter: function (v) { return int(v) + '%'; },
  hatPasses: int,
  hatCross: function (v) { return int(v) + '°'; },
  hatMask: function (v) { return int(v) + '%'; },
  hatScale: function (v) { return v.toFixed(1); }
};

var RANGE_IDS = Object.keys(FORMAT);
var STORE_KEY = 'plot.state.v1';

function initUI() {
  var $ = function (id) { return document.getElementById(id); };
  var preview = $('preview'), statusEl = $('status'), notesEl = $('notes');
  var gen = DEFAULTS.gen, invert = DEFAULTS.invert;
  var last = null, timer = null;

  function panels() {
    Object.keys(GENERATORS).forEach(function (g) {
      $('panel-' + g).hidden = (g !== gen);
      $('tab-' + g).setAttribute('aria-pressed', String(g === gen));
    });
  }

  function syncOut(id) {
    var el = $(id + 'Val');
    if (el) el.textContent = FORMAT[id](parseFloat($(id).value));
  }

  function newSeed() {
    var b = new Uint32Array(1);
    if (typeof crypto !== 'undefined' && crypto.getRandomValues) crypto.getRandomValues(b);
    else b[0] = (Date.now() ^ Math.floor(performance.now() * 1000)) >>> 0;
    return b[0].toString(16).toUpperCase();
  }

  function applyState(st) {
    gen = GENERATORS[st.gen] ? st.gen : DEFAULTS.gen;
    invert = !!st.invert;
    $('seed').value = st.seed;
    $('width').value = st.width;
    $('height').value = st.height;
    $('truVariant').value = st.truVariant;
    RANGE_IDS.forEach(function (id) { $(id).value = st[id]; syncOut(id); });
    $('invert').setAttribute('aria-pressed', String(invert));
    syncPreset();
    panels();
  }

  function syncPreset() {
    var key = $('width').value + 'x' + $('height').value;
    var sel = $('preset'), found = false;
    for (var i = 0; i < sel.options.length; i++) {
      if (sel.options[i].value === key) { sel.value = key; found = true; break; }
    }
    if (!found) sel.value = 'custom';
  }

  /* returns null (and explains itself) when the canvas size is unusable */
  function readState() {
    var w = parseInt($('width').value, 10), h = parseInt($('height').value, 10);
    if (!isFinite(w) || !isFinite(h) || w < 100 || h < 100 || w > 6000 || h > 6000) {
      statusEl.textContent = 'Canvas size must be 100–6000 px on each side. Nothing rendered.';
      notesEl.textContent = '';
      return null;
    }
    var st = {
      gen: gen,
      seed: $('seed').value.trim() || DEFAULTS.seed,
      width: w, height: h, invert: invert,
      truVariant: $('truVariant').value
    };
    RANGE_IDS.forEach(function (id) { st[id] = parseFloat($(id).value); });
    return st;
  }

  function save(st) {
    try { localStorage.setItem(STORE_KEY, JSON.stringify(st)); } catch (e) { /* private mode */ }
  }

  function load() {
    try {
      var raw = localStorage.getItem(STORE_KEY);
      if (!raw) return null;
      var st = JSON.parse(raw);
      var merged = {};
      Object.keys(DEFAULTS).forEach(function (k) {
        merged[k] = (typeof st[k] === typeof DEFAULTS[k] && st[k] !== null) ? st[k] : DEFAULTS[k];
      });
      return merged;
    } catch (e) { return null; }
  }

  function kb(bytes) {
    return bytes >= 1048576 ? (bytes / 1048576).toFixed(1) + ' MB' : Math.round(bytes / 1024) + ' KB';
  }

  function draw(st) {
    try {
      var t0 = performance.now();
      var r = generate(st);
      var built = buildSVG(r.subpaths, st);
      var ms = performance.now() - t0;
      preview.innerHTML = built.svg;
      last = { svg: built.svg, state: st };
      $('statPaths').textContent = int(r.subpaths.length);
      $('statEls').textContent = int(built.elements);
      $('statMs').textContent = Math.round(ms) + ' ms';
      $('statSize').textContent = kb(built.svg.length);
      statusEl.textContent = GENERATORS[st.gen].label.toLowerCase() + ' · ' +
        st.width + ' × ' + st.height + ' · seed ' + st.seed + ' · ' +
        int(r.subpaths.length) + ' paths.';
      notesEl.textContent = r.notes.join(' ');
    } catch (err) {
      statusEl.textContent = 'Render failed: ' + (err && err.message ? err.message : 'unknown error') + '.';
      notesEl.textContent = '';
    }
  }

  function render() {
    var st = readState();
    if (!st) return;
    save(st);
    statusEl.textContent = 'Rendering…';
    // yield once so the status line paints before a heavy synchronous build
    requestAnimationFrame(function () { requestAnimationFrame(function () { draw(st); }); });
  }

  function queue() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(render, 130);
  }

  /* ---- exports ---- */

  function fileName(ext) {
    var s = last.state;
    return 'plot-' + s.gen + '-' + s.seed + '-' + s.width + 'x' + s.height + '.' + ext;
  }

  function download(blob, name) {
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(function () { URL.revokeObjectURL(url); }, 4000);
  }

  $('exportSVG').addEventListener('click', function () {
    if (!last) { statusEl.textContent = 'Nothing rendered yet.'; return; }
    download(new Blob([last.svg], { type: 'image/svg+xml;charset=utf-8' }), fileName('svg'));
    statusEl.textContent = 'Saved ' + fileName('svg') + ' · ' + kb(last.svg.length) + '.';
  });

  $('exportPNG').addEventListener('click', function () {
    if (!last) { statusEl.textContent = 'Nothing rendered yet.'; return; }
    var s = last.state;
    var url = URL.createObjectURL(new Blob([last.svg], { type: 'image/svg+xml;charset=utf-8' }));
    var img = new Image();
    statusEl.textContent = 'Rasterising ' + s.width + ' × ' + s.height + '…';
    img.onload = function () {
      URL.revokeObjectURL(url);
      var c = document.createElement('canvas');
      c.width = s.width; c.height = s.height;
      var g = c.getContext('2d');
      g.fillStyle = s.invert ? '#000000' : '#ffffff';
      g.fillRect(0, 0, s.width, s.height);
      g.drawImage(img, 0, 0, s.width, s.height);
      c.toBlob(function (b) {
        if (!b) { statusEl.textContent = 'PNG export failed — the canvas produced no data.'; return; }
        download(b, fileName('png'));
        statusEl.textContent = 'Saved ' + fileName('png') + ' · ' + kb(b.size) + '.';
      }, 'image/png');
    };
    img.onerror = function () {
      URL.revokeObjectURL(url);
      statusEl.textContent = 'PNG export failed — this browser could not rasterise the SVG. Export SVG instead.';
    };
    img.src = url;
  });

  /* ---- events ---- */

  Object.keys(GENERATORS).forEach(function (g) {
    $('tab-' + g).addEventListener('click', function () { gen = g; panels(); render(); });
  });

  RANGE_IDS.forEach(function (id) {
    $(id).addEventListener('input', function () { syncOut(id); queue(); });
  });

  $('seed').addEventListener('input', queue);
  $('truVariant').addEventListener('change', render);

  $('preset').addEventListener('change', function () {
    var v = $('preset').value;
    if (v === 'custom') return;
    var parts = v.split('x');
    $('width').value = parts[0];
    $('height').value = parts[1];
    render();
  });

  ['width', 'height'].forEach(function (id) {
    $(id).addEventListener('input', function () { syncPreset(); queue(); });
  });

  $('swap').addEventListener('click', function () {
    var w = $('width').value;
    $('width').value = $('height').value;
    $('height').value = w;
    syncPreset();
    render();
  });

  $('invert').addEventListener('click', function () {
    invert = !invert;
    $('invert').setAttribute('aria-pressed', String(invert));
    render();
  });

  $('newSeed').addEventListener('click', function () {
    $('seed').value = newSeed();
    render();
  });

  $('reset').addEventListener('click', function () {
    applyState(DEFAULTS);
    render();
  });

  applyState(load() || DEFAULTS);
  render();
}

if (typeof document !== 'undefined') initUI();
