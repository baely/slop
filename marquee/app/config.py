"""Runtime configuration, all via environment variables.

Marquee is config-driven: drop in your TMDB + Radarr details and the real
integrations light up. Leave them blank and it falls back to a small demo
fixture set so the app is still fully explorable.
"""
import os

# Load a local .env if present (no-op in Docker, where env is passed directly).
try:
    from dotenv import load_dotenv
    load_dotenv(os.path.join(os.path.dirname(__file__), "..", ".env"))
except Exception:
    pass

# --- TMDB (metadata + posters) ---
TMDB_API_KEY = os.getenv("TMDB_API_KEY", "").strip()           # v3 api key
TMDB_BEARER = os.getenv("TMDB_BEARER", "").strip()             # v4 read access token (optional)
TMDB_IMG = "https://image.tmdb.org/t/p"

# --- Radarr (the download loop) ---
RADARR_URL = os.getenv("RADARR_URL", "").strip().rstrip("/")    # e.g. http://radarr:7878
RADARR_API_KEY = os.getenv("RADARR_API_KEY", "").strip()
# Optional overrides; auto-detected from Radarr if left blank.
RADARR_QUALITY_PROFILE_ID = os.getenv("RADARR_QUALITY_PROFILE_ID", "").strip()
RADARR_ROOT_FOLDER = os.getenv("RADARR_ROOT_FOLDER", "").strip()
RADARR_MIN_AVAILABILITY = os.getenv("RADARR_MIN_AVAILABILITY", "released").strip()

# --- App ---
DB_PATH = os.getenv("DB_PATH", os.path.join(os.path.dirname(__file__), "..", "data", "marquee.db"))
APP_NAME = os.getenv("APP_NAME", "Marquee")
OWNER = os.getenv("OWNER", "you")

TMDB_ENABLED = bool(TMDB_API_KEY or TMDB_BEARER)
RADARR_ENABLED = bool(RADARR_URL and RADARR_API_KEY)
DEMO_MODE = not TMDB_ENABLED  # no metadata source -> serve fixtures
