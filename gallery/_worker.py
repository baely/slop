"""Per-image work for build.py, run in a process pool.

Given a source scan, writes a web-sized copy and a thumbnail. Returns None
for blank "missing frame" slides (near-white images) so the build drops them.
"""

from PIL import Image, ImageOps
import numpy as np

FULL_MAX = 2000  # long edge of the theater image
THUMB_MAX = 480  # long edge of the grid thumbnail


def process(job):
    src, dest_full, dest_thumb = job
    img = Image.open(src)
    # decode at reduced scale straight from the JPEG DCT: big speedup on 20MP scans
    img.draft("RGB", (FULL_MAX * 2, FULL_MAX * 2))
    img = ImageOps.exif_transpose(img).convert("RGB")

    small = img.resize((200, 133), Image.NEAREST)
    if (np.asarray(small) > 240).all(axis=2).mean() > 0.85:
        return None

    full = img.copy()
    full.thumbnail((FULL_MAX, FULL_MAX), Image.LANCZOS)
    full.save(dest_full, "JPEG", quality=85, optimize=True, progressive=True)

    thumb = img.copy()
    thumb.thumbnail((THUMB_MAX, THUMB_MAX), Image.LANCZOS)
    thumb.save(dest_thumb, "JPEG", quality=78, optimize=True)
    return True
