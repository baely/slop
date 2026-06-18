"""Radarr v3 client — the download loop.

This is what makes Marquee more than a diary: a watchlist item becomes an
instruction. We can ask Radarr to grab a film, then report its live status:

    addable  -> not in Radarr yet
    wanted   -> monitored, searching / missing
    grabbing -> in the download queue (with % progress)
    available-> file on disk, ready to watch

If Radarr isn't configured the whole module degrades to a demo simulation so
the UI still tells the story.
"""
import time
import httpx
from . import config, fixtures

_cache = {"movies": None, "queue": None, "ts": 0.0, "defaults": None}
_TTL = 12  # seconds


def configured() -> bool:
    return config.RADARR_ENABLED


def _client():
    return httpx.Client(
        base_url=f"{config.RADARR_URL}/api/v3",
        headers={"X-Api-Key": config.RADARR_API_KEY},
        timeout=12,
    )


def health() -> dict:
    if not configured():
        return {"ok": False, "demo": True, "msg": "Radarr not configured — showing a simulated loop."}
    try:
        with _client() as c:
            r = c.get("/system/status")
            r.raise_for_status()
            j = r.json()
            return {"ok": True, "demo": False, "version": j.get("version")}
    except Exception as e:  # noqa: BLE001
        return {"ok": False, "demo": False, "msg": str(e)}


def _defaults():
    if _cache["defaults"]:
        return _cache["defaults"]
    qp = config.RADARR_QUALITY_PROFILE_ID
    root = config.RADARR_ROOT_FOLDER
    with _client() as c:
        if not qp:
            profiles = c.get("/qualityProfile").json()
            qp = profiles[0]["id"] if profiles else 1
        if not root:
            folders = c.get("/rootFolder").json()
            root = folders[0]["path"] if folders else "/movies"
    _cache["defaults"] = {"quality_profile_id": int(qp), "root_folder": root}
    return _cache["defaults"]


def _refresh():
    """Pull movie list + queue with a short TTL cache."""
    now = time.time()
    if not configured():
        # Demo simulation is time-based; recompute each call so progress is smooth.
        _cache["movies"], _cache["queue"], _cache["ts"] = fixtures.radarr_movies(), fixtures.radarr_queue(), now
        return
    if _cache["movies"] is not None and now - _cache["ts"] < _TTL:
        return
    try:
        with _client() as c:
            movies = c.get("/movie").json()
            queue = c.get("/queue", params={"includeMovie": "true", "pageSize": "200"}).json().get("records", [])
        _cache["movies"], _cache["queue"], _cache["ts"] = movies, queue, now
    except Exception:
        # Radarr unreachable: degrade gracefully rather than 500 the whole app.
        # Keep any stale cache; otherwise treat the library as empty and retry soon.
        if _cache["movies"] is None:
            _cache["movies"], _cache["queue"] = [], []
        _cache["ts"] = now - _TTL + 3  # retry in ~3s


def _by_tmdb():
    _refresh()
    return {m["tmdbId"]: m for m in (_cache["movies"] or [])}


def _queue_by_movie():
    _refresh()
    out = {}
    for q in (_cache["queue"] or []):
        mid = q.get("movieId")
        if mid is not None:
            out[mid] = q
    return out


def status_for(tmdb_ids):
    """Return {tmdb_id: {state, progress, ...}} for the loop badges."""
    movies = _by_tmdb()
    queue = _queue_by_movie()
    out = {}
    for tid in tmdb_ids:
        m = movies.get(tid)
        if not m:
            out[tid] = {"state": "addable", "label": "Add to Radarr"}
            continue
        if m.get("hasFile"):
            size_gb = round((m.get("sizeOnDisk") or 0) / 1_073_741_824, 1)
            out[tid] = {"state": "available", "label": "In your library", "radarr_id": m["id"], "size_gb": size_gb}
            continue
        q = queue.get(m["id"])
        if q:
            size = q.get("size") or 0
            left = q.get("sizeleft") or 0
            pct = round((1 - left / size) * 100) if size else 0
            out[tid] = {
                "state": "grabbing", "label": "Downloading", "radarr_id": m["id"],
                "progress": pct, "timeleft": q.get("timeleft"),
            }
            continue
        out[tid] = {"state": "wanted", "label": "Wanted", "radarr_id": m["id"]}
    return out


def add(tmdb_id: int, film: dict):
    """Ask Radarr to monitor + search for a film."""
    if not configured():
        fixtures.radarr_add(tmdb_id, film)
        _cache["ts"] = 0  # force refresh
        return {"ok": True, "demo": True}
    try:
        d = _defaults()
    except Exception as e:  # noqa: BLE001
        return {"ok": False, "error": f"radarr unreachable: {e}"}
    body = {
        "title": film["title"],
        "year": film.get("year") or 0,
        "tmdbId": tmdb_id,
        "qualityProfileId": d["quality_profile_id"],
        "rootFolderPath": d["root_folder"],
        "monitored": True,
        "minimumAvailability": config.RADARR_MIN_AVAILABILITY,
        "addOptions": {"searchForMovie": True},
    }
    try:
        with _client() as c:
            r = c.post("/movie", json=body)
            if r.status_code >= 400:
                return {"ok": False, "error": r.text}
    except Exception as e:  # noqa: BLE001
        return {"ok": False, "error": f"radarr unreachable: {e}"}
    _cache["ts"] = 0
    return {"ok": True}


def library():
    """All movies Radarr knows about, normalized for the library grid."""
    _refresh()
    out = []
    for m in (_cache["movies"] or []):
        poster = None
        for im in m.get("images", []):
            if im.get("coverType") == "poster":
                poster = im.get("remoteUrl") or im.get("url")
                break
        out.append({
            "tmdb_id": m.get("tmdbId"),
            "title": m.get("title"),
            "year": m.get("year"),
            "poster": poster,
            "has_file": bool(m.get("hasFile")),
            "runtime": m.get("runtime"),
            "genres": m.get("genres", []),
            "overview": m.get("overview"),
        })
    out.sort(key=lambda x: (not x["has_file"], x["title"] or ""))
    return out
