'use strict';
/*
 * Covers: a tiny zero-dependency Node server.
 * Serves the static frontend from ./public and a small JSON REST API
 * backed by an atomically-written JSON file on disk. Single-user auth
 * via a shared password (APP_PASSWORD) exchanged for a bearer token.
 */
const http = require('http');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const PORT = process.env.PORT || 8080;
const PASSWORD = process.env.APP_PASSWORD || 'covers';
const DATA_DIR = process.env.DATA_DIR || path.join(__dirname, 'data');
const DB_FILE = path.join(DATA_DIR, 'db.json');
const PUBLIC_DIR = path.join(__dirname, 'public');

if (!process.env.APP_PASSWORD) {
  console.warn('[covers] WARNING: APP_PASSWORD not set; using the default "covers". Set APP_PASSWORD in production.');
}

/* ---------- store ---------- */
fs.mkdirSync(DATA_DIR, { recursive: true });
let db = { entries: [], sessions: [] };
try {
  db = JSON.parse(fs.readFileSync(DB_FILE, 'utf8'));
  if (!Array.isArray(db.entries)) db.entries = [];
  if (!Array.isArray(db.sessions)) db.sessions = [];
} catch { /* fresh db */ }

let writeChain = Promise.resolve();
function persist() {
  // serialize writes; write to a temp file then rename for atomicity
  writeChain = writeChain.then(() => new Promise((resolve) => {
    const tmp = DB_FILE + '.tmp';
    fs.writeFile(tmp, JSON.stringify(db), (err) => {
      if (err) { console.error('[covers] write error', err); return resolve(); }
      fs.rename(tmp, DB_FILE, (err2) => {
        if (err2) console.error('[covers] rename error', err2);
        resolve();
      });
    });
  }));
  return writeChain;
}

/* ---------- helpers ---------- */
function send(res, status, obj, headers) {
  const body = obj == null ? '' : JSON.stringify(obj);
  res.writeHead(status, Object.assign({ 'Content-Type': 'application/json' }, headers || {}));
  res.end(body);
}
function readBody(req) {
  return new Promise((resolve, reject) => {
    let data = '', size = 0;
    req.on('data', (c) => {
      size += c.length;
      if (size > 5_000_000) { reject(new Error('payload too large')); req.destroy(); return; }
      data += c;
    });
    req.on('end', () => { try { resolve(data ? JSON.parse(data) : {}); } catch (e) { reject(new Error('invalid JSON')); } });
    req.on('error', reject);
  });
}
function safeEqual(a, b) {
  const ab = Buffer.from(String(a)), bb = Buffer.from(String(b));
  if (ab.length !== bb.length) return false;
  return crypto.timingSafeEqual(ab, bb);
}
function tokenOf(req) {
  const m = /^Bearer (.+)$/.exec(req.headers['authorization'] || '');
  return m ? m[1] : null;
}
function authed(req) {
  const t = tokenOf(req);
  return !!t && db.sessions.includes(t);
}
function genId() { return Date.now().toString(36) + crypto.randomBytes(3).toString('hex'); }
function num(v) { const n = Number(v); return isFinite(n) ? n : 0; }

function sanitize(b) {
  if (!b || typeof b !== 'object') return null;
  const name = String(b.name || '').trim();
  const date = String(b.date || '').trim();
  if (!name || !date) return null;
  const e = {
    name,
    location: b.location ? String(b.location).trim() : '',
    date,
    meal: b.meal ? String(b.meal).slice(0, 16) : '',
    rating: num(b.rating) || 0,
    amount: num(b.amount) || 0,
    currency: b.currency ? String(b.currency).slice(0, 4) : '$',
  };
  if (Array.isArray(b.items)) {
    e.items = b.items
      .map((it) => {
        const o = { name: String((it && it.name) || '').trim() };
        if (it && it.price != null && isFinite(Number(it.price))) o.price = Number(it.price);
        return o;
      })
      .filter((it) => it.name || it.price != null);
  } else if (b.food) {
    e.food = String(b.food);
  }
  if (Array.isArray(b.tags)) {
    const tags = [...new Set(b.tags.map((t) => String(t).trim()).filter(Boolean).map((t) => t.slice(0, 40)))].slice(0, 30);
    if (tags.length) e.tags = tags;
  }
  const notes = b.notes != null ? String(b.notes).trim().slice(0, 4000) : '';
  if (notes) e.notes = notes;
  if (b.lat != null && b.lng != null && isFinite(Number(b.lat)) && isFinite(Number(b.lng))) {
    e.lat = Number(b.lat);
    e.lng = Number(b.lng);
  }
  return e;
}

/* ---------- static ---------- */
const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.ico': 'image/x-icon',
  '.png': 'image/png', '.webmanifest': 'application/manifest+json',
};
function serveStatic(req, res) {
  let p = decodeURIComponent((req.url || '/').split('?')[0]);
  if (p === '/') p = '/index.html';
  const filePath = path.normalize(path.join(PUBLIC_DIR, p));
  if (!filePath.startsWith(PUBLIC_DIR)) { res.writeHead(403); return res.end('Forbidden'); }
  fs.readFile(filePath, (err, content) => {
    if (err) { res.writeHead(404); return res.end('Not found'); }
    res.writeHead(200, { 'Content-Type': MIME[path.extname(filePath)] || 'application/octet-stream' });
    res.end(content);
  });
}

/* ---------- routes ---------- */
const server = http.createServer(async (req, res) => {
  const url = (req.url || '/').split('?')[0];

  if (url === '/api/health') return send(res, 200, { ok: true });

  if (url.startsWith('/api/')) {
    try {
      // login is public
      if (url === '/api/login' && req.method === 'POST') {
        const { password } = await readBody(req);
        if (typeof password === 'string' && safeEqual(password, PASSWORD)) {
          const token = crypto.randomBytes(24).toString('hex');
          db.sessions.push(token);
          await persist();
          return send(res, 200, { token });
        }
        return send(res, 401, { error: 'Invalid password' });
      }

      // public read: anyone can view the log (no auth required)
      if (url === '/api/entries' && req.method === 'GET') {
        return send(res, 200, db.entries);
      }

      // everything below needs a valid token (owner only)
      if (!authed(req)) return send(res, 401, { error: 'Unauthorized' });

      if (url === '/api/session' && req.method === 'GET') {
        return send(res, 200, { ok: true });
      }

      if (url === '/api/logout' && req.method === 'POST') {
        const t = tokenOf(req);
        db.sessions = db.sessions.filter((x) => x !== t);
        await persist();
        return send(res, 204, null);
      }

      if (url === '/api/entries' && req.method === 'POST') {
        const e = sanitize(await readBody(req));
        if (!e) return send(res, 400, { error: 'name and date are required' });
        e.id = genId(); e.createdAt = Date.now(); e.updatedAt = e.createdAt;
        db.entries.push(e);
        await persist();
        return send(res, 201, e);
      }

      if (url === '/api/entries' && req.method === 'DELETE') {
        db.entries = [];
        await persist();
        return send(res, 204, null);
      }

      if (url === '/api/import' && req.method === 'POST') {
        const body = await readBody(req);
        const arr = Array.isArray(body.entries) ? body.entries : [];
        let added = 0;
        for (const raw of arr) {
          const e = sanitize(raw);
          if (e) { e.id = genId(); e.createdAt = Date.now(); e.updatedAt = e.createdAt; db.entries.push(e); added++; }
        }
        await persist();
        return send(res, 200, { added, entries: db.entries });
      }

      const m = /^\/api\/entries\/([\w-]+)$/.exec(url);
      if (m) {
        const id = m[1];
        const idx = db.entries.findIndex((e) => e.id === id);
        if (idx < 0) return send(res, 404, { error: 'Not found' });
        if (req.method === 'PUT') {
          const e = sanitize(await readBody(req));
          if (!e) return send(res, 400, { error: 'name and date are required' });
          e.id = id;
          e.createdAt = db.entries[idx].createdAt || Date.now();
          e.updatedAt = Date.now();
          db.entries[idx] = e;
          await persist();
          return send(res, 200, e);
        }
        if (req.method === 'DELETE') {
          db.entries.splice(idx, 1);
          await persist();
          return send(res, 204, null);
        }
      }

      return send(res, 404, { error: 'Not found' });
    } catch (err) {
      return send(res, 400, { error: String((err && err.message) || err) });
    }
  }

  if (req.method === 'GET') return serveStatic(req, res);
  res.writeHead(405); res.end('Method not allowed');
});

server.listen(PORT, () => console.log(`[covers] listening on :${PORT}`));
