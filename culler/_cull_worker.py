import os, io, base64
from PIL import Image

IMG_DIR = os.environ["IMG_DIR"]


def process(args):
    idx, src, name = args
    img = Image.open(src).convert("RGB")
    fname = f"{idx:04d}.jpg"

    big = img.copy()
    big.thumbnail((1800, 1800), Image.LANCZOS)
    big.save(os.path.join(IMG_DIR, fname), "JPEG", quality=85)

    thumb = img.copy()
    thumb.thumbnail((260, 260), Image.LANCZOS)
    buf = io.BytesIO()
    thumb.save(buf, "JPEG", quality=78)
    t = base64.b64encode(buf.getvalue()).decode()

    return {"f": f"img/{fname}", "n": name, "t": t}
