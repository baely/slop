import {
  loadData, poster, el, setHeaderMeta,
  title, year, decade, runtime, ratingStars, fmtNum,
} from "/lib.js";

let DATA = null;

function entriesInRange(from, to) {
  // emit one entry per diary date within [from, to]
  const out = [];
  for (const f of DATA.films) {
    const dates = (f.diary_dates && f.diary_dates.length ? f.diary_dates : f.diary_logged_dates) || [];
    for (const d of dates) {
      if (!d) continue;
      if (from && d < from) continue;
      if (to && d > to) continue;
      out.push({ film: f, date: d });
    }
  }
  out.sort((a, b) => a.date.localeCompare(b.date));
  return out;
}

function renderStats(entries) {
  const wrap = document.getElementById("recap-stats");
  wrap.innerHTML = "";
  const rated = entries.filter(e => e.film.rating != null);
  let mins = 0;
  for (const e of entries) if (runtime(e.film)) mins += runtime(e.film);
  const avg = rated.length ? (rated.reduce((s, e) => s + e.film.rating, 0) / rated.length).toFixed(2) : "—";
  const uniqueFilms = new Set(entries.map(e => e.film.id)).size;
  const stats = [
    { v: fmtNum(entries.length), l: "diary entries", sub: `${uniqueFilms} unique films` },
    { v: `${(mins / 60).toFixed(1)}h`, l: "screen time", sub: `${fmtNum(mins)} min` },
    { v: avg, l: "avg rating", sub: `${rated.length} rated` },
    { v: countRewatches(entries), l: "rewatches" },
  ];
  for (const s of stats) {
    wrap.appendChild(el("div", { class: "stat" }, [
      el("div", { class: "v" }, [String(s.v)]),
      el("div", { class: "l" }, [s.l]),
      s.sub ? el("div", { class: "sub" }, [s.sub]) : null,
    ]));
  }
}

function countRewatches(entries) {
  // an entry is a rewatch if there are multiple diary dates for that film
  // and this date is not the earliest one
  const earliest = new Map();
  for (const e of entries) {
    const cur = earliest.get(e.film.id);
    if (!cur || e.date < cur) earliest.set(e.film.id, e.date);
  }
  let n = 0;
  for (const e of entries) {
    if (earliest.get(e.film.id) !== e.date) n++;
  }
  return n;
}

function renderStandouts(entries) {
  const wrap = document.getElementById("standouts");
  wrap.innerHTML = "";
  const seen = new Set();
  const standouts = entries
    .filter(e => e.film.rating != null && e.film.rating >= 4)
    .filter(e => {
      if (seen.has(e.film.id)) return false;
      seen.add(e.film.id);
      return true;
    })
    .sort((a, b) => b.film.rating - a.film.rating);
  if (!standouts.length) {
    wrap.appendChild(el("p", { class: "muted" }, ["nothing rated 4★ or higher in this window."]));
    return;
  }
  for (const e of standouts.slice(0, 30)) {
    const f = e.film;
    const a = el("a", { class: "poster", href: f.uri, target: "_blank", rel: "noopener" });
    const p = poster(f);
    if (p) a.appendChild(el("img", { src: p, alt: title(f), loading: "lazy" }));
    else a.appendChild(el("div", { class: "ph" }, [title(f)]));
    a.appendChild(el("div", { class: "t" }, [title(f)]));
    a.appendChild(el("div", { class: "y" }, [String(year(f) || "")]));
    a.appendChild(el("div", { class: "r" }, [ratingStars(f.rating)]));
    wrap.appendChild(a);
  }
}

function renderDecades(entries) {
  const wrap = document.getElementById("recap-decade");
  wrap.innerHTML = "";
  const m = new Map();
  for (const e of entries) {
    const d = decade(e.film);
    if (d == null) continue;
    if (!m.has(d)) m.set(d, { count: 0, sum: 0, n: 0 });
    const b = m.get(d);
    b.count++;
    if (e.film.rating != null) { b.sum += e.film.rating; b.n++; }
  }
  const rows = [...m.entries()].sort((a, b) => a[0] - b[0]);
  if (!rows.length) {
    wrap.appendChild(el("p", { class: "muted" }, ["no data in this window."]));
    return;
  }
  const max = Math.max(...rows.map(r => r[1].count));
  for (const [d, b] of rows) {
    const w = (b.count / max) * 100;
    const avg = b.n ? b.sum / b.n : null;
    const dot = avg != null ? (avg / 5) * 100 : null;
    wrap.appendChild(el("div", { class: "decade-row" }, [
      el("div", { class: "label" }, [`${d}s`]),
      el("div", { class: "bar-track" }, [
        el("div", { class: "bar-fill", style: `width:${w}%` }),
        dot != null ? el("div", { class: "dot", style: `left:${dot}%` }) : null,
      ]),
      el("div", { class: "count" }, [String(b.count)]),
      el("div", { class: "avg" }, [avg != null ? avg.toFixed(2) : "—"]),
    ]));
  }
}

function renderTimeline(entries) {
  const list = document.getElementById("timeline");
  list.innerHTML = "";
  // newest first reads better as a recap
  const ordered = [...entries].reverse();
  for (const e of ordered) {
    const f = e.film;
    const p = poster(f, "w92");
    list.appendChild(el("li", {}, [
      el("span", { class: "date" }, [e.date]),
      el("a", {
        class: "mini-poster",
        href: f.uri,
        target: "_blank",
        rel: "noopener",
        style: p ? `background-image:url(${p})` : "",
      }),
      el("a", { href: f.uri, target: "_blank", rel: "noopener", class: "name" }, [title(f)]),
      el("span", { class: "y" }, [String(year(f) || "")]),
      el("span", { class: "r" }, [f.rating != null ? ratingStars(f.rating) : ""]),
    ]));
  }
  if (!ordered.length) {
    list.appendChild(el("li", { class: "muted" }, ["no diary entries in this window."]));
  }
}

function build(from, to) {
  const entries = entriesInRange(from, to);
  renderStats(entries);
  renderStandouts(entries);
  renderDecades(entries);
  renderTimeline(entries);
}

function setupPresets() {
  const allDates = DATA.films.flatMap(f => f.diary_dates || []).filter(Boolean);
  if (!allDates.length) return;
  const sorted = allDates.slice().sort();
  const earliest = sorted[0];
  const latest = sorted[sorted.length - 1];
  const minYear = Number(earliest.slice(0, 4));
  const maxYear = Number(latest.slice(0, 4));

  const preset = document.getElementById("preset");
  preset.appendChild(el("option", { value: "all" }, ["all time"]));
  preset.appendChild(el("option", { value: "last365" }, ["last 365 days"]));
  for (let y = maxYear; y >= minYear; y--) {
    preset.appendChild(el("option", { value: `y:${y}` }, [String(y)]));
  }

  const apply = (val) => {
    let from = "", to = "";
    if (val === "all") { from = earliest; to = latest; }
    else if (val === "last365") {
      const d = new Date(latest);
      d.setUTCDate(d.getUTCDate() - 365);
      from = d.toISOString().slice(0, 10);
      to = latest;
    } else if (val.startsWith("y:")) {
      const y = val.slice(2);
      from = `${y}-01-01`;
      to = `${y}-12-31`;
    }
    document.getElementById("from").value = from;
    document.getElementById("to").value = to;
    build(from, to);
  };
  preset.addEventListener("change", e => apply(e.target.value));
  document.getElementById("apply").addEventListener("click", () => {
    build(document.getElementById("from").value, document.getElementById("to").value);
  });

  // default: current year if it has entries, else latest year
  const currentYear = new Date().getUTCFullYear();
  const has = DATA.films.some(f => (f.diary_dates || []).some(d => d.startsWith(String(currentYear))));
  const defaultVal = has ? `y:${currentYear}` : `y:${maxYear}`;
  preset.value = defaultVal;
  apply(defaultVal);
}

(async () => {
  DATA = await loadData();
  setHeaderMeta(DATA.profile);
  setupPresets();
})();
