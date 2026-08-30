'use strict';

/* ── config ─────────────────────────────────────────── */

const SAVE_KEY = 'descent.v1';
const HULL_CAPS  = [200, 1000, 4000, 6000, 11000, Infinity];
const HULL_COSTS = [150, 2500, 30000, 250000, 2200000];
const OFFLINE_CAP = 8 * 3600; // seconds of offline progress honoured
const NAMED_ZONES = ZONES.slice(0, 6);
const BY_DEPTH = [...CREATURES].sort((a, b) => a.depth - b.depth);
const WARP = Number(new URLSearchParams(location.search).get('warp')) || 1;

/* ── state ──────────────────────────────────────────── */

const DEFAULTS = () => ({
  t: Date.now(),
  depth: 0,
  runMax: 0,
  allMax: 0,
  research: 0,
  totalResearch: 0,
  photos: 0,
  pings: 0,
  contactsDone: 0,
  resurfaces: 0,
  grants: 0,        // spendable balance (✦)
  grantsEarned: 0,  // lifetime total, small intrinsic bonus
  up: { ballast: 0, lights: 0, sonar: 0, drones: 0, hydro: 0, hull: 0 },
  dock: { funding: 0, pilots: 0, biologist: 0, sensors: 0, keel: 0, refit: 0 },
  disc: [],
  wrecks: [],
  relics: [],
  miles: [],
  muted: false,
  seenIntro: false,
});

let S = DEFAULTS();
const discovered = new Set();
const wrecksSeen = new Set();
const relics = new Set();
const miles = new Set();

function load() {
  try {
    const raw = localStorage.getItem(SAVE_KEY);
    if (!raw) return;
    const d = JSON.parse(raw);
    S = {
      ...DEFAULTS(), ...d,
      up: { ...DEFAULTS().up, ...(d.up || {}) },
      dock: { ...DEFAULTS().dock, ...(d.dock || {}) },
    };
    for (const k of ['depth', 'runMax', 'allMax', 'research', 'totalResearch', 'photos',
                     'pings', 'contactsDone', 'resurfaces', 'grants', 'grantsEarned'])
      if (!Number.isFinite(S[k])) S[k] = 0;
    // v1 saves predate the dry dock: earned grants become spendable balance
    if (d.dock === undefined && d.grants) S.grantsEarned = d.grants;
    (S.disc || []).forEach(id => discovered.add(id));
    (S.wrecks || []).forEach(id => wrecksSeen.add(id));
    (S.relics || []).forEach(id => relics.add(id));
    (S.miles || []).forEach(id => miles.add(id));
  } catch (e) { /* corrupted save: start fresh */ }
}

let wiped = false;
function save() {
  if (wiped) return;
  S.t = Date.now();
  S.disc = [...discovered];
  S.wrecks = [...wrecksSeen];
  S.relics = [...relics];
  S.miles = [...miles];
  try { localStorage.setItem(SAVE_KEY, JSON.stringify(S)); } catch (e) {}
}

/* ── economy ────────────────────────────────────────── */

// summed permanent effects from salvaged relics + expedition records
function eff(key) {
  let s = 0;
  for (const w of WRECKS) if (relics.has(w.id) && w.eff[key]) s += w.eff[key];
  for (const m of MILESTONES) if (miles.has(m.id) && m.eff[key]) s += m.eff[key];
  return s;
}

const researchMult = () => (1 + 0.02 * S.grantsEarned) * (1 + 0.10 * S.dock.funding) * (1 + eff('research'));
const descentRate  = () => 1.2 * Math.pow(1.4, S.up.ballast) * (1 + 0.08 * S.dock.pilots) * (1 + eff('descent')) * WARP;
const hullCap      = () => HULL_CAPS[S.up.hull];
const depthFactor  = d => 1 + Math.pow(d / 200, 0.9);
const passiveRate  = () => (0.15 * Math.pow(1.35, S.up.lights) + 0.25 * S.up.lights) * depthFactor(S.depth) * researchMult();
const pingValue    = () => (1.5 + S.depth / 150) * Math.pow(1.45, S.up.sonar) * (1 + eff('ping')) * researchMult();
const attractChance = () => Math.min(0.6, 0.18 + 0.05 * S.up.sonar);
const photoValue   = (c, rare) => c.value * (1 + 0.15 * S.up.sonar) * (1 + 0.15 * S.dock.biologist)
  * (1 + eff('photo')) * researchMult() * (rare ? 6 : 1);
const droneInterval = () => Math.max(2.5, 14 * Math.pow(0.82, S.up.drones) * (1 - eff('drone')));
const rareChance   = () => 0.05 + 0.02 * S.dock.sensors;
const contactEvery = () => Math.max(8, 24 * Math.pow(0.9, S.up.hydro) * (1 - eff('contact')));

function pendingGrants() {
  if (S.runMax < 1000) return 0;
  const base = Math.floor(Math.pow(S.runMax / 1000, 1.35) * (1 + 0.05 * discovered.size));
  return base >= 1 ? base + eff('grantBonus') : 0;
}

function gainResearch(n) {
  S.research += n;
  S.totalResearch += n;
}

const zoneIndex = d => {
  let i = 0;
  while (i + 1 < NAMED_ZONES.length && d >= NAMED_ZONES[i + 1].depth) i++;
  return i;
};
const zoneOf = d => NAMED_ZONES[zoneIndex(d)];

/* ── formatting ─────────────────────────────────────── */

const SUFFIX = ['', 'K', 'M', 'B', 'T', 'Qa', 'Qi'];
function fmt(n) {
  if (!Number.isFinite(n)) return '∞';
  if (n < 1000) return n < 10 && n % 1 !== 0 ? n.toFixed(1) : String(Math.floor(n));
  let tier = Math.min(SUFFIX.length - 1, Math.floor(Math.log10(n) / 3));
  const v = n / Math.pow(10, tier * 3);
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + SUFFIX[tier];
}
const fmtDepth = d => Math.floor(d).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
function fmtDur(s) {
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`;
}

/* ── dom refs ───────────────────────────────────────── */

const $ = id => document.getElementById(id);
const ocean = $('ocean'), rays = $('rays'), creatureLayer = $('creatures'), pingLayer = $('pings');
const depthEl = $('depth'), zoneEl = $('zone'), statusEl = $('status');
const resAmt = $('res-amt'), resRate = $('res-rate');
const upgradesEl = $('upgrades'), toastsEl = $('toasts');
const modal = $('modal'), modalBody = $('modal-body');
const btnLog = $('btn-log'), btnDock = $('btn-dock'), btnSurface = $('btn-surface'), btnSound = $('btn-sound');

/* ── audio ──────────────────────────────────────────── */

let AC = null;
function ac() {
  if (!AC) AC = new (window.AudioContext || window.webkitAudioContext)();
  if (AC.state === 'suspended') AC.resume();
  return AC;
}
function tone(freq0, freq1, dur, vol = 0.06, type = 'sine', when = 0) {
  if (S.muted) return;
  try {
    const ctx = ac(), t = ctx.currentTime + when;
    const o = ctx.createOscillator(), g = ctx.createGain();
    o.type = type;
    o.frequency.setValueAtTime(freq0, t);
    o.frequency.exponentialRampToValueAtTime(Math.max(1, freq1), t + dur);
    g.gain.setValueAtTime(vol, t);
    g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    o.connect(g).connect(ctx.destination);
    o.start(t); o.stop(t + dur + 0.05);
  } catch (e) {}
}
const sfxPing   = () => tone(1200, 320, 0.5, 0.045);
const sfxPhoto  = () => { tone(640, 900, 0.08, 0.05, 'triangle'); tone(220, 180, 0.1, 0.03, 'square'); };
const sfxFound  = () => { tone(520, 520, 0.25, 0.05); tone(784, 784, 0.35, 0.05, 'sine', 0.14); };
const sfxRecord = () => { tone(660, 660, 0.18, 0.05); tone(880, 880, 0.26, 0.05, 'sine', 0.12); };
const sfxContact = () => tone(160, 340, 0.35, 0.04);

/* ── toasts ─────────────────────────────────────────── */

function toast(head, lore, { minor = false, ttl = 6 } = {}) {
  while (toastsEl.children.length >= 3) toastsEl.firstChild.remove();
  const el = document.createElement('div');
  el.className = 'toast' + (minor ? ' minor' : '');
  el.style.setProperty('--ttl', ttl + 's');
  el.innerHTML = `<div class="t-head"></div>${lore ? '<div class="t-lore"></div>' : ''}`;
  el.querySelector('.t-head').textContent = head;
  if (lore) el.querySelector('.t-lore').textContent = lore;
  toastsEl.appendChild(el);
  setTimeout(() => el.remove(), (ttl + 0.8) * 1000);
}

/* ── discoveries & wrecks ───────────────────────────── */

function checkDiscoveries({ quiet = false } = {}) {
  const fresh = BY_DEPTH.filter(c => !discovered.has(c.id) && c.depth <= S.depth);
  if (!fresh.length) return { count: 0, bonus: 0 };
  let bonus = 0;
  for (const c of fresh) {
    discovered.add(c.id);
    bonus += c.value * 10 * researchMult();
  }
  gainResearch(bonus);
  if (!quiet) {
    if (fresh.length === 1) {
      const c = fresh[0];
      toast(`new specimen · ${c.name} — ${fmtDepth(c.depth)} m`, c.lore, { ttl: 8 });
      sfxFound();
      spawnCreature(c);
    } else {
      toast(`${fresh.length} specimens logged on the way down · +${fmt(bonus)} ◇`, null, { ttl: 7 });
      sfxFound();
    }
  }
  renderPanel();
  return { count: fresh.length, bonus };
}

function checkWrecks({ quiet = false } = {}) {
  let n = 0;
  for (const w of WRECKS) {
    if (wrecksSeen.has(w.id) || w.depth > S.depth) continue;
    wrecksSeen.add(w.id);
    n++;
    if (!quiet) {
      toast(`wreck on sonar · ${w.name} — ${fmtDepth(w.depth)} m · salvage from the log`, w.lore, { ttl: 9 });
      sfxContact();
    }
  }
  return n;
}

const MHELP = {
  zoneDone: i => BY_DEPTH.filter(c => zoneIndex(c.depth) === i).every(c => discovered.has(c.id)),
};

function checkMilestones() {
  for (const m of MILESTONES) {
    if (miles.has(m.id)) continue;
    let ok = false;
    try { ok = m.cond(S, MHELP); } catch (e) {}
    if (ok) {
      miles.add(m.id);
      toast(`expedition record · ${m.name} — ${m.effText}`, null, { ttl: 6 });
      sfxRecord();
    }
  }
}

/* ── creatures ──────────────────────────────────────── */

let spawnTimer = 6; // first visitor arrives quickly

function unlockedPool() {
  return BY_DEPTH.filter(c => c.depth <= S.runMax || c.depth <= S.depth);
}

function pickCreature() {
  const pool = unlockedPool();
  if (!pool.length) return null;
  const zi = zoneIndex(S.depth);
  const local = pool.filter(c => zoneIndex(c.depth) === zi);
  if (local.length && Math.random() < 0.7) return local[(Math.random() * local.length) | 0];
  return pool[(Math.random() * pool.length) | 0];
}

function spawnCreature(c = pickCreature(), { burst = false, forceRare = false } = {}) {
  if (!c || creatureLayer.querySelectorAll('.creature').length >= (burst ? 9 : 5)) return;
  const rare = forceRare || Math.random() < rareChance();
  const el = document.createElement('div');
  el.className = 'creature' + (rare ? ' rare' : '');
  const w = c.size * 2, h = c.size * 1.2;
  const goingRight = Math.random() < 0.5;
  if (!goingRight) el.classList.add('flip');
  const y = 12 + Math.random() * 66; // vh
  const dur = 14 + Math.random() * 10;
  el.style.setProperty('--x0', goingRight ? `-${w + 40}px` : `calc(100vw + 40px)`);
  el.style.setProperty('--x1', goingRight ? `calc(100vw + 40px)` : `-${w + 40}px`);
  el.style.setProperty('--y', y + 'vh');
  el.style.animationDuration = dur + 's';
  el.style.color = rare ? '#ffe9a8' : NAMED_ZONES[zoneIndex(c.depth)].glow;
  el.innerHTML = `<svg width="${w}" height="${h}" viewBox="0 0 100 60">${ARCHETYPES[c.shape] || ARCHETYPES.fish}</svg>`;
  el.querySelector('svg').style.animationDuration = (2 + Math.random() * 2) + 's';
  el.addEventListener('animationend', e => { if (e.animationName === 'swim') el.remove(); });
  el.addEventListener('pointerdown', e => {
    e.stopPropagation();
    photograph(c, el, e.clientX, e.clientY, rare);
  }, { once: true });
  creatureLayer.appendChild(el);
}

function photograph(c, el, x, y, rare) {
  const v = photoValue(c, rare);
  gainResearch(v);
  S.photos++;
  sfxPhoto();
  if (rare) sfxFound();
  const svg = el.querySelector('svg');
  if (svg) svg.style.animationDuration = '0.9s';
  el.classList.add('snapped');
  setTimeout(() => el.remove(), 950);
  floatVal(`+${fmt(v)} ◇ ${rare ? 'RARE ' : ''}${c.name}`, x, y);
}

function floatVal(text, x, y) {
  const el = document.createElement('div');
  el.className = 'float-val';
  el.textContent = text;
  el.style.left = Math.min(x, innerWidth - 160) + 'px';
  el.style.top = y + 'px';
  ocean.appendChild(el);
  setTimeout(() => el.remove(), 1700);
}

/* ── sonar contacts ─────────────────────────────────── */

let contactTimer = 14;

function spawnContact() {
  if (creatureLayer.querySelector('.contact')) return; // one at a time
  const el = document.createElement('div');
  el.className = 'contact';
  el.style.left = (8 + Math.random() * 62) + 'vw';
  el.style.top = (18 + Math.random() * 55) + 'vh';
  el.innerHTML = `<div class="c-ring"></div><div class="c-dot"></div><div class="c-tag">contact</div>`;
  const expire = setTimeout(() => {
    el.classList.add('gone');
    setTimeout(() => el.remove(), 700);
  }, 15000);
  el.addEventListener('pointerdown', e => {
    e.stopPropagation();
    clearTimeout(expire);
    el.remove();
    resolveContact();
  }, { once: true });
  creatureLayer.appendChild(el);
  sfxContact();
}

function resolveContact() {
  S.contactsDone++;
  const roll = Math.random();
  if (roll < 0.40) {
    const n = 3 + ((Math.random() * 3) | 0);
    for (let i = 0; i < n; i++) setTimeout(() => spawnCreature(pickCreature(), { burst: true }), i * 650);
    toast(`biological contact · ${n} signatures closing`, null, { minor: true, ttl: 4 });
    sfxFound();
  } else if (roll < 0.66) {
    const v = Math.max(passiveRate() * (60 + Math.random() * 60), pingValue() * 8);
    gainResearch(v);
    toast(`data cache recovered · +${fmt(v)} ◇`, null, { minor: true, ttl: 4 });
    sfxRecord();
  } else if (roll < 0.82) {
    spawnCreature(pickCreature(), { burst: true, forceRare: true });
    toast('strong return · something rare is close', null, { minor: true, ttl: 5 });
    sfxFound();
  } else if (roll < 0.90) {
    const v = Math.max(passiveRate() * 300, pingValue() * 30);
    gainResearch(v);
    toast(`debris field · instruments everywhere · +${fmt(v)} ◇`, null, { ttl: 5 });
    sfxRecord();
  } else {
    toast('thermocline echo · nothing there', null, { minor: true, ttl: 3 });
  }
}

/* ── sonar ping ─────────────────────────────────────── */

let lastPing = 0;
ocean.addEventListener('pointerdown', e => {
  const now = performance.now();
  if (now - lastPing < 200 || S.surfacing) return;
  lastPing = now;
  const ring = document.createElement('div');
  ring.className = 'ping';
  ring.style.left = e.clientX + 'px';
  ring.style.top = e.clientY + 'px';
  pingLayer.appendChild(ring);
  setTimeout(() => ring.remove(), 1200);
  sfxPing();
  S.pings++;
  const v = pingValue();
  gainResearch(v);
  floatVal(`+${fmt(v)} ◇`, e.clientX + 14, e.clientY - 10);
  if (Math.random() < attractChance()) spawnCreature();
});

/* ── upgrades panel ─────────────────────────────────── */

const UPGRADES = [
  {
    key: 'ballast', name: 'Ballast Trim',
    cost: () => 25 * Math.pow(1.75, S.up.ballast),
    desc: () => `descent ${(descentRate() / WARP).toFixed(1)} → ${(descentRate() / WARP * 1.4).toFixed(1)} m/s`,
  },
  {
    key: 'lights', name: 'Floodlights',
    cost: () => 20 * Math.pow(1.9, S.up.lights),
    desc: () => `passive research +${fmt(passiveRate())}/s now`,
  },
  {
    key: 'sonar', name: 'Sonar Array',
    cost: () => 45 * Math.pow(2.2, S.up.sonar),
    desc: () => `stronger pings & photos · attracts life`,
  },
  {
    key: 'hydro', name: 'Hydrophone Array',
    cost: () => 120 * Math.pow(2.3, S.up.hydro),
    desc: () => `contacts surface every ~${Math.round(contactEvery() + 8)} s`,
  },
  {
    key: 'drones', name: 'Camera Drones',
    cost: () => 400 * Math.pow(2.6, S.up.drones),
    desc: () => S.up.drones
      ? `auto-photograph every ${droneInterval().toFixed(0)} s`
      : `deploy drones that photograph for you`,
  },
  {
    key: 'hull', name: 'Pressure Hull',
    cost: () => HULL_COSTS[S.up.hull] ?? Infinity,
    desc: () => S.up.hull >= HULL_COSTS.length
      ? 'anomalous alloy · no rated limit'
      : `rated ${fmtDepth(hullCap())} m → ${HULL_CAPS[S.up.hull + 1] === Infinity ? '∞' : fmtDepth(HULL_CAPS[S.up.hull + 1]) + ' m'}`,
    maxed: () => S.up.hull >= HULL_COSTS.length,
  },
];

const rows = new Map();

function buildPanel() {
  upgradesEl.innerHTML = '';
  for (const u of UPGRADES) {
    const btn = document.createElement('button');
    btn.className = 'up';
    btn.innerHTML = `<span class="up-name"></span><span class="up-desc"></span><span class="up-cost"></span>`;
    btn.addEventListener('click', () => buy(u));
    upgradesEl.appendChild(btn);
    rows.set(u.key, {
      btn,
      name: btn.querySelector('.up-name'),
      desc: btn.querySelector('.up-desc'),
      cost: btn.querySelector('.up-cost'),
    });
  }
  renderPanel();
}

function buy(u) {
  if (u.maxed && u.maxed()) return;
  const c = u.cost();
  if (S.research < c) return;
  S.research -= c;
  S.up[u.key]++;
  tone(340, 480, 0.12, 0.05, 'triangle');
  renderPanel();
  save();
}

function renderPanel() {
  for (const u of UPGRADES) {
    const r = rows.get(u.key);
    if (!r) continue;
    const maxed = u.maxed && u.maxed();
    const lvl = S.up[u.key];
    r.name.innerHTML = `${u.name}<em>${maxed ? 'max' : 'L' + lvl}</em>`;
    r.desc.textContent = u.desc();
    r.cost.textContent = maxed ? '—' : fmt(u.cost()) + ' ◇';
    r.btn.classList.toggle('maxed', !!maxed);
    r.btn.disabled = maxed || S.research < u.cost();
    r.btn.classList.toggle('urgent',
      u.key === 'hull' && !maxed && S.depth >= hullCap() - 1 && S.research >= u.cost());
  }
}

/* ── background gradient ────────────────────────────── */

const lerp = (a, b, t) => a + (b - a) * t;
const mix = (c1, c2, t) => c1.map((v, i) => Math.round(lerp(v, c2[i], t)));

function paintOcean() {
  const d = S.depth;
  let i = 0;
  while (i + 1 < ZONES.length && d >= ZONES[i + 1].depth) i++;
  let top = ZONES[i].top, bottom = ZONES[i].bottom;
  if (i + 1 < ZONES.length) {
    const t = (d - ZONES[i].depth) / (ZONES[i + 1].depth - ZONES[i].depth);
    top = mix(ZONES[i].top, ZONES[i + 1].top, t);
    bottom = mix(ZONES[i].bottom, ZONES[i + 1].bottom, t);
  }
  ocean.style.background = `linear-gradient(to bottom, rgb(${top}), rgb(${bottom}))`;
  rays.style.opacity = Math.max(0, 1 - d / 280);
}

/* ── marine snow ────────────────────────────────────── */

const snow = $('snow'), sctx = snow.getContext('2d');
let flakes = [];

function resizeSnow() {
  snow.width = innerWidth * devicePixelRatio;
  snow.height = innerHeight * devicePixelRatio;
  sctx.scale(devicePixelRatio, devicePixelRatio);
  const n = Math.min(130, Math.floor(innerWidth * innerHeight / 14000));
  flakes = Array.from({ length: n }, () => ({
    x: Math.random() * innerWidth,
    y: Math.random() * innerHeight,
    r: 0.5 + Math.random() * 1.7,
    v: 0.3 + Math.random() * 0.8,
    drift: (Math.random() - 0.5) * 0.3,
  }));
}

function drawSnow(dt) {
  sctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
  sctx.clearRect(0, 0, innerWidth, innerHeight);
  const sinking = S.depth < hullCap() && !S.surfacing;
  const flow = sinking ? -Math.min(140, 18 + descentRate() * 2.2) : 8;
  sctx.fillStyle = 'rgba(225, 245, 250, 0.5)';
  for (const f of flakes) {
    f.y += flow * f.v * dt;
    f.x += f.drift;
    if (f.y < -4) { f.y = innerHeight + 4; f.x = Math.random() * innerWidth; }
    if (f.y > innerHeight + 4) { f.y = -4; f.x = Math.random() * innerWidth; }
    if (f.x < -4) f.x = innerWidth + 4;
    if (f.x > innerWidth + 4) f.x = -4;
    sctx.globalAlpha = 0.15 + f.r / 4;
    sctx.beginPath();
    sctx.arc(f.x, f.y, f.r, 0, 7);
    sctx.fill();
  }
  sctx.globalAlpha = 1;
}

/* ── simulation ─────────────────────────────────────── */

function advance(dt) {
  // chunked so the depth factor tracks the moving depth on big jumps
  const steps = dt > 30 ? 24 : 1;
  const h = dt / steps;
  let gained = 0;
  for (let i = 0; i < steps; i++) {
    S.depth = Math.min(S.depth + descentRate() * h, hullCap());
    const g = passiveRate() * h;
    gained += g;
    gainResearch(g);
  }
  S.runMax = Math.max(S.runMax, S.depth);
  S.allMax = Math.max(S.allMax, S.depth);
  return gained;
}

let droneTimer = 0;
function tickDrones(dt) {
  if (!S.up.drones) return;
  droneTimer += dt;
  const iv = droneInterval();
  while (droneTimer >= iv) {
    droneTimer -= iv;
    const c = pickCreature();
    if (!c) break;
    const v = photoValue(c, false) * 0.6;
    gainResearch(v);
    S.photos++;
    floatVal(`+${fmt(v)} ◇ drone`, innerWidth - 180, 90 + Math.random() * 40);
  }
}

/* ── main loop ──────────────────────────────────────── */

let last = performance.now();
let uiTimer = 0, saveTimer = 0, titleTimer = 0;

function frame(now) {
  requestAnimationFrame(frame);
  let dt = (now - last) / 1000;
  last = now;
  if (dt <= 0) return;
  if (S.surfacing) { drawSnow(Math.min(dt, 0.1)); return; }

  if (dt > 60) {
    // tab slept a long while: silent catch-up, then batched discovery toast
    advance(Math.min(dt, OFFLINE_CAP));
    checkDiscoveries();
    checkWrecks();
    paintOcean();
  } else {
    dt = Math.min(dt, 2);
    advance(dt);
    checkDiscoveries();
    checkWrecks();
    tickDrones(dt);
    spawnTimer -= dt;
    if (spawnTimer <= 0) {
      spawnCreature();
      spawnTimer = (8 + Math.random() * 8) * Math.pow(0.96, S.up.sonar);
    }
    contactTimer -= dt;
    if (contactTimer <= 0) {
      spawnContact();
      contactTimer = contactEvery() + Math.random() * 16;
    }
  }

  drawSnow(Math.min(dt, 0.1));

  uiTimer += dt;
  if (uiTimer >= 0.15) { uiTimer = 0; renderHUD(); paintOcean(); checkMilestones(); }
  saveTimer += dt;
  if (saveTimer >= 5) { saveTimer = 0; save(); }
  titleTimer += dt;
  if (titleTimer >= 2) { titleTimer = 0; document.title = `−${fmtDepth(S.depth)} m · DESCENT`; }
}

function renderHUD() {
  depthEl.innerHTML = `${fmtDepth(S.depth)}<span class="unit">m</span>`;
  zoneEl.textContent = zoneOf(S.depth).name;
  zoneEl.style.color = zoneOf(S.depth).glow;
  const atCap = S.depth >= hullCap() - 0.5;
  statusEl.textContent = atCap
    ? `hull at limit · rated ${fmtDepth(hullCap())} m`
    : `descending · ${(descentRate() / WARP).toFixed(1)} m/s`;
  statusEl.classList.toggle('limit', atCap);
  resAmt.textContent = fmt(S.research);
  resRate.textContent = `+${fmt(passiveRate())}/s`;
  const pg = pendingGrants();
  btnSurface.disabled = pg < 1;
  btnSurface.classList.toggle('ready', pg >= 1);
  btnSurface.textContent = pg >= 1 ? `resurface +${pg} ✦` : 'resurface';
  btnDock.textContent = S.grants > 0 ? `dry dock · ${S.grants} ✦` : 'dry dock';
  // cheap affordability refresh
  for (const u of UPGRADES) {
    const r = rows.get(u.key);
    if (!r) continue;
    const maxed = u.maxed && u.maxed();
    r.btn.disabled = maxed || S.research < u.cost();
    if (u.key === 'hull') r.btn.classList.toggle('urgent', !maxed && atCap && S.research >= u.cost());
  }
}

/* ── modals ─────────────────────────────────────────── */

function openModal(html) {
  modalBody.innerHTML = html;
  modal.classList.remove('hidden');
}
function closeModal() { modal.classList.add('hidden'); }
$('modal-close').addEventListener('click', closeModal);
modal.addEventListener('pointerdown', e => { if (e.target === modal) closeModal(); });

/* ── specimen log (tabs: specimens · salvage · records) */

let logTab = 'specimens';

function renderLog() {
  const tabs = ['specimens', 'salvage', 'records'];
  let html = `<div class="m-title">expedition log</div>
    <div class="m-tabs">${tabs.map(t =>
      `<button class="m-tab${t === logTab ? ' active' : ''}" data-tab="${t}">${t}</button>`).join('')}</div>`;

  if (logTab === 'specimens') {
    html += `<div class="m-sub">${discovered.size} of ${CREATURES.length} lifeforms identified. Sightings persist across expeditions.</div>`;
    const groups = NAMED_ZONES.map((z, i) => ({
      zone: z, i,
      creatures: BY_DEPTH.filter(c => zoneIndex(c.depth) === i),
    })).filter(g => g.creatures.length);
    for (const g of groups) {
      const seen = g.creatures.filter(c => discovered.has(c.id)).length;
      const upper = NAMED_ZONES[g.i + 1] ? fmtDepth(NAMED_ZONES[g.i + 1].depth) + ' m' : '???';
      html += `<div class="zone-head">${g.zone.name} · ${fmtDepth(g.zone.depth)}–${upper} · ${seen}/${g.creatures.length}</div><div class="log-grid">`;
      for (const c of g.creatures) {
        const known = discovered.has(c.id);
        html += known
          ? `<div class="log-cell" style="color:${g.zone.glow}">
              <svg viewBox="0 0 100 60">${ARCHETYPES[c.shape] || ARCHETYPES.fish}</svg>
              <div class="c-name">${c.name}</div>
              <div class="c-depth">${fmtDepth(c.depth)} m</div>
              <div class="c-lore">${c.lore}</div>
            </div>`
          : `<div class="log-cell unknown" style="color:${g.zone.glow}">
              <svg viewBox="0 0 100 60">${ARCHETYPES[c.shape] || ARCHETYPES.fish}</svg>
              <div class="c-name">?????</div>
              <div class="c-depth">descend past ${fmtDepth(c.depth)} m</div>
            </div>`;
      }
      html += `</div>`;
    }
  }

  if (logTab === 'salvage') {
    html += `<div class="m-sub">Wrecks appear on sonar as you pass them. Salvage costs research; each relic is a permanent effect, kept through every resurface.</div>`;
    for (const w of WRECKS) {
      const seen = wrecksSeen.has(w.id), got = relics.has(w.id);
      if (!seen) {
        html += `<div class="wreck unknown"><div class="w-main">
          <div class="c-name">?????</div>
          <div class="c-depth">something rests below ${fmtDepth(w.depth)} m</div>
        </div></div>`;
      } else {
        html += `<div class="wreck${got ? ' got' : ''}"><div class="w-main">
          <div class="c-name">${w.name}</div>
          <div class="c-depth">${fmtDepth(w.depth)} m · ${w.effText}</div>
          <div class="c-lore">${w.lore}</div>
        </div>
        ${got
          ? `<span class="w-got">recovered</span>`
          : `<button class="w-buy" data-wreck="${w.id}" ${S.research < w.cost ? 'disabled' : ''}>salvage · ${fmt(w.cost)} ◇</button>`}
        </div>`;
      }
    }
  }

  if (logTab === 'records') {
    html += `<div class="m-sub">${miles.size} of ${MILESTONES.length} expedition records set. Each is a permanent bonus.</div><div class="miles">`;
    for (const m of MILESTONES) {
      const got = miles.has(m.id);
      html += `<div class="mile${got ? '' : ' unknown'}">
        <span class="mi-name">${m.name}</span>
        <span class="mi-desc">${m.desc}</span>
        <span class="mi-eff">${got ? m.effText : '·'}</span>
      </div>`;
    }
    html += `</div>
    <div class="m-stats">
      <div>deepest <b>${fmtDepth(S.allMax)} m</b></div>
      <div>photographs <b>${fmt(S.photos)}</b></div>
      <div>pings <b>${fmt(S.pings)}</b></div>
      <div>contacts <b>${fmt(S.contactsDone)}</b></div>
      <div>expeditions <b>${S.resurfaces + 1}</b></div>
      <div>grants earned <b>${S.grantsEarned} ✦</b></div>
      <div>research mult <b>×${researchMult().toFixed(2)}</b></div>
    </div>`;
  }

  openModal(html);
  modalBody.querySelectorAll('.m-tab').forEach(b =>
    b.addEventListener('click', () => { logTab = b.dataset.tab; renderLog(); }));
  modalBody.querySelectorAll('.w-buy').forEach(b =>
    b.addEventListener('click', () => {
      const w = WRECKS.find(x => x.id === b.dataset.wreck);
      if (!w || relics.has(w.id) || S.research < w.cost) return;
      S.research -= w.cost;
      relics.add(w.id);
      toast(`salvaged · ${w.name} — ${w.effText}`, null, { ttl: 7 });
      sfxRecord();
      save();
      renderLog();
      renderHUD();
    }));
}

btnLog.addEventListener('click', renderLog);

/* ── dry dock ───────────────────────────────────────── */

function renderDock() {
  let html = `<div class="m-title">dry dock</div>
    <div class="m-sub">Permanent refits between expeditions, paid in grants. Earn grants by resurfacing — deeper dives and fuller logs pay better.</div>
    <div class="dock-balance">✦ ${S.grants} <span>grants available</span></div>
    <div class="dock-items">`;
  for (const d of DOCK) {
    const lvl = S.dock[d.key];
    const maxed = lvl >= d.max;
    const cost = maxed ? null : d.cost(lvl);
    html += `<div class="dock-item">
      <div class="w-main">
        <div class="c-name">${d.name}<em class="dk-lvl">${maxed ? 'max' : 'L' + lvl}</em></div>
        <div class="c-lore">${d.desc}</div>
      </div>
      ${maxed
        ? `<span class="w-got">—</span>`
        : `<button class="w-buy" data-dock="${d.key}" ${S.grants < cost ? 'disabled' : ''}>${cost} ✦</button>`}
    </div>`;
  }
  html += `</div>`;
  openModal(html);
  modalBody.querySelectorAll('.w-buy').forEach(b =>
    b.addEventListener('click', () => {
      const d = DOCK.find(x => x.key === b.dataset.dock);
      const lvl = S.dock[d.key];
      if (lvl >= d.max || S.grants < d.cost(lvl)) return;
      S.grants -= d.cost(lvl);
      S.dock[d.key]++;
      // keel & refit apply immediately if they beat current fit
      if (d.key === 'keel' && S.up.hull < S.dock.keel) S.up.hull = S.dock.keel;
      if (d.key === 'refit') {
        if (S.up.lights < S.dock.refit) S.up.lights = S.dock.refit;
        if (S.up.sonar < S.dock.refit) S.up.sonar = S.dock.refit;
      }
      tone(340, 520, 0.15, 0.05, 'triangle');
      save();
      renderDock();
      renderPanel();
      renderHUD();
    }));
}

btnDock.addEventListener('click', renderDock);

/* ── resurface (prestige) ───────────────────────────── */

btnSurface.addEventListener('click', () => {
  const pg = pendingGrants();
  if (pg < 1) return;
  openModal(`
    <div class="m-title">resurface</div>
    <div class="m-lore">End the expedition. The vessel is refitted from the keel up; the specimen log, salvage, records and dry dock survive the journey home.</div>
    <div class="m-figures">
      <div class="m-fig"><span>deepest point this expedition</span><b>${fmtDepth(S.runMax)} m</b></div>
      <div class="m-fig"><span>expedition grants awarded</span><b class="gain">+${pg} ✦</b></div>
      <div class="m-fig"><span>spendable in the dry dock</span><b class="gain">${S.grants + pg} ✦</b></div>
      <div class="m-fig"><span>depth, research & upgrades</span><b>reset</b></div>
    </div>
    <div class="m-actions">
      <button class="primary" id="do-surface">blow ballast</button>
      <button id="no-surface">keep descending</button>
    </div>`);
  $('do-surface').addEventListener('click', () => doResurface(pg));
  $('no-surface').addEventListener('click', closeModal);
});

function doResurface(pg) {
  closeModal();
  S.surfacing = true;
  ocean.classList.add('surfacing');
  tone(180, 720, 1.6, 0.05);
  const from = S.depth, t0 = performance.now();
  (function rise(now) {
    const t = Math.min(1, (now - t0) / 2600);
    S.depth = from * (1 - Math.pow(t, 0.6));
    depthEl.innerHTML = `${fmtDepth(S.depth)}<span class="unit">m</span>`;
    statusEl.textContent = 'surfacing';
    statusEl.classList.remove('limit');
    paintOcean();
    if (t < 1) return requestAnimationFrame(rise);
    S.grants += pg;
    S.grantsEarned += pg;
    S.resurfaces++;
    S.depth = 0; S.runMax = 0; S.research = 0;
    S.up = { ...DEFAULTS().up, hull: S.dock.keel, lights: S.dock.refit, sonar: S.dock.refit };
    droneTimer = 0; spawnTimer = 6; contactTimer = 14;
    S.surfacing = false;
    creatureLayer.innerHTML = '';
    ocean.classList.remove('surfacing');
    toast(`expedition complete · +${pg} ✦ — the dry dock is open`, 'The crane lifts the vessel out of the water. It is already being rebuilt.', { ttl: 8 });
    renderPanel(); renderHUD(); paintOcean(); save();
  })(t0);
}

/* ── sound toggle ───────────────────────────────────── */

function renderSound() { btnSound.textContent = S.muted ? '·' : '∿'; }
btnSound.addEventListener('click', () => {
  S.muted = !S.muted;
  renderSound();
  if (!S.muted) sfxPing();
  save();
});

/* ── boot ───────────────────────────────────────────── */

load();

// offline progress
{
  const away = Math.min((Date.now() - S.t) / 1000, OFFLINE_CAP);
  if (away > 90) {
    const d0 = S.depth;
    const gained = advance(away);
    const disc = checkDiscoveries({ quiet: true });
    const wrecksFound = checkWrecks({ quiet: true });
    openModal(`
      <div class="m-title">while you were under</div>
      <div class="m-lore">The vessel kept its course. The instruments kept their counsel.</div>
      <div class="m-figures">
        <div class="m-fig"><span>time submerged</span><b>${fmtDur(away)}</b></div>
        <div class="m-fig"><span>depth gained</span><b>+${fmtDepth(S.depth - d0)} m</b></div>
        <div class="m-fig"><span>research collected</span><b class="gain">+${fmt(gained + disc.bonus)} ◇</b></div>
        ${disc.count ? `<div class="m-fig"><span>new specimens logged</span><b class="gain">${disc.count}</b></div>` : ''}
        ${wrecksFound ? `<div class="m-fig"><span>wrecks on sonar</span><b class="gain">${wrecksFound}</b></div>` : ''}
      </div>
      <div class="m-actions"><button class="primary" id="resume">resume descent</button></div>`);
    $('resume').addEventListener('click', closeModal);
  } else {
    advance(Math.max(0, away));
    checkDiscoveries({ quiet: true });
    checkWrecks({ quiet: true });
  }
}

buildPanel();
renderSound();
resizeSnow();
paintOcean();
renderHUD();
addEventListener('resize', resizeSnow);
addEventListener('visibilitychange', () => { if (document.hidden) save(); });
addEventListener('beforeunload', save);
addEventListener('pointerdown', () => { if (!S.muted) ac(); }, { once: true });

if (!S.seenIntro) {
  S.seenIntro = true;
  setTimeout(() => toast('autonomous survey vessel away · click the water to ping', 'Everything below two hundred metres is unlit, unmapped, and mostly unnamed. Log what you find.', { ttl: 10 }), 800);
}

requestAnimationFrame(frame);

// debug handle
window.DESCENT = {
  S,
  reset: () => { wiped = true; localStorage.removeItem(SAVE_KEY); location.reload(); },
};
