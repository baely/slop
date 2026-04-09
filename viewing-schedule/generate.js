const fs = require('fs');
const schedule = JSON.parse(fs.readFileSync(process.argv[2], 'utf-8'));

const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Viewing Schedule</title>
<style>
*, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }

body {
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 13px;
  line-height: 1.5;
  color: #111;
  background: #fff;
  -webkit-font-smoothing: antialiased;
}

.app {
  max-width: 1100px;
  margin: 0 auto;
  padding: 24px 16px 64px;
}

header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 16px;
  border-bottom: 1px solid #e0e0e0;
  margin-bottom: 32px;
}

.logo {
  display: flex;
  align-items: center;
  gap: 10px;
}
.logo-mark {
  width: 32px;
  height: 32px;
  background: #111;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 700;
}
.logo-text { font-size: 18px; font-weight: 600; color: #111; letter-spacing: -0.01em; }

.date-range { font-size: 12px; color: #999; letter-spacing: 0.04em; text-transform: uppercase; }

.layout {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 48px;
  align-items: start;
}

/* Today panel */
.section-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: #999;
  margin-bottom: 16px;
}

.today-card {
  margin-bottom: 24px;
}

.today-movie {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: 20px;
  margin-bottom: 24px;
}

.today-movie img {
  width: 140px;
  border-radius: 2px;
  display: block;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}

.today-meta h2 {
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  margin-bottom: 2px;
}
.today-meta .year { color: #999; font-weight: 400; }
.today-meta .director { font-size: 12px; color: #666; margin-bottom: 6px; }
.today-meta .genres { font-size: 11px; color: #999; margin-bottom: 8px; display: flex; gap: 6px; flex-wrap: wrap; }
.today-meta .genre-tag {
  border: 1px solid #e0e0e0;
  padding: 1px 6px;
  border-radius: 2px;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.today-meta .runtime { font-size: 11px; color: #999; margin-bottom: 8px; }
.today-meta .overview { font-size: 12px; color: #444; line-height: 1.6; }
.today-meta .rating { font-size: 11px; color: #999; margin-top: 6px; }
.today-reason {
  font-size: 12px;
  color: #666;
  font-style: italic;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #f0f0f0;
  line-height: 1.6;
}

.no-today {
  color: #999;
  font-size: 13px;
  padding: 32px 0;
}

/* Upcoming list */
.upcoming-list { list-style: none; }

.upcoming-item {
  display: grid;
  grid-template-columns: 40px 1fr;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid #f0f0f0;
  align-items: start;
}
.upcoming-item:first-child { padding-top: 0; }
.upcoming-item.is-past { opacity: 0.35; }

.upcoming-date {
  text-align: center;
}
.upcoming-date .d-month {
  font-size: 9px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #999;
  font-weight: 600;
}
.upcoming-date .d-day {
  font-size: 18px;
  font-weight: 700;
  color: #111;
  line-height: 1.1;
}
.upcoming-date .d-weekday {
  font-size: 9px;
  color: #bbb;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.upcoming-movies {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.upcoming-movie {
  display: flex;
  gap: 10px;
  align-items: start;
}

.upcoming-movie img {
  width: 36px;
  border-radius: 1px;
  flex-shrink: 0;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1);
}

.upcoming-movie-info h3 {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.3;
}
.upcoming-movie-info h3 .yr { font-weight: 400; color: #999; font-size: 11px; }
.upcoming-movie-info .um-detail {
  font-size: 11px;
  color: #999;
}

.upcoming-item.is-today {
  background: #fafafa;
  margin: 0 -8px;
  padding-left: 8px;
  padding-right: 8px;
  border-radius: 4px;
  border-bottom-color: transparent;
}

/* Empty poster fallback */
.no-poster {
  width: 100%;
  aspect-ratio: 2/3;
  background: #f5f5f5;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #ccc;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border-radius: 2px;
}
.upcoming-movie .no-poster {
  width: 36px;
  font-size: 7px;
}

@media (max-width: 700px) {
  .layout {
    grid-template-columns: 1fr;
    gap: 32px;
  }
  .today-movie {
    grid-template-columns: 100px 1fr;
    gap: 14px;
  }
  .today-movie img { width: 100px; }
}
</style>
</head>
<body>
<div class="app">
  <header>
    <div class="logo">
      <div class="logo-mark">VS</div>
      <div class="logo-text">Viewing Schedule</div>
    </div>
    <div class="date-range">Apr 10 — May 9, 2026</div>
  </header>
  <div class="layout">
    <div class="today-panel">
      <div class="section-label">Today</div>
      <div id="today"></div>
    </div>
    <div class="upcoming-panel">
      <div class="section-label">Schedule</div>
      <ul class="upcoming-list" id="upcoming"></ul>
    </div>
  </div>
</div>
<script>
const SCHEDULE = ${JSON.stringify(schedule)};

function formatDate(ds) {
  const d = new Date(ds + 'T12:00:00');
  const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
  return { month: months[d.getMonth()], day: d.getDate(), weekday: ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'][d.getDay()] };
}

function todayStr() {
  const d = new Date();
  return d.getFullYear() + '-' + String(d.getMonth()+1).padStart(2,'0') + '-' + String(d.getDate()).padStart(2,'0');
}

function posterImg(url, cls) {
  if (url) return '<img src="' + url + '" alt="" loading="lazy">';
  return '<div class="no-poster ' + (cls||'') + '">No poster</div>';
}

function renderToday() {
  const today = todayStr();
  const entry = SCHEDULE.find(s => s.date === today);
  const el = document.getElementById('today');

  if (!entry) {
    // Find next upcoming
    const next = SCHEDULE.find(s => s.date > today);
    if (next) {
      const fd = formatDate(next.date);
      el.innerHTML = '<div class="no-today">Nothing scheduled today.<br>Next up on <strong>' + fd.month + ' ' + fd.day + '</strong>.</div>';
    } else {
      el.innerHTML = '<div class="no-today">Schedule complete.</div>';
    }
    return;
  }

  let html = '<div class="today-card">';
  entry.movies.forEach(m => {
    html += '<div class="today-movie">';
    html += '<div>' + posterImg(m.poster_lg || m.poster) + '</div>';
    html += '<div class="today-meta">';
    html += '<h2>' + m.tmdb_title + ' <span class="year">(' + m.year + ')</span></h2>';
    if (m.director) html += '<div class="director">Directed by ' + m.director + '</div>';
    if (m.genres && m.genres.length) html += '<div class="genres">' + m.genres.map(g => '<span class="genre-tag">' + g + '</span>').join('') + '</div>';
    if (m.runtime) html += '<div class="runtime">' + m.runtime + ' min</div>';
    if (m.overview) html += '<div class="overview">' + m.overview + '</div>';
    if (m.rating) html += '<div class="rating">TMDB ' + m.rating.toFixed(1) + '</div>';
    html += '</div></div>';
  });
  if (entry.reason) html += '<div class="today-reason">' + entry.reason + '</div>';
  html += '</div>';
  el.innerHTML = html;
}

function renderUpcoming() {
  const today = todayStr();
  const el = document.getElementById('upcoming');
  let html = '';

  SCHEDULE.forEach(entry => {
    const fd = formatDate(entry.date);
    const isPast = entry.date < today;
    const isToday = entry.date === today;

    html += '<li class="upcoming-item' + (isPast ? ' is-past' : '') + (isToday ? ' is-today' : '') + '">';
    html += '<div class="upcoming-date"><div class="d-month">' + fd.month + '</div><div class="d-day">' + fd.day + '</div><div class="d-weekday">' + fd.weekday + '</div></div>';
    html += '<div class="upcoming-movies">';
    entry.movies.forEach(m => {
      html += '<div class="upcoming-movie">';
      html += posterImg(m.poster, '');
      html += '<div class="upcoming-movie-info"><h3>' + (m.tmdb_title || m.title) + ' <span class="yr">' + m.year + '</span></h3>';
      const parts = [];
      if (m.director) parts.push(m.director);
      if (m.runtime) parts.push(m.runtime + 'm');
      if (m.genres && m.genres.length) parts.push(m.genres.slice(0,2).join(', '));
      if (parts.length) html += '<div class="um-detail">' + parts.join(' &middot; ') + '</div>';
      html += '</div></div>';
    });
    html += '</div></li>';
  });

  el.innerHTML = html;
}

renderToday();
renderUpcoming();
</script>
</body>
</html>`;

console.log(html);
