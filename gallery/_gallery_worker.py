import os, base64, io, json
from PIL import Image
import numpy as np

ORIGINALS_DIR = os.environ["ORIGINALS_DIR"]

def process_image(args):
    idx, src, meta = args
    img = Image.open(src).convert("RGB")

    # Check for missing frame slide
    small = img.resize((200, 133), Image.NEAREST)
    arr = np.array(small)
    if (arr > 240).all(axis=2).mean() > 0.85:
        return None

    fname = f"{idx:04d}.jpg"

    # Resize original for web
    orig = img.copy()
    orig.thumbnail((2000, 2000), Image.LANCZOS)
    orig.save(os.path.join(ORIGINALS_DIR, fname), "JPEG", quality=85)

    # Generate thumbnail in memory
    thumb = img.copy()
    thumb.thumbnail((200, 200), Image.LANCZOS)
    buf = io.BytesIO()
    thumb.save(buf, "JPEG", quality=80)
    thumb_b64 = base64.b64encode(buf.getvalue()).decode()

    result = {"t": thumb_b64, "o": f"img/{fname}"}
    if meta.get("stock"):
        result["s"] = meta["stock"]
    if meta.get("date"):
        result["d"] = meta["date"]
    if meta.get("location"):
        result["l"] = meta["location"]
    if meta.get("maps"):
        result["m"] = meta["maps"]

    return result
