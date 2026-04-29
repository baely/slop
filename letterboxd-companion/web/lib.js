// shared utilities
export async function loadData() {
  const res = await fetch("/data.json", { cache: "no-cache" });
  return res.json();
}

export function poster(film, size = "w185") {
  const path = film.tmdb && film.tmdb.poster;
  if (!path) return null;
  return `https://image.tmdb.org/t/p/${size}${path}`;
}

export function backdrop(film, size = "w1280") {
  const path = film.tmdb && film.tmdb.backdrop;
  if (!path) return null;
  return `https://image.tmdb.org/t/p/${size}${path}`;
}

export function lbUrl(film) { return film.uri; }

export function title(film) {
  return (film.tmdb && film.tmdb.title) || film.letterboxd_title;
}
export function year(film) {
  if (film.tmdb && film.tmdb.release_date) return Number(film.tmdb.release_date.slice(0, 4));
  return film.letterboxd_year;
}
export function decade(film) {
  const y = year(film);
  return y ? Math.floor(y / 10) * 10 : null;
}
export function runtime(film) { return film.tmdb && film.tmdb.runtime; }
export function directors(film) { return (film.tmdb && film.tmdb.directors) || []; }
export function genres(film) { return (film.tmdb && film.tmdb.genres) || []; }

export function ratingStars(r) {
  if (r == null) return "";
  const full = Math.floor(r);
  const half = (r - full) >= 0.5;
  return "★".repeat(full) + (half ? "½" : "");
}

export function fmtNum(n) { return Number(n).toLocaleString(); }

export function diaryDates(film) {
  // prefer "Watched Date" entries from diary, else fall back to first-seen
  if (film.diary_dates && film.diary_dates.length) return film.diary_dates;
  if (film.diary_logged_dates && film.diary_logged_dates.length) return film.diary_logged_dates;
  return [];
}

export function el(tag, attrs = {}, children = []) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") e.className = v;
    else if (k === "style") e.style.cssText = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (k === "html") e.innerHTML = v;
    else if (v != null && v !== false) e.setAttribute(k, v);
  }
  for (const c of [].concat(children)) {
    if (c == null) continue;
    e.appendChild(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return e;
}

export function posterCard(film, opts = {}) {
  const p = poster(film, opts.size || "w185");
  const t = title(film);
  const y = year(film);
  const a = el("a", { class: "poster", href: lbUrl(film), target: "_blank", rel: "noopener" });
  if (p) a.appendChild(el("img", { src: p, alt: `${t} poster`, loading: "lazy" }));
  else a.appendChild(el("div", { class: "ph" }, [t]));
  a.appendChild(el("div", { class: "t" }, [t]));
  if (y) a.appendChild(el("div", { class: "y" }, [String(y)]));
  if (opts.rating != null) a.appendChild(el("div", { class: "r" }, [ratingStars(opts.rating)]));
  return a;
}

export function setHeaderMeta(profile) {
  const meta = document.getElementById("header-meta");
  if (meta && profile && profile.username) {
    meta.textContent = `@${profile.username} — joined ${profile.joined}`;
  }
  const footer = document.getElementById("site-footer");
  if (footer) {
    footer.innerHTML = "";
    footer.appendChild(el("span", {}, [`@${profile.username || ""}`]));
    footer.appendChild(el("span", {}, ["data via letterboxd export + tmdb"]));
  }
}
