'use strict';

const $ = (sel, el) => (el || document).querySelector(sel);

const state = {
  profiles: [],
  queue: [],
  queueByMovie: new Map(),
  movies: [],
  gridLabel: '',
  current: null,      // movie shown in the sheet
  releases: null,
  relFilter: 'all',
  selectedProfile: null,
  view: 'search',
};

// ---------- api ----------

async function api(path, opts) {
  const resp = await fetch(path, opts);
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(body.error || `${resp.status} ${resp.statusText}`);
  return body;
}

const post = (path, data) => api(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(data),
});

// ---------- formatting ----------

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function fmtSize(bytes) {
  if (!bytes) return '';
  const gb = bytes / 2 ** 30;
  if (gb >= 1) return gb.toFixed(1) + ' GB';
  return Math.round(bytes / 2 ** 20) + ' MB';
}

function fmtAge(days) {
  if (days < 1) return 'today';
  if (days < 60) return days + 'd';
  if (days < 730) return Math.round(days / 30) + 'mo';
  return Math.round(days / 365) + 'y';
}

function fmtRuntime(mins) {
  if (!mins) return '';
  return Math.floor(mins / 60) + 'h ' + (mins % 60) + 'm';
}

// Radarr timeleft: "hh:mm:ss" or "d.hh:mm:ss"
function fmtTimeleft(t) {
  if (!t) return '';
  let days = 0, rest = t;
  if (t.includes('.')) [days, rest] = [parseInt(t, 10), t.split('.')[1]];
  const [h, m] = rest.split(':').map(n => parseInt(n, 10));
  if (days > 0) return `${days}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function humanState(item) {
  if (item.state === 'importPending') return 'Import Pending';
  if (item.state === 'importBlocked') return 'Import Blocked';
  if (item.state === 'importing') return 'Importing';
  const s = item.status || '';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// poster <img> with graceful fallback to a text block
function posterHTML(m, cls) {
  if (!m.poster) return `<div class="no-art">${esc(m.title)}</div>`;
  return `<img src="${esc(m.poster)}" alt="" loading="lazy" data-t="${esc(m.title)}" onerror="pfail(this)">`;
}
window.pfail = img => {
  const d = document.createElement('div');
  d.className = 'no-art';
  d.textContent = img.dataset.t || '';
  img.replaceWith(d);
};

// ---------- views ----------

function setView(view) {
  state.view = view;
  $('#view-search').hidden = view !== 'search';
  $('#view-downloads').hidden = view !== 'downloads';
  if (view === 'search') {
    $('#nav-search').setAttribute('aria-current', 'page');
    $('#nav-downloads').removeAttribute('aria-current');
  } else {
    $('#nav-downloads').setAttribute('aria-current', 'page');
    $('#nav-search').removeAttribute('aria-current');
    renderQueue();
  }
}

function movieState(m) {
  const id = m.id || 0;
  if (id && state.queueByMovie.has(id)) return 'Downloading';
  if (m.hasFile) return 'Downloaded';
  if (id) return 'Added';
  return '';
}

function renderGrid() {
  $('#grid-label').textContent = state.gridLabel;
  const grid = $('#grid');
  if (!state.movies.length) {
    grid.innerHTML = `<div class="empty">0 results.</div>`;
    return;
  }
  grid.innerHTML = state.movies.map((m, i) => {
    const st = movieState(m);
    return `
    <div class="movie-card" data-i="${i}">
      <div class="poster">${posterHTML(m)}</div>
      <div class="m-title">${esc(m.title)}</div>
      <div class="m-meta">${m.year || ''}${st ? `<span class="chip">${st}</span>` : ''}</div>
    </div>`;
  }).join('');
  grid.querySelectorAll('.movie-card').forEach(card => {
    card.addEventListener('click', () => openSheet(state.movies[+card.dataset.i]));
  });
}

// ---------- search ----------

let searchTimer = null;
let searchSeq = 0;

async function loadRecent() {
  const seq = ++searchSeq;
  try {
    const movies = await api('/api/recent');
    if (seq !== searchSeq) return;
    state.movies = movies;
    state.gridLabel = 'Recently Added';
    renderGrid();
  } catch (e) {
    $('#grid').innerHTML = `<div class="error-line">${esc(e.message)}</div>`;
  }
}

async function doSearch(q) {
  const seq = ++searchSeq;
  try {
    const movies = await api('/api/search?q=' + encodeURIComponent(q));
    if (seq !== searchSeq) return;
    state.movies = movies;
    state.gridLabel = `${movies.length} result${movies.length === 1 ? '' : 's'}`;
    renderGrid();
  } catch (e) {
    if (seq !== searchSeq) return;
    $('#grid').innerHTML = `<div class="error-line">${esc(e.message)}</div>`;
  }
}

$('#q').addEventListener('input', () => {
  clearTimeout(searchTimer);
  const q = $('#q').value.trim();
  searchTimer = setTimeout(() => {
    if (q.length < 2) loadRecent();
    else doSearch(q);
  }, 350);
});

// ---------- detail sheet ----------

function openSheet(m) {
  state.current = m;
  state.releases = null;
  state.relFilter = 'all';
  const preferred = state.profiles.find(p => p.name === 'HD-1080p') || state.profiles[0];
  state.selectedProfile = m.qualityProfileId || (preferred ? preferred.id : 0);
  renderSheet();
  $('#scrim').hidden = false;
  document.body.style.overflow = 'hidden';
}

function closeSheet() {
  $('#scrim').hidden = true;
  state.current = null;
  document.body.style.overflow = '';
}

$('#scrim').addEventListener('click', e => { if (e.target.id === 'scrim') closeSheet(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape' && !$('#scrim').hidden) closeSheet(); });

function renderSheet() {
  const m = state.current;
  if (!m) return;
  const inLibrary = (m.id || 0) > 0;
  const qItem = inLibrary ? state.queueByMovie.get(m.id) : null;

  const meta = [
    m.year,
    fmtRuntime(m.runtime),
    m.certification,
    m.status,
  ].filter(Boolean).join(' · ');

  const chips = [];
  if (m.hasFile) chips.push(`Downloaded${m.fileQuality ? ' · ' + esc(m.fileQuality) : ''}`);
  if (qItem) chips.push('Downloading');
  if (inLibrary && !m.hasFile && !qItem) chips.push('In Library');

  let actions;
  if (!inLibrary) {
    actions = `
      <div class="action-block">
        <label class="section-label" style="margin:0 0 2px">Quality Profile</label>
        <div class="seg" id="profile-seg">
          ${state.profiles.map(p => `<button class="btn${p.id === state.selectedProfile ? ' selected' : ''}" data-pid="${p.id}">${esc(p.name)}</button>`).join('')}
        </div>
        <div class="btn-row">
          <button class="btn primary" id="btn-add-auto">Add &amp; Auto-Grab</button>
          <button class="btn" id="btn-add-pick">Add &amp; Pick Release</button>
        </div>
        <div class="hint">Auto-grab lets Radarr take the best release for the profile. Pick shows every release so you choose.</div>
        <div class="status-line" id="sheet-status"></div>
      </div>`;
  } else {
    actions = `
      <div class="action-block">
        <div class="btn-row">
          <button class="btn primary" id="btn-releases">Find Releases</button>
          <button class="btn" id="btn-autosearch">Auto Search</button>
        </div>
        <div class="status-line" id="sheet-status">${qItem ? esc(queueLine(qItem)) : ''}</div>
      </div>`;
  }

  $('#sheet').innerHTML = `
    <div class="sheet-close-row"><button class="btn" id="btn-close">Close</button></div>
    <div class="sheet-head">
      <div class="poster">${posterHTML(m)}</div>
      <div class="sheet-info">
        <h2>${esc(m.title)}</h2>
        <div class="meta-line">${esc(meta)}</div>
        ${chips.length ? `<div class="chips">${chips.map(c => `<span class="chip">${c}</span>`).join('')}</div>` : ''}
        <div class="overview">${esc(m.overview)}</div>
      </div>
    </div>
    ${actions}
    <div class="rel-block" id="rel-block"></div>`;

  $('#btn-close').addEventListener('click', closeSheet);

  if (!inLibrary) {
    $('#profile-seg').querySelectorAll('.btn').forEach(b => {
      b.addEventListener('click', () => {
        state.selectedProfile = +b.dataset.pid;
        $('#profile-seg').querySelectorAll('.btn').forEach(x => x.classList.toggle('selected', x === b));
      });
    });
    $('#btn-add-auto').addEventListener('click', () => addMovie(true));
    $('#btn-add-pick').addEventListener('click', () => addMovie(false));
  } else {
    $('#btn-releases').addEventListener('click', findReleases);
    $('#btn-autosearch').addEventListener('click', autoSearch);
  }

  if (state.releases) renderReleases();
}

function sheetStatus(msg, isError) {
  const el = $('#sheet-status');
  if (!el) return;
  el.textContent = msg;
  el.style.color = isError ? '#b91c1c' : '';
}

async function addMovie(autoSearchAfter) {
  const m = state.current;
  ['btn-add-auto', 'btn-add-pick'].forEach(id => { const b = document.getElementById(id); if (b) b.disabled = true; });
  sheetStatus('Adding…');
  try {
    const added = await post('/api/add', {
      tmdbId: m.tmdbId,
      qualityProfileId: state.selectedProfile,
      search: autoSearchAfter,
    });
    // reflect the new library state in the grid + sheet
    Object.assign(m, added);
    const gm = state.movies.find(x => x.tmdbId === m.tmdbId);
    if (gm) Object.assign(gm, added);
    renderGrid();
    renderSheet();
    if (autoSearchAfter) {
      sheetStatus('Added. Auto-search started.');
    } else {
      findReleases();
    }
  } catch (e) {
    ['btn-add-auto', 'btn-add-pick'].forEach(id => { const b = document.getElementById(id); if (b) b.disabled = false; });
    sheetStatus(e.message, true);
  }
}

async function autoSearch() {
  try {
    $('#btn-autosearch').disabled = true;
    sheetStatus('Auto-search started.');
    await post('/api/autosearch', { movieId: state.current.id });
  } catch (e) {
    sheetStatus(e.message, true);
  }
}

// ---------- releases ----------

let relSeq = 0;

async function findReleases() {
  const m = state.current;
  const seq = ++relSeq;
  const block = $('#rel-block');
  const btn = $('#btn-releases');
  if (btn) btn.disabled = true;
  block.innerHTML = `<div class="searching">Searching indexers… this can take a minute.</div>`;
  try {
    const rels = await api('/api/releases?movieId=' + m.id);
    if (seq !== relSeq || state.current !== m) return;
    // Releases Radarr couldn't match to this movie sink to the bottom.
    const junk = r => r.rejections.some(x => x.startsWith('Unknown Movie')) ? 1 : 0;
    rels.sort((a, b) => (junk(a) - junk(b)) || (b.seeders - a.seeders) || (b.size - a.size));
    state.releases = rels;
    state.relFilter = 'all';
    renderReleases();
  } catch (e) {
    if (seq !== relSeq) return;
    block.innerHTML = `<div class="error-line">${esc(e.message)}</div>`;
  } finally {
    if (btn && state.current === m) btn.disabled = false;
  }
}

const REL_FILTERS = [
  { key: 'all', label: 'All', match: () => true },
  { key: '2160', label: '4K', match: r => r.resolution === 2160 },
  { key: '1080', label: '1080p', match: r => r.resolution === 1080 },
  { key: '720', label: '720p', match: r => r.resolution === 720 },
  { key: 'other', label: 'Other', match: r => ![2160, 1080, 720].includes(r.resolution) },
];

function renderReleases() {
  const rels = state.releases;
  const block = $('#rel-block');
  if (!rels) return;
  if (!rels.length) {
    block.innerHTML = `<div class="empty">No releases found.</div>`;
    return;
  }
  const filters = REL_FILTERS.map(f => ({ ...f, n: rels.filter(f.match).length }))
    .filter(f => f.key === 'all' || f.n > 0);
  const active = filters.find(f => f.key === state.relFilter) || filters[0];
  const shown = rels.filter(active.match);

  block.innerHTML = `
    <div class="section-label">${rels.length} releases</div>
    <div class="rel-filter">
      ${filters.map(f => `<button class="btn${f.key === active.key ? ' selected' : ''}" data-f="${f.key}">${f.label}<span class="n">${f.n}</span></button>`).join('')}
    </div>
    <div id="rel-list">
      ${shown.map((r, i) => relRowHTML(r, i)).join('')}
    </div>`;

  block.querySelectorAll('.rel-filter .btn').forEach(b => {
    b.addEventListener('click', () => { state.relFilter = b.dataset.f; renderReleases(); });
  });
  block.querySelectorAll('[data-grab]').forEach(b => {
    b.addEventListener('click', () => grab(shown[+b.dataset.grab], b));
  });
}

function relRowHTML(r, i) {
  const stats = [];
  if (r.protocol === 'torrent') stats.push(`${r.seeders}/${r.leechers} seed`);
  const sub = [
    `<span class="q">${esc(r.quality)}</span>`,
    esc(r.indexer),
    fmtAge(r.ageDays),
    r.languages.length && r.languages[0] !== 'English' ? esc(r.languages.join(', ')) : '',
    r.flags.length ? esc(r.flags.join(', ')) : '',
  ].filter(Boolean).join(' · ');
  return `
  <div class="rel-row">
    <div class="rel-main">
      <div class="rel-title">${esc(r.title)}</div>
      <div class="rel-sub">${sub}</div>
      ${r.rejected ? `<div class="rel-reject">${esc(r.rejections.join('; '))}</div>` : ''}
    </div>
    <div class="rel-stats">
      <div class="size">${fmtSize(r.size)}</div>
      <div>${stats.join(' ')}</div>
    </div>
    <button class="btn" data-grab="${i}">Grab</button>
  </div>`;
}

async function grab(r, btn) {
  btn.disabled = true;
  btn.textContent = 'Grabbing…';
  try {
    await post('/api/grab', { guid: r.guid, indexerId: r.indexerId });
    btn.textContent = 'Grabbed';
    sheetStatus('Sent to download client.');
    setTimeout(loadQueue, 3000);
  } catch (e) {
    btn.disabled = false;
    btn.textContent = 'Grab';
    sheetStatus(e.message, true);
  }
}

// ---------- downloads ----------

function queueLine(item) {
  if (item.size > 0 && item.sizeleft > 0) {
    const pct = Math.round((1 - item.sizeleft / item.size) * 100);
    const eta = fmtTimeleft(item.timeleft);
    return `${pct}% · ${fmtSize(item.sizeleft)} left${eta ? ' · ' + eta : ''}`;
  }
  return humanState(item);
}

async function loadQueue() {
  try {
    const queue = await api('/api/queue');
    state.queue = queue;
    state.queueByMovie = new Map(queue.map(i => [i.movieId, i]));
    const active = queue.filter(i => i.status === 'downloading' && i.sizeleft > 0).length;
    const badge = $('#queue-count');
    badge.hidden = active === 0;
    badge.textContent = active;
    if (state.view === 'downloads') renderQueue();
  } catch (e) {
    // quiet failure on poll; downloads view shows errors on demand
    console.error(e);
  }
}

function renderQueue() {
  const list = $('#queue-list');
  if (!state.queue.length) {
    list.innerHTML = `<div class="empty">Queue is empty.</div>`;
    return;
  }
  const active = state.queue.filter(i => i.status === 'downloading' && i.sizeleft > 0);
  const rest = state.queue.filter(i => !active.includes(i));

  const row = i => {
    const isActive = i.status === 'downloading' && i.sizeleft > 0;
    const pct = i.size > 0 ? Math.round((1 - i.sizeleft / i.size) * 100) : 0;
    return `
    <div class="dl-row">
      <div class="dl-poster"><img src="${esc(i.poster)}" alt="" loading="lazy" data-t="" onerror="pfail(this)"></div>
      <div class="dl-main">
        <div class="dl-title">${esc(i.movieTitle || i.releaseTitle)}${i.year ? ` <span class="mono" style="font-weight:400;color:var(--text-2)">${i.year}</span>` : ''}</div>
        <div class="dl-release">${esc(i.releaseTitle)}</div>
        ${isActive ? `<div class="dl-bar"><b style="width:${pct}%"></b></div>` : ''}
      </div>
      <div class="dl-stats">
        ${isActive
          ? `<div class="pct">${pct}%</div><div>${fmtSize(i.sizeleft)} left${i.timeleft ? ' · ' + fmtTimeleft(i.timeleft) : ''}</div>`
          : `<div>${esc(humanState(i))}</div><div>${fmtSize(i.size)}</div>`}
      </div>
    </div>`;
  };

  list.innerHTML =
    (active.length ? `<div class="section-label">Downloading</div>` + active.map(row).join('') : '') +
    (rest.length ? `<div class="section-label">Waiting</div>` + rest.map(row).join('') : '') ||
    `<div class="empty">Queue is empty.</div>`;
}

// ---------- nav + init ----------

$('#nav-search').addEventListener('click', e => { e.preventDefault(); setView('search'); });
$('#nav-downloads').addEventListener('click', e => { e.preventDefault(); setView('downloads'); });

(async function init() {
  loadRecent();
  loadQueue();
  setInterval(loadQueue, 15000);
  try {
    state.profiles = await api('/api/profiles');
  } catch (e) {
    console.error(e);
  }
})();
