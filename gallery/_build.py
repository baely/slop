import os, sys, json, re
from concurrent.futures import ProcessPoolExecutor, as_completed

sys.path.insert(0, os.environ["GALLERY_DIR"])
from _gallery_worker import process_image

GALLERY_DIR = os.environ["GALLERY_DIR"]
DIST_DIR = os.environ["DIST_DIR"]
ORIGINALS_DIR = os.environ["ORIGINALS_DIR"]

def find_images():
    imgs = []
    for root, dirs, files in os.walk(GALLERY_DIR):
        if any(x in root for x in ("resized", "dist", ".thumbs_tmp")):
            continue
        for f in files:
            if f.lower().endswith((".jpg", ".jpeg", ".png")):
                imgs.append(os.path.join(root, f))
    imgs.sort()
    return imgs

def load_metadata():
    path = os.path.join(GALLERY_DIR, "metadata.json")
    if os.path.exists(path):
        with open(path) as f:
            entries = json.load(f)
        # Index by src path
        return {e["src"]: e for e in entries}
    return {}

def meta_for_image(src, meta_map):
    rel = os.path.relpath(src, GALLERY_DIR)
    if rel in meta_map:
        return meta_map[rel]
    # Fallback: infer from path
    parts = rel.split(os.sep)
    date_match = re.search(r'(\d{4}-\d{2}-\d{2})', parts[0]) if parts else None
    date = date_match.group(1) if date_match else ""
    roll = parts[1] if len(parts) > 1 else ""
    stock = re.sub(r'^\d+\s+', '', roll)
    if stock.isdigit():
        stock = ""
    return {"stock": stock, "date": date, "location": "", "maps": ""}

if __name__ == "__main__":
    images = find_images()
    meta_map = load_metadata()
    print(f"Found {len(images)} images, {len(meta_map)} metadata entries")

    results = [None] * len(images)
    skipped = 0
    ncpu = os.cpu_count() or 4

    tasks = []
    for i, img in enumerate(images):
        meta = meta_for_image(img, meta_map)
        tasks.append((i, img, meta))

    with ProcessPoolExecutor(max_workers=ncpu * 2) as pool:
        futures = {pool.submit(process_image, t): t[0] for t in tasks}
        done = 0
        for future in as_completed(futures):
            done += 1
            if done % 100 == 0:
                print(f"Processed {done} / {len(images)}")
            i = futures[future]
            result = future.result()
            if result is None:
                skipped += 1
            else:
                results[i] = result

    # Re-index sequentially
    final = []
    new_idx = 0
    for r in results:
        if r is not None:
            old_fname = r["o"].split("/")[1]
            new_fname = f"{new_idx:04d}.jpg"
            if old_fname != new_fname:
                old_path = os.path.join(ORIGINALS_DIR, old_fname)
                new_path = os.path.join(ORIGINALS_DIR, new_fname)
                if os.path.exists(old_path):
                    os.rename(old_path, new_path)
            r["o"] = f"img/{new_fname}"
            final.append(r)
            new_idx += 1

    for f in os.listdir(ORIGINALS_DIR):
        num = int(f.split(".")[0])
        if num >= new_idx:
            os.remove(os.path.join(ORIGINALS_DIR, f))

    print(f"Skipped {skipped} missing frame slides")
    print(f"Kept {len(final)} images")

    json_data = json.dumps(final, separators=(",", ":"))

    html = """<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Gallery</title>
<style>
  *, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }
  body { background: #000; color: #fff; font-family: -apple-system, BlinkMacSystemFont, sans-serif; }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 2px;
    padding: 2px;
  }
  .grid img {
    width: 100%;
    aspect-ratio: 3/2;
    object-fit: cover;
    cursor: pointer;
    display: block;
    opacity: 0;
    transition: opacity 0.3s;
  }
  .grid img.loaded { opacity: 1; }
  .grid img:hover { opacity: 0.8; }
  .theater {
    display: none;
    position: fixed;
    inset: 0;
    z-index: 100;
    background: #000;
    flex-direction: column;
  }
  .theater.open { display: flex; }
  .theater-img-wrap {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 0;
  }
  .theater img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }
  .theater-close {
    position: fixed;
    top: 16px;
    right: 20px;
    font-size: 32px;
    color: #fff;
    cursor: pointer;
    z-index: 101;
    opacity: 0.6;
    background: none;
    border: none;
    font-family: inherit;
  }
  .theater-close:hover { opacity: 1; }
  .theater-nav {
    position: fixed;
    top: 50%;
    transform: translateY(-50%);
    font-size: 48px;
    color: #fff;
    cursor: pointer;
    z-index: 101;
    opacity: 0.4;
    background: none;
    border: none;
    padding: 20px;
    font-family: inherit;
    user-select: none;
  }
  .theater-nav:hover { opacity: 0.9; }
  .theater-prev { left: 0; }
  .theater-next { right: 0; }
  .theater-info {
    flex-shrink: 0;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 13px;
    color: rgba(255,255,255,0.5);
    z-index: 101;
    font-family: -apple-system, BlinkMacSystemFont, 'SF Mono', monospace;
    letter-spacing: 0.02em;
  }
  .theater-info span { white-space: nowrap; }
  .theater-info .sep { margin: 0 10px; opacity: 0.3; }
  .theater-info a {
    color: rgba(255,255,255,0.5);
    text-decoration: none;
  }
  .theater-info a:hover {
    color: rgba(255,255,255,0.8);
    text-decoration: underline;
  }
</style>
</head>
<body>
<div class="grid" id="grid"></div>
<div class="theater" id="theater">
  <button class="theater-close" id="close">&times;</button>
  <button class="theater-nav theater-prev" id="prev">&#8249;</button>
  <button class="theater-nav theater-next" id="next">&#8250;</button>
  <div class="theater-img-wrap"><img id="theater-img" src="" alt=""></div>
  <div class="theater-info" id="info"></div>
</div>
<script>
const images = """ + json_data + """;
const grid = document.getElementById('grid');
const theater = document.getElementById('theater');
const theaterImg = document.getElementById('theater-img');
const info = document.getElementById('info');
let current = 0;

images.forEach((img, i) => {
  const el = document.createElement('img');
  el.loading = 'lazy';
  el.src = 'data:image/jpeg;base64,' + img.t;
  el.alt = '';
  el.addEventListener('load', () => el.classList.add('loaded'));
  el.addEventListener('click', () => openTheater(i));
  grid.appendChild(el);
});

function buildInfo(i) {
  const img = images[i];
  const parts = [];
  parts.push((i + 1) + '/' + images.length);
  if (img.s) parts.push(img.s);
  if (img.d) parts.push(img.d);
  if (img.l) {
    if (img.m) {
      parts.push('<a href="' + img.m + '" target="_blank" rel="noopener">' + img.l + '</a>');
    } else {
      parts.push(img.l);
    }
  }
  info.innerHTML = parts.map(p => '<span>' + p + '</span>').join('<span class="sep">|</span>');
}

function openTheater(i) {
  current = i;
  theaterImg.src = images[i].o;
  buildInfo(i);
  theater.classList.add('open');
  document.body.style.overflow = 'hidden';
}

function closeTheater() {
  theater.classList.remove('open');
  document.body.style.overflow = '';
}

function nav(dir) {
  current = (current + dir + images.length) % images.length;
  theaterImg.src = images[current].o;
  buildInfo(current);
}

document.getElementById('close').addEventListener('click', closeTheater);
document.getElementById('prev').addEventListener('click', () => nav(-1));
document.getElementById('next').addEventListener('click', () => nav(1));

document.addEventListener('keydown', (e) => {
  if (!theater.classList.contains('open')) return;
  if (e.key === 'Escape') closeTheater();
  if (e.key === 'ArrowLeft') nav(-1);
  if (e.key === 'ArrowRight') nav(1);
});

theater.addEventListener('click', (e) => {
  if (e.target === theater) closeTheater();
});
</script>
</body>
</html>"""

    with open(os.path.join(DIST_DIR, "index.html"), "w") as f:
        f.write(html)

    print("Done!")
