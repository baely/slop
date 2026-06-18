"""Ongoing ingestion from Letterboxd via the public per-user RSS feed.

The feed carries the last ~50 diary entries, each with the TMDB id already
embedded (plus rating, watched date, rewatch, like, and review HTML) — so no
title-search resolution is needed. We dedupe on the item guid, and also against
existing logs (tmdb_id + watched_date) so it never double-counts entries that
were already brought in by the bulk import.

Likes ride along on diary entries (`memberLike`); a like added to an old film
*without* a new viewing won't appear in the feed — that's a Letterboxd limit,
not ours.
"""
import re
import httpx
from . import config, db, tmdb

RSS = "https://letterboxd.com/{user}/rss/"


def _tag(tag, s):
    # tolerate attributes, e.g. <guid isPermaLink="false">…</guid>
    m = re.search(rf"<{re.escape(tag)}(?:\s[^>]*)?>(.*?)</{re.escape(tag)}>", s, re.S)
    if not m:
        return None
    v = m.group(1).strip().replace("<![CDATA[", "").replace("]]>", "").strip()
    return v or None


def _review(desc):
    """Description HTML = poster <img> then optional review paragraphs."""
    if not desc:
        return None
    t = re.sub(r"<img[^>]*>", "", desc)
    t = re.sub(r"<[^>]+>", " ", t)
    t = re.sub(r"\s+", " ", t).strip()
    return t or None


def _fnum(x):
    try:
        return float(x)
    except (TypeError, ValueError):
        return None


def run():
    """Pull the feed and file any new viewings. Returns a small summary dict."""
    if not config.SYNC_ENABLED:
        return {"ok": False, "reason": "sync not configured (set LETTERBOXD_USERNAME)"}
    url = RSS.format(user=config.LETTERBOXD_USERNAME)
    try:
        r = httpx.get(url, timeout=20, headers={"User-Agent": "Marquee/1.0"})
        r.raise_for_status()
    except Exception as e:  # noqa: BLE001
        return {"ok": False, "reason": f"feed fetch failed: {e}"}

    items = re.findall(r"<item>(.*?)</item>", r.text, re.S)
    new = 0
    for it in items:
        guid = _tag("guid", it)
        tid = _tag("tmdb:movieId", it)
        if not guid or not tid:
            continue                       # list/activity items without a film
        if db.lb_seen(guid):
            continue
        tid = int(tid)
        watched = _tag("letterboxd:watchedDate", it)
        if db.log_exists(tid, watched):    # already imported (bulk) — just remember it
            db.lb_mark(guid)
            continue
        if not db.get_film(tid):           # cache metadata for a film we've never seen
            m = tmdb.movie(tid)
            if m:
                db.upsert_film(m)
        db.add_log(
            tid, watched_on=watched,
            rating=_fnum(_tag("letterboxd:memberRating", it)),
            review=_review(_tag("description", it)),
            rewatch=(_tag("letterboxd:rewatch", it) == "Yes"),
            liked=(_tag("letterboxd:memberLike", it) == "Yes"),
        )
        db.remove_watchlist(tid)           # watching it clears any hold
        db.lb_mark(guid)
        new += 1
    return {"ok": True, "new": new, "scanned": len(items)}
