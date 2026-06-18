"""Marquee — a personal cinema you operate.

Discover -> want -> Radarr grabs it -> lands in your library -> watch -> log it.
FastAPI + SQLite, single-user, your data. TMDB for metadata, Radarr for the loop.
"""
import os
from fastapi import FastAPI, Request, Form
from fastapi.responses import JSONResponse, RedirectResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates

import asyncio
from . import db, tmdb, radarr, config, sync

BASE_DIR = os.path.dirname(__file__)
app = FastAPI(title="Marquee")
app.mount("/static", StaticFiles(directory=os.path.join(BASE_DIR, "static")), name="static")
templates = Jinja2Templates(directory=os.path.join(BASE_DIR, "templates"))


@app.on_event("startup")
def _startup():
    db.init()


@app.on_event("startup")
async def _start_poller():
    """Auto-pull new Letterboxd activity on an interval (best-effort)."""
    if not (config.SYNC_ENABLED and config.SYNC_INTERVAL_MIN > 0):
        return

    async def loop():
        await asyncio.sleep(3)  # let startup settle, then sync immediately
        while True:
            try:
                res = await asyncio.to_thread(sync.run)
                if res.get("new"):
                    print(f"[sync] filed {res['new']} new entr{'y' if res['new']==1 else 'ies'} from Letterboxd")
            except Exception as e:  # noqa: BLE001
                print("[sync] error:", e)
            await asyncio.sleep(config.SYNC_INTERVAL_MIN * 60)

    asyncio.create_task(loop())


def ctx(request, **kw):
    base = {
        "request": request, "app_name": config.APP_NAME, "owner": config.OWNER,
        "radarr_health": radarr.health(), "tmdb_enabled": config.TMDB_ENABLED,
        "demo": config.DEMO_MODE, "nav": kw.pop("nav", ""),
        "sync_enabled": config.SYNC_ENABLED, "last_sync": db.last_sync(),
    }
    base.update(kw)
    return base


def ensure_film(tmdb_id: int):
    """Return cached film metadata, fetching + caching from TMDB if needed.

    A bulk import caches lightweight "stub" records (no director/runtime); the
    first time such a film's record is opened we upgrade it to full detail.
    """
    f = db.get_film(tmdb_id)
    if f and (f.get("runtime") or f.get("director") or not config.TMDB_ENABLED):
        return f
    m = tmdb.movie(tmdb_id)
    if m:
        db.upsert_film(m)
        return m
    return f


# --- pages ----------------------------------------------------------------
@app.get("/")
def home(request: Request):
    trending = tmdb.trending()
    wl = db.watchlist()
    statuses = radarr.status_for([f["tmdb_id"] for f in wl]) if wl else {}
    ready = [f for f in wl if statuses.get(f["tmdb_id"], {}).get("state") == "available"]
    grabbing = [f for f in wl if statuses.get(f["tmdb_id"], {}).get("state") == "grabbing"]
    return templates.TemplateResponse("home.html", ctx(
        request, nav="home", trending=trending, ready=ready, grabbing=grabbing,
        statuses=statuses, stats=db.stats(), watched=db.watched_ids(),
        watchlist_ids=set(db.watchlist_ids()),
    ))


@app.get("/discover")
def discover(request: Request, q: str = ""):
    results = tmdb.search(q) if q else tmdb.trending()
    return templates.TemplateResponse("discover.html", ctx(
        request, nav="discover", q=q, results=results,
        statuses=radarr.status_for([f["tmdb_id"] for f in results]),
        watched=db.watched_ids(), watchlist_ids=set(db.watchlist_ids()),
    ))


@app.get("/watchlist")
def watchlist_page(request: Request):
    wl = db.watchlist()
    statuses = radarr.status_for([f["tmdb_id"] for f in wl]) if wl else {}
    order = {"available": 0, "grabbing": 1, "wanted": 2, "addable": 3}
    wl.sort(key=lambda f: order.get(statuses.get(f["tmdb_id"], {}).get("state"), 9))
    return templates.TemplateResponse("watchlist.html", ctx(
        request, nav="watchlist", films=wl, statuses=statuses,
        watched=db.watched_ids(), watchlist_ids=set(db.watchlist_ids()),
    ))


@app.get("/library")
def library_page(request: Request):
    lib = radarr.library()
    watched = db.watched_ids()
    return templates.TemplateResponse("library.html", ctx(
        request, nav="library", films=lib, watched=watched,
        available=sum(1 for f in lib if f["has_file"]),
    ))


@app.get("/diary")
def diary_page(request: Request):
    return templates.TemplateResponse("diary.html", ctx(
        request, nav="diary", entries=db.diary()))


@app.get("/you")
def you_page(request: Request):
    return templates.TemplateResponse("you.html", ctx(
        request, nav="you", stats=db.stats(), diary=db.diary(limit=12)))


@app.get("/film/{tmdb_id}")
def film_page(request: Request, tmdb_id: int):
    film = ensure_film(tmdb_id)
    if not film:
        return RedirectResponse("/discover")
    state = db.film_user_state(tmdb_id)
    status = radarr.status_for([tmdb_id]).get(tmdb_id, {})
    return templates.TemplateResponse("film.html", ctx(
        request, nav="", film=film, state=state, status=status))


# --- actions (JSON) -------------------------------------------------------
@app.post("/api/film/{tmdb_id}/watchlist")
def toggle_watchlist(tmdb_id: int):
    ensure_film(tmdb_id)
    if db.in_watchlist(tmdb_id):
        db.remove_watchlist(tmdb_id)
        return {"in_watchlist": False}
    db.add_watchlist(tmdb_id)
    return {"in_watchlist": True}


@app.post("/api/film/{tmdb_id}/radarr")
def send_to_radarr(tmdb_id: int):
    film = ensure_film(tmdb_id)
    if not film:
        return JSONResponse({"ok": False, "error": "unknown film"}, status_code=404)
    db.add_watchlist(tmdb_id)  # wanting it implies it's on the list
    res = radarr.add(tmdb_id, film)
    status = radarr.status_for([tmdb_id]).get(tmdb_id, {})
    return {"ok": res.get("ok", False), "status": status, **({"error": res["error"]} if res.get("error") else {})}


@app.post("/api/film/{tmdb_id}/log")
def log_film(tmdb_id: int, rating: float = Form(None), review: str = Form(""),
             watched_on: str = Form(None), rewatch: bool = Form(False), liked: bool = Form(False)):
    ensure_film(tmdb_id)
    db.add_log(tmdb_id, watched_on=watched_on or None, rating=rating,
               review=review or None, rewatch=rewatch, liked=liked)
    db.remove_watchlist(tmdb_id)  # watched -> off the watchlist
    return RedirectResponse(f"/film/{tmdb_id}", status_code=303)


@app.post("/api/log/{log_id}/delete")
def remove_log(log_id: int, tmdb_id: int = Form(...)):
    db.delete_log(log_id)
    return RedirectResponse(f"/film/{tmdb_id}", status_code=303)


@app.get("/api/status")
def api_status(ids: str = ""):
    tmdb_ids = [int(x) for x in ids.split(",") if x.strip().isdigit()]
    return radarr.status_for(tmdb_ids)


@app.post("/api/sync")
def api_sync():
    return sync.run()


@app.get("/api/search")
def api_search(q: str = ""):
    results = tmdb.search(q) if q else tmdb.trending()
    statuses = radarr.status_for([f["tmdb_id"] for f in results])
    wl = set(db.watchlist_ids())
    watched = db.watched_ids()
    for f in results:
        f["status"] = statuses.get(f["tmdb_id"], {})
        f["in_watchlist"] = f["tmdb_id"] in wl
        f["watched"] = f["tmdb_id"] in watched
    return {"results": results}


@app.get("/healthz")
def healthz():
    return {"ok": True}
