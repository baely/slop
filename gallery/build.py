#!/usr/bin/env python3
"""Build the gallery site: photos/ -> dist/

    photos/YYYY-MM-DD/ROLL Stock Name/frame.jpg   (see import.py)

becomes

    dist/index.html      page with the roll manifest embedded
    dist/img/<id>.jpg    theater image, long edge 2000px
    dist/t/<id>.jpg      grid thumbnail, long edge 480px

Rolls are ordered newest first. Frames that are blank "missing frame" slides
are dropped. Optional per-roll labels live in rolls.json:

    { "18954": { "stock": "Kodak Gold 200", "location": "Melbourne", "maps": "https://..." } }

Usage:
    python3 build.py [--clean]

Incremental by default: a frame is reprocessed only when its outputs are
missing or older than the source. --clean wipes dist/ first.
"""

import json
import os
import re
import shutil
import sys
from concurrent.futures import ProcessPoolExecutor, ThreadPoolExecutor, as_completed

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _worker import process  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
PHOTOS = os.path.join(HERE, "photos")
DIST = os.path.join(HERE, "dist")
IMG = os.path.join(DIST, "img")
THUMB = os.path.join(DIST, "t")
TEMPLATE = os.path.join(HERE, "template.html")
OVERRIDES = os.path.join(HERE, "rolls.json")


def natural(s):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", s)]


def frame_id(roll, path):
    stem = os.path.splitext(os.path.basename(path))[0]
    return stem if stem.startswith(roll + "-") else f"{roll}-{stem}"


def discover():
    overrides = {}
    if os.path.exists(OVERRIDES):
        with open(OVERRIDES) as f:
            overrides = json.load(f)

    rolls = []
    for date in sorted(os.listdir(PHOTOS)):
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", date):
            continue
        for name in sorted(os.listdir(os.path.join(PHOTOS, date)), key=natural):
            roll_dir = os.path.join(PHOTOS, date, name)
            if not os.path.isdir(roll_dir):
                continue
            tokens = name.split()
            roll, stock = tokens[0], " ".join(tokens[1:])
            frames = sorted(
                (os.path.join(roll_dir, f) for f in os.listdir(roll_dir) if f.lower().endswith((".jpg", ".jpeg"))),
                key=natural,
            )
            if not frames:
                continue
            meta = {"date": date, "roll": roll, "stock": stock, "location": "", "maps": ""}
            meta.update(overrides.get(roll, {}))
            meta["frames"] = frames
            rolls.append(meta)

    # newest date first; within a date, roll id ascending (two stable sorts)
    rolls.sort(key=lambda r: natural(r["roll"]))
    rolls.sort(key=lambda r: r["date"], reverse=True)
    return rolls


def needs_work(src, full, thumb):
    if not (os.path.exists(full) and os.path.exists(thumb)):
        return True
    m = os.path.getmtime(src)
    return os.path.getmtime(full) < m or os.path.getmtime(thumb) < m


def main():
    clean = "--clean" in sys.argv
    if clean and os.path.isdir(DIST):
        shutil.rmtree(DIST)
    os.makedirs(IMG, exist_ok=True)
    os.makedirs(THUMB, exist_ok=True)

    rolls = discover()
    jobs = []  # (src, full, thumb)
    plan = []  # per roll: list of (id, src, full, thumb)
    for r in rolls:
        items = []
        for src in r["frames"]:
            fid = frame_id(r["roll"], src)
            full, thumb = os.path.join(IMG, fid + ".jpg"), os.path.join(THUMB, fid + ".jpg")
            items.append((fid, src, full, thumb))
            if needs_work(src, full, thumb):
                jobs.append((src, full, thumb))
        plan.append(items)

    total = sum(len(p) for p in plan)
    print(f"{len(rolls)} rolls, {total} frames, {len(jobs)} to process")

    blank = set()
    if jobs:
        done = 0
        workers = os.cpu_count() or 4
        try:
            pool = ProcessPoolExecutor(max_workers=workers)
        except (PermissionError, OSError):  # sandboxed: fall back to threads (Pillow releases the GIL)
            pool = ThreadPoolExecutor(max_workers=workers)
        with pool:
            futures = {pool.submit(process, j): j for j in jobs}
            for fut in as_completed(futures):
                src = futures[fut][0]
                if fut.result() is None:
                    blank.add(src)
                done += 1
                if done % 100 == 0 or done == len(jobs):
                    print(f"  processed {done}/{len(jobs)}", flush=True)

    manifest = []
    kept = set()
    dropped = 0
    for r, items in zip(rolls, plan):
        ids = []
        for fid, src, full, thumb in items:
            if src in blank or not (os.path.exists(full) and os.path.exists(thumb)):
                dropped += 1
                continue
            ids.append(fid)
            kept.add(fid + ".jpg")
        if not ids:
            continue
        entry = {"d": r["date"], "r": r["roll"], "f": ids}
        for key, short in (("stock", "s"), ("location", "l"), ("maps", "m")):
            if r.get(key):
                entry[short] = r[key]
        manifest.append(entry)

    # remove outputs for frames that no longer exist
    stale = 0
    for d in (IMG, THUMB):
        for f in os.listdir(d):
            if f not in kept:
                os.remove(os.path.join(d, f))
                stale += 1

    with open(TEMPLATE) as f:
        html = f.read()
    html = html.replace("__ROLLS__", json.dumps(manifest, separators=(",", ":"), ensure_ascii=False))
    with open(os.path.join(DIST, "index.html"), "w") as f:
        f.write(html)

    size = sum(os.path.getsize(os.path.join(dp, f)) for dp, _, fs in os.walk(DIST) for f in fs)
    print(f"kept {len(kept)} frames in {len(manifest)} rolls, dropped {dropped} blank, removed {stale} stale")
    print(f"dist/ {size / 1e6:.0f} MB, index.html {os.path.getsize(os.path.join(DIST, 'index.html')) / 1e3:.0f} kB")


if __name__ == "__main__":
    main()
