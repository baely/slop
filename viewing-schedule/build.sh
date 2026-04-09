#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CSV="${SCRIPT_DIR}/../viewing_calendar_apr10_may09_2026.csv"
TMDB_TOKEN="${TMDB_TOKEN:?Set TMDB_TOKEN env var}"
OUT="${SCRIPT_DIR}/dist"

mkdir -p "$OUT"

# Parse CSV and fetch TMDB data, output JSON array
node "${SCRIPT_DIR}/fetch_movies.js" "$CSV" "$TMDB_TOKEN" > "$OUT/movies.json"

# Generate the HTML page
node "${SCRIPT_DIR}/generate.js" "$OUT/movies.json" > "$OUT/index.html"

echo "Built to $OUT"
