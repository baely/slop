#!/bin/bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
rm -rf "$DIR/dist"
mkdir -p "$DIR/dist/img"
python3 "$DIR/build.py"
echo "Files: $(find "$DIR/dist" -type f | wc -l | tr -d ' ')"
