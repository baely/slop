import {
  loadData, el, setHeaderMeta, ratingStars,
  title, year, runtime, directors, genres, diaryDates,
} from "/lib.js";

let DATA = null;
let STATE = {
  q: "",
  status: "all",
  sort: "title",
  dir: 1,
  page: 0,
};
const PAGE_SIZE = 50;

function searchableText(f) {
  const t = title(f).toLowerCase();
  const dirs = directors(f).join(" ").toLowerCase();
  const gs = genres(f).join(" ").toLowerCase();
  return `${t} ${dirs} ${gs}`;
}

function filtered() {
  const q = STATE.q.trim().toLowerCase();
  let out = DATA.films.filter(f => {
    if (STATE.status === "watched" && !f.watched) return false;
    if (STATE.status === "rated" && f.rating == null) return false;
    if (STATE.status === "watchlist" && !f.watchlist) return false;
    if (STATE.status === "liked" && !f.liked) return false;
    if (STATE.status === "favorite" && !f.favorite) return false;
    if (q && !searchableText(f).includes(q)) return false;
    return true;
  });

  const dir = STATE.dir;
  const cmp = {
    title: (a, b) => title(a).localeCompare(title(b)),
    year: (a, b) => (year(a) || 0) - (year(b) || 0),
    rating: (a, b) => (a.rating ?? -1) - (b.rating ?? -1),
    runtime: (a, b) => (runtime(a) || 0) - (runtime(b) || 0),
    director: (a, b) => (directors(a)[0] || "").localeCompare(directors(b)[0] || ""),
    genre: (a, b) => (genres(a)[0] || "").localeCompare(genres(b)[0] || ""),
    watched_date: (a, b) => {
      const da = diaryDates(a)[0] || "";
      const db = diaryDates(b)[0] || "";
      return da.localeCompare(db);
    },
  }[STATE.sort];
  out.sort((a, b) => dir * cmp(a, b));
  return out;
}

function row(f) {
  const dirs = directors(f);
  const gs = genres(f);
  const dates = diaryDates(f);
  const r = el("tr", { class: "film-row" });

  r.appendChild(el("td", { class: "title-cell" }, [
    el("a", { href: f.uri, target: "_blank", rel: "noopener" }, [title(f)]),
    statusPills(f),
  ]));
  r.appendChild(el("td", { class: "num" }, [year(f) ? String(year(f)) : "—"]));
  r.appendChild(el("td", { class: "num rating" }, [
    f.rating != null ? ratingStars(f.rating) : "—",
  ]));
  r.appendChild(el("td", { class: "num" }, [runtime(f) ? `${runtime(f)}m` : "—"]));
  r.appendChild(el("td", {}, [dirs.slice(0, 2).join(", ") || "—"]));
  r.appendChild(el("td", { class: "muted" }, [gs.slice(0, 2).join(" · ") || "—"]));
  r.appendChild(el("td", { class: "num muted" }, [dates[0] || "—"]));
  return r;
}

function statusPills(f) {
  const wrap = el("span", { class: "pills" });
  if (f.favorite) wrap.appendChild(el("span", { class: "pill pill-fav", title: "favorite" }, ["♥"]));
  if (f.liked) wrap.appendChild(el("span", { class: "pill pill-like", title: "liked" }, ["♡"]));
  if (f.watchlist && !f.watched) wrap.appendChild(el("span", { class: "pill pill-wl", title: "on watchlist" }, ["wl"]));
  return wrap;
}

function render() {
  const list = filtered();
  const total = list.length;
  const start = STATE.page * PAGE_SIZE;
  const slice = list.slice(start, start + PAGE_SIZE);

  document.getElementById("result-count").textContent =
    `${total} film${total === 1 ? "" : "s"}`;

  const tbody = document.getElementById("rows");
  tbody.innerHTML = "";
  for (const f of slice) tbody.appendChild(row(f));
  if (!slice.length) {
    tbody.appendChild(el("tr", {}, [
      el("td", { colspan: 7, class: "muted", style: "text-align:center; padding:40px" }, [
        "no films match.",
      ]),
    ]));
  }

  const pager = document.getElementById("pager");
  pager.innerHTML = "";
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (totalPages > 1) {
    pager.appendChild(el("button", {
      onclick: () => { STATE.page = Math.max(0, STATE.page - 1); render(); },
      disabled: STATE.page === 0,
    }, ["← prev"]));
    pager.appendChild(el("span", { class: "muted" }, [
      `page ${STATE.page + 1} of ${totalPages}`,
    ]));
    pager.appendChild(el("button", {
      onclick: () => { STATE.page = Math.min(totalPages - 1, STATE.page + 1); render(); },
      disabled: STATE.page >= totalPages - 1,
    }, ["next →"]));
  }

  // sort indicator
  document.querySelectorAll("th[data-sort]").forEach(th => {
    th.classList.toggle("sorted", th.dataset.sort === STATE.sort);
    th.classList.toggle("sort-desc", th.dataset.sort === STATE.sort && STATE.dir === -1);
  });
}

function setupHandlers() {
  document.getElementById("q").addEventListener("input", e => {
    STATE.q = e.target.value;
    STATE.page = 0;
    render();
  });
  document.querySelectorAll(".tab").forEach(t => {
    t.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach(x => x.classList.remove("active"));
      t.classList.add("active");
      STATE.status = t.dataset.status;
      STATE.page = 0;
      render();
    });
  });
  document.querySelectorAll("th[data-sort]").forEach(th => {
    th.addEventListener("click", () => {
      const key = th.dataset.sort;
      if (STATE.sort === key) STATE.dir *= -1;
      else { STATE.sort = key; STATE.dir = key === "title" ? 1 : -1; }
      STATE.page = 0;
      render();
    });
  });
}

(async () => {
  DATA = await loadData();
  setHeaderMeta(DATA.profile);
  setupHandlers();
  // default: most recently watched at the top
  STATE.sort = "watched_date"; STATE.dir = -1;
  render();
})();
