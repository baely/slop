(function () {
  const D = window.REEL;
  const $ = (id) => document.getElementById(id);

  const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June',
    'July', 'August', 'September', 'October', 'November', 'December'];

  function longDate(iso) {
    const [y, m, d] = iso.split('-').map(Number);
    return `${d} ${MONTHS[m - 1]} ${y}`;
  }

  function monthName(ym) {
    const [y, m] = ym.split('-').map(Number);
    return `${MONTHS[m - 1]} ${y}`;
  }

  function ago(iso) {
    const days = Math.floor((Date.now() - new Date(iso + 'T00:00:00')) / 86400000);
    if (days < 1) return 'today';
    if (days < 14) return days === 1 ? 'yesterday' : `${days} days ago`;
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

  // hero
  $('profile-link').href = D.profile.url;
  $('hero').textContent = `${D.watched} films.`;
  $('hero-sub').textContent =
    `Everything ${D.profile.username} has logged on Letterboxd since ${longDate(D.profile.joined)}.`;

  // stat row
  const stats = [
    ['Avg Rating', D.ratings.avg.toFixed(2), `${D.ratings.count} rated`, true],
    ['Five Stars', D.five_stars.length, `of ${D.ratings.count} rated`, false],
    ['Rewatches', D.diary.rewatches, `in ${D.diary.count} diary entries`, false],
    ['Watchlist', D.watchlist.count, 'films waiting', false],
  ];
  for (const [label, value, context, mono] of stats) {
    const s = el('div', 'stat');
    s.append(el('div', 'stat-label', label));
    s.append(el('div', 'stat-value' + (mono ? ' mono' : ''), String(value)));
    s.append(el('div', 'stat-context', context));
    $('stats').append(s);
  }

  // bar chart: items = [{label, value, title?}]
  function chart(mount, items) {
    const max = Math.max(...items.map((i) => i.value));
    const labels = el('div', 'chart-labels');
    for (const item of items) {
      const col = el('div', 'col');
      col.append(el('div', 'val', item.value ? String(item.value) : ''));
      const bar = el('div', 'bar' + (item.value ? '' : ' zero'));
      bar.style.height = item.value ? `${Math.max((item.value / max) * 100, 1.5)}%` : '2px';
      if (item.title) col.title = item.title;
      col.append(bar);
      labels.append(el('span', null, item.label));
      mount.append(col);
    }
    mount.after(labels);
  }

  // decades
  $('decades-sub').textContent =
    `Release years ${D.release_range[0]}–${D.release_range[1]}.`;
  chart($('decades-chart'), D.decades.map((d) => ({
    label: d.decade % 100 === 0 ? String(d.decade) : String(d.decade % 100),
    value: d.count,
    title: `${d.decade}s: ${d.count} films`,
  })));

  // ratings
  $('ratings-sub').textContent =
    `${D.ratings.count} rated, average ${D.ratings.avg.toFixed(2)}.`;
  chart($('ratings-chart'), D.ratings.dist.map((r) => ({
    label: r.stars,
    value: r.count,
    title: `${r.stars} stars: ${r.count} films`,
  })));

  // pace
  $('pace-sub').textContent =
    `${D.diary.count} diary entries. Busiest month: ${monthName(D.diary.busiest_month.month)}, ` +
    `${D.diary.busiest_month.count} films. Most common night: ${D.diary.top_dow.day}.`;
  chart($('pace-chart'), D.diary.by_year.map((y) => ({
    label: `'${String(y.year).slice(2)}`,
    value: y.count,
    title: `${y.year}: ${y.count} logged`,
  })));

  // five stars
  for (const f of D.five_stars) {
    const tr = el('tr');
    tr.append(el('td', null, f.name));
    tr.append(el('td', 'num', f.year));
    $('five-stars').tBodies[0].append(tr);
  }

  // watchlist
  $('watchlist-sub').textContent =
    `${D.watchlist.count} films waiting. The five that have waited longest:`;
  for (const w of D.watchlist.oldest) {
    const tr = el('tr');
    tr.append(el('td', null, w.name));
    tr.append(el('td', 'num', w.year));
    const added = el('td', 'num', ago(w.added));
    added.title = w.added;
    tr.append(added);
    $('watchlist').tBodies[0].append(tr);
  }

  // tags
  for (const [tag, count] of D.tags) {
    const tr = el('tr');
    tr.append(el('td', null, tag));
    tr.append(el('td', 'num', String(count)));
    $('tags').tBodies[0].append(tr);
  }

  // reviews
  for (const r of D.reviews) {
    const card = el('div', 'review');
    card.append(el('div', 'film', `${r.name} (${r.year})`));
    const meta = el('div', 'meta', `Rated ${r.rating} · ${ago(r.watched)}`);
    meta.title = r.watched;
    card.append(meta);
    card.append(el('blockquote', null, r.text));
    $('reviews').append(card);
  }

  // colophon
  $('colophon').textContent =
    `Built from a Letterboxd data export dated ${D.export_date}.`;
})();
