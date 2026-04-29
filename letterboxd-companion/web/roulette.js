import {
  loadData, poster, backdrop, el, setHeaderMeta,
  title, year, decade, runtime, directors, genres,
} from "/lib.js";

let DATA = null;
let LAST_PICK_ID = null;

function buildTaste(data) {
  // build affinity scores from rated films
  const dirAffinity = new Map();
  const decAffinity = new Map();
  const genAffinity = new Map();
  let baselineSum = 0, baselineN = 0;
  for (const f of data.films) {
    if (f.rating == null) continue;
    baselineSum += f.rating; baselineN++;
  }
  const baseline = baselineN ? baselineSum / baselineN : 3.5;

  function bump(map, key, delta) {
    if (!map.has(key)) map.set(key, { sum: 0, n: 0 });
    const e = map.get(key);
    e.sum += delta; e.n += 1;
  }
  for (const f of data.films) {
    if (f.rating == null) continue;
    const delta = f.rating - baseline;
    for (const d of directors(f)) bump(dirAffinity, d, delta);
    for (const g of genres(f)) bump(genAffinity, g, delta);
    const dec = decade(f);
    if (dec != null) bump(decAffinity, dec, delta);
  }
  const flatten = m => {
    const out = new Map();
    for (const [k, v] of m) out.set(k, v.n >= 2 ? v.sum / v.n : v.sum / 4);
    return out;
  };
  return {
    baseline,
    dir: flatten(dirAffinity),
    dec: flatten(decAffinity),
    gen: flatten(genAffinity),
  };
}

function tasteScore(film, taste) {
  let s = 0, w = 0;
  for (const d of directors(film)) {
    if (taste.dir.has(d)) { s += taste.dir.get(d) * 1.2; w += 1.2; }
  }
  for (const g of genres(film)) {
    if (taste.gen.has(g)) { s += taste.gen.get(g) * 0.6; w += 0.6; }
  }
  const dec = decade(film);
  if (dec != null && taste.dec.has(dec)) { s += taste.dec.get(dec) * 0.4; w += 0.4; }
  if (film.tmdb && film.tmdb.vote_average != null) {
    s += ((film.tmdb.vote_average - 6) / 4) * 0.5; w += 0.5;
  }
  return w > 0 ? s / w : 0;
}

function applyFilters() {
  const dec = document.getElementById("filter-decade").value;
  const rt = document.getElementById("filter-runtime").value;
  const gen = document.getElementById("filter-genre").value;
  const tuned = document.getElementById("filter-tuned").checked;

  let pool = DATA.films.filter(f => f.watchlist && !f.watched);
  if (dec) pool = pool.filter(f => String(decade(f)) === dec);
  if (rt) pool = pool.filter(f => runtime(f) != null && runtime(f) <= Number(rt));
  if (gen) pool = pool.filter(f => genres(f).includes(gen));
  return { pool, tuned };
}

function spin() {
  const { pool, tuned } = applyFilters();
  document.getElementById("pool-size").textContent =
    `${pool.length} film${pool.length === 1 ? "" : "s"} match`;

  if (!pool.length) {
    renderPick(null);
    return;
  }

  let pick;
  if (tuned) {
    const taste = buildTaste(DATA);
    const ranked = pool.map(f => ({ f, s: tasteScore(f, taste) }))
      .sort((a, b) => b.s - a.s);
    // weighted pick from top half, biased toward the top
    const candidates = ranked.slice(0, Math.max(8, Math.ceil(ranked.length / 2)));
    const weights = candidates.map((_, i) => 1 / (i + 1));
    const total = weights.reduce((a, b) => a + b, 0);
    let r = Math.random() * total;
    for (let i = 0; i < candidates.length; i++) {
      r -= weights[i];
      if (r <= 0) { pick = candidates[i].f; break; }
    }
    pick = pick || candidates[0].f;
    pick.__match = candidates.find(c => c.f === pick).s;
  } else {
    pick = pool[Math.floor(Math.random() * pool.length)];
    pick.__match = null;
  }
  // avoid showing the same pick twice in a row when there's choice
  if (pool.length > 1 && pick.id === LAST_PICK_ID) {
    return spin();
  }
  LAST_PICK_ID = pick.id;
  renderPick(pick);
}

function renderPick(film) {
  const wrap = document.getElementById("pick");
  wrap.innerHTML = "";
  if (!film) {
    wrap.classList.add("empty");
    wrap.appendChild(el("div", { class: "pick-empty muted" }, ["nothing matches those filters."]));
    return;
  }
  wrap.classList.remove("empty");

  const bd = backdrop(film, "w1280");
  if (bd) wrap.appendChild(el("div", { class: "backdrop", style: `background-image:url(${bd})` }));

  const p = poster(film, "w342");
  wrap.appendChild(el("div", {
    class: "poster-large",
    style: p ? `background-image:url(${p})` : "",
  }));

  const meta = el("div", { class: "meta" });
  if (runtime(film)) meta.appendChild(el("span", { class: "pill" }, [`${runtime(film)} min`]));
  if (genres(film).length) meta.appendChild(el("span", { class: "pill" }, [genres(film).slice(0, 2).join(" · ")]));
  if (film.tmdb && film.tmdb.vote_average) {
    meta.appendChild(el("span", { class: "pill" }, [`${film.tmdb.vote_average.toFixed(1)} tmdb`]));
  }
  if (film.__match != null) {
    const s = film.__match;
    const label = s > 0.4 ? "strong match" : s > 0.1 ? "good match" : s > -0.1 ? "neutral" : "stretch";
    meta.appendChild(el("span", { class: "pill match" }, [`taste: ${label}`]));
  }

  const dirText = directors(film).join(", ");
  const cast = (film.tmdb && film.tmdb.cast) || [];

  const info = el("div", { class: "info" }, [
    el("h2", {}, [title(film)]),
    el("div", { class: "y" }, [year(film) ? String(year(film)) : ""]),
    meta,
    film.tmdb && film.tmdb.overview ? el("p", { class: "overview" }, [film.tmdb.overview]) : null,
    el("div", { class: "credits" }, [
      dirText ? el("div", {}, [el("b", {}, ["dir"]), ` ${dirText}`]) : null,
      cast.length ? el("div", {}, [el("b", {}, ["cast"]), ` ${cast.join(", ")}`]) : null,
    ]),
    el("a", { class: "lb-link", href: film.uri, target: "_blank", rel: "noopener" }, ["open on letterboxd →"]),
  ]);
  wrap.appendChild(info);
}

function populateFilters() {
  const decadeSelect = document.getElementById("filter-decade");
  const decades = new Set();
  for (const f of DATA.films) {
    if (f.watchlist && !f.watched) {
      const d = decade(f);
      if (d != null) decades.add(d);
    }
  }
  for (const d of [...decades].sort((a, b) => a - b)) {
    decadeSelect.appendChild(el("option", { value: String(d) }, [`${d}s`]));
  }

  const genreSelect = document.getElementById("filter-genre");
  const gset = new Set();
  for (const f of DATA.films) {
    if (f.watchlist && !f.watched) for (const g of genres(f)) gset.add(g);
  }
  for (const g of [...gset].sort()) {
    genreSelect.appendChild(el("option", { value: g }, [g]));
  }
}

function renderWatchlistGrid() {
  const grid = document.getElementById("watchlist-grid");
  const wl = DATA.films.filter(f => f.watchlist && !f.watched);
  // sort by year desc
  wl.sort((a, b) => (year(b) || 0) - (year(a) || 0));

  function refresh() {
    const dec = document.getElementById("filter-decade").value;
    const rt = document.getElementById("filter-runtime").value;
    const gen = document.getElementById("filter-genre").value;
    grid.innerHTML = "";
    let count = 0;
    for (const f of wl) {
      if (dec && String(decade(f)) !== dec) continue;
      if (rt && (runtime(f) == null || runtime(f) > Number(rt))) continue;
      if (gen && !genres(f).includes(gen)) continue;
      const card = document.createElement("a");
      card.className = "poster";
      card.href = f.uri;
      card.target = "_blank";
      card.rel = "noopener";
      const p = poster(f, "w185");
      if (p) {
        const img = document.createElement("img");
        img.loading = "lazy";
        img.src = p;
        img.alt = title(f);
        card.appendChild(img);
      } else {
        const ph = document.createElement("div");
        ph.className = "ph";
        ph.textContent = title(f);
        card.appendChild(ph);
      }
      const t = document.createElement("div");
      t.className = "t";
      t.textContent = title(f);
      card.appendChild(t);
      const y = document.createElement("div");
      y.className = "y";
      y.textContent = year(f) || "";
      card.appendChild(y);
      grid.appendChild(card);
      count++;
    }
    document.getElementById("pool-size").textContent =
      `${count} film${count === 1 ? "" : "s"} match`;
  }
  refresh();
  for (const id of ["filter-decade", "filter-runtime", "filter-genre", "filter-tuned"]) {
    document.getElementById(id).addEventListener("change", refresh);
  }
}

(async () => {
  DATA = await loadData();
  setHeaderMeta(DATA.profile);
  populateFilters();
  document.getElementById("spin").addEventListener("click", spin);
  renderWatchlistGrid();
  spin();
})();
