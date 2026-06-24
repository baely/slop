import os, sys, json
from concurrent.futures import ProcessPoolExecutor, as_completed

BASE = os.path.dirname(os.path.abspath(__file__))
SRC = os.environ.get("SRC_DIR", os.path.join(BASE, "..", "gallery", "Bailey Butler 2026-06-01"))
DIST = os.path.join(BASE, "dist")
IMG_DIR = os.path.join(DIST, "img")

os.environ["IMG_DIR"] = IMG_DIR
sys.path.insert(0, BASE)
from _cull_worker import process


def find_images():
    out = []
    for root, dirs, files in os.walk(SRC):
        for f in files:
            if f.lower().endswith((".jpg", ".jpeg", ".png")):
                full = os.path.join(root, f)
                rel = os.path.relpath(full, SRC)
                out.append((rel, full))
    out.sort(key=lambda x: x[0])
    return out


HTML_TEMPLATE = r"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, user-scalable=no">
<title>Culler</title>
<style>
  *,*::before,*::after{margin:0;padding:0;box-sizing:border-box}
  :root{ --accent:#ff4d6d; --gold:#ffd24d; }
  html,body{height:100%}
  body{background:#000;color:#fff;font-family:-apple-system,BlinkMacSystemFont,'SF Pro Text',sans-serif;overflow:hidden;-webkit-user-select:none;user-select:none}
  button{font-family:inherit}

  .screen{position:fixed;inset:0;display:none}
  .screen.active{display:block}

  /* shared top/bottom bars */
  .bar{position:fixed;top:0;left:0;right:0;height:52px;display:flex;align-items:center;gap:12px;padding:0 16px;z-index:30;background:linear-gradient(#000d,transparent)}
  .bar .title{font-size:14px;font-weight:600;letter-spacing:.01em}
  .bar .spacer{flex:1}
  .bar .acts{display:flex;align-items:center;gap:8px}
  .bar .acts span{font-size:12px;color:#9a9a9a;font-variant-numeric:tabular-nums}
  .btn{background:#ffffff14;border:1px solid #ffffff22;color:#fff;height:34px;padding:0 14px;border-radius:9px;font-size:13px;cursor:pointer}
  .btn:hover{background:#ffffff26}
  .btn:disabled{opacity:.35;cursor:default}
  .btn.primary{background:#fff;color:#000;border-color:#fff;font-weight:600}
  .btn.icon{width:38px;padding:0;font-size:16px}
  .back{background:none;border:none;color:#cfcfcf;font-size:14px;cursor:pointer;padding:6px 4px}
  .back:hover{color:#fff}

  /* ---------- HOME ---------- */
  #home .wrap{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:34px;padding:24px}
  #home .count{font-size:15px;color:#8a8a8a;letter-spacing:.04em;text-transform:uppercase}
  #home .modes{display:flex;gap:18px;flex-wrap:wrap;justify-content:center;max-width:760px}
  .modecard{background:#0e0e0e;border:1px solid #ffffff1f;border-radius:16px;padding:30px 28px;width:330px;max-width:88vw;text-align:left;cursor:pointer;transition:border-color .15s,transform .15s}
  .modecard:hover{border-color:#ffffff45;transform:translateY(-2px)}
  .modecard .mt{font-size:22px;font-weight:600;margin-bottom:8px}
  .modecard .md{font-size:13px;line-height:1.5;color:#9a9a9a}

  /* ---------- SHORTLIST ---------- */
  #shortlist .imgwrap{position:absolute;inset:52px 0 76px 0;display:flex;align-items:center;justify-content:center}
  #photo{max-width:100%;max-height:100%;display:block;transform-origin:center center;transition:transform .12s ease}
  #frame{position:absolute;inset:52px 0 76px 0;pointer-events:none;border:0 solid var(--accent);transition:border-width .1s}
  #frame.on{border-width:4px}
  .nav{position:fixed;top:50%;transform:translateY(-50%);font-size:46px;line-height:1;color:#fff;opacity:.28;background:none;border:none;padding:24px 18px;cursor:pointer;z-index:25}
  .nav:hover{opacity:.85}
  .prev{left:0}.next{right:0}
  #sbottom{position:fixed;bottom:0;left:0;right:0;height:76px;display:flex;align-items:center;gap:12px;padding:0 16px;z-index:30;background:linear-gradient(transparent,#000d)}
  #filename{flex:1;font-size:12px;color:#9a9a9a;font-family:ui-monospace,'SF Mono',monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  #likeBtn{background:#ffffff14;border:1px solid #ffffff2a;color:#fff;height:42px;padding:0 20px;border-radius:21px;font-size:15px;cursor:pointer;display:flex;align-items:center;gap:8px;transition:all .12s}
  #likeBtn.on{background:var(--accent);border-color:var(--accent)}
  #likeBtn .h{font-size:17px}
  #finishBtn{background:none;border:1px solid #ffffff33;color:#fff;height:42px;padding:0 18px;border-radius:21px;font-size:14px;cursor:pointer}
  #finishBtn.ready{background:#fff;color:#000;border-color:#fff;font-weight:600}
  .hint{position:fixed;bottom:84px;left:50%;transform:translateX(-50%);font-size:11px;color:#6a6a6a;z-index:25;text-align:center}
  #status b{color:#fff;font-weight:600}
  #status .heart{color:var(--accent)}

  /* shortlist survivors overlay */
  #results{position:fixed;inset:0;background:#0a0a0a;z-index:50;display:none;flex-direction:column}
  #results.open{display:flex}
  #rhead{display:flex;align-items:center;justify-content:space-between;padding:18px 20px;border-bottom:1px solid #ffffff15}
  #rhead h2{font-size:16px;font-weight:600}
  #rhead .sub{font-size:12px;color:#8a8a8a;margin-top:3px}
  #ractions{display:flex;gap:8px}

  /* ---------- shared grids ---------- */
  .grid{position:absolute;inset:52px 0 0 0;overflow:auto;display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:3px;padding:3px;align-content:start}
  .cell{position:relative;cursor:pointer;background:#111}
  .cell img{width:100%;aspect-ratio:3/2;object-fit:cover;display:block;opacity:.9}
  .cell:hover img{opacity:1}
  .cell .fn{position:absolute;left:0;right:0;bottom:0;font-size:10px;font-family:ui-monospace,monospace;color:#fff;background:#000a;padding:3px 5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .cell.sel{outline:3px solid var(--accent);outline-offset:-3px}
  .cell .check{position:absolute;top:6px;right:6px;width:22px;height:22px;border-radius:50%;background:var(--accent);color:#fff;font-size:13px;display:none;align-items:center;justify-content:center}
  .cell.sel .check{display:flex}
  .cell .rankno{position:absolute;top:6px;left:6px;min-width:24px;height:24px;padding:0 6px;border-radius:12px;background:#000b;border:1px solid #ffffff33;color:#fff;font-size:12px;font-weight:600;display:flex;align-items:center;justify-content:center}
  .cell.top1 .rankno{background:var(--gold);color:#000;border-color:var(--gold)}
  .cell.top2 .rankno,.cell.top3 .rankno{background:#fff;color:#000;border-color:#fff}

  /* ---------- PAIRWISE ---------- */
  #pair .versus{position:fixed;inset:52px 0 60px 0;display:flex;gap:8px;padding:8px}
  .side{flex:1;position:relative;display:flex;align-items:center;justify-content:center;cursor:pointer;background:#0b0b0b;border:2px solid transparent;border-radius:10px;overflow:hidden}
  .side:hover{border-color:#ffffff45}
  .side:active{border-color:var(--accent)}
  .side img{max-width:100%;max-height:100%;display:block;transform-origin:center center}
  .side .fn{position:absolute;bottom:0;left:0;right:0;font-size:11px;font-family:ui-monospace,monospace;color:#cfcfcf;background:#000b;padding:5px 6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;text-align:center}
  .side .key{position:absolute;top:10px;left:10px;font-size:11px;color:#888;background:#000a;border:1px solid #ffffff22;border-radius:6px;padding:2px 7px}
  .vs{display:flex;align-items:center;justify-content:center;color:#666;font-size:13px;letter-spacing:.1em}
  .pairhint{position:fixed;bottom:22px;left:0;right:0;text-align:center;font-size:12px;color:#7a7a7a;z-index:20}
  #prog{position:fixed;bottom:0;left:0;height:3px;background:var(--accent);width:0;transition:width .2s;z-index:31}
  @media(max-width:700px){ #pair .versus{flex-direction:column} }

  #flash{position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);background:#000d;border:1px solid #ffffff30;padding:14px 22px;border-radius:12px;font-size:14px;z-index:60;opacity:0;transition:opacity .2s;pointer-events:none}
  #flash.show{opacity:1}
  @media (max-width:640px){ .hint{display:none} #filename{display:none} }
</style>
</head>
<body>

<!-- ===== HOME ===== -->
<div class="screen active" id="home">
  <div class="wrap">
    <div class="count" id="homeCount"></div>
    <div class="modes">
      <button class="modecard" id="goShortlist">
        <div class="mt">Shortlist</div>
        <div class="md">Round-based culling. Go through full-screen, like the keepers, finish the round to discard the rest. Repeat until you've got your tops.</div>
      </button>
      <button class="modecard" id="goRank">
        <div class="mt">Rank</div>
        <div class="md">Pick a batch, then compare them two at a time. Produces a definitive 1&ndash;N ranking with the fewest comparisons.</div>
      </button>
    </div>
  </div>
</div>

<!-- ===== SHORTLIST ===== -->
<div class="screen" id="shortlist">
  <div class="bar">
    <button class="back" data-home>&#8249; Home</button>
    <div id="status"></div>
    <div class="spacer"></div>
    <div class="acts">
      <button class="btn icon" id="rotate" title="Rotate (R)">&#8635;</button>
      <button class="btn icon" id="resultsBtn" title="Survivors (S)">&#9638;</button>
    </div>
  </div>
  <div class="imgwrap"><img id="photo" alt=""></div>
  <div id="frame"></div>
  <button class="nav prev" id="prev">&#8249;</button>
  <button class="nav next" id="next">&#8250;</button>
  <div id="sbottom">
    <div id="filename"></div>
    <button id="finishBtn">Finish round</button>
    <button id="likeBtn"><span class="h">&#9825;</span><span class="lbl">Like</span></button>
  </div>
  <div class="hint">&larr;&rarr; navigate &nbsp;&middot;&nbsp; space = like &nbsp;&middot;&nbsp; R = rotate &nbsp;&middot;&nbsp; enter = finish round</div>
  <div id="results">
    <div id="rhead">
      <div><h2 id="rtitle">Survivors</h2><div class="sub" id="rsub"></div></div>
      <div id="ractions">
        <button class="btn" id="copyBtn">Copy filenames</button>
        <button class="btn primary" id="closeResults">Keep culling</button>
      </div>
    </div>
    <div class="grid" id="rgrid" style="inset:auto;position:relative;flex:1"></div>
  </div>
</div>

<!-- ===== BATCH SELECT ===== -->
<div class="screen" id="batch">
  <div class="bar">
    <button class="back" data-home>&#8249; Home</button>
    <div class="title">Select a batch to rank</div>
    <div class="spacer"></div>
    <div class="acts">
      <span id="batchCount">0 selected</span>
      <button class="btn" id="useSurvivors" title="Use shortlist survivors">Survivors</button>
      <button class="btn" id="selAll">All</button>
      <button class="btn" id="selClear">Clear</button>
      <button class="btn primary" id="startRank" disabled>Rank &#8594;</button>
    </div>
  </div>
  <div class="grid" id="selgrid"></div>
</div>

<!-- ===== PAIRWISE ===== -->
<div class="screen" id="pair">
  <div class="bar">
    <button class="back" id="pairBack">&#8249; Batch</button>
    <div class="title" id="pairProgress"></div>
    <div class="spacer"></div>
    <div class="acts">
      <button class="btn" id="undoBtn">&#8630; Undo</button>
    </div>
  </div>
  <div class="versus">
    <div class="side" id="sideA"><span class="key">&larr;</span><img alt=""><span class="fn"></span></div>
    <div class="vs">VS</div>
    <div class="side" id="sideB"><span class="key">&rarr;</span><img alt=""><span class="fn"></span></div>
  </div>
  <div class="pairhint">click the better photo &nbsp;&middot;&nbsp; or press &larr; / &rarr; &nbsp;&middot;&nbsp; U to undo</div>
  <div id="prog"></div>
</div>

<!-- ===== RANKING RESULT ===== -->
<div class="screen" id="rank">
  <div class="bar">
    <button class="back" data-home>&#8249; Home</button>
    <div class="title">Final ranking</div>
    <div class="spacer"></div>
    <div class="acts">
      <button class="btn" id="reRank">Adjust batch</button>
      <button class="btn primary" id="copyRank">Copy ranking</button>
    </div>
  </div>
  <div class="grid" id="rankgrid"></div>
</div>

<div id="flash"></div>

<script>
/*__DATA__*/

const ITEMS = IMAGES.map(im => ({f:im.f, n:im.n, t:im.t, rot:0, liked:false, sel:false}));

const $ = id => document.getElementById(id);
const screens = ['home','shortlist','batch','pair','rank'];
let activeScreen = 'home';
function showScreen(name){
  screens.forEach(s => $(s).classList.toggle('active', s===name));
  activeScreen = name;
}
$('homeCount').textContent = ITEMS.length + ' photos';

let flashTimer;
function flash(msg){
  $('flash').textContent = msg;
  $('flash').classList.add('show');
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => $('flash').classList.remove('show'), 1600);
}

function fitImg(img, box, rot){
  const r = ((rot % 360) + 360) % 360;
  let s = 1;
  if(r === 90 || r === 270){
    const cw = box.clientWidth, ch = box.clientHeight, iw = img.clientWidth, ih = img.clientHeight;
    if(iw && ih) s = Math.min(cw/ih, ch/iw);
  }
  img.style.transform = `rotate(${r}deg) scale(${s})`;
}

/* ============ SHORTLIST ============ */
let pool = ITEMS.slice();
let round = 1, pos = 0;
const photo = $('photo'), sImgwrap = document.querySelector('#shortlist .imgwrap');

function likedCount(){ return pool.filter(p=>p.liked).length; }
function sFit(){ fitImg(photo, sImgwrap, pool[pos].rot); }
function sShow(){
  const item = pool[pos];
  photo.src = item.f;
  $('filename').textContent = item.n;
  $('status').innerHTML = `<b>Round ${round}</b> &nbsp; ${pos+1}/${pool.length} &nbsp; <span class="heart">&#9829;</span> ${likedCount()}`;
  const on = item.liked;
  $('likeBtn').classList.toggle('on', on);
  $('likeBtn').querySelector('.h').innerHTML = on ? '&#9829;' : '&#9825;';
  $('likeBtn').querySelector('.lbl').textContent = on ? 'Liked' : 'Like';
  $('frame').classList.toggle('on', on);
  $('finishBtn').classList.toggle('ready', pos === pool.length - 1);
  [pos+1, pos+2].forEach(i => { if(pool[i]){ const im = new Image(); im.src = pool[i].f; }});
}
photo.onload = sFit;
function sGo(d){ pos = (pos + d + pool.length) % pool.length; sShow(); }
function sLike(){ pool[pos].liked = !pool[pos].liked; sShow(); }
function sRotate(){ pool[pos].rot += 90; sFit(); }
function finishRound(){
  const survivors = pool.filter(p => p.liked);
  if(survivors.length === 0){ flash('Like at least one to continue'); return; }
  if(survivors.length === pool.length){ flash('You liked them all — be choosier to narrow down'); return; }
  pool = survivors; pool.forEach(p => p.liked = false);
  round++; pos = 0; sShow();
  flash(`Round ${round} — ${pool.length} left`);
}
function openResults(){
  const kept = pool.filter(p => p.liked);
  const list = kept.length ? kept : pool;
  $('rtitle').textContent = kept.length ? 'Liked this round' : `Round ${round} survivors`;
  $('rsub').textContent = `${list.length} photo${list.length===1?'':'s'} — click to jump, or copy the filenames`;
  const grid = $('rgrid'); grid.innerHTML = '';
  list.forEach(item => {
    const cell = document.createElement('div');
    cell.className = 'cell';
    cell.innerHTML = `<img src="data:image/jpeg;base64,${item.t}"><span class="fn">${item.n}</span>`;
    cell.addEventListener('click', () => { pos = pool.indexOf(item); $('results').classList.remove('open'); sShow(); });
    grid.appendChild(cell);
  });
  grid._list = list;
  $('results').classList.add('open');
}
$('copyBtn').addEventListener('click', () => {
  const list = $('rgrid')._list || [];
  navigator.clipboard.writeText(list.map(p=>p.n).join('\n'))
    .then(() => flash(`Copied ${list.length} filenames`), () => flash('Copy failed'));
});
$('closeResults').addEventListener('click', () => $('results').classList.remove('open'));
$('prev').addEventListener('click', () => sGo(-1));
$('next').addEventListener('click', () => sGo(1));
$('rotate').addEventListener('click', sRotate);
$('resultsBtn').addEventListener('click', openResults);
$('likeBtn').addEventListener('click', sLike);
$('finishBtn').addEventListener('click', finishRound);
function enterShortlist(){ showScreen('shortlist'); sShow(); }

/* ============ BATCH SELECT ============ */
function buildSelGrid(){
  const grid = $('selgrid'); grid.innerHTML = '';
  ITEMS.forEach(item => {
    const cell = document.createElement('div');
    cell.className = 'cell' + (item.sel ? ' sel' : '');
    cell.innerHTML = `<img src="data:image/jpeg;base64,${item.t}"><div class="check">&#10003;</div><span class="fn">${item.n}</span>`;
    cell.addEventListener('click', () => {
      item.sel = !item.sel;
      cell.classList.toggle('sel', item.sel);
      updateBatchCount();
    });
    grid.appendChild(cell);
  });
  updateBatchCount();
}
function updateBatchCount(){
  const n = ITEMS.filter(i=>i.sel).length;
  $('batchCount').textContent = n + ' selected';
  $('startRank').disabled = n < 2;
}
$('selAll').addEventListener('click', () => { ITEMS.forEach(i=>i.sel=true); buildSelGrid(); });
$('selClear').addEventListener('click', () => { ITEMS.forEach(i=>i.sel=false); buildSelGrid(); });
$('useSurvivors').addEventListener('click', () => {
  if(pool.length === ITEMS.length){ flash('No shortlist yet — cull first or pick manually'); return; }
  ITEMS.forEach(i => i.sel = false);
  pool.forEach(i => i.sel = true);
  buildSelGrid();
  flash(`Selected ${pool.length} survivors`);
});
$('startRank').addEventListener('click', () => {
  const batch = ITEMS.filter(i=>i.sel);
  if(batch.length < 2) return;
  startSort(batch);
});
function enterBatch(){ showScreen('batch'); buildSelGrid(); }

/* ============ PAIRWISE RANK (interactive merge sort + undo) ============ */
let currentBatch = [];
let decisions = [], replayIndex = 0, epoch = 0;
let pendingResolve = null, pendingPair = null;

function estComparisons(n){ return n <= 1 ? 0 : Math.ceil(n * Math.log2(n) - n + 1); }

function compare(a, b, myEpoch){
  return new Promise(resolve => {
    if(replayIndex < decisions.length){          // silent replay
      resolve(decisions[replayIndex++]);
      return;
    }
    pendingPair = {a, b};
    pendingResolve = (d) => {
      if(myEpoch !== epoch) return;              // abandoned by undo/restart
      decisions.push(d); replayIndex++;
      pendingResolve = null;
      resolve(d);
    };
    renderPair(a, b);
  });
}

async function mergeSort(arr, myEpoch){
  if(arr.length <= 1) return arr;
  const mid = arr.length >> 1;
  const left = await mergeSort(arr.slice(0, mid), myEpoch);
  const right = await mergeSort(arr.slice(mid), myEpoch);
  const res = []; let i = 0, j = 0;
  while(i < left.length && j < right.length){
    const c = await compare(left[i], right[j], myEpoch);
    if(myEpoch !== epoch) return res;            // bail out of abandoned run
    if(c <= 0) res.push(left[i++]); else res.push(right[j++]);
  }
  while(i < left.length) res.push(left[i++]);
  while(j < right.length) res.push(right[j++]);
  return res;
}

function startSort(batch){
  currentBatch = batch.slice();
  decisions = [];
  runSort();
  showScreen('pair');
}
function runSort(){                              // (re)start the sort engine at current decisions
  epoch++;
  const myEpoch = epoch;
  replayIndex = 0;
  pendingResolve = null;
  mergeSort(currentBatch.slice(), myEpoch).then(sorted => {
    if(myEpoch !== epoch) return;
    showRanking(sorted);
  });
}

function renderPair(a, b){
  const total = estComparisons(currentBatch.length);
  const num = decisions.length + 1;
  $('pairProgress').textContent = `Comparison ${num} of ~${total}`;
  $('prog').style.width = Math.min(99, (decisions.length / Math.max(1,total)) * 100) + '%';
  $('undoBtn').disabled = decisions.length === 0;
  setSide('sideA', a);
  setSide('sideB', b);
}
function setSide(id, item){
  const side = $(id), img = side.querySelector('img');
  img.onload = () => fitImg(img, side, item.rot);
  img.src = item.f;
  side.querySelector('.fn').textContent = item.n;
}
function choose(side){
  if(!pendingResolve) return;
  pendingResolve(side === 'A' ? -1 : 1);
}
function undoCompare(){
  if(decisions.length === 0) return;
  decisions.pop();
  runSort();                                     // replays remaining decisions, stops at the one to redo
}

$('sideA').addEventListener('click', () => choose('A'));
$('sideB').addEventListener('click', () => choose('B'));
$('undoBtn').addEventListener('click', undoCompare);
$('pairBack').addEventListener('click', () => { epoch++; enterBatch(); });
window.addEventListener('resize', () => {
  if(activeScreen === 'pair' && pendingPair){
    fitImg($('sideA').querySelector('img'), $('sideA'), pendingPair.a.rot);
    fitImg($('sideB').querySelector('img'), $('sideB'), pendingPair.b.rot);
  } else if(activeScreen === 'shortlist'){ sFit(); }
});

/* ============ RANKING RESULT ============ */
let lastRanking = [];
function showRanking(sorted){
  lastRanking = sorted;
  $('prog').style.width = '100%';
  const grid = $('rankgrid'); grid.innerHTML = '';
  sorted.forEach((item, i) => {
    const cell = document.createElement('div');
    cell.className = 'cell' + (i < 3 ? ' top' + (i+1) : '');
    cell.innerHTML = `<img src="data:image/jpeg;base64,${item.t}"><div class="rankno">${i+1}</div><span class="fn">${item.n}</span>`;
    grid.appendChild(cell);
  });
  showScreen('rank');
}
$('copyRank').addEventListener('click', () => {
  const text = lastRanking.map((p,i) => `${i+1}. ${p.n}`).join('\n');
  navigator.clipboard.writeText(text)
    .then(() => flash(`Copied ranking of ${lastRanking.length}`), () => flash('Copy failed'));
});
$('reRank').addEventListener('click', enterBatch);

/* ============ NAV / KEYS ============ */
$('goShortlist').addEventListener('click', enterShortlist);
$('goRank').addEventListener('click', enterBatch);
document.querySelectorAll('[data-home]').forEach(b => b.addEventListener('click', () => { epoch++; showScreen('home'); }));

document.addEventListener('keydown', (e) => {
  if(activeScreen === 'shortlist'){
    if($('results').classList.contains('open')){
      if(e.key === 'Escape' || e.key === 's' || e.key === 'S') $('results').classList.remove('open');
      return;
    }
    switch(e.key){
      case 'ArrowLeft': sGo(-1); break;
      case 'ArrowRight': sGo(1); break;
      case ' ': case 'ArrowUp': e.preventDefault(); sLike(); break;
      case 'r': case 'R': sRotate(); break;
      case 'Enter': finishRound(); break;
      case 's': case 'S': openResults(); break;
      case 'Escape': showScreen('home'); break;
    }
  } else if(activeScreen === 'pair'){
    switch(e.key){
      case 'ArrowLeft': choose('A'); break;
      case 'ArrowRight': choose('B'); break;
      case 'u': case 'U': case 'Backspace': e.preventDefault(); undoCompare(); break;
      case 'Escape': epoch++; enterBatch(); break;
    }
  } else if(activeScreen === 'batch' || activeScreen === 'rank'){
    if(e.key === 'Escape') showScreen('home');
  }
});

showScreen('home');
</script>
</body>
</html>"""


if __name__ == "__main__":
    os.makedirs(IMG_DIR, exist_ok=True)
    for f in os.listdir(IMG_DIR):
        os.remove(os.path.join(IMG_DIR, f))

    imgs = find_images()
    print(f"Found {len(imgs)} images in {SRC}")

    tasks = [(i, full, rel) for i, (rel, full) in enumerate(imgs)]
    results = [None] * len(tasks)
    ncpu = os.cpu_count() or 4
    with ProcessPoolExecutor(max_workers=ncpu) as pool:
        futures = {pool.submit(process, t): t[0] for t in tasks}
        done = 0
        for fut in as_completed(futures):
            done += 1
            if done % 25 == 0:
                print(f"Processed {done}/{len(tasks)}")
            results[futures[fut]] = fut.result()

    data = json.dumps([r for r in results if r], separators=(",", ":"))
    html = HTML_TEMPLATE.replace("/*__DATA__*/", "const IMAGES = " + data + ";")
    with open(os.path.join(DIST, "index.html"), "w") as f:
        f.write(html)
    print(f"Wrote {len([r for r in results if r])} images. Done!")
