(() => {
  'use strict';

  // ---------- canvas setup ----------

  const canvas = document.getElementById('table');
  const ctx = canvas.getContext('2d');
  let W = 0, H = 0, DPR = 1;

  function resize() {
    DPR = Math.min(window.devicePixelRatio || 1, 2);
    W = window.innerWidth;
    H = window.innerHeight;
    canvas.width = W * DPR;
    canvas.height = H * DPR;
    canvas.style.width = W + 'px';
    canvas.style.height = H + 'px';
    ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
    buildFeltTexture();
    spriteCache.clear();
    dirty = true;
  }
  window.addEventListener('resize', resize);

  // ---------- ball palette ----------

  // Refined classic-pool palette. We rotate through these as balls are placed.
  const PALETTE = [
    { fill: '#e6b34a', sheen: '#f7d685' }, // 1 yellow
    { fill: '#2f5d8e', sheen: '#5b8ec0' }, // 2 blue
    { fill: '#bf3e3e', sheen: '#df6f6f' }, // 3 red
    { fill: '#6b3e7a', sheen: '#9a6cb1' }, // 4 purple
    { fill: '#d57333', sheen: '#f0a060' }, // 5 orange
    { fill: '#296d4d', sheen: '#56a07d' }, // 6 green
    { fill: '#73262e', sheen: '#a1525a' }, // 7 maroon
    { fill: '#1c1c1c', sheen: '#4a4a4a' }, // 8 black
    { fill: '#f1e6cf', sheen: '#ffffff' }, // cue
  ];

  // ---------- physics ----------

  const BASE_RADIUS = 13;        // radius for mass = 1
  const FRICTION = 90;           // velocity-units/s² of deceleration
  const WALL_RESTITUTION = 0.92; // energy retained per wall bounce
  const MAX_SPEED = 4500;        // hard cap so insane drags don't break things
  const SUBSTEPS = 6;
  const GRAVITY = 3600;          // px/s² — only applies to pivot-constrained balls
  const PEND_DAMP = 0.05;        // air-drag rate for pendulums when friction is on

  const balls = [];

  class Ball {
    constructor(x, y, mass, palette) {
      this.x = x;
      this.y = y;
      this.vx = 0;
      this.vy = 0;
      this.mass = mass;
      this.radius = Math.sqrt(mass) * BASE_RADIUS;
      this.color = palette;
      this.spin = Math.random() * Math.PI * 2; // for the highlight position
      this.pivot = null; // optional { x, y, length } — turns the ball into a pendulum
    }
  }

  function radiusFor(mass) { return Math.sqrt(mass) * BASE_RADIUS; }

  function step(dt, restitution) {
    const sdt = dt / SUBSTEPS;
    for (let s = 0; s < SUBSTEPS; s++) {
      // gravity (pendulums only) + friction + integrate
      for (const b of balls) {
        if (b.pivot) {
          b.vy += GRAVITY * sdt;
          if (frictionOn) {
            const damp = 1 - PEND_DAMP * sdt;
            b.vx *= damp;
            b.vy *= damp;
          }
        } else if (frictionOn) {
          const sp = Math.hypot(b.vx, b.vy);
          if (sp > 0) {
            const dec = FRICTION * sdt;
            if (dec >= sp) { b.vx = 0; b.vy = 0; }
            else {
              const k = (sp - dec) / sp;
              b.vx *= k; b.vy *= k;
            }
          }
        }
        b.x += b.vx * sdt;
        b.y += b.vy * sdt;
      }
      // walls — skip for pendulums (their arc constraint keeps them in place)
      const wr = frictionOn ? WALL_RESTITUTION : 1;
      for (const b of balls) {
        if (b.pivot) continue;
        if (b.x - b.radius < 0)         { b.x = b.radius;         b.vx = -b.vx * wr; }
        else if (b.x + b.radius > W)    { b.x = W - b.radius;     b.vx = -b.vx * wr; }
        if (b.y - b.radius < 0)         { b.y = b.radius;         b.vy = -b.vy * wr; }
        else if (b.y + b.radius > H)    { b.y = H - b.radius;     b.vy = -b.vy * wr; }
      }
      // pairwise
      for (let i = 0; i < balls.length; i++) {
        for (let j = i + 1; j < balls.length; j++) {
          collide(balls[i], balls[j], restitution);
        }
      }
      // pendulum constraint projection — clamp ball to its arc, zero radial velocity
      for (const b of balls) {
        if (!b.pivot) continue;
        const dx = b.x - b.pivot.x;
        const dy = b.y - b.pivot.y;
        const d = Math.hypot(dx, dy);
        if (d < 1e-4) continue;
        const k = b.pivot.length / d;
        b.x = b.pivot.x + dx * k;
        b.y = b.pivot.y + dy * k;
        const nx = dx / d, ny = dy / d;
        const vRad = b.vx * nx + b.vy * ny;
        b.vx -= vRad * nx;
        b.vy -= vRad * ny;
      }
    }
  }

  function collide(a, b, e) {
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const d2 = dx*dx + dy*dy;
    const minD = a.radius + b.radius;
    if (d2 >= minD*minD) return;
    let dist = Math.sqrt(d2);
    if (dist === 0) {
      // exact overlap — nudge apart deterministically
      dist = 0.0001;
    }
    const nx = dx / dist;
    const ny = dy / dist;

    // positional separation, weighted by inverse mass so heavier moves less
    const overlap = minD - dist;
    const invA = 1 / a.mass;
    const invB = 1 / b.mass;
    const sumInv = invA + invB;
    a.x -= nx * overlap * (invA / sumInv);
    a.y -= ny * overlap * (invA / sumInv);
    b.x += nx * overlap * (invB / sumInv);
    b.y += ny * overlap * (invB / sumInv);

    // relative velocity along normal
    const rvx = b.vx - a.vx;
    const rvy = b.vy - a.vy;
    const relN = rvx * nx + rvy * ny;
    if (relN > 0) return; // already separating

    const j = -(1 + e) * relN / sumInv;
    const ix = j * nx;
    const iy = j * ny;
    a.vx -= ix * invA;
    a.vy -= iy * invA;
    b.vx += ix * invB;
    b.vy += iy * invB;
  }

  // ---------- rendering ----------

  let feltCanvas;
  function buildFeltTexture() {
    feltCanvas = document.createElement('canvas');
    feltCanvas.width = W;
    feltCanvas.height = H;
    const fctx = feltCanvas.getContext('2d');

    // base radial gradient
    const cx = W / 2, cy = H / 2;
    const r = Math.hypot(cx, cy);
    const g = fctx.createRadialGradient(cx, cy * 0.85, r * 0.1, cx, cy, r);
    g.addColorStop(0, '#1a4334');
    g.addColorStop(0.5, '#10312a');
    g.addColorStop(1, '#061714');
    fctx.fillStyle = g;
    fctx.fillRect(0, 0, W, H);

    // fine noise — felt fibers
    const img = fctx.getImageData(0, 0, W, H);
    const d = img.data;
    for (let i = 0; i < d.length; i += 4) {
      const n = (Math.random() - 0.5) * 14;
      d[i]   = Math.max(0, Math.min(255, d[i]   + n));
      d[i+1] = Math.max(0, Math.min(255, d[i+1] + n));
      d[i+2] = Math.max(0, Math.min(255, d[i+2] + n));
    }
    fctx.putImageData(img, 0, 0);

    // inner rail shadow — subtle vignette toward edges
    const v = fctx.createRadialGradient(cx, cy, r * 0.6, cx, cy, r);
    v.addColorStop(0, 'rgba(0,0,0,0)');
    v.addColorStop(1, 'rgba(0,0,0,0.55)');
    fctx.fillStyle = v;
    fctx.fillRect(0, 0, W, H);
  }

  // Pre-rendered ball sprites — keyed by fill-color + integer radius. Eliminates
  // per-frame radial gradients and `ctx.filter = blur(...)` (the dominant cost).
  const spriteCache = new Map();

  function getSprite(palette, radius) {
    const r = Math.round(radius);
    const key = palette.fill + '_' + r;
    let cached = spriteCache.get(key);
    if (cached) return cached;

    const pad = 6;
    const size = (r + pad) * 2;
    const sc = document.createElement('canvas');
    sc.width = size * DPR;
    sc.height = size * DPR;
    const sx = sc.getContext('2d');
    sx.scale(DPR, DPR);

    const cx = size / 2, cy = size / 2;

    sx.beginPath();
    sx.ellipse(cx + 2, cy + r * 0.55, r * 0.95, r * 0.32, 0, 0, Math.PI * 2);
    sx.fillStyle = 'rgba(0, 0, 0, 0.45)';
    sx.filter = 'blur(4px)';
    sx.fill();
    sx.filter = 'none';

    const hx = cx - r * 0.35;
    const hy = cy - r * 0.4;
    const grad = sx.createRadialGradient(hx, hy, r * 0.05, cx, cy, r);
    grad.addColorStop(0, palette.sheen);
    grad.addColorStop(0.35, palette.fill);
    grad.addColorStop(1, shade(palette.fill, -0.45));
    sx.beginPath();
    sx.arc(cx, cy, r, 0, Math.PI * 2);
    sx.fillStyle = grad;
    sx.fill();

    sx.lineWidth = 0.8;
    sx.strokeStyle = 'rgba(0, 0, 0, 0.6)';
    sx.stroke();

    sx.beginPath();
    sx.ellipse(hx, hy, r * 0.32, r * 0.22, -0.5, 0, Math.PI * 2);
    sx.fillStyle = 'rgba(255, 255, 255, 0.55)';
    sx.filter = 'blur(1.5px)';
    sx.fill();
    sx.filter = 'none';

    cached = { canvas: sc, half: size / 2 };
    spriteCache.set(key, cached);
    return cached;
  }

  function drawBall(b) {
    const s = getSprite(b.color, b.radius);
    ctx.drawImage(s.canvas, b.x - s.half, b.y - s.half, s.half * 2, s.half * 2);
  }

  function drawVelocityVector(x0, y0, x1, y1, options = {}) {
    const dx = x1 - x0;
    const dy = y1 - y0;
    const len = Math.hypot(dx, dy);
    if (len < 4) return;
    const ux = dx / len, uy = dy / len;
    const speed = options.speed ?? len;
    // color shifts green → amber → red
    const t = Math.min(speed / 2200, 1);
    const color = blendColors('#7ed6a8', '#d9b94c', '#df5a3a', t);

    // shaft — translucent halo underlay + crisp top stroke (cheaper than shadowBlur)
    ctx.save();
    ctx.lineCap = 'round';
    ctx.strokeStyle = color;
    ctx.globalAlpha = 0.22;
    ctx.lineWidth = 8;
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1 - ux * 10, y1 - uy * 10);
    ctx.stroke();
    ctx.globalAlpha = 1;
    ctx.lineWidth = 3.2;
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1 - ux * 10, y1 - uy * 10);
    ctx.stroke();

    // arrowhead
    const ah = 11;
    const aw = 7;
    const px = -uy, py = ux;
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x1 - ux * ah + px * aw, y1 - uy * ah + py * aw);
    ctx.lineTo(x1 - ux * ah - px * aw, y1 - uy * ah - py * aw);
    ctx.closePath();
    ctx.fillStyle = color;
    ctx.fill();
    ctx.restore();

    // speed label
    if (options.label) {
      ctx.save();
      ctx.font = '500 11px ui-monospace, "SF Mono", Menlo, monospace';
      ctx.fillStyle = color;
      ctx.textAlign = 'left';
      ctx.textBaseline = 'middle';
      ctx.fillText(options.label, x1 + ux * 12, y1 + uy * 12);
      ctx.restore();
    }
  }

  function drawGravityVector(b) {
    // F = m·g — length scales with mass; drawn from below the ball straight down
    const len = 6 + 7 * b.mass;
    const x0 = b.x;
    const y0 = b.y + b.radius + 2;
    const x1 = x0;
    const y1 = y0 + len;
    const color = '#df6a3a';

    ctx.save();
    ctx.lineCap = 'round';

    // halo
    ctx.strokeStyle = color;
    ctx.globalAlpha = 0.20;
    ctx.lineWidth = 7;
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1, y1 - 9);
    ctx.stroke();

    // shaft
    ctx.globalAlpha = 1;
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1, y1 - 9);
    ctx.stroke();

    // arrowhead
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x1 - 5, y1 - 9);
    ctx.lineTo(x1 + 5, y1 - 9);
    ctx.closePath();
    ctx.fillStyle = color;
    ctx.fill();

    // label
    ctx.font = '500 10px ui-monospace, "SF Mono", Menlo, monospace';
    ctx.fillStyle = color;
    ctx.textAlign = 'left';
    ctx.textBaseline = 'middle';
    ctx.fillText('mg', x1 + 6, y1 - 2);

    ctx.restore();
  }

  function drawPlacementGhost(x, y, mass) {
    const r = radiusFor(mass);
    ctx.save();
    ctx.globalAlpha = 0.55;
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(201,165,92,0.85)';
    ctx.setLineDash([4, 4]);
    ctx.lineWidth = 1.2;
    ctx.stroke();
    ctx.restore();
  }

  function drawPivots() {
    // group pivots by Y so we can draw a connecting beam
    const groups = new Map();
    let any = false;
    for (const b of balls) {
      if (!b.pivot) continue;
      any = true;
      const key = Math.round(b.pivot.y);
      let arr = groups.get(key);
      if (!arr) { arr = []; groups.set(key, arr); }
      arr.push(b.pivot);
    }
    if (!any) return;

    // draw each beam
    for (const [y, list] of groups) {
      if (list.length < 2) continue;
      list.sort((a, b) => a.x - b.x);
      const xMin = list[0].x;
      const xMax = list[list.length - 1].x;
      const overhang = 28;
      const beamH = 8;
      const beamY = y - 14;
      // shadow under beam
      ctx.fillStyle = 'rgba(0, 0, 0, 0.45)';
      ctx.fillRect(xMin - overhang - 1, beamY + 1, (xMax - xMin) + overhang * 2 + 2, beamH + 1);
      // beam itself — dark wood
      const grad = ctx.createLinearGradient(0, beamY, 0, beamY + beamH);
      grad.addColorStop(0, '#2a1f15');
      grad.addColorStop(0.5, '#1a120c');
      grad.addColorStop(1, '#100a07');
      ctx.fillStyle = grad;
      ctx.fillRect(xMin - overhang, beamY, (xMax - xMin) + overhang * 2, beamH);
      // brass cap on each end
      ctx.fillStyle = '#7a6038';
      ctx.fillRect(xMin - overhang, beamY, 6, beamH);
      ctx.fillRect(xMax + overhang - 6, beamY, 6, beamH);
    }

    // strings + anchor pins
    ctx.lineCap = 'round';
    for (const b of balls) {
      if (!b.pivot) continue;
      const p = b.pivot;
      // soft shadow string
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.35)';
      ctx.lineWidth = 1.4;
      ctx.beginPath();
      ctx.moveTo(p.x + 0.5, p.y - 6 + 0.5);
      ctx.lineTo(b.x + 0.5, b.y + 0.5);
      ctx.stroke();
      // string
      ctx.strokeStyle = 'rgba(190, 175, 145, 0.55)';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(p.x, p.y - 6);
      ctx.lineTo(b.x, b.y);
      ctx.stroke();
      // anchor pin
      ctx.fillStyle = '#1a120c';
      ctx.beginPath();
      ctx.arc(p.x, p.y - 6, 2.6, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = '#c9a55c';
      ctx.beginPath();
      ctx.arc(p.x, p.y - 6, 1.2, 0, Math.PI * 2);
      ctx.fill();
    }
  }

  function render() {
    // background felt
    if (feltCanvas) ctx.drawImage(feltCanvas, 0, 0);

    // pendulum hardware (under balls)
    drawPivots();

    // permanent vector overlay
    if (showVectors) {
      for (const b of balls) {
        const sp = Math.hypot(b.vx, b.vy);
        if (sp >= 5) {
          const k = 0.18;
          drawVelocityVector(b.x, b.y, b.x + b.vx * k, b.y + b.vy * k);
        }
        if (b.pivot) drawGravityVector(b);
      }
    }

    // balls
    for (const b of balls) drawBall(b);

    // active drag indicator
    if (drag.active && drag.ball) {
      const x0 = drag.ball.x;
      const y0 = drag.ball.y;
      const dx = drag.x - x0;
      const dy = drag.y - y0;
      const len = Math.hypot(dx, dy);
      const speed = Math.min(len * DRAG_TO_SPEED, MAX_SPEED);
      // visualize at the velocity scale used for moving-ball arrows
      const vlen = speed * 0.18;
      const ux = len > 0 ? dx / len : 0;
      const uy = len > 0 ? dy / len : 0;
      drawVelocityVector(x0, y0, x0 + ux * vlen, y0 + uy * vlen, {
        speed,
        label: `${(speed / 100).toFixed(2)} u/s`,
      });

      // aim guide — long thin line
      ctx.save();
      ctx.strokeStyle = 'rgba(201,165,92,0.18)';
      ctx.setLineDash([3, 6]);
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(x0 + ux * drag.ball.radius, y0 + uy * drag.ball.radius);
      ctx.lineTo(x0 + ux * 2000, y0 + uy * 2000);
      ctx.stroke();
      ctx.restore();
    }

    // placement preview
    if (hover.placing && !pickBall(hover.x, hover.y)) {
      drawPlacementGhost(hover.x, hover.y, currentMass);
    }
  }

  // ---------- interaction ----------

  const DRAG_TO_SPEED = 6; // pixel-distance → velocity multiplier

  const drag = { active: false, ball: null, x: 0, y: 0 };
  const hover = { x: -1e6, y: -1e6, placing: true };

  let currentMass = 2;
  let restitution = 0.97;
  let frictionOn = true;
  let showVectors = false;
  let ecoMode = false;
  let paletteIndex = 0;
  let dirty = true;
  let cursorOnCanvas = false;

  function pickBall(x, y) {
    for (let i = balls.length - 1; i >= 0; i--) {
      const b = balls[i];
      const dx = x - b.x, dy = y - b.y;
      if (dx * dx + dy * dy <= b.radius * b.radius) return b;
    }
    return null;
  }

  function overlapsAny(x, y, r) {
    for (const b of balls) {
      const dx = x - b.x, dy = y - b.y;
      const m = b.radius + r;
      if (dx * dx + dy * dy < m * m) return true;
    }
    return false;
  }

  function placeBall(x, y, mass) {
    const r = radiusFor(mass);
    if (x - r < 0 || x + r > W || y - r < 0 || y + r > H) return false;
    if (overlapsAny(x, y, r)) return false;
    const palette = PALETTE[paletteIndex % PALETTE.length];
    paletteIndex++;
    balls.push(new Ball(x, y, mass, palette));
    fadeHint();
    return true;
  }

  function evtPos(e) {
    const t = e.touches ? e.touches[0] : e;
    return { x: t.clientX, y: t.clientY };
  }

  canvas.addEventListener('mousedown', onDown);
  canvas.addEventListener('mousemove', onMove);
  window.addEventListener('mouseup', onUp);
  canvas.addEventListener('contextmenu', (e) => e.preventDefault());

  // touch
  canvas.addEventListener('touchstart', (e) => { e.preventDefault(); onDown(e); }, { passive: false });
  canvas.addEventListener('touchmove',  (e) => { e.preventDefault(); onMove(e); }, { passive: false });
  canvas.addEventListener('touchend',   (e) => { e.preventDefault(); onUp(e); },   { passive: false });

  function onDown(e) {
    cursorOnCanvas = true;
    const { x, y } = evtPos(e);
    const hit = pickBall(x, y);
    if (e.shiftKey && hit) {
      const idx = balls.indexOf(hit);
      if (idx >= 0) balls.splice(idx, 1);
      dirty = true;
      return;
    }
    if (hit) {
      drag.active = true;
      drag.ball = hit;
      drag.x = x;
      drag.y = y;
    } else {
      placeBall(x, y, currentMass);
    }
    dirty = true;
  }

  function onMove(e) {
    cursorOnCanvas = true;
    const { x, y } = evtPos(e);
    hover.x = x;
    hover.y = y;
    if (drag.active) {
      drag.x = x;
      drag.y = y;
    }
    dirty = true;
  }

  function onUp(e) {
    dirty = true;
    if (!drag.active || !drag.ball) { drag.active = false; drag.ball = null; return; }
    const b = drag.ball;
    const dx = drag.x - b.x;
    const dy = drag.y - b.y;
    const len = Math.hypot(dx, dy);
    if (len > 4) {
      let speed = len * DRAG_TO_SPEED;
      if (speed > MAX_SPEED) speed = MAX_SPEED;
      b.vx = (dx / len) * speed;
      b.vy = (dy / len) * speed;
    }
    drag.active = false;
    drag.ball = null;
  }

  canvas.addEventListener('mouseleave', () => { cursorOnCanvas = false; dirty = true; });
  canvas.addEventListener('mouseenter', () => { cursorOnCanvas = true; dirty = true; });

  // ---------- HUD wiring ----------

  const massEl = document.getElementById('mass');
  const massValEl = document.getElementById('mass-val');
  const restEl = document.getElementById('restitution');
  const restValEl = document.getElementById('restitution-val');
  const fricEl = document.getElementById('friction');
  const vectEl = document.getElementById('vectors');
  const ecoEl = document.getElementById('eco');
  const clearBtn = document.getElementById('clear');
  const layoutsBtn = document.getElementById('layouts-btn');
  const layoutMenu = document.getElementById('layout-menu');
  const hintEl = document.getElementById('hint');

  const statBalls = document.getElementById('stat-balls');
  const statKE = document.getElementById('stat-ke');
  const statP = document.getElementById('stat-p');

  massEl.addEventListener('input', () => {
    currentMass = parseFloat(massEl.value);
    massValEl.textContent = currentMass.toFixed(1);
    dirty = true;
  });
  restEl.addEventListener('input', () => {
    restitution = parseFloat(restEl.value);
    restValEl.textContent = restitution.toFixed(2);
    dirty = true;
  });
  fricEl.addEventListener('change', () => { frictionOn = fricEl.checked; dirty = true; });
  vectEl.addEventListener('change', () => { showVectors = vectEl.checked; dirty = true; });
  ecoEl.addEventListener('change', () => { ecoMode = ecoEl.checked; dirty = true; });

  clearBtn.addEventListener('click', () => { balls.length = 0; dirty = true; });

  // ---------- layouts ----------

  function clearAll() { balls.length = 0; paletteIndex = 0; }

  function addBall(x, y, mass, palIdx) {
    const p = PALETTE[(palIdx ?? paletteIndex++) % PALETTE.length];
    balls.push(new Ball(x, y, mass, p));
    return balls[balls.length - 1];
  }

  // tries to fit `mass`-ball at (x,y); skips if it'd overlap or clip a wall
  function tryAdd(x, y, mass, palIdx) {
    const r = radiusFor(mass);
    if (x - r < 4 || x + r > W - 4 || y - r < 4 || y + r > H - 4) return null;
    if (overlapsAny(x, y, r * 1.001)) return null;
    return addBall(x, y, mass, palIdx);
  }

  const LAYOUTS = {
    rack() {
      clearAll();
      const apexX = W * 0.62, apexY = H / 2;
      const r = radiusFor(2);
      const sp = r * 2 + 0.6;
      for (let row = 0; row < 5; row++) {
        for (let col = 0; col <= row; col++) {
          const x = apexX + row * sp * Math.cos(Math.PI / 6);
          const y = apexY + (col - row / 2) * sp;
          tryAdd(x, y, 2);
        }
      }
      tryAdd(W * 0.22, apexY, 2, 8); // cue ball
    },

    grid() {
      clearAll();
      const r = radiusFor(2);
      const sp = r * 2.6;
      const cols = Math.min(10, Math.floor((W - 80) / sp));
      const rows = Math.min(7, Math.floor((H - 220) / sp));
      const totalW = (cols - 1) * sp;
      const totalH = (rows - 1) * sp;
      const x0 = (W - totalW) / 2;
      const y0 = (H - totalH) / 2;
      for (let r2 = 0; r2 < rows; r2++) {
        for (let c = 0; c < cols; c++) {
          tryAdd(x0 + c * sp, y0 + r2 * sp, 2);
        }
      }
    },

    line() {
      clearAll();
      const r = radiusFor(2);
      const cy = H / 2;
      const sp = r * 2 + 0.4;
      const n = Math.min(11, Math.floor((W * 0.55) / sp));
      const totalW = (n - 1) * sp;
      const x0 = (W - totalW) / 2 + W * 0.1;
      for (let i = 0; i < n; i++) tryAdd(x0 + i * sp, cy, 2);
      const cue = tryAdd(W * 0.12, cy, 2, 8);
      if (cue) { cue.vx = 1100; }
    },

    wall() {
      clearAll();
      const r = radiusFor(3);
      const sp = r * 2 + 0.5;
      const yBottom = H - r - 18;
      const yTop = r + 90;
      const n = Math.floor((W - 40) / sp);
      const x0 = (W - (n - 1) * sp) / 2;
      for (let i = 0; i < n; i++) {
        tryAdd(x0 + i * sp, yBottom, 3);
        tryAdd(x0 + i * sp, yTop, 3);
      }
      // a heavy cue ball in the middle, falling
      const c = tryAdd(W / 2, H / 2, 6, 8);
      if (c) { c.vx = 600; c.vy = 200; }
    },

    cluster() {
      clearAll();
      const r = radiusFor(2);
      const sp = r * 2 + 0.4;
      const cx = W / 2, cy = H / 2;
      // hex-packed diamond
      const rows = [4, 5, 6, 7, 6, 5, 4];
      const sin60 = Math.sin(Math.PI / 3);
      const startY = cy - (rows.length - 1) * sp * sin60 / 2;
      for (let i = 0; i < rows.length; i++) {
        const count = rows[i];
        const startX = cx - (count - 1) * sp / 2;
        for (let j = 0; j < count; j++) {
          tryAdd(startX + j * sp, startY + i * sp * sin60, 2);
        }
      }
    },

    swarm() {
      clearAll();
      const r = radiusFor(1);
      const target = Math.min(80, Math.floor((W * H) / 22000));
      const margin = r * 2 + 8;
      let attempts = 0;
      while (balls.length < target && attempts < 8000) {
        attempts++;
        const x = margin + Math.random() * (W - margin * 2);
        const y = margin + Math.random() * (H - margin * 2);
        tryAdd(x, y, 1);
      }
      // give them random initial velocities
      for (const b of balls) {
        const a = Math.random() * Math.PI * 2;
        const sp = 250 + Math.random() * 550;
        b.vx = Math.cos(a) * sp;
        b.vy = Math.sin(a) * sp;
      }
    },

    giants() {
      clearAll();
      const cy = H / 2;
      tryAdd(W * 0.30, cy - 110, 10);
      tryAdd(W * 0.30, cy + 110, 10);
      tryAdd(W * 0.55, cy,        9);
      tryAdd(W * 0.78, cy - 90,   8);
      tryAdd(W * 0.78, cy + 90,   8);
      const c = tryAdd(W * 0.10, cy, 1, 8);
      if (c) { c.vx = 1600; }
    },

    rings() {
      clearAll();
      const cx = W / 2, cy = H / 2;
      const r2 = radiusFor(2);
      tryAdd(cx, cy, 3);
      const ring = (radius) => {
        const minStep = (r2 * 2 + 4);
        const n = Math.max(6, Math.floor(2 * Math.PI * radius / minStep));
        for (let i = 0; i < n; i++) {
          const a = (i / n) * Math.PI * 2;
          tryAdd(cx + Math.cos(a) * radius, cy + Math.sin(a) * radius, 2);
        }
      };
      ring(80);
      if (Math.min(W, H) > 380) ring(160);
      if (Math.min(W, H) > 560) ring(240);
      if (Math.min(W, H) > 740) ring(320);
    },

    double() {
      clearAll();
      const buildTri = (apexX, apexY, dirX) => {
        const r = radiusFor(2);
        const sp = r * 2 + 0.6;
        for (let row = 0; row < 4; row++) {
          for (let col = 0; col <= row; col++) {
            const x = apexX + dirX * row * sp * Math.cos(Math.PI / 6);
            const y = apexY + (col - row / 2) * sp;
            tryAdd(x, y, 2);
          }
        }
      };
      buildTri(W * 0.32, H / 2,  1);
      buildTri(W * 0.68, H / 2, -1);
      const cue = tryAdd(W / 2, H * 0.22, 2, 8);
      if (cue) { cue.vy = 700; }
    },

    cross() {
      clearAll();
      const cx = W / 2, cy = H / 2;
      const r = radiusFor(2);
      const sp = r * 2 + 1;
      const reach = Math.min(5, Math.floor((Math.min(W, H) / 2 - 40) / sp));
      for (let i = -reach; i <= reach; i++) tryAdd(cx + i * sp, cy, 2);
      for (let i = -reach; i <= reach; i++) {
        if (i === 0) continue;
        tryAdd(cx, cy + i * sp, 2);
      }
    },

    funnel() {
      clearAll();
      const r = radiusFor(3);
      const sp = r * 2 + 1.2;
      const cx = W / 2, cy = H / 2;
      const arms = 7;
      const angle = Math.PI / 5; // 36°
      for (let i = 1; i <= arms; i++) {
        const dx = Math.cos(angle) * sp * i;
        const dy = Math.sin(angle) * sp * i;
        tryAdd(cx - dx, cy - dy, 3);
        tryAdd(cx - dx, cy + dy, 3);
        tryAdd(cx + dx, cy - dy, 3);
        tryAdd(cx + dx, cy + dy, 3);
      }
      const cue = tryAdd(W * 0.08, cy, 2, 8);
      if (cue) { cue.vx = 1500; }
    },

    cradle() {
      clearAll();
      const N = 5;
      const mass = 3;
      const r = radiusFor(mass);
      const length = Math.min(240, H * 0.42);
      const ceilY = Math.max(70, H * 0.18);
      const restY = ceilY + length;
      const sp = r * 2 + 0.4; // tiny gap so adjacent balls aren't perpetually overlapping
      const totalW = (N - 1) * sp;
      const x0 = (W - totalW) / 2;
      // four right-side balls hang at rest
      for (let i = 1; i < N; i++) {
        const ax = x0 + i * sp;
        const b = addBall(ax, restY, mass);
        b.pivot = { x: ax, y: ceilY, length };
      }
      // leftmost ball pulled aside ~40°
      const angle = Math.PI * 0.22;
      const ax0 = x0;
      const b0 = addBall(
        ax0 - Math.sin(angle) * length,
        ceilY + Math.cos(angle) * length,
        mass
      );
      b0.pivot = { x: ax0, y: ceilY, length };
    },

    pendulum() {
      clearAll();
      // a heavy wrecking-ball pendulum about to demolish a small rack
      const length = Math.min(300, H * 0.5);
      const ceilY = Math.max(70, H * 0.16);
      const ax = W * 0.30;
      const angle = Math.PI * 0.32;
      const pend = addBall(
        ax - Math.sin(angle) * length,
        ceilY + Math.cos(angle) * length,
        6
      );
      pend.pivot = { x: ax, y: ceilY, length };

      // 15-ball triangle in the pendulum's swing path
      const r = radiusFor(1);
      const sp = r * 2 + 0.4;
      const swingY = ceilY + length;
      const apexX = ax + radiusFor(6) + 110;
      for (let row = 0; row < 5; row++) {
        for (let col = 0; col <= row; col++) {
          const x = apexX + row * sp * Math.cos(Math.PI / 6);
          const y = swingY + (col - row / 2) * sp;
          tryAdd(x, y, 1);
        }
      }
    },

    chaos() {
      clearAll();
      // mixed masses scattered, all moving at random
      const target = 35;
      let attempts = 0;
      while (balls.length < target && attempts < 6000) {
        attempts++;
        const m = [1, 1, 1, 2, 2, 2, 3, 4, 6][Math.floor(Math.random() * 9)];
        const r = radiusFor(m);
        const x = r + 6 + Math.random() * (W - r * 2 - 12);
        const y = r + 6 + Math.random() * (H - r * 2 - 12);
        tryAdd(x, y, m);
      }
      for (const b of balls) {
        const a = Math.random() * Math.PI * 2;
        const sp = 200 + Math.random() * 800;
        b.vx = Math.cos(a) * sp;
        b.vy = Math.sin(a) * sp;
      }
    },
  };

  function applyLayout(name) {
    const fn = LAYOUTS[name];
    if (!fn) return;
    fn();
    fadeHint();
    dirty = true;
  }

  layoutsBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    const open = layoutMenu.hasAttribute('hidden');
    if (open) {
      layoutMenu.removeAttribute('hidden');
      layoutsBtn.setAttribute('aria-expanded', 'true');
    } else {
      layoutMenu.setAttribute('hidden', '');
      layoutsBtn.setAttribute('aria-expanded', 'false');
    }
  });

  layoutMenu.addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-layout]');
    if (!btn) return;
    applyLayout(btn.dataset.layout);
    layoutMenu.setAttribute('hidden', '');
    layoutsBtn.setAttribute('aria-expanded', 'false');
  });

  document.addEventListener('click', (e) => {
    if (layoutMenu.hasAttribute('hidden')) return;
    if (layoutsBtn.contains(e.target) || layoutMenu.contains(e.target)) return;
    layoutMenu.setAttribute('hidden', '');
    layoutsBtn.setAttribute('aria-expanded', 'false');
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !layoutMenu.hasAttribute('hidden')) {
      layoutMenu.setAttribute('hidden', '');
      layoutsBtn.setAttribute('aria-expanded', 'false');
    }
  });

  function fadeHint() {
    if (!hintEl.classList.contains('faded')) {
      hintEl.classList.add('faded');
    }
  }

  // ---------- main loop ----------

  let last = performance.now();
  let frameCount = 0;
  function frame(now) {
    const dt = Math.min((now - last) / 1000, 1 / 30); // clamp
    last = now;
    frameCount++;

    step(dt, restitution);

    let anyMoving = false;
    for (const b of balls) {
      if (b.vx !== 0 || b.vy !== 0) { anyMoving = true; break; }
    }
    if (anyMoving) dirty = true;

    const interactionDirty = drag.active || cursorOnCanvas;
    const ecoSkip = ecoMode && (frameCount & 1);

    if ((dirty || interactionDirty) && !ecoSkip) {
      render();
      dirty = false;
    }

    updateStats();
    requestAnimationFrame(frame);
  }

  function updateStats() {
    let ke = 0, px = 0, py = 0;
    for (const b of balls) {
      const sp2 = b.vx * b.vx + b.vy * b.vy;
      ke += 0.5 * b.mass * sp2;
      px += b.mass * b.vx;
      py += b.mass * b.vy;
    }
    // rescale display to friendlier units
    statBalls.textContent = balls.length.toString().padStart(2, '0');
    statKE.textContent = (ke / 100000).toFixed(2);
    statP.textContent  = (Math.hypot(px, py) / 1000).toFixed(2);
  }

  // ---------- helpers ----------

  function shade(hex, amount) {
    // amount in [-1, 1]; negative darkens, positive lightens
    const c = hex.replace('#', '');
    const r = parseInt(c.substring(0, 2), 16);
    const g = parseInt(c.substring(2, 4), 16);
    const b = parseInt(c.substring(4, 6), 16);
    const f = (v) => Math.max(0, Math.min(255,
      amount >= 0 ? v + (255 - v) * amount : v + v * amount));
    return `rgb(${f(r)|0}, ${f(g)|0}, ${f(b)|0})`;
  }

  function hexToRgb(hex) {
    const c = hex.replace('#', '');
    return [
      parseInt(c.substring(0, 2), 16),
      parseInt(c.substring(2, 4), 16),
      parseInt(c.substring(4, 6), 16),
    ];
  }
  function blendColors(a, b, c, t) {
    // piecewise through three colors: t in [0,1]
    const A = hexToRgb(a), B = hexToRgb(b), C = hexToRgb(c);
    let p, q, k;
    if (t < 0.5) { p = A; q = B; k = t * 2; }
    else         { p = B; q = C; k = (t - 0.5) * 2; }
    const r = Math.round(p[0] + (q[0] - p[0]) * k);
    const g = Math.round(p[1] + (q[1] - p[1]) * k);
    const bl = Math.round(p[2] + (q[2] - p[2]) * k);
    return `rgb(${r}, ${g}, ${bl})`;
  }

  // ---------- boot ----------

  resize();
  // start with a small demo so the table isn't empty
  (function seed() {
    const cx = W / 2, cy = H / 2;
    if (W < 400 || H < 400) return;
    balls.push(new Ball(cx - 180, cy, 2, PALETTE[8]));
    balls.push(new Ball(cx + 60, cy - 30, 2, PALETTE[0]));
    balls.push(new Ball(cx + 60, cy + 30, 2, PALETTE[2]));
    balls.push(new Ball(cx + 120, cy, 2, PALETTE[5]));
  })();

  requestAnimationFrame(frame);
})();
