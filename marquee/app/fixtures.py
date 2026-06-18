"""Demo fixtures + a simulated Radarr loop.

Active only when TMDB / Radarr aren't configured. Lets the whole app — including
the discover -> want -> grab -> available loop — be explored with no backend.
Films you "send to Radarr" in demo mode visibly progress over ~25s:
    wanted -> grabbing (climbing %) -> available.
"""
import time

def _f(id, title, year, director, runtime, genres, overview, vote=None):
    return {"tmdb_id": id, "title": title, "year": year, "poster": None, "backdrop": None,
            "overview": overview, "runtime": runtime, "director": director,
            "genres": genres, "tagline": None, "vote": vote}

DEMO_FILMS = {f["tmdb_id"]: f for f in [
    _f(496243, "Parasite", 2019, "Bong Joon-ho", 132, ["Thriller", "Drama"],
       "A poor family schemes to become employed by a wealthy household.", 8.5),
    _f(843, "In the Mood for Love", 2000, "Wong Kar-wai", 98, ["Drama", "Romance"],
       "Two neighbours form a bond after suspecting their spouses of affairs.", 8.1),
    _f(244786, "Whiplash", 2014, "Damien Chazelle", 106, ["Drama", "Music"],
       "A young drummer is pushed to his limit by a ruthless instructor.", 8.4),
    _f(313369, "La La Land", 2016, "Damien Chazelle", 128, ["Comedy", "Drama", "Romance"],
       "A jazz pianist and an aspiring actress fall in love in Los Angeles.", 7.9),
    _f(531428, "Portrait of a Lady on Fire", 2019, "Céline Sciamma", 122, ["Drama", "Romance"],
       "On an isolated island, a painter is commissioned to paint a young bride.", 8.1),
    _f(503919, "Burning", 2018, "Lee Chang-dong", 148, ["Drama", "Mystery"],
       "A deliveryman becomes entangled with an old friend and a wealthy stranger.", 7.5),
    _f(614934, "Drive My Car", 2021, "Ryusuke Hamaguchi", 179, ["Drama"],
       "A grieving actor and his quiet chauffeur drive toward an understanding.", 7.6),
    _f(705996, "Decision to Leave", 2022, "Park Chan-wook", 139, ["Crime", "Drama", "Romance"],
       "A detective investigating a death becomes drawn to the widow.", 7.5),
    _f(666277, "Past Lives", 2023, "Celine Song", 105, ["Drama", "Romance"],
       "Two childhood friends reunite decades later for one fateful week.", 7.8),
    _f(947813, "Aftersun", 2022, "Charlotte Wells", 102, ["Drama"],
       "A woman recalls a sunlit holiday with her father twenty years on.", 7.7),
    _f(467244, "The Zone of Interest", 2023, "Jonathan Glazer", 105, ["Drama", "History", "War"],
       "A commandant builds an idyllic life beside the wall of Auschwitz.", 7.4),
    _f(915935, "Anatomy of a Fall", 2023, "Justine Triet", 151, ["Crime", "Drama", "Thriller"],
       "A woman stands trial over the ambiguous death of her husband.", 7.7),
    _f(792307, "Poor Things", 2023, "Yorgos Lanthimos", 141, ["Science Fiction", "Romance", "Comedy"],
       "A woman brought back to life embarks on a whirlwind of self-discovery.", 7.9),
    _f(817758, "Tár", 2022, "Todd Field", 158, ["Drama", "Music"],
       "A celebrated conductor's life unravels at the height of her powers.", 7.4),
    _f(10494, "Perfect Blue", 1997, "Satoshi Kon", 81, ["Animation", "Thriller", "Horror"],
       "A pop idol turned actress loses her grip on what is real.", 8.2),
    _f(11104, "Chungking Express", 1994, "Wong Kar-wai", 102, ["Drama", "Romance", "Comedy"],
       "Two lovesick policemen drift through neon-lit Hong Kong.", 8.0),
    _f(1084736, "Anora", 2024, "Sean Baker", 139, ["Comedy", "Drama", "Romance"],
       "A Brooklyn escort marries the son of a Russian oligarch.", 7.1),
    _f(933260, "The Substance", 2024, "Coralie Fargeat", 141, ["Horror", "Science Fiction"],
       "A fading star uses a black-market drug that creates a younger self.", 7.3),
]}

_LIBRARY_IDS = [496243, 843, 244786, 313369, 531428, 503919]   # hasFile
_PRESET_DOWNLOADING = {614934: 46, 705996: 81}                 # tmdb_id -> %
_PRESET_WANTED = [666277, 947813]                              # monitored, no file
_SIM = {}  # tmdb_id -> added_at (films sent to Radarr in demo)

# --- TMDB-side fixtures ---------------------------------------------------
def _all():
    return list(DEMO_FILMS.values())

def search(query):
    q = (query or "").strip().lower()
    if not q:
        return _all()[:12]
    hits = [f for f in _all() if q in f["title"].lower()
            or q in (f["director"] or "").lower()
            or any(q in g.lower() for g in f["genres"])]
    return hits or _all()[:8]

def trending():
    return _all()[:18]

def movie(tmdb_id):
    return DEMO_FILMS.get(int(tmdb_id))

# --- Radarr-side simulation ----------------------------------------------
def radarr_add(tmdb_id, film):
    DEMO_FILMS.setdefault(int(tmdb_id), {**film})
    _SIM[int(tmdb_id)] = time.time()

def _sim_state(elapsed):
    if elapsed < 3:
        return "wanted", 0
    if elapsed < 25:
        return "grabbing", min(98, int((elapsed - 3) / 22 * 100))
    return "available", 100

def _movie_obj(tmdb_id, has_file, monitored=True):
    f = DEMO_FILMS.get(tmdb_id, {})
    return {
        "id": tmdb_id, "tmdbId": tmdb_id, "title": f.get("title", "Unknown"),
        "year": f.get("year"), "hasFile": has_file, "monitored": monitored,
        "sizeOnDisk": 4_800_000_000 if has_file else 0,
        "runtime": f.get("runtime"), "genres": f.get("genres", []),
        "overview": f.get("overview"), "images": [{"coverType": "poster", "remoteUrl": None}],
    }

def radarr_movies():
    out = [_movie_obj(i, True) for i in _LIBRARY_IDS]
    out += [_movie_obj(i, False) for i in _PRESET_WANTED]
    out += [_movie_obj(i, False) for i in _PRESET_DOWNLOADING]
    for tid, added in _SIM.items():
        state, _ = _sim_state(time.time() - added)
        out.append(_movie_obj(tid, state == "available"))
    # de-dupe by tmdbId, last wins
    seen = {}
    for m in out:
        seen[m["tmdbId"]] = m
    return list(seen.values())

def _queue_record(tmdb_id, pct):
    size = 5_000_000_000
    return {"movieId": tmdb_id, "size": size, "sizeleft": int(size * (1 - pct / 100)),
            "timeleft": f"00:{max(1, 30 - pct // 4):02d}:00", "status": "downloading"}

def radarr_queue():
    out = [_queue_record(tid, pct) for tid, pct in _PRESET_DOWNLOADING.items()]
    for tid, added in _SIM.items():
        state, pct = _sim_state(time.time() - added)
        if state == "grabbing":
            out.append(_queue_record(tid, pct))
    return out
