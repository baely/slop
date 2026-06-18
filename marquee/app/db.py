"""SQLite persistence. Single-user, file-backed, your data only.

Tables
  films     : metadata cache keyed by TMDB id (so we don't refetch constantly)
  watchlist : films you want (the thing that talks to Radarr)
  logs      : diary entries — each viewing, with rating / review / rewatch
"""
import sqlite3
import json
import os
import time
from contextlib import contextmanager
from . import config

_SCHEMA = """
CREATE TABLE IF NOT EXISTS films (
  tmdb_id      INTEGER PRIMARY KEY,
  title        TEXT NOT NULL,
  year         INTEGER,
  poster       TEXT,
  backdrop     TEXT,
  overview     TEXT,
  runtime      INTEGER,
  director     TEXT,
  genres       TEXT,            -- json array
  tagline      TEXT,
  cached_at    REAL
);

CREATE TABLE IF NOT EXISTS watchlist (
  tmdb_id      INTEGER PRIMARY KEY,
  added_at     REAL NOT NULL,
  FOREIGN KEY (tmdb_id) REFERENCES films(tmdb_id)
);

CREATE TABLE IF NOT EXISTS logs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  tmdb_id      INTEGER NOT NULL,
  watched_on   TEXT,            -- yyyy-mm-dd
  rating       REAL,            -- 0.5 .. 5.0
  review       TEXT,
  rewatch      INTEGER DEFAULT 0,
  liked        INTEGER DEFAULT 0,
  created_at   REAL NOT NULL,
  FOREIGN KEY (tmdb_id) REFERENCES films(tmdb_id)
);

CREATE INDEX IF NOT EXISTS idx_logs_film ON logs(tmdb_id);
CREATE INDEX IF NOT EXISTS idx_logs_date ON logs(watched_on);

CREATE TABLE IF NOT EXISTS lb_synced (
  guid         TEXT PRIMARY KEY,        -- Letterboxd RSS item guid
  synced_at    REAL NOT NULL
);
"""


def init():
    os.makedirs(os.path.dirname(os.path.abspath(config.DB_PATH)), exist_ok=True)
    with connect() as c:
        c.executescript(_SCHEMA)


@contextmanager
def connect():
    conn = sqlite3.connect(config.DB_PATH, timeout=10)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys=ON")
    try:
        yield conn
        conn.commit()
    finally:
        conn.close()


# --- film cache -----------------------------------------------------------
def upsert_film(f: dict):
    """f: normalized film dict (see tmdb.normalize)."""
    with connect() as c:
        c.execute(
            """INSERT INTO films (tmdb_id,title,year,poster,backdrop,overview,runtime,director,genres,tagline,cached_at)
               VALUES (:tmdb_id,:title,:year,:poster,:backdrop,:overview,:runtime,:director,:genres,:tagline,:cached_at)
               ON CONFLICT(tmdb_id) DO UPDATE SET
                 title=excluded.title, year=excluded.year, poster=excluded.poster,
                 backdrop=excluded.backdrop, overview=excluded.overview, runtime=excluded.runtime,
                 director=excluded.director, genres=excluded.genres, tagline=excluded.tagline,
                 cached_at=excluded.cached_at""",
            {
                "tmdb_id": f["tmdb_id"], "title": f["title"], "year": f.get("year"),
                "poster": f.get("poster"), "backdrop": f.get("backdrop"),
                "overview": f.get("overview"), "runtime": f.get("runtime"),
                "director": f.get("director"), "genres": json.dumps(f.get("genres") or []),
                "tagline": f.get("tagline"), "cached_at": time.time(),
            },
        )


def get_film(tmdb_id: int):
    with connect() as c:
        r = c.execute("SELECT * FROM films WHERE tmdb_id=?", (tmdb_id,)).fetchone()
    return _film_row(r) if r else None


def _film_row(r):
    d = dict(r)
    d["genres"] = json.loads(d.get("genres") or "[]")
    return d


# --- watchlist ------------------------------------------------------------
def add_watchlist(tmdb_id: int):
    with connect() as c:
        c.execute("INSERT OR IGNORE INTO watchlist (tmdb_id, added_at) VALUES (?,?)",
                  (tmdb_id, time.time()))


def remove_watchlist(tmdb_id: int):
    with connect() as c:
        c.execute("DELETE FROM watchlist WHERE tmdb_id=?", (tmdb_id,))


def in_watchlist(tmdb_id: int) -> bool:
    with connect() as c:
        return c.execute("SELECT 1 FROM watchlist WHERE tmdb_id=?", (tmdb_id,)).fetchone() is not None


def watchlist(limit=500):
    with connect() as c:
        rows = c.execute(
            """SELECT f.* FROM watchlist w JOIN films f ON f.tmdb_id=w.tmdb_id
               ORDER BY w.added_at DESC LIMIT ?""", (limit,)).fetchall()
    return [_film_row(r) for r in rows]


def watchlist_ids():
    with connect() as c:
        return [r[0] for r in c.execute("SELECT tmdb_id FROM watchlist").fetchall()]


# --- logs / diary ---------------------------------------------------------
def add_log(tmdb_id, watched_on=None, rating=None, review=None, rewatch=False, liked=False):
    with connect() as c:
        c.execute(
            """INSERT INTO logs (tmdb_id,watched_on,rating,review,rewatch,liked,created_at)
               VALUES (?,?,?,?,?,?,?)""",
            (tmdb_id, watched_on, rating, review, 1 if rewatch else 0, 1 if liked else 0, time.time()),
        )


def delete_log(log_id):
    with connect() as c:
        c.execute("DELETE FROM logs WHERE id=?", (log_id,))


def logs_for(tmdb_id):
    with connect() as c:
        rows = c.execute("SELECT * FROM logs WHERE tmdb_id=? ORDER BY watched_on DESC, id DESC",
                         (tmdb_id,)).fetchall()
    return [dict(r) for r in rows]


def diary(limit=300):
    """Recent diary entries joined with film metadata."""
    with connect() as c:
        rows = c.execute(
            """SELECT l.*, f.title, f.year, f.poster, f.director
               FROM logs l JOIN films f ON f.tmdb_id=l.tmdb_id
               ORDER BY COALESCE(l.watched_on,'') DESC, l.id DESC LIMIT ?""", (limit,)).fetchall()
    return [dict(r) for r in rows]


def watched_ids():
    with connect() as c:
        return {r[0] for r in c.execute("SELECT DISTINCT tmdb_id FROM logs").fetchall()}


def log_exists(tmdb_id, watched_on):
    """Has this exact viewing already been recorded? (dedupe for RSS sync)"""
    with connect() as c:
        return c.execute("SELECT 1 FROM logs WHERE tmdb_id=? AND watched_on=?",
                         (tmdb_id, watched_on)).fetchone() is not None


# --- Letterboxd RSS sync bookkeeping --------------------------------------
def lb_seen(guid):
    with connect() as c:
        return c.execute("SELECT 1 FROM lb_synced WHERE guid=?", (guid,)).fetchone() is not None


def lb_mark(guid):
    with connect() as c:
        c.execute("INSERT OR IGNORE INTO lb_synced (guid, synced_at) VALUES (?,?)", (guid, time.time()))


def last_sync():
    with connect() as c:
        r = c.execute("SELECT MAX(synced_at) FROM lb_synced").fetchone()
        return r[0]


def film_user_state(tmdb_id):
    """Aggregate of the user's relationship to a film."""
    logs = logs_for(tmdb_id)
    rating = next((l["rating"] for l in logs if l["rating"] is not None), None)
    return {
        "watched": len(logs) > 0,
        "watch_count": len(logs),
        "rating": rating,
        "liked": any(l["liked"] for l in logs),
        "in_watchlist": in_watchlist(tmdb_id),
        "logs": logs,
    }


def stats():
    """Numbers for the You page."""
    with connect() as c:
        total_watched = c.execute("SELECT COUNT(DISTINCT tmdb_id) FROM logs").fetchone()[0]
        total_logs = c.execute("SELECT COUNT(*) FROM logs").fetchone()[0]
        rewatches = c.execute("SELECT COUNT(*) FROM logs WHERE rewatch=1").fetchone()[0]
        wl = c.execute("SELECT COUNT(*) FROM watchlist").fetchone()[0]
        ratings = [r[0] for r in c.execute("SELECT rating FROM logs WHERE rating IS NOT NULL").fetchall()]
        dist = {}
        for r in c.execute("SELECT rating, COUNT(*) FROM logs WHERE rating IS NOT NULL GROUP BY rating").fetchall():
            dist[str(r[0])] = r[1]
        # genre + decade tallies from cached films that have been logged
        genre_rows = c.execute(
            """SELECT f.genres, f.year FROM films f
               WHERE f.tmdb_id IN (SELECT DISTINCT tmdb_id FROM logs)""").fetchall()
    genres, decades = {}, {}
    for gr in genre_rows:
        for g in json.loads(gr[0] or "[]"):
            genres[g] = genres.get(g, 0) + 1
        if gr[1]:
            d = (int(gr[1]) // 10) * 10
            decades[d] = decades.get(d, 0) + 1
    mean = round(sum(ratings) / len(ratings), 2) if ratings else None
    return {
        "total_watched": total_watched, "total_logs": total_logs, "rewatches": rewatches,
        "watchlist": wl, "mean_rating": mean, "rating_dist": dist,
        "top_genres": sorted(genres.items(), key=lambda x: -x[1])[:8],
        "decades": sorted(decades.items()),
    }
