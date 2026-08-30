#!/usr/bin/env python3
"""Local web tool to downscale + recompress JPGs and export them to a flat zip."""

import hashlib
import io
import os
import threading
import time
import uuid
import zipfile
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime
from pathlib import Path

from flask import Flask, jsonify, render_template, request, send_file, abort
from PIL import Image, ImageOps

DEFAULT_FILM_DIR = (
    "/Users/bailey/Library/CloudStorage/"
    "GoogleDrive-bailey.butler.234@gmail.com/My Drive/Media/Film"
)
FILM_DIR = Path(os.environ.get("FILM_DIR", DEFAULT_FILM_DIR)).expanduser()
APP_DIR = Path(__file__).resolve().parent
OUTPUT_DIR = APP_DIR / "output"
OUTPUT_DIR.mkdir(exist_ok=True)

JPG_EXTS = {".jpg", ".jpeg"}
ESTIMATE_SAMPLE_SIZE = 24
THUMBS_PER_ROLL = 10
THUMB_PX = 220
# Reads are network-bound (Google Drive streams each file), so process many at
# once. The download and Pillow's C decode both release the GIL.
WORKERS = int(os.environ.get("WORKERS", "12"))

THUMB_DIR = APP_DIR / ".thumbs"
THUMB_DIR.mkdir(exist_ok=True)

app = Flask(__name__)


# --------------------------------------------------------------------------- #
# File discovery
# --------------------------------------------------------------------------- #
def _is_resized(rel: Path) -> bool:
    """True if any folder component of the relative path is a 'resized ...' dir."""
    return any(part.lower().startswith("resized") for part in rel.parts)


def scan_rolls():
    """Group every JPG under FILM_DIR by its immediate parent directory.

    Returns a dict: roll_id (POSIX rel path of the parent) -> sorted [Paths].
    """
    rolls = {}
    for p in FILM_DIR.rglob("*"):
        if not p.is_file() or p.suffix.lower() not in JPG_EXTS:
            continue
        rel = p.relative_to(FILM_DIR)
        rolls.setdefault(rel.parent.as_posix(), []).append(p)
    for files in rolls.values():
        files.sort(key=lambda p: str(p).lower())
    return rolls


def list_jpgs(roll_ids=None):
    """Flatten the selected rolls into a sorted list of JPG Paths.

    roll_ids=None selects every roll. An empty collection selects nothing.
    """
    rolls = scan_rolls()
    if roll_ids is None:
        chosen = rolls.keys()
    else:
        wanted = set(roll_ids)
        chosen = [rid for rid in rolls if rid in wanted]
    files = [p for rid in chosen for p in rolls[rid]]
    files.sort(key=lambda p: str(p).lower())
    return files


def even_sample(items, n):
    """Deterministic, evenly spaced sample of up to n items."""
    if len(items) <= n:
        return list(items)
    step = len(items) / n
    return [items[int(i * step)] for i in range(n)]


# --------------------------------------------------------------------------- #
# Image processing
# --------------------------------------------------------------------------- #
def target_size(w, h, mode, value):
    """Compute output (w, h) for a given mode. Never upscales."""
    if mode == "longest":
        longest = max(w, h)
        factor = min(1.0, value / longest) if longest else 1.0
    elif mode == "width":
        factor = min(1.0, value / w) if w else 1.0
    elif mode == "percent":
        factor = min(1.0, value / 100.0)
    else:
        factor = 1.0
    return max(1, round(w * factor)), max(1, round(h * factor))


def process_image(path: Path, mode, value, quality) -> bytes:
    """Decode, downscale and re-encode an image; return JPEG bytes."""
    with Image.open(path) as im:
        w, h = im.size
        tw, th = target_size(w, h, mode, value)
        # draft() lets the JPEG decoder load at a reduced scale -> much faster.
        im.draft("RGB", (tw, th))
        im = ImageOps.exif_transpose(im)
        if im.mode != "RGB":
            im = im.convert("RGB")
        if (tw, th) != im.size:
            im = im.resize((tw, th), Image.LANCZOS)
        buf = io.BytesIO()
        im.save(buf, format="JPEG", quality=int(quality), optimize=True)
        return buf.getvalue()


def parse_settings(src):
    mode = src.get("mode", "longest")
    if mode not in ("longest", "width", "percent"):
        mode = "longest"
    try:
        value = float(src.get("value", 2000))
    except (TypeError, ValueError):
        value = 2000.0
    try:
        quality = int(src.get("quality", 85))
    except (TypeError, ValueError):
        quality = 85
    quality = max(1, min(100, quality))
    rolls = src.get("rolls", None)
    if rolls is not None and not isinstance(rolls, list):
        rolls = None
    return mode, value, quality, rolls


# --------------------------------------------------------------------------- #
# Routes
# --------------------------------------------------------------------------- #
@app.route("/")
def index():
    return render_template("index.html", film_dir=str(FILM_DIR))


@app.route("/api/rolls")
def rolls():
    """List rolls (grouped by folder) with sample thumbnail paths."""
    grouped = scan_rolls()
    out = []
    for rid in sorted(grouped, key=str.lower):
        files = grouped[rid]
        parent = Path(rid)
        sample = even_sample(files, THUMBS_PER_ROLL)
        out.append({
            "id": rid,
            "name": parent.name or rid,
            "batch": parent.parts[0] if parent.parts else "",
            "count": len(files),
            "resized": _is_resized(parent),
            "thumbs": [p.relative_to(FILM_DIR).as_posix() for p in sample],
        })
    out.sort(key=lambda r: (r["batch"].lower(), r["name"].lower()))
    return jsonify(rolls=out, total=sum(r["count"] for r in out))


def safe_film_path(rel_str):
    """Resolve a client-supplied relative path, refusing anything outside FILM_DIR."""
    if not rel_str:
        return None
    target = (FILM_DIR / rel_str).resolve()
    root = FILM_DIR.resolve()
    if root not in target.parents:
        return None
    if target.suffix.lower() not in JPG_EXTS or not target.is_file():
        return None
    return target


def make_thumb(src: Path) -> Path:
    """Return a cached small JPEG thumbnail for src, generating it if needed."""
    try:
        mtime = src.stat().st_mtime_ns
    except OSError:
        mtime = 0
    key = hashlib.sha1(f"{src}|{mtime}|{THUMB_PX}".encode()).hexdigest()
    cached = THUMB_DIR / f"{key}.jpg"
    if cached.exists():
        return cached
    with Image.open(src) as im:
        im.draft("RGB", (THUMB_PX, THUMB_PX))
        im = ImageOps.exif_transpose(im)
        if im.mode != "RGB":
            im = im.convert("RGB")
        im.thumbnail((THUMB_PX, THUMB_PX), Image.LANCZOS)
        tmp = cached.with_suffix(".tmp")
        im.save(tmp, format="JPEG", quality=72, optimize=True)
        tmp.replace(cached)
    return cached


@app.route("/api/thumb")
def thumb():
    src = safe_film_path(request.args.get("path", ""))
    if not src:
        abort(404)
    try:
        cached = make_thumb(src)
    except Exception:
        abort(404)
    return send_file(cached, mimetype="image/jpeg", max_age=86400)


@app.route("/api/estimate", methods=["POST"])
def estimate():
    data = request.get_json(force=True, silent=True) or {}
    mode, value, quality, rolls = parse_settings(data)

    files = list_jpgs(rolls)
    count = len(files)
    if count == 0:
        return jsonify(
            file_count=0, original_bytes=0, estimated_bytes=0,
            sample_size=0, reduction_pct=0,
        )

    original_total = sum(p.stat().st_size for p in files)

    sample = even_sample(files, ESTIMATE_SAMPLE_SIZE)
    sample_in = sample_out = 0
    used = 0

    def measure(p):
        return p.stat().st_size, len(process_image(p, mode, value, quality))

    with ThreadPoolExecutor(max_workers=WORKERS) as ex:
        for fut in as_completed(ex.submit(measure, p) for p in sample):
            try:
                in_b, out_b = fut.result()
            except Exception:
                continue
            sample_in += in_b
            sample_out += out_b
            used += 1

    if used == 0 or sample_in == 0:
        return jsonify(
            file_count=count, original_bytes=original_total,
            estimated_bytes=0, sample_size=0, reduction_pct=0,
        )

    ratio = sample_out / sample_in
    estimated_total = int(original_total * ratio)
    reduction = (1 - estimated_total / original_total) * 100 if original_total else 0

    return jsonify(
        file_count=count,
        original_bytes=original_total,
        estimated_bytes=estimated_total,
        sample_size=used,
        reduction_pct=round(reduction, 1),
    )


# In-memory job registry for exports.
jobs = {}
jobs_lock = threading.Lock()


def run_export(job_id, mode, value, quality, rolls):
    files = list_jpgs(rolls)
    total = len(files)
    width = max(4, len(str(total)))
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    zip_path = OUTPUT_DIR / f"film-downscaled-{stamp}.zip"

    with jobs_lock:
        jobs[job_id].update(total=total, zip_path=str(zip_path))

    def work(idx, p):
        return idx, p, process_image(p, mode, value, quality)

    try:
        # Decode/encode in parallel (network + C decode release the GIL); the
        # zip itself is written from this one thread. The output name is the
        # source index, so completion order doesn't affect numbering.
        done = 0
        with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_STORED) as zf, \
                ThreadPoolExecutor(max_workers=WORKERS) as ex:
            futures = [ex.submit(work, i, p) for i, p in enumerate(files, start=1)]
            for fut in as_completed(futures):
                try:
                    idx, p, data = fut.result()
                except Exception as e:
                    with jobs_lock:
                        jobs[job_id]["skipped"].append(str(e))
                    done += 1
                    with jobs_lock:
                        jobs[job_id]["done"] = done
                    continue
                zf.writestr(f"{idx:0{width}d}.jpg", data)
                done += 1
                with jobs_lock:
                    jobs[job_id]["done"] = done
        with jobs_lock:
            jobs[job_id]["status"] = "done"
    except Exception as e:
        with jobs_lock:
            jobs[job_id]["status"] = "error"
            jobs[job_id]["error"] = str(e)


@app.route("/api/export", methods=["POST"])
def export():
    data = request.get_json(force=True, silent=True) or {}
    mode, value, quality, rolls = parse_settings(data)
    job_id = uuid.uuid4().hex
    with jobs_lock:
        jobs[job_id] = {
            "status": "running", "done": 0, "total": 0,
            "zip_path": None, "error": None, "skipped": [],
            "started": time.time(),
        }
    t = threading.Thread(
        target=run_export,
        args=(job_id, mode, value, quality, rolls),
        daemon=True,
    )
    t.start()
    return jsonify(job_id=job_id)


@app.route("/api/export/<job_id>")
def export_status(job_id):
    with jobs_lock:
        job = jobs.get(job_id)
        if not job:
            abort(404)
        out = {
            "status": job["status"],
            "done": job["done"],
            "total": job["total"],
            "zip_path": job["zip_path"],
            "error": job["error"],
            "skipped": len(job["skipped"]),
        }
        if job["status"] == "done" and job["zip_path"]:
            out["zip_bytes"] = os.path.getsize(job["zip_path"])
    return jsonify(out)


@app.route("/api/export/<job_id>/download")
def export_download(job_id):
    with jobs_lock:
        job = jobs.get(job_id)
    if not job or job["status"] != "done" or not job["zip_path"]:
        abort(404)
    return send_file(
        job["zip_path"],
        as_attachment=True,
        download_name=Path(job["zip_path"]).name,
    )


if __name__ == "__main__":
    print(f"Film dir: {FILM_DIR}")
    print("Open http://127.0.0.1:5000")
    app.run(host="127.0.0.1", port=5000, threaded=True)
