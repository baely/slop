#!/bin/bash
set -euo pipefail

GALLERY_DIR="$(cd "$(dirname "$0")" && pwd)"
DIST_DIR="$GALLERY_DIR/dist"
ORIGINALS_DIR="$DIST_DIR/img"

rm -rf "$DIST_DIR"
mkdir -p "$ORIGINALS_DIR"

export GALLERY_DIR DIST_DIR ORIGINALS_DIR

python3 "$GALLERY_DIR/_build.py"

echo "Files: $(find "$DIST_DIR" -type f | wc -l)"
