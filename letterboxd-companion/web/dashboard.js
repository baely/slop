import {
  loadData, posterCard, poster, el, setHeaderMeta,
  title, year, decade, directors, genres, diaryDates, fmtNum, ratingStars,
} from "/lib.js";

function renderHeroStats(data) {
  const films = data.films;
  const watched = films.filter(f => f.watched);
  const rated = films.filter(f => f.rating != null);
  const watchlist = films.filter(f => f.watchlist);
  const cleared = watchlist.filter(f => f.watched).length;

  let totalMinutes = 0;
  for (const f of watched) {
    if (f.tmdb && f.tmdb.runtime) totalMinutes += f.tmdb.runtime;
  }
  const days = totalMinutes / (60 * 24);

  const avgRating = rated.length
    ? (rated.reduce((s, f) => s + f.rating, 0) / rated.length).toFixed(2)
    : "—";

  const allDates = films.flatMap(diaryDates).filter(Boolean);
  const earliest = allDates.length ? allDates.slice().sort()[0] : null;
  const latest = allDates.length ? allDates.slice().sort().slice(-1)[0] : null;

  const stats = [
    { v: fmtNum(watched.length), l: "films watched", sub: earliest ? `since ${earliest}` : "" },
    { v: `${days.toFixed(1)}d`, l: "screen time", sub: `${fmtNum(totalMinutes)} min total` },
    { v: avgRating, l: "average rating", sub: `${rated.length} rated` },
    { v: `${watchlist.length - cleared}`, l: "watchlist pending", sub: `${cleared} cleared` },
  ];

  const wrap = document.getElementById("hero-stats");
  for (const s of stats) {
    const card = el("div", { class: "stat" }, [
      el("div", { class: "v" }, [s.v]),
      el("div", { class: "l" }, [s.l]),
      s.sub ? el("div", { class: "sub" }, [s.sub]) : null,
    ]);
    wrap.appendChild(card);
  }
}

function renderFavorites(data) {
  const wrap = document.getElementById("favorite-posters");
  const favs = data.films.filter(f => f.favorite);
  if (!favs.length) {
    wrap.appendChild(el("p", { class: "muted" }, ["no favorites set."]));
    return;
  }
  for (const f of favs) wrap.appendChild(posterCard(f, { rating: f.rating }));
}

function renderHeatmap(data) {
  // build set of dates -> count
  const counts = new Map();
  for (const f of data.films) {
    for (const d of diaryDates(f)) {
      counts.set(d, (counts.get(d) || 0) + 1);
    }
  }
  if (!counts.size) return;

  const dates = [...counts.keys()].sort();
  const start = new Date(dates[0]);
  // start on a Sunday (UTC) to align rows
  start.setUTCDate(start.getUTCDate() - start.getUTCDay());
  const end = new Date(dates[dates.length - 1]);
  end.setUTCDate(end.getUTCDate() + (6 - end.getUTCDay()));

  const max = Math.max(...counts.values());
  const wrap = document.getElementById("heatmap");
  let cursor = new Date(start);
  let lastYearLabeled = null;

  while (cursor <= end) {
    const iso = cursor.toISOString().slice(0, 10);
    const c = counts.get(iso) || 0;
    let bucket = 0;
    if (c > 0) bucket = 1;
    if (c >= Math.max(2, max * 0.25)) bucket = 2;
    if (c >= Math.max(3, max * 0.5)) bucket = 3;
    if (c >= Math.max(4, max * 0.75)) bucket = 4;
    const cell = el("div", {
      class: `cell c${bucket}`,
      title: c ? `${iso}: ${c} film${c > 1 ? "s" : ""}` : iso,
    });
    wrap.appendChild(cell);

    // year label on first Sunday of each year
    if (cursor.getUTCDay() === 0 && cursor.getUTCFullYear() !== lastYearLabeled
        && cursor.getUTCMonth() < 2) {
      cell.classList.add("year-marker");
      cell.setAttribute("data-year", String(cursor.getUTCFullYear()));
      lastYearLabeled = cursor.getUTCFullYear();
    }

    cursor.setUTCDate(cursor.getUTCDate() + 1);
  }
}

function renderRatingHist(data) {
  const buckets = new Array(10).fill(0); // 0.5 .. 5.0
  for (const f of data.films) {
    if (f.rating == null) continue;
    const idx = Math.round(f.rating * 2) - 1;
    if (idx >= 0 && idx < 10) buckets[idx]++;
  }
  const max = Math.max(...buckets, 1);
  const wrap = document.getElementById("rating-hist");
  buckets.forEach((count, i) => {
    const r = (i + 1) / 2;
    const h = (count / max) * 100;
    wrap.appendChild(el("div", { class: "bar", title: `${r}★: ${count}` }, [
      el("div", { class: "count" }, [String(count || "")]),
      el("div", { class: "b", style: `height:${h}%` }),
      el("div", { class: "label" }, [r.toString().replace(".5", "½").replace(/^0½$/, "½")]),
    ]));
  });
}

function renderWatchlistProgress(data) {
  const wl = data.films.filter(f => f.watchlist);
  const cleared = wl.filter(f => f.watched).length;
  const pct = wl.length ? Math.round((cleared / wl.length) * 100) : 0;

  const wrap = document.getElementById("watchlist-progress");
  wrap.appendChild(el("div", { class: "progress-meta" }, [
    el("span", {}, [`${cleared} of ${wl.length} cleared`]),
    el("span", {}, [`${pct}%`]),
  ]));
  const bar = el("div", { class: "progress-bar" }, [
    el("div", { class: "fill", style: `width:${Math.max(pct, 4)}%` }, [pct >= 8 ? `${pct}%` : ""]),
  ]);
  wrap.appendChild(bar);

  const oldest = wl.filter(f => year(f)).sort((a, b) => year(a) - year(b))[0];
  const newest = wl.filter(f => year(f)).sort((a, b) => year(b) - year(a))[0];
  if (oldest && newest) {
    wrap.appendChild(el("p", { class: "muted", style: "margin:8px 0 0; font-size:13px" }, [
      `oldest: ${title(oldest)} (${year(oldest)}). newest: ${title(newest)} (${year(newest)}).`,
    ]));
  }
}

function renderDecadeChart(data) {
  const buckets = new Map();
  for (const f of data.films) {
    if (!f.watched) continue;
    const d = decade(f);
    if (d == null) continue;
    if (!buckets.has(d)) buckets.set(d, { count: 0, ratingSum: 0, ratingN: 0 });
    const b = buckets.get(d);
    b.count++;
    if (f.rating != null) { b.ratingSum += f.rating; b.ratingN++; }
  }
  const rows = [...buckets.entries()].sort((a, b) => a[0] - b[0]);
  const max = Math.max(...rows.map(r => r[1].count), 1);
  const wrap = document.getElementById("decade-chart");
  for (const [d, b] of rows) {
    const avg = b.ratingN ? (b.ratingSum / b.ratingN) : null;
    const w = (b.count / max) * 100;
    const dot = avg != null ? Math.min(100, (avg / 5) * 100) : null;
    wrap.appendChild(el("div", { class: "decade-row" }, [
      el("div", { class: "label" }, [`${d}s`]),
      el("div", { class: "bar-track" }, [
        el("div", { class: "bar-fill", style: `width:${w}%` }),
        dot != null ? el("div", { class: "dot", style: `left:${dot}%`, title: `avg ${avg.toFixed(2)}` }) : null,
      ]),
      el("div", { class: "count" }, [String(b.count)]),
      el("div", { class: "avg" }, [avg != null ? avg.toFixed(2) : "—"]),
    ]));
  }
}

function renderTop(listEl, items, limit = 10) {
  const top = items.slice(0, limit);
  for (let i = 0; i < top.length; i++) {
    const it = top[i];
    listEl.appendChild(el("li", {}, [
      el("span", { class: "rank" }, [String(i + 1).padStart(2, "0")]),
      el("span", { class: "name" }, [it.name]),
      el("span", { class: "count" }, [`${it.count}`]),
      el("span", { class: "avg" }, [it.avg != null ? it.avg.toFixed(2) : "—"]),
    ]));
  }
}

function topByKey(data, getter) {
  const m = new Map();
  for (const f of data.films) {
    if (!f.watched) continue;
    for (const k of getter(f)) {
      if (!m.has(k)) m.set(k, { name: k, count: 0, ratingSum: 0, ratingN: 0 });
      const e = m.get(k);
      e.count++;
      if (f.rating != null) { e.ratingSum += f.rating; e.ratingN++; }
    }
  }
  return [...m.values()]
    .map(e => ({ ...e, avg: e.ratingN ? e.ratingSum / e.ratingN : null }))
    .sort((a, b) => b.count - a.count || (b.avg || 0) - (a.avg || 0));
}

function renderTopDirectors(data) {
  const list = topByKey(data, directors).filter(e => e.count >= 2);
  renderTop(document.getElementById("top-directors"), list);
}

function renderTopGenres(data) {
  const list = topByKey(data, genres);
  renderTop(document.getElementById("top-genres"), list, 12);
}

function renderHallOfFame(data) {
  const wrap = document.getElementById("five-star");
  const five = data.films
    .filter(f => f.rating === 5)
    .sort((a, b) => (year(b) || 0) - (year(a) || 0));
  if (!five.length) {
    wrap.appendChild(el("p", { class: "muted" }, ["no perfect ratings yet."]));
    return;
  }
  for (const f of five) wrap.appendChild(posterCard(f, { rating: f.rating }));
}

function renderLanguages(data) {
  const m = new Map();
  for (const f of data.films) {
    if (!f.watched) continue;
    const lang = f.tmdb && f.tmdb.original_language;
    if (!lang) continue;
    m.set(lang, (m.get(lang) || 0) + 1);
  }
  const total = [...m.values()].reduce((a, b) => a + b, 0) || 1;
  const NAMES = {
    en: "English", ja: "Japanese", fr: "French", es: "Spanish", de: "German",
    it: "Italian", ko: "Korean", zh: "Chinese", cn: "Chinese", da: "Danish",
    sv: "Swedish", ru: "Russian", pl: "Polish", pt: "Portuguese", nl: "Dutch",
    no: "Norwegian", fi: "Finnish", hi: "Hindi", th: "Thai", tr: "Turkish",
    fa: "Persian", he: "Hebrew", ar: "Arabic", cs: "Czech", el: "Greek",
    hu: "Hungarian", ro: "Romanian", is: "Icelandic", id: "Indonesian",
  };
  const rows = [...m.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([k, count]) => ({
      name: NAMES[k] || k.toUpperCase(),
      count,
      avg: count / total,
    }));
  const list = document.getElementById("language-list");
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i];
    const pct = (r.avg * 100).toFixed(1) + "%";
    list.appendChild(el("li", {}, [
      el("span", { class: "rank" }, [String(i + 1).padStart(2, "0")]),
      el("span", { class: "name" }, [r.name]),
      el("span", { class: "count" }, [String(r.count)]),
      el("span", { class: "avg" }, [pct]),
    ]));
  }
}

function renderMonthChart(data) {
  const counts = new Map();
  for (const f of data.films) {
    for (const d of diaryDates(f)) {
      const m = d.slice(0, 7); // YYYY-MM
      counts.set(m, (counts.get(m) || 0) + 1);
    }
  }
  if (!counts.size) return;
  const months = [...counts.keys()].sort();
  const start = months[0], end = months[months.length - 1];
  const max = Math.max(...counts.values());

  const cur = new Date(start + "-01T00:00:00Z");
  const last = new Date(end + "-01T00:00:00Z");
  const wrap = document.getElementById("month-chart");
  let lastYear = null;
  while (cur <= last) {
    const key = cur.toISOString().slice(0, 7);
    const c = counts.get(key) || 0;
    const yyyy = cur.getUTCFullYear();
    const isYearEdge = yyyy !== lastYear;
    lastYear = yyyy;
    const h = (c / max) * 100;
    const bar = el("div", {
      class: `m${c === 0 ? " empty" : ""}${isYearEdge ? " year-edge" : ""}`,
      style: `height: ${Math.max(h, 2)}%`,
      title: `${key}: ${c} entr${c === 1 ? "y" : "ies"}`,
    });
    if (isYearEdge) bar.setAttribute("data-year", String(yyyy));
    wrap.appendChild(bar);
    cur.setUTCMonth(cur.getUTCMonth() + 1);
  }
}

function renderHotTakes(data) {
  const wrap = document.getElementById("hot-takes");
  const candidates = data.films
    .filter(f => f.rating != null && f.tmdb && f.tmdb.vote_average != null && f.tmdb.vote_average > 0)
    .map(f => {
      const yourScore = f.rating * 2;          // out of 10
      const theirScore = f.tmdb.vote_average;   // out of 10
      return { f, you: f.rating, them: theirScore / 2, delta: yourScore - theirScore };
    });
  // top 8 by absolute delta, prefer 0.5+ stars apart
  const sorted = candidates
    .filter(c => Math.abs(c.delta) >= 1.5)
    .sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta))
    .slice(0, 8);

  if (!sorted.length) {
    wrap.appendChild(el("li", { class: "muted", style: "border:none; padding: 20px 0" }, ["no big disagreements yet."]));
    return;
  }

  for (const c of sorted) {
    const cold = c.delta < 0;
    const p = poster(c.f, "w92");
    const li = el("li", { class: cold ? "cold" : "hot" }, [
      el("a", {
        class: "mini",
        href: c.f.uri, target: "_blank", rel: "noopener",
        style: p ? `background-image:url(${p})` : "",
      }),
      el("div", { class: "info" }, [
        el("div", { class: "t" }, [el("a", {
          href: c.f.uri, target: "_blank", rel: "noopener",
        }, [title(c.f)])]),
        el("div", { class: "y" }, [String(year(c.f) || "")]),
      ]),
      el("div", { class: "delta" + (cold ? " cold" : "") }, [
        el("span", { class: "you" }, [ratingStars(c.you)]),
        el("span", { class: "vs" }, [" vs "]),
        el("span", { class: "tmdb" }, [c.them.toFixed(1)]),
      ]),
    ]);
    wrap.appendChild(li);
  }
}

function renderHiddenGems(data) {
  const wrap = document.getElementById("hidden-gems");
  // films you rated 4+ where TMDB vote is below 7.0 — wider audience underrates them
  const gems = data.films
    .filter(f => f.rating != null && f.rating >= 4 && f.tmdb && f.tmdb.vote_average != null && f.tmdb.vote_average > 0 && f.tmdb.vote_average < 7.0)
    .map(f => ({
      f,
      you: f.rating,
      them: f.tmdb.vote_average / 2,
      score: f.rating - f.tmdb.vote_average / 2,
    }))
    .sort((a, b) => b.score - a.score)
    .slice(0, 8);

  if (!gems.length) {
    wrap.appendChild(el("li", { class: "muted", style: "border:none; padding: 20px 0" }, ["nothing's hiding."]));
    return;
  }
  for (const g of gems) {
    const p = poster(g.f, "w92");
    wrap.appendChild(el("li", {}, [
      el("a", {
        class: "mini",
        href: g.f.uri, target: "_blank", rel: "noopener",
        style: p ? `background-image:url(${p})` : "",
      }),
      el("div", { class: "info" }, [
        el("div", { class: "t" }, [el("a", {
          href: g.f.uri, target: "_blank", rel: "noopener",
        }, [title(g.f)])]),
        el("div", { class: "y" }, [String(year(g.f) || "")]),
      ]),
      el("div", { class: "delta" }, [
        el("span", { class: "you" }, [ratingStars(g.you)]),
        el("span", { class: "vs" }, [" · tmdb "]),
        el("span", { class: "tmdb" }, [g.f.tmdb.vote_average.toFixed(1)]),
      ]),
    ]));
  }
}

function renderSuperlatives(data) {
  const wrap = document.getElementById("superlatives");
  const allDates = [];
  for (const f of data.films) for (const d of diaryDates(f)) allDates.push({ d, f });
  if (!allDates.length) return;

  // most films in a single day
  const byDay = new Map();
  for (const e of allDates) {
    if (!byDay.has(e.d)) byDay.set(e.d, []);
    byDay.get(e.d).push(e.f);
  }
  const busiestDay = [...byDay.entries()].sort((a, b) => b[1].length - a[1].length)[0];

  // most-watched year (calendar year of viewing)
  const byYear = new Map();
  for (const e of allDates) {
    const y = e.d.slice(0, 4);
    byYear.set(y, (byYear.get(y) || 0) + 1);
  }
  const topYear = [...byYear.entries()].sort((a, b) => b[1] - a[1])[0];

  // longest streak of consecutive days
  const days = [...new Set(allDates.map(e => e.d))].sort();
  let bestStreak = 1, currentStreak = 1, streakStart = days[0], bestStart = days[0], bestEnd = days[0];
  for (let i = 1; i < days.length; i++) {
    const prev = new Date(days[i - 1]);
    const cur = new Date(days[i]);
    const diff = (cur - prev) / 86400000;
    if (diff === 1) {
      currentStreak++;
      if (currentStreak > bestStreak) {
        bestStreak = currentStreak;
        bestStart = streakStart;
        bestEnd = days[i];
      }
    } else {
      currentStreak = 1;
      streakStart = days[i];
    }
  }

  // average runtime of watched films
  const runtimes = data.films
    .filter(f => f.watched && f.tmdb && f.tmdb.runtime)
    .map(f => f.tmdb.runtime);
  const avgRuntime = runtimes.length
    ? Math.round(runtimes.reduce((a, b) => a + b, 0) / runtimes.length)
    : null;

  // most prolific decade
  const byDecade = new Map();
  for (const f of data.films) {
    if (!f.watched) continue;
    const d = decade(f);
    if (d == null) continue;
    byDecade.set(d, (byDecade.get(d) || 0) + 1);
  }
  const topDecade = [...byDecade.entries()].sort((a, b) => b[1] - a[1])[0];

  // longest single film watched
  const longest = data.films
    .filter(f => f.watched && f.tmdb && f.tmdb.runtime)
    .sort((a, b) => b.tmdb.runtime - a.tmdb.runtime)[0];

  const items = [
    {
      l: "longest streak",
      v: `${bestStreak} day${bestStreak === 1 ? "" : "s"}`,
      sub: bestStart === bestEnd ? bestStart : `${bestStart} → ${bestEnd}`,
    },
    {
      l: "biggest single day",
      v: `${busiestDay[1].length} films`,
      sub: `${busiestDay[0]} — ${busiestDay[1].slice(0, 2).map(f => title(f)).join(", ")}${busiestDay[1].length > 2 ? "…" : ""}`,
    },
    {
      l: "most-watched year",
      v: `${topYear[0]}`,
      sub: `${topYear[1]} entr${topYear[1] === 1 ? "y" : "ies"}`,
    },
    topDecade ? {
      l: "favorite decade",
      v: `${topDecade[0]}s`,
      sub: `${topDecade[1]} films watched`,
    } : null,
    avgRuntime ? {
      l: "avg runtime",
      v: `${Math.floor(avgRuntime / 60)}h ${avgRuntime % 60}m`,
      sub: `${runtimes.length} films sampled`,
    } : null,
    longest ? {
      l: "longest sit",
      v: `${Math.floor(longest.tmdb.runtime / 60)}h ${longest.tmdb.runtime % 60}m`,
      sub: title(longest),
    } : null,
  ].filter(Boolean);

  for (const it of items) {
    wrap.appendChild(el("div", { class: "super" }, [
      el("div", { class: "l" }, [it.l]),
      el("div", { class: "v" }, [it.v]),
      it.sub ? el("div", { class: "sub" }, [it.sub]) : null,
    ]));
  }
}

(async () => {
  const data = await loadData();
  setHeaderMeta(data.profile);
  renderHeroStats(data);
  renderFavorites(data);
  renderMonthChart(data);
  renderHeatmap(data);
  renderSuperlatives(data);
  renderRatingHist(data);
  renderWatchlistProgress(data);
  renderDecadeChart(data);
  renderTopDirectors(data);
  renderTopGenres(data);
  renderHallOfFame(data);
  renderHotTakes(data);
  renderHiddenGems(data);
  renderLanguages(data);
})();
