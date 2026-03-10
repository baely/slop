import json
import math
import os
import threading
import time
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo

from flask import Flask, request, jsonify, send_from_directory
from flask_cors import CORS
import requests as http_requests

app = Flask(__name__, static_folder=".", static_url_path="")
CORS(app)

TRACK_TOKEN = os.environ["TRACK_TOKEN"]
UP_TOKEN = os.environ["UP_TOKEN"]

TRACK_BASE = "https://track.baileys.dev/api"
UP_BASE = "https://api.up.com.au/api/v1"
LASTFM_API_KEY = "caece86e4fab81a547c3cd87f4a4d43d"
LASTFM_USER = "baileynsamrb"
LASTFM_BASE = "https://ws.audioscrobbler.com/2.0/"
LOCAL_TZ = ZoneInfo("Australia/Sydney")
DEVICE_ID = 1

# Office presence config
IBBITOT_URL = os.environ.get("IBBITOT_URL", "")
IBBITOT_API_KEY = os.environ.get("IBBITOT_API_KEY", "")
OFFICE_STATE_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "office_state.json")
OFFICE_DEBOUNCE_SECS = 15 * 60    # 15 minutes
OFFICE_MIN_POINTS = 3
OFFICE_CHECK_INTERVAL = 2 * 60    # 2 minutes

# --- Office presence state management ---

_office_lock = threading.Lock()

_OFFICE_STATE_DEFAULT = {
    "confirmed_state": {
        "location": None,
        "candidate_location": None,
        "candidate_since_epoch": None,
        "candidate_point_count": 0,
    },
}

def _load_office_state():
    try:
        if os.path.exists(OFFICE_STATE_FILE):
            with open(OFFICE_STATE_FILE) as f:
                data = json.load(f)
            state = json.loads(json.dumps(_OFFICE_STATE_DEFAULT))
            state["confirmed_state"].update(data.get("confirmed_state", {}))
            return state
    except Exception as e:
        print(f"office_state load error: {e}")
    return json.loads(json.dumps(_OFFICE_STATE_DEFAULT))

def _save_office_state(state):
    tmp = OFFICE_STATE_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(state, f, indent=2)
    os.replace(tmp, OFFICE_STATE_FILE)

_office_state = _load_office_state()


# Trip detection constants
REGULAR_SPOTS = [
    {"name": "Home", "lat": -37.817416, "lng": 144.995483},
    {"name": "Work", "lat": -37.816086, "lng": 144.961648},
]
REGULAR_RADIUS = 50  # meters
WORK_RADIUS = 175  # meters
TRIP_DEBOUNCE = 15 * 60 * 1000  # 15 minutes in ms
NEARBY_THRESHOLD = 5 * 60 * 1000  # 5 minutes in ms

# In-memory cache
_trips_cache = {"trips": [], "ready": threading.Event()}
_tx_cache = {"transactions": [], "ready": threading.Event()}
_listening_cache = {"tracks": [], "ready": threading.Event()}
_response_cache = {"transactions": {}, "listening": {}}


# --- Trip detection logic ---

def distance_meters(lat1, lng1, lat2, lng2):
    R = 6371000
    d_lat = math.radians(lat2 - lat1)
    d_lng = math.radians(lng2 - lng1)
    a = (math.sin(d_lat / 2) ** 2
         + math.cos(math.radians(lat1)) * math.cos(math.radians(lat2))
         * math.sin(d_lng / 2) ** 2)
    return R * 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def is_near_regular_spot(pos):
    for spot in REGULAR_SPOTS:
        radius = WORK_RADIUS if spot["name"] == "Work" else REGULAR_RADIUS
        if distance_meters(pos["latitude"], pos["longitude"], spot["lat"], spot["lng"]) <= radius:
            return spot
    return None


def fix_time_ms(pos):
    return int(datetime.fromisoformat(pos["fixTime"].replace("Z", "+00:00")).timestamp() * 1000)


def aggregate_positions(positions, bucket_ms=30000):
    buckets = {}
    for p in positions:
        key = fix_time_ms(p) // bucket_ms
        if key not in buckets:
            buckets[key] = p
    return sorted(buckets.values(), key=lambda p: p["fixTime"])


def detect_trips(positions):
    if not positions:
        return []

    trips = []
    in_trip = False
    trip_positions = []
    regular_entry_time = None
    regular_entry_idx = None
    at_regular = is_near_regular_spot(positions[0]) is not None

    for i, pos in enumerate(positions):
        pos_time = fix_time_ms(pos)
        near_spot = is_near_regular_spot(pos)

        if not in_trip:
            if not near_spot and at_regular:
                in_trip = True
                if i > 0:
                    prev_time = fix_time_ms(positions[i - 1])
                    if pos_time - prev_time <= NEARBY_THRESHOLD:
                        trip_positions = [positions[i - 1], pos]
                    else:
                        trip_positions = [pos]
                else:
                    trip_positions = [pos]
                regular_entry_time = None
                regular_entry_idx = None
            at_regular = near_spot is not None
        else:
            trip_positions.append(pos)
            if near_spot:
                if regular_entry_time is None:
                    regular_entry_time = pos_time
                    regular_entry_idx = len(trip_positions) - 1
                if pos_time - regular_entry_time >= TRIP_DEBOUNCE:
                    end_idx = regular_entry_idx
                    if regular_entry_idx > 0:
                        last_outside_time = fix_time_ms(trip_positions[regular_entry_idx - 1])
                        if regular_entry_time - last_outside_time > NEARBY_THRESHOLD:
                            end_idx = regular_entry_idx - 1
                    trips.append(trip_positions[:end_idx + 1])
                    trip_positions = []
                    in_trip = False
                    at_regular = True
                    regular_entry_time = None
                    regular_entry_idx = None
            else:
                regular_entry_time = None
                regular_entry_idx = None

    if in_trip and len(trip_positions) > 1:
        trips.append(trip_positions)

    return [t for t in trips
            if fix_time_ms(t[-1]) - fix_time_ms(t[0]) >= TRIP_DEBOUNCE]


def label_trips(trips):
    labeled = []
    day_counts = {}
    for positions in trips:
        start = datetime.fromisoformat(positions[0]["fixTime"].replace("Z", "+00:00")).astimezone(LOCAL_TZ)
        end = datetime.fromisoformat(positions[-1]["fixTime"].replace("Z", "+00:00")).astimezone(LOCAL_TZ)
        date_key = start.strftime("%A %-d %B")
        day_counts[date_key] = day_counts.get(date_key, 0) + 1
        labeled.append({
            "positions": positions,
            "date": date_key,
            "tripNum": day_counts[date_key],
            "startTime": positions[0]["fixTime"],
            "endTime": positions[-1]["fixTime"],
        })
    return labeled


def fetch_positions_upstream(hours, bucket_ms=30000):
    now = datetime.now(timezone.utc)
    from_time = now - timedelta(hours=hours)
    resp = http_requests.get(
        f"{TRACK_BASE}/positions",
        params={
            "deviceId": DEVICE_ID,
            "from": from_time.isoformat(),
            "to": now.isoformat(),
        },
        headers={"Authorization": f"Bearer {TRACK_TOKEN}"},
    )
    resp.raise_for_status()
    return aggregate_positions(resp.json(), bucket_ms)


def fetch_transactions_upstream(since, until):
    all_transactions = []
    url = f"{UP_BASE}/transactions"
    params = {"page[size]": "100"}
    if since:
        params["filter[since]"] = since
    if until:
        params["filter[until]"] = until
    while url:
        resp = http_requests.get(
            url,
            params=params,
            headers={"Authorization": f"Bearer {UP_TOKEN}"},
        )
        resp.raise_for_status()
        data = resp.json()
        for t in data["data"]:
            all_transactions.append({
                "id": t["id"],
                "createdAt": t["attributes"]["createdAt"],
                "description": t["attributes"]["description"],
                "amount": {
                    "value": t["attributes"]["amount"]["value"],
                    "currency": t["attributes"]["amount"]["currencyCode"],
                },
            })
        url = data.get("links", {}).get("next")
        params = {}
    return all_transactions


def fetch_listening_upstream(since_epoch, until_epoch):
    all_tracks = []
    page = 1
    while True:
        resp = http_requests.get(LASTFM_BASE, params={
            "method": "user.getrecenttracks",
            "user": LASTFM_USER,
            "api_key": LASTFM_API_KEY,
            "format": "json",
            "limit": 200,
            "from": int(since_epoch),
            "to": int(until_epoch),
            "page": page,
        })
        resp.raise_for_status()
        data = resp.json()
        tracks = data.get("recenttracks", {}).get("track", [])
        if not tracks:
            break
        for t in tracks:
            # Skip "now playing" tracks (no date field)
            if "@attr" in t and t["@attr"].get("nowplaying") == "true":
                continue
            if "date" not in t:
                continue
            image_url = ""
            for img in t.get("image", []):
                if img.get("size") == "medium" and img.get("#text"):
                    image_url = img["#text"]
            all_tracks.append({
                "artist": t.get("artist", {}).get("#text", ""),
                "track": t.get("name", ""),
                "album": t.get("album", {}).get("#text", ""),
                "timestamp": int(t["date"]["uts"]),
                "image": image_url,
            })
        total_pages = int(data.get("recenttracks", {}).get("@attr", {}).get("totalPages", 1))
        if page >= total_pages:
            break
        page += 1
    return all_tracks


def background_sync():
    while True:
        now = datetime.now(timezone.utc)
        since = (now - timedelta(days=30)).isoformat()
        until = now.isoformat()

        try:
            raw = fetch_positions_upstream(30 * 24, 30000)
            trips = detect_trips(raw)
            _trips_cache["trips"] = label_trips(trips)
        except Exception as e:
            print(f"Trip sync failed: {e}")
        _trips_cache["ready"].set()

        try:
            _tx_cache["transactions"] = fetch_transactions_upstream(since, until)
        except Exception as e:
            print(f"Transaction sync failed: {e}")
        _tx_cache["ready"].set()

        try:
            since_epoch = (now - timedelta(days=30)).timestamp()
            until_epoch = now.timestamp()
            _listening_cache["tracks"] = fetch_listening_upstream(since_epoch, until_epoch)
        except Exception as e:
            print(f"Listening sync failed: {e}")
        _listening_cache["ready"].set()

        _response_cache["transactions"] = {}
        _response_cache["listening"] = {}

        time.sleep(600)


# --- Office presence logic ---

def _classify_position(pos):
    spot = is_near_regular_spot(pos)
    return spot["name"].lower() if spot else "away"

def _update_ibbitot(status, description):
    if not IBBITOT_URL or not IBBITOT_API_KEY:
        print("ibbitot not configured, skipping update")
        return
    try:
        resp = http_requests.post(
            f"{IBBITOT_URL}/api/update",
            json={"status": status, "description": description},
            headers={"Authorization": f"Bearer {IBBITOT_API_KEY}"},
            timeout=10,
        )
        resp.raise_for_status()
        print(f"ibbitot updated: status={status}")
    except Exception as e:
        print(f"ibbitot update error: {e}")

def check_office_presence():
    try:
        positions = fetch_positions_upstream(hours=0.5, bucket_ms=30000)
    except Exception as e:
        print(f"Office presence fetch error: {e}")
        return
    if not positions:
        return

    now_epoch = time.time()
    cutoff_epoch = now_epoch - OFFICE_DEBOUNCE_SECS
    recent = [p for p in positions if fix_time_ms(p) / 1000 >= cutoff_epoch]
    if not recent:
        return

    counts = {}
    for p in recent:
        loc = _classify_position(p)
        counts[loc] = counts.get(loc, 0) + 1
    dominant = max(counts, key=counts.get)
    dominant_count = counts[dominant]
    oldest_epoch = fix_time_ms(recent[0]) / 1000

    with _office_lock:
        cs = _office_state["confirmed_state"]
        confirmed = cs["location"]
        candidate = cs["candidate_location"]

        if dominant != confirmed:
            if dominant == candidate:
                cs["candidate_point_count"] = dominant_count
                duration = now_epoch - cs["candidate_since_epoch"]
                if duration >= OFFICE_DEBOUNCE_SECS and dominant_count >= OFFICE_MIN_POINTS:
                    old_loc = confirmed
                    cs["location"] = dominant
                    cs["candidate_location"] = None
                    cs["candidate_since_epoch"] = None
                    cs["candidate_point_count"] = 0
                    _save_office_state(_office_state)
                    # Fire ibbitot update on state transition
                    if dominant == "work":
                        _update_ibbitot("yes", "According to track.baileys.app")
                    elif old_loc == "work":
                        _update_ibbitot("no", "")
                else:
                    _save_office_state(_office_state)
            else:
                cs["candidate_location"] = dominant
                cs["candidate_since_epoch"] = oldest_epoch
                cs["candidate_point_count"] = dominant_count
                _save_office_state(_office_state)
        else:
            if candidate is not None:
                cs["candidate_location"] = None
                cs["candidate_since_epoch"] = None
                cs["candidate_point_count"] = 0
                _save_office_state(_office_state)

def office_check_loop():
    time.sleep(60)
    while True:
        check_office_presence()
        time.sleep(OFFICE_CHECK_INTERVAL)


# --- Routes ---

@app.route("/")
def index():
    return send_from_directory(".", "index.html")


@app.route("/api/positions")
def positions():
    params = {}
    for key in ("deviceId", "from", "to"):
        if key in request.args:
            params[key] = request.args[key]
    resp = http_requests.get(
        f"{TRACK_BASE}/positions",
        params=params,
        headers={"Authorization": f"Bearer {TRACK_TOKEN}"},
    )
    resp.raise_for_status()
    return jsonify(resp.json())


@app.route("/api/positions/live")
def positions_live():
    resp = http_requests.get(
        f"{TRACK_BASE}/positions",
        headers={"Authorization": f"Bearer {TRACK_TOKEN}"},
    )
    resp.raise_for_status()
    return jsonify(resp.json())


def parse_iso(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


@app.route("/api/transactions")
def transactions():
    _tx_cache["ready"].wait()
    since = request.args.get("since")
    until = request.args.get("until")
    cache_key = (since, until)
    if cache_key in _response_cache["transactions"]:
        return jsonify(_response_cache["transactions"][cache_key])
    result = _tx_cache["transactions"]
    if since or until:
        since_dt = parse_iso(since) if since else None
        until_dt = parse_iso(until) if until else None
        result = [
            t for t in result
            if (not since_dt or parse_iso(t["createdAt"]) >= since_dt)
            and (not until_dt or parse_iso(t["createdAt"]) <= until_dt)
        ]
    _response_cache["transactions"][cache_key] = result
    return jsonify(result)


@app.route("/api/listening")
def listening():
    _listening_cache["ready"].wait()
    since = request.args.get("since")
    until = request.args.get("until")
    cache_key = (since, until)
    if cache_key in _response_cache["listening"]:
        return jsonify(_response_cache["listening"][cache_key])
    result = _listening_cache["tracks"]
    if since or until:
        since_epoch = parse_iso(since).timestamp() if since else None
        until_epoch = parse_iso(until).timestamp() if until else None
        result = [
            t for t in result
            if (not since_epoch or t["timestamp"] >= since_epoch)
            and (not until_epoch or t["timestamp"] <= until_epoch)
        ]
    _response_cache["listening"][cache_key] = result
    return jsonify(result)


@app.route("/api/trips")
def trips():
    _trips_cache["ready"].wait()
    return jsonify(_trips_cache["trips"])


@app.route("/api/ibbitot/demo", methods=["POST"])
def ibbitot_demo():
    _update_ibbitot("yes", "According to track.baileys.app")
    return jsonify({"status": "sent"})


if __name__ == "__main__":
    t1 = threading.Thread(target=background_sync, daemon=True)
    t1.start()
    t2 = threading.Thread(target=office_check_loop, daemon=True)
    t2.start()
    app.run(host="0.0.0.0", port=8080, debug=True, use_reloader=False)
