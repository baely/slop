#!/usr/bin/env python3
"""Import film scans from the Google Drive "Film" folder into photos/.

The lab drops rolls into Drive under a few different layouts:

    Bailey Butler 2023-01-10/5662 Agfa Vista 200/5662 Agfa Vista 200/000036.JPG
    Bailey Butler 2024-04-30_6/4309 Kodak Gold 200/4309 Kodak Gold 200/000045.JPG
    20260601/01 5702 Fujifilm Colour Negative 200/5702-0001.jpg
    20260907/B26159-Ilford-HP5-Plus/000261590001.jpg
    7082 Ilford HP5/7082-0001.jpg                 (undated copy of a dated roll)

This script normalises all of them to:

    photos/YYYY-MM-DD/ROLL Stock Name/frame.jpg

Rules:
  - the top-level directory gives the date (YYYY-MM-DD or YYYYMMDD anywhere in the name)
  - the roll directory gives the roll id (first token) and stock (the rest);
    a leading order index ("01 5702 ...") is dropped, hyphen-joined names are split
  - nested duplicate directories, "resized" directories, contact sheets,
    FND_MissingFrames slides, .xmp sidecars and .zip archives are ignored
  - undated top-level roll directories are skipped when the same roll id
    already exists under a dated directory, otherwise dated from EXIF
  - files are copied (never moved) and only when missing or a different size

Usage:
    python3 import.py [SOURCE_DIR] [--prune] [--dry-run]

--prune removes files under photos/ that no longer exist in the source.
"""

import os
import re
import shutil
import sys
from concurrent.futures import ThreadPoolExecutor

DEFAULT_SRC = (
    "/Users/bailey/Library/CloudStorage/GoogleDrive-bailey.butler.234@gmail.com"
    "/My Drive/Media/Film"
)
HERE = os.path.dirname(os.path.abspath(__file__))
DEST = os.path.join(HERE, "photos")

IMAGE_EXT = {".jpg", ".jpeg"}
SKIP_FILE = re.compile(r"^(contact_sheet|FND_MissingFrames)", re.I)
SKIP_DIR = re.compile(r"^resized", re.I)
ROLL_ID = re.compile(r"^[A-Z]?\d{3,}$", re.I)


def parse_date(name):
    m = re.search(r"(\d{4})-(\d{2})-(\d{2})", name) or re.search(r"(?<!\d)(\d{4})(\d{2})(\d{2})(?!\d)", name)
    return f"{m[1]}-{m[2]}-{m[3]}" if m else None


def parse_roll(name):
    """Return (roll_id, stock) for a roll directory name."""
    n = name.strip()
    if " " not in n and "-" in n:  # B26159-Ilford-HP5-Plus
        n = n.replace("-", " ")
    tokens = n.split()
    # drop a leading order index like "01" when a real roll id follows
    if len(tokens) >= 2 and re.fullmatch(r"\d{1,2}", tokens[0]) and ROLL_ID.match(tokens[1]):
        tokens = tokens[1:]
    if not tokens or not ROLL_ID.match(tokens[0]):
        return None, None
    return tokens[0], " ".join(tokens[1:])


def frames_in(roll_dir):
    out = []
    for root, dirs, files in os.walk(roll_dir):
        dirs[:] = [d for d in dirs if not SKIP_DIR.match(d) and not d.startswith(".")]
        for f in files:
            if SKIP_FILE.match(f) or f.startswith("."):
                continue
            if os.path.splitext(f)[1].lower() in IMAGE_EXT:
                out.append(os.path.join(root, f))
    return sorted(out)


def exif_date(path):
    try:
        from PIL import Image

        dt = Image.open(path).getexif().get(306)  # DateTime
        if dt:
            return dt[:10].replace(":", "-")
    except Exception:
        pass
    return None


def dest_name(src_file):
    stem, ext = os.path.splitext(os.path.basename(src_file))
    return stem + ".jpg"  # .JPG / .jpeg -> .jpg


def discover(src):
    rolls = []  # dicts: date, roll, stock, src, frames, undated
    for top in sorted(os.listdir(src)):
        top_path = os.path.join(src, top)
        if not os.path.isdir(top_path) or top.startswith("."):
            continue
        date = parse_date(top)
        if date:
            for sub in sorted(os.listdir(top_path)):
                sub_path = os.path.join(top_path, sub)
                if not os.path.isdir(sub_path) or SKIP_DIR.match(sub) or sub.startswith("."):
                    continue
                roll, stock = parse_roll(sub)
                if roll is None:
                    print(f"  ! cannot parse roll directory, skipping: {top}/{sub}")
                    continue
                rolls.append(dict(date=date, roll=roll, stock=stock, src=sub_path, frames=frames_in(sub_path), undated=False))
        else:
            roll, stock = parse_roll(top)
            if roll is None:
                print(f"  ! cannot parse top-level directory, skipping: {top}")
                continue
            rolls.append(dict(date=None, roll=roll, stock=stock, src=top_path, frames=frames_in(top_path), undated=True))

    dated_ids = {r["roll"] for r in rolls if not r["undated"]}
    final = []
    for r in rolls:
        if r["undated"]:
            if r["roll"] in dated_ids:
                print(f"  - skipping undated duplicate of roll {r['roll']}: {os.path.basename(r['src'])}")
                continue
            r["date"] = exif_date(r["frames"][0]) if r["frames"] else None
            if not r["date"]:
                print(f"  ! no date for roll {r['roll']}, skipping: {os.path.basename(r['src'])}")
                continue
            print(f"  ~ roll {r['roll']} dated {r['date']} from EXIF")
        if not r["frames"]:
            print(f"  ! no frames in roll {r['roll']}, skipping")
            continue
        final.append(r)
    return final


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    flags = {a for a in sys.argv[1:] if a.startswith("--")}
    src = args[0] if args else DEFAULT_SRC
    prune = "--prune" in flags
    dry = "--dry-run" in flags

    if not os.path.isdir(src):
        sys.exit(f"source not found: {src}")

    print(f"source: {src}")
    rolls = discover(src)

    jobs = []  # (src_file, dest_file)
    wanted = set()
    for r in rolls:
        dir_name = f"{r['roll']} {r['stock']}".strip()
        dest_dir = os.path.join(DEST, r["date"], dir_name)
        for f in r["frames"]:
            d = os.path.join(dest_dir, dest_name(f))
            wanted.add(d)
            if os.path.exists(d) and os.path.getsize(d) == os.path.getsize(f):
                continue
            jobs.append((f, d))

    print(f"\n{'date':<10}  {'roll':<7} {'frames':>6}  stock")
    for r in rolls:
        print(f"{r['date']:<10}  {r['roll']:<7} {len(r['frames']):>6}  {r['stock'] or '?'}")
    total = sum(len(r["frames"]) for r in rolls)
    print(f"\n{len(rolls)} rolls, {total} frames, {len(jobs)} to copy")

    if dry:
        for s, d in jobs[:20]:
            print(f"  {os.path.relpath(s, src)}  ->  {os.path.relpath(d, HERE)}")
        if len(jobs) > 20:
            print(f"  ... {len(jobs) - 20} more")
        return

    for _, d in jobs:
        os.makedirs(os.path.dirname(d), exist_ok=True)

    def copy(job):
        s, d = job
        shutil.copy2(s, d + ".part")
        os.replace(d + ".part", d)
        return d

    done = 0
    with ThreadPoolExecutor(max_workers=8) as pool:
        for _ in pool.map(copy, jobs):
            done += 1
            if done % 50 == 0 or done == len(jobs):
                print(f"  copied {done}/{len(jobs)}", flush=True)

    if prune:
        removed = 0
        for root, dirs, files in os.walk(DEST, topdown=False):
            for f in files:
                p = os.path.join(root, f)
                if p not in wanted and not f.startswith("."):
                    os.remove(p)
                    removed += 1
            if not os.listdir(root) and root != DEST:
                os.rmdir(root)
        print(f"pruned {removed} stale files")

    print("done")


if __name__ == "__main__":
    main()
