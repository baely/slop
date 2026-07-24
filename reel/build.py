#!/usr/bin/env python3
"""Generate site/data.js from a Letterboxd data export zip.

Usage: python3 build.py path/to/letterboxd-export.zip

Reads the CSVs inside the export and writes aggregated stats to site/data.js.
Only aggregates and public-profile film data end up in the output — the raw
export is never committed or deployed.
"""

import csv
import io
import json
import re
import sys
import zipfile
from collections import Counter
from datetime import date
from pathlib import Path


def rows(zf: zipfile.ZipFile, name: str) -> list[dict]:
    with zf.open(name) as f:
        text = io.TextIOWrapper(f, encoding="utf-8-sig", newline="")
        return list(csv.DictReader(text))


def main() -> None:
    if len(sys.argv) != 2:
        sys.exit("usage: build.py <letterboxd-export.zip>")
    zip_path = Path(sys.argv[1])

    with zipfile.ZipFile(zip_path) as zf:
        profile = rows(zf, "profile.csv")[0]
        watched = rows(zf, "watched.csv")
        ratings = rows(zf, "ratings.csv")
        diary = rows(zf, "diary.csv")
        watchlist = rows(zf, "watchlist.csv")
        reviews = rows(zf, "reviews.csv")

    # Export date from the standard letterboxd-<user>-<date>-<time>-utc.zip name.
    m = re.search(r"(\d{4}-\d{2}-\d{2})", zip_path.name)
    export_date = m.group(1) if m else None

    dist = Counter(r["Rating"] for r in ratings if r["Rating"])
    rating_steps = [f"{n / 2:g}" for n in range(1, 11)]
    avg = sum(float(r["Rating"]) for r in ratings if r["Rating"]) / len(ratings)

    release_years = sorted(int(w["Year"]) for w in watched if w["Year"])
    decade_counts = Counter(y // 10 * 10 for y in release_years)
    decades = [
        {"decade": d, "count": decade_counts.get(d, 0)}
        for d in range(release_years[0] // 10 * 10, release_years[-1] // 10 * 10 + 1, 10)
    ]

    log_years = Counter(int(d["Watched Date"][:4]) for d in diary if d["Watched Date"])
    by_year = [
        {"year": y, "count": log_years.get(y, 0)}
        for y in range(min(log_years), max(log_years) + 1)
    ]
    months = Counter(d["Watched Date"][:7] for d in diary if d["Watched Date"])
    busiest_month, busiest_count = months.most_common(1)[0]

    dows = Counter(
        date.fromisoformat(d["Watched Date"]).strftime("%A")
        for d in diary
        if d["Watched Date"]
    )

    five_stars = [
        {"name": r["Name"], "year": r["Year"]} for r in ratings if r["Rating"] == "5"
    ]

    oldest = sorted(watchlist, key=lambda w: w["Date"])[:5]

    tags = Counter()
    for d in diary:
        for t in (d["Tags"] or "").split(","):
            t = t.strip().lower()
            if t and t != "watchlist":  # meta tag, not a place
                tags[t] += 1

    data = {
        "export_date": export_date,
        "profile": {
            "username": profile["Username"],
            "joined": profile["Date Joined"],
            "url": f"https://letterboxd.com/{profile['Username']}/",
        },
        "watched": len(watched),
        "release_range": [release_years[0], release_years[-1]],
        "ratings": {
            "count": len(ratings),
            "avg": round(avg, 2),
            "dist": [{"stars": s, "count": dist.get(s, 0)} for s in rating_steps],
        },
        "decades": decades,
        "diary": {
            "count": len(diary),
            "rewatches": sum(1 for d in diary if d["Rewatch"] == "Yes"),
            "by_year": by_year,
            "busiest_month": {"month": busiest_month, "count": busiest_count},
            "top_dow": {"day": dows.most_common(1)[0][0], "count": dows.most_common(1)[0][1]},
        },
        "five_stars": five_stars,
        "watchlist": {
            "count": len(watchlist),
            "oldest": [
                {"name": w["Name"], "year": w["Year"], "added": w["Date"]} for w in oldest
            ],
        },
        "tags": tags.most_common(10),
        "reviews": [
            {
                "name": r["Name"],
                "year": r["Year"],
                "rating": r["Rating"],
                "text": r["Review"],
                "watched": r["Watched Date"],
            }
            for r in reviews
        ],
    }

    out = Path(__file__).parent / "site" / "data.js"
    out.write_text("window.REEL = " + json.dumps(data, indent=2) + ";\n")
    print(f"wrote {out} ({len(watched)} films, export {export_date})")


if __name__ == "__main__":
    main()
