"""TMDB client — search, trending, and film metadata with posters.

Falls back to bundled fixtures when no API key is configured, so the UI is
always explorable.
"""
import httpx
from . import config, fixtures

BASE = "https://api.themoviedb.org/3"


def _client():
    headers = {"accept": "application/json"}
    params = {}
    if config.TMDB_BEARER:
        headers["Authorization"] = f"Bearer {config.TMDB_BEARER}"
    elif config.TMDB_API_KEY:
        params["api_key"] = config.TMDB_API_KEY
    return httpx.Client(base_url=BASE, headers=headers, params=params, timeout=12)


def img(path, size="w500"):
    if not path:
        return None
    if path.startswith("http"):
        return path
    return f"{config.TMDB_IMG}/{size}{path}"


def normalize(m: dict) -> dict:
    """Normalize a TMDB movie payload to Marquee's film shape."""
    year = None
    rd = m.get("release_date") or ""
    if len(rd) >= 4 and rd[:4].isdigit():
        year = int(rd[:4])
    director = None
    credits = m.get("credits") or {}
    for c in credits.get("crew", []):
        if c.get("job") == "Director":
            director = c.get("name")
            break
    genres = [g["name"] for g in m.get("genres", [])] if m.get("genres") else m.get("genre_names", [])
    return {
        "tmdb_id": m["id"],
        "title": m.get("title") or m.get("name"),
        "year": year,
        "poster": img(m.get("poster_path")),
        "backdrop": img(m.get("backdrop_path"), "w1280"),
        "overview": m.get("overview"),
        "runtime": m.get("runtime"),
        "director": director,
        "genres": genres,
        "tagline": m.get("tagline"),
        "vote": round(m.get("vote_average", 0), 1) if m.get("vote_average") else None,
    }


def search(query: str):
    if not config.TMDB_ENABLED:
        return fixtures.search(query)
    with _client() as c:
        r = c.get("/search/movie", params={"query": query, "include_adult": "false"})
        r.raise_for_status()
        return [normalize(m) for m in r.json().get("results", [])[:24]]


def trending():
    if not config.TMDB_ENABLED:
        return fixtures.trending()
    with _client() as c:
        r = c.get("/trending/movie/week")
        r.raise_for_status()
        return [normalize(m) for m in r.json().get("results", [])[:18]]


def movie(tmdb_id: int):
    if not config.TMDB_ENABLED:
        return fixtures.movie(tmdb_id)
    with _client() as c:
        r = c.get(f"/movie/{tmdb_id}", params={"append_to_response": "credits"})
        r.raise_for_status()
        return normalize(r.json())
