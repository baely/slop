#!/bin/bash
# Build the gallery into dist/. Pass --clean to rebuild everything.
set -euo pipefail
cd "$(dirname "$0")"
python3 build.py "$@"
