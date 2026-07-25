#!/usr/bin/env python3
"""Generate site/data.js from a Letterboxd data export zip.

Usage: python3 build.py path/to/letterboxd-export.zip

Pulls the rated films out of the export — that's the whole game. Only title,
year, rating and (where known) the last watch date are emitted; the raw export
is never committed or deployed.
"""

import csv
import io
import json
import re
import sys
import zipfile
from pathlib import Path


def rows(zf: zipfile.ZipFile, name: str) -> list[dict]:
    with zf.open(name) as f:
        return list(csv.DictReader(io.TextIOWrapper(f, encoding="utf-8-sig", newline="")))


def main() -> None:
    if len(sys.argv) != 2:
        sys.exit("usage: build.py <letterboxd-export.zip>")
    zip_path = Path(sys.argv[1])

    with zipfile.ZipFile(zip_path) as zf:
        profile = rows(zf, "profile.csv")[0]
        ratings = rows(zf, "ratings.csv")
        diary = rows(zf, "diary.csv")
        reviews = rows(zf, "reviews.csv")

    # Most recent watch date per film, for the reveal line.
    last_watched: dict[str, str] = {}
    for d in diary:
        key = d["Letterboxd URI"]
        when = d["Watched Date"]
        if when and when > last_watched.get(d["Name"], ""):
            last_watched[d["Name"]] = when

    review_text = {r["Name"]: r["Review"] for r in reviews if r["Review"]}

    films = []
    for r in ratings:
        if not r["Rating"]:
            continue
        film = {
            "name": r["Name"],
            "year": int(r["Year"]) if r["Year"] else None,
            "rating": float(r["Rating"]),
            "url": r["Letterboxd URI"],
        }
        if r["Name"] in last_watched:
            film["watched"] = last_watched[r["Name"]]
        if r["Name"] in review_text:
            film["review"] = review_text[r["Name"]]
        films.append(film)

    films.sort(key=lambda f: (-f["rating"], f["name"]))

    m = re.search(r"(\d{4}-\d{2}-\d{2})", zip_path.name)

    data = {
        "export_date": m.group(1) if m else None,
        "username": profile["Username"],
        "profile_url": f"https://letterboxd.com/{profile['Username']}/",
        "films": films,
    }

    out = Path(__file__).parent / "site" / "data.js"
    out.write_text("window.HINDSIGHT = " + json.dumps(data, separators=(",", ":")) + ";\n")
    print(f"wrote {out} ({len(films)} rated films, export {data['export_date']})")


if __name__ == "__main__":
    main()
