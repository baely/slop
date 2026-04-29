#!/usr/bin/env python3
"""Build a static Letterboxd companion site from a Letterboxd export.

Reads CSVs from ../data, enriches each unique film via TMDB (with a JSON cache
on disk), and writes ../dist/data.json plus copies the HTML/CSS/JS shell.

Usage:
    TMDB_TOKEN=... python3 build.py
"""

from __future__ import annotations

import csv
import json
import os
import shutil
import ssl
import sys
import time
import urllib.parse
import urllib.request
from collections import defaultdict
from pathlib import Path

try:
    import certifi
    SSL_CTX = ssl.create_default_context(cafile=certifi.where())
except ImportError:
    SSL_CTX = ssl.create_default_context()

ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = ROOT / "data"
DIST_DIR = ROOT / "dist"
WEB_DIR = ROOT / "web"
CACHE_PATH = ROOT / "build" / "tmdb-cache.json"

TMDB_BASE = "https://api.themoviedb.org/3"
TMDB_IMG = "https://image.tmdb.org/t/p"


def read_csv(path: Path) -> list[dict[str, str]]:
    if not path.exists():
        return []
    with path.open(newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def boxd_id(uri: str) -> str:
    """Extract the trailing path segment of a https://boxd.it/XXXX url."""
    return uri.rstrip("/").rsplit("/", 1)[-1]


class TMDB:
    def __init__(self, token: str, cache_path: Path):
        self.token = token
        self.cache_path = cache_path
        self.cache: dict[str, dict] = {}
        if cache_path.exists():
            self.cache = json.loads(cache_path.read_text())
        self.dirty = False

    def save(self) -> None:
        if self.dirty:
            self.cache_path.write_text(json.dumps(self.cache, indent=2, sort_keys=True))
            self.dirty = False

    def _get(self, path: str, params: dict) -> dict:
        qs = urllib.parse.urlencode(params)
        url = f"{TMDB_BASE}{path}?{qs}"
        req = urllib.request.Request(
            url,
            headers={
                "Authorization": f"Bearer {self.token}",
                "Accept": "application/json",
            },
        )
        for attempt in range(3):
            try:
                with urllib.request.urlopen(req, timeout=20, context=SSL_CTX) as resp:
                    return json.loads(resp.read())
            except urllib.error.HTTPError as e:
                if e.code == 429:
                    time.sleep(2 ** attempt)
                    continue
                if e.code == 404:
                    return {}
                raise
            except Exception:
                if attempt == 2:
                    raise
                time.sleep(1 + attempt)
        return {}

    def lookup(self, title: str, year: int | None, key: str) -> dict | None:
        """Return enriched film dict, cached by boxd id."""
        if key in self.cache:
            return self.cache[key]

        params = {"query": title, "include_adult": "false"}
        if year:
            params["year"] = str(year)
        search = self._get("/search/movie", params)
        results = search.get("results") or []
        if not results and year:
            # retry without year filter; some entries are off by 1
            results = (self._get("/search/movie", {"query": title}).get("results") or [])

        if not results:
            self.cache[key] = {}
            self.dirty = True
            self.save()
            return self.cache[key]

        # prefer exact-year match if available
        match = results[0]
        if year:
            for r in results:
                rdate = r.get("release_date") or ""
                if rdate.startswith(str(year)):
                    match = r
                    break

        details = self._get(
            f"/movie/{match['id']}",
            {"append_to_response": "credits"},
        )

        directors = [
            c["name"]
            for c in (details.get("credits") or {}).get("crew", [])
            if c.get("job") == "Director"
        ]
        cast = [
            c["name"]
            for c in (details.get("credits") or {}).get("cast", [])[:5]
        ]

        enriched = {
            "tmdb_id": details.get("id"),
            "title": details.get("title") or match.get("title"),
            "original_title": details.get("original_title"),
            "release_date": details.get("release_date"),
            "runtime": details.get("runtime"),
            "overview": details.get("overview"),
            "tagline": details.get("tagline"),
            "poster": details.get("poster_path"),
            "backdrop": details.get("backdrop_path"),
            "genres": [g["name"] for g in details.get("genres", [])],
            "directors": directors,
            "cast": cast,
            "vote_average": details.get("vote_average"),
            "original_language": details.get("original_language"),
            "production_countries": [
                c.get("iso_3166_1") for c in details.get("production_countries", [])
            ],
        }
        self.cache[key] = enriched
        self.dirty = True
        # Periodic save in case we crash mid-run.
        if len(self.cache) % 25 == 0:
            self.save()
        return enriched


def build_films() -> tuple[dict[str, dict], dict]:
    """Merge all CSVs into a per-film dict keyed by (title, year).

    Letterboxd uses two URI formats: short canonical IDs in ratings/
    watchlist/likes (e.g. /29VI), and per-log IDs in watched/diary
    (e.g. /1CdmJX), so we can't dedupe by URI alone.
    """
    profile_rows = read_csv(DATA_DIR / "profile.csv")
    profile = profile_rows[0] if profile_rows else {}
    fav_uris = [u.strip() for u in (profile.get("Favorite Films") or "").split(",") if u.strip()]
    fav_ids = {boxd_id(u) for u in fav_uris}

    films: dict[str, dict] = {}

    def make_key(title: str, year: str) -> str:
        y = year if year and year.isdigit() else "0"
        return f"{title.strip().lower()}|{y}"

    def slot(uri: str, title: str, year: str) -> dict:
        key = make_key(title, year)
        bid = boxd_id(uri)
        f = films.get(key)
        if f is None:
            f = {
                "id": key,
                "uri": uri,
                "letterboxd_title": title,
                "letterboxd_year": int(year) if year and year.isdigit() else None,
                "rating": None,
                "watched": False,
                "watched_dates": [],
                "diary_dates": [],
                "diary_logged_dates": [],
                "rewatch": False,
                "tags": [],
                "watchlist": False,
                "liked": False,
                "favorite": bid in fav_ids,
                "_seen_ids": {bid},
            }
            films[key] = f
        else:
            f["_seen_ids"].add(bid)
            # prefer the canonical short URI when we see one (matches favorites)
            if bid in fav_ids:
                f["uri"] = uri
                f["favorite"] = True
        return f

    for row in read_csv(DATA_DIR / "watched.csv"):
        f = slot(row["Letterboxd URI"], row["Name"], row["Year"])
        f["watched"] = True
        # the date in watched.csv is the date added (logged), not necessarily watched
        if row.get("Date"):
            f["watched_dates"].append(row["Date"])

    for row in read_csv(DATA_DIR / "ratings.csv"):
        f = slot(row["Letterboxd URI"], row["Name"], row["Year"])
        try:
            f["rating"] = float(row["Rating"])
        except (TypeError, ValueError):
            pass

    for row in read_csv(DATA_DIR / "diary.csv"):
        f = slot(row["Letterboxd URI"], row["Name"], row["Year"])
        if row.get("Watched Date"):
            f["diary_dates"].append(row["Watched Date"])
        if row.get("Date"):
            f["diary_logged_dates"].append(row["Date"])
        if row.get("Rewatch", "").lower() in ("yes", "true", "1"):
            f["rewatch"] = True
        if row.get("Tags"):
            f["tags"].extend([t.strip() for t in row["Tags"].split(",") if t.strip()])
        if row.get("Rating"):
            try:
                f["rating"] = float(row["Rating"])
            except ValueError:
                pass

    for row in read_csv(DATA_DIR / "watchlist.csv"):
        f = slot(row["Letterboxd URI"], row["Name"], row["Year"])
        f["watchlist"] = True

    for row in read_csv(DATA_DIR / "likes" / "films.csv"):
        f = slot(row["Letterboxd URI"], row["Name"], row["Year"])
        f["liked"] = True

    return films, profile


def main() -> int:
    token = os.environ.get("TMDB_TOKEN")
    if not token:
        print("error: TMDB_TOKEN env var required", file=sys.stderr)
        return 1

    films, profile = build_films()
    print(f"merged {len(films)} unique films from CSVs")

    tmdb = TMDB(token, CACHE_PATH)

    enriched_count = 0
    skipped = 0
    for i, (key, f) in enumerate(sorted(films.items()), 1):
        meta = tmdb.lookup(f["letterboxd_title"], f["letterboxd_year"], key)
        if meta:
            f["tmdb"] = meta
            enriched_count += 1
        else:
            skipped += 1
        if i % 25 == 0:
            print(f"  {i}/{len(films)}  enriched={enriched_count} skipped={skipped}", flush=True)
        # _seen_ids is for build-time dedup only; drop before serializing
        f.pop("_seen_ids", None)
    tmdb.save()
    print(f"tmdb enrichment: {enriched_count} matched, {skipped} unmatched")

    DIST_DIR.mkdir(parents=True, exist_ok=True)
    payload = {
        "profile": {
            "username": profile.get("Username"),
            "joined": profile.get("Date Joined"),
            "given_name": profile.get("Given Name"),
            "favorite_film_ids": list({boxd_id(u) for u in (profile.get("Favorite Films") or "").split(",") if u.strip()}),
        },
        "generated_at": time.strftime("%Y-%m-%d"),
        "films": list(films.values()),
        "tmdb_image_base": TMDB_IMG,
    }
    (DIST_DIR / "data.json").write_text(json.dumps(payload, separators=(",", ":")))
    print(f"wrote dist/data.json ({(DIST_DIR / 'data.json').stat().st_size // 1024} KB)")

    if WEB_DIR.exists():
        for src in WEB_DIR.iterdir():
            dst = DIST_DIR / src.name
            if src.is_dir():
                if dst.exists():
                    shutil.rmtree(dst)
                shutil.copytree(src, dst)
            else:
                shutil.copy2(src, dst)
        print("copied web/ shell into dist/")
    return 0


if __name__ == "__main__":
    sys.exit(main())
