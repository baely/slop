"""Import a Letterboxd export into Marquee.

Letterboxd exports identify films only by title + year (no TMDB id), so we
resolve each against TMDB search, cache a lightweight stub, then build the
diary (logs), ratings, likes, reviews and watchlist from the CSVs.

Usage:
    python scripts/import_letterboxd.py /path/to/letterboxd/export [--keep]

Run from the marquee/ directory with the same env as the app (TMDB_BEARER set).
"""
import csv, os, sys, time
from concurrent.futures import ThreadPoolExecutor
import httpx

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from app import config, db  # noqa: E402

EXPORT = next((a for a in sys.argv[1:] if not a.startswith("-")), "../letterboxd-report/data")
KEEP = "--keep" in sys.argv
IMG = "https://image.tmdb.org/t/p"


def rows(name):
    p = os.path.join(EXPORT, name)
    if not os.path.exists(p):
        return []
    with open(p, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def key(name, year):
    return (name.strip().lower(), str(year).strip())


# --- TMDB resolution ------------------------------------------------------
_headers = {"accept": "application/json"}
if config.TMDB_BEARER:
    _headers["Authorization"] = f"Bearer {config.TMDB_BEARER}"
_params = {} if config.TMDB_BEARER else {"api_key": config.TMDB_API_KEY}
client = httpx.Client(base_url="https://api.themoviedb.org/3", headers=_headers,
                      params=_params, timeout=20)

GENRES = {}
def load_genres():
    try:
        for g in client.get("/genre/movie/list").json().get("genres", []):
            GENRES[g["id"]] = g["name"]
    except Exception as e:
        print("genre map failed:", e)


def img(path, size="w500"):
    return f"{IMG}/{size}{path}" if path else None


def resolve(name, year):
    """Return a stub film dict for the best TMDB match, or None."""
    for attempt in range(3):
        try:
            params = {"query": name, "include_adult": "false"}
            if str(year).isdigit():
                params["primary_release_year"] = year
            r = client.get("/search/movie", params=params)
            if r.status_code == 429:
                time.sleep(1.5); continue
            results = r.json().get("results", [])
            if not results and str(year).isdigit():
                results = client.get("/search/movie", params={"query": name}).json().get("results", [])
                # pick closest year
                results.sort(key=lambda m: abs(int((m.get("release_date") or "0000")[:4] or 0) - int(year)))
            if not results:
                return None
            m = results[0]
            yr = (m.get("release_date") or "")[:4]
            return {
                "tmdb_id": m["id"], "title": m.get("title") or name,
                "year": int(yr) if yr.isdigit() else (int(year) if str(year).isdigit() else None),
                "poster": img(m.get("poster_path")), "backdrop": img(m.get("backdrop_path"), "w1280"),
                "overview": m.get("overview"), "runtime": None, "director": None,
                "genres": [GENRES.get(g) for g in m.get("genre_ids", []) if GENRES.get(g)], "tagline": None,
            }
        except Exception:
            time.sleep(1)
    return None


def main():
    if not os.path.isdir(EXPORT):
        print("export dir not found:", EXPORT); sys.exit(1)
    if not config.TMDB_ENABLED:
        print("TMDB not configured — cannot resolve films."); sys.exit(1)

    watched = rows("watched.csv")
    diary = rows("diary.csv")
    ratings = rows("ratings.csv")
    reviews = rows("reviews.csv")
    watchlist = rows("watchlist.csv")
    likes = rows("likes/films.csv")
    print(f"loaded: watched={len(watched)} diary={len(diary)} ratings={len(ratings)} "
          f"reviews={len(reviews)} watchlist={len(watchlist)} likes={len(likes)}")

    load_genres()

    # universe of unique films to resolve
    universe = {}
    for src in (watched, diary, ratings, watchlist, likes, reviews):
        for r in src:
            universe[key(r["Name"], r["Year"])] = (r["Name"], r["Year"])
    print(f"resolving {len(universe)} unique films against TMDB…")

    resolved = {}
    done = [0]
    def work(item):
        k, (n, y) = item
        resolved[k] = resolve(n, y)
        done[0] += 1
        if done[0] % 75 == 0:
            print(f"  …{done[0]}/{len(universe)}")
    with ThreadPoolExecutor(max_workers=8) as ex:
        list(ex.map(work, list(universe.items())))
    hits = sum(1 for v in resolved.values() if v)
    print(f"resolved {hits}/{len(universe)} ({len(universe)-hits} unmatched)")

    db.init()
    if not KEEP:
        with db.connect() as c:
            c.execute("DELETE FROM logs"); c.execute("DELETE FROM watchlist")
        print("cleared existing logs + watchlist")

    # cache film metadata
    for v in resolved.values():
        if v:
            db.upsert_film(v)

    # lookups
    ratings_map = {key(r["Name"], r["Year"]): r["Rating"] for r in ratings if r.get("Rating")}
    likes_set = {key(r["Name"], r["Year"]) for r in likes}
    rev_by_kd, rev_by_k = {}, {}
    for r in reviews:
        k = key(r["Name"], r["Year"])
        rev_by_kd[(k, r.get("Watched Date"))] = r.get("Review")
        rev_by_k.setdefault(k, r.get("Review"))
    watched_keys = {key(r["Name"], r["Year"]) for r in watched}

    def fnum(x):
        try: return float(x)
        except Exception: return None

    n_logs = 0
    diary_keys = set()
    for d in diary:
        k = key(d["Name"], d["Year"]); f = resolved.get(k)
        if not f: continue
        diary_keys.add(k)
        wd = d.get("Watched Date") or None
        review = rev_by_kd.get((k, wd)) or (rev_by_k.get(k) if k not in diary_keys else None)
        db.add_log(f["tmdb_id"], watched_on=wd, rating=fnum(d.get("Rating")) or fnum(ratings_map.get(k)),
                   review=review, rewatch=(d.get("Rewatch") == "Yes"), liked=k in likes_set)
        n_logs += 1

    # watched / rated but never logged in the diary -> a dateless "seen" entry
    seen_extra = (watched_keys | set(ratings_map)) - diary_keys
    for k in seen_extra:
        f = resolved.get(k)
        if not f: continue
        db.add_log(f["tmdb_id"], watched_on=None, rating=fnum(ratings_map.get(k)),
                   review=rev_by_k.get(k), rewatch=False, liked=k in likes_set)
        n_logs += 1

    # watchlist (only films not already seen)
    n_wl = 0
    for w in watchlist:
        k = key(w["Name"], w["Year"]); f = resolved.get(k)
        if not f or k in watched_keys or k in diary_keys:
            continue
        db.add_watchlist(f["tmdb_id"]); n_wl += 1

    s = db.stats()
    print(f"\nIMPORT COMPLETE\n  logs filed:        {n_logs}\n  watchlist holds:   {n_wl}\n"
          f"  titles viewed:     {s['total_watched']}\n  mean appraisal:    {s['mean_rating']}\n"
          f"  top genres:        {s['top_genres'][:5]}")


if __name__ == "__main__":
    main()
