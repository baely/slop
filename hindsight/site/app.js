(function () {
  const D = window.HINDSIGHT;
  const FILMS = D.films;
  const $ = (id) => document.getElementById(id);
  const STORE_KEY = 'hindsight.v1';

  // ---------- state ----------

  const saved = load();
  const state = {
    mode: 'any',
    pair: null,      // [filmA, filmB]
    picked: null,    // index of the card the player chose
    rounds: saved.rounds || 0,
    correct: saved.correct || 0,
    streak: saved.streak || 0,
    best: saved.best || 0,
    seen: new Set(), // pair keys already served this session
  };

  function load() {
    try { return JSON.parse(localStorage.getItem(STORE_KEY)) || {}; } catch (e) { return {}; }
  }
  function save() {
    const { rounds, correct, streak, best } = state;
    try { localStorage.setItem(STORE_KEY, JSON.stringify({ rounds, correct, streak, best })); } catch (e) { /* private mode */ }
  }

  // ---------- pairing ----------

  // Every unordered pair with a rating gap, bucketed by gap so mode selection
  // is a lookup rather than a rejection loop.
  const PAIRS = { any: [], close: [] };
  for (let i = 0; i < FILMS.length; i++) {
    for (let j = i + 1; j < FILMS.length; j++) {
      const gap = Math.abs(FILMS[i].rating - FILMS[j].rating);
      if (gap === 0) continue;
      PAIRS.any.push([i, j]);
      if (gap <= 0.5) PAIRS.close.push([i, j]);
    }
  }

  function nextPair() {
    const pool = PAIRS[state.mode];
    if (state.seen.size >= pool.length) state.seen.clear();
    let idx;
    do { idx = Math.floor(Math.random() * pool.length); } while (state.seen.has(idx));
    state.seen.add(idx);
    const [a, b] = pool[idx];
    // Coin-flip the display order so the higher rating isn't always on one side.
    return Math.random() < 0.5 ? [FILMS[a], FILMS[b]] : [FILMS[b], FILMS[a]];
  }

  // ---------- formatting ----------

  function stars(n) {
    return Number.isInteger(n) ? `${n}.0` : n.toFixed(1);
  }

  function ago(iso) {
    const days = Math.floor((Date.now() - new Date(iso + 'T00:00:00')) / 86400000);
    if (days < 1) return 'today';
    if (days === 1) return 'yesterday';
    if (days < 14) return `${days} days ago`;
    if (days < 60) return `${Math.floor(days / 7)} weeks ago`;
    if (days < 730) return `${Math.floor(days / 30.44)} months ago`;
    return `${Math.floor(days / 365.25)} years ago`;
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  // ---------- render ----------

  function renderCard(side) {
    const film = state.pair[side];
    const card = $(`film-${side}`);
    card.className = 'film';
    card.disabled = false;
    card.replaceChildren();

    card.append(el('div', 'title', film.name));
    card.append(el('div', 'year', film.year ? String(film.year) : ''));

    const reveal = el('div', 'reveal');
    reveal.append(el('div', 'rating', stars(film.rating)));
    if (film.watched) {
      const seen = el('div', 'seen', `Watched ${ago(film.watched)}`);
      seen.title = film.watched;
      reveal.append(seen);
    } else {
      reveal.append(el('div', 'seen', 'No diary entry'));
    }
    if (film.review) reveal.append(el('div', 'note', `“${film.review}”`));
    reveal.append(el('div', 'chips'));
    card.append(reveal);
  }

  function renderStats() {
    const pct = state.rounds ? Math.round((state.correct / state.rounds) * 100) : 0;
    const items = [
      ['Score', `${state.correct}/${state.rounds}`, state.rounds ? `${pct}% correct` : 'No rounds yet'],
      ['Streak', String(state.streak), state.streak >= 5 ? 'On a run' : 'Current'],
      ['Best Streak', String(state.best), 'All time'],
      ['Pool', String(PAIRS[state.mode].length.toLocaleString()), state.mode === 'close' ? 'Half-star pairs' : 'Possible pairs'],
    ];
    $('stats').replaceChildren();
    for (const [label, value, context] of items) {
      const s = el('div', 'stat');
      s.append(el('div', 'stat-label', label));
      s.append(el('div', 'stat-value', value));
      s.append(el('div', 'stat-context', context));
      $('stats').append(s);
    }
  }

  function newRound() {
    state.pair = nextPair();
    state.picked = null;
    renderCard(0);
    renderCard(1);
    $('status').className = 'status';
    $('status').replaceChildren(el('span', 'detail', 'Pick One. Keys: 1, 2.'));
    $('next').hidden = true;
    renderStats();
  }

  function pick(side) {
    if (state.picked !== null) return;
    state.picked = side;

    const [a, b] = state.pair;
    const higher = a.rating > b.rating ? 0 : 1;
    const win = side === higher;
    const gap = Math.abs(a.rating - b.rating);

    state.rounds++;
    if (win) {
      state.correct++;
      state.streak++;
      if (state.streak > state.best) state.best = state.streak;
    } else {
      state.streak = 0;
    }
    save();

    for (const s of [0, 1]) {
      const card = $(`film-${s}`);
      card.disabled = true;
      card.classList.add('revealed');
      if (s === higher) card.classList.add('higher');
      const chips = card.querySelector('.chips');
      if (s === side) chips.append(el('span', 'chip', 'Your Pick'));
      if (s === higher) chips.append(el('span', 'chip', 'Rated Higher'));
    }

    const status = $('status');
    status.className = 'status ' + (win ? 'correct' : 'wrong');
    status.replaceChildren();
    status.append(el('span', 'tag', win ? 'Correct.' : 'Wrong.'));
    // Film names link out, so a surprising result is one click from the entry.
    const detail = el('span', 'detail');
    for (const f of [state.pair[higher], state.pair[1 - higher]]) {
      const link = el('a', null, f.name);
      link.href = f.url;
      link.rel = 'noopener';
      detail.append(link, ` ${stars(f.rating)} · `);
    }
    detail.append(`${stars(gap).replace(/^0\./, '.')} apart`);
    status.append(detail);

    $('next').hidden = false;
    $('next').focus();
    renderStats();
  }

  // ---------- wiring ----------

  $('profile-link').href = D.profile_url;
  $('intro-sub').textContent =
    `Two films ${D.username} has rated on Letterboxd. Pick the one that scored higher. ` +
    `${FILMS.length} rated films in the pot.`;
  $('colophon-text').textContent =
    `Letterboxd export ${D.export_date}. ${FILMS.length} rated films.`;

  for (const side of [0, 1]) {
    $(`film-${side}`).addEventListener('click', () => pick(side));
  }
  // Guarded: a keyboard Enter/Space may already have advanced the round.
  $('next').addEventListener('click', () => {
    if (state.picked !== null) newRound();
  });

  for (const btn of document.querySelectorAll('.mode')) {
    btn.addEventListener('click', () => {
      if (state.mode === btn.dataset.mode) return;
      state.mode = btn.dataset.mode;
      state.seen.clear();
      for (const b of document.querySelectorAll('.mode')) {
        b.setAttribute('aria-pressed', String(b === btn));
      }
      newRound();
    });
  }

  $('reset').addEventListener('click', () => {
    state.rounds = 0;
    state.correct = 0;
    state.streak = 0;
    state.best = 0;
    save();
    newRound();
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === '1' || e.key === 'ArrowLeft') pick(0);
    else if (e.key === '2' || e.key === 'ArrowRight') pick(1);
    else if ((e.key === 'Enter' || e.key === ' ') && state.picked !== null) {
      e.preventDefault();
      newRound();
    }
  });

  newRound();
})();
