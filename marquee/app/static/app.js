/* MARQUEE catalogue client — acquisition actions, status polling, register search. */
const Marquee = (() => {
  const esc = s => (s == null ? "" : String(s).replace(/[&<>"']/g, c =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])));
  const pad7 = n => String(n).padStart(7, "0");

  function stampHTML(st) {
    const s = (st && st.state) || "addable";
    if (s === "available") return '<span class="stamp stamp--available">&#10003; IN STOCK</span>';
    if (s === "grabbing") return `<span class="stamp stamp--grabbing">RECV<span class="pb"><i style="width:${(st && st.progress) || 0}%"></i></span>${(st && st.progress) || 0}%</span>`;
    if (s === "wanted") return '<span class="stamp stamp--wanted">&#9679; ON ORDER</span>';
    return '<span class="stamp stamp--none">NOT HELD</span>';
  }
  function bigStampHTML(st) {
    const s = (st && st.state) || "addable";
    if (s === "available") return '<span class="bigstamp s-available">In Stock</span>';
    if (s === "grabbing") return `<span class="bigstamp s-grabbing">Receiving ${(st && st.progress) || 0}%</span>`;
    if (s === "wanted") return '<span class="bigstamp s-wanted">On Order</span>';
    return '<span class="bigstamp s-none">Not Held</span>';
  }

  function posterHTML(f) {
    if (f.poster) return `<img class="pi" src="${esc(f.poster)}" alt="${esc(f.title)}" loading="lazy">`;
    return `<div class="pi pi--text"><span class="pt-title">${esc(f.title)}</span><span class="pt-foot">No Cover On File</span></div>`;
  }
  function recHTML(f) {
    return `<a class="rec" href="/film/${f.tmdb_id}" data-tmdb="${f.tmdb_id}">
      <div class="rec-art">${posterHTML(f)}</div>
      <div class="rec-lab">
        <div class="rec-no">ACC&middot;${pad7(f.tmdb_id)}</div>
        <div class="rec-t">${esc(f.title)}</div>
        <div class="rec-y">${f.year || "----"}${f.director ? " &middot; " + esc((f.director || "").toUpperCase()) : ""}</div>
        <div class="rec-stamprow"><span data-status>${stampHTML(f.status)}</span>${f.watched ? ' <span class="seenflag">VIEWED</span>' : ""}</div>
      </div></a>`;
  }

  function toast(msg) {
    let t = document.querySelector(".toast");
    if (!t) { t = document.createElement("div"); t.className = "toast"; document.body.appendChild(t); }
    t.textContent = msg; t.classList.add("show");
    clearTimeout(t._t); t._t = setTimeout(() => t.classList.remove("show"), 2600);
  }
  async function post(url) { const r = await fetch(url, { method: "POST" }); if (!r.ok) throw new Error(r.status); return r.json(); }

  function renderStatus(id, st) {
    document.querySelectorAll(`[data-tmdb="${id}"] [data-status]`).forEach(n => {
      n.innerHTML = n.closest(".acqbox") ? bigStampHTML(st) : stampHTML(st);
    });
  }
  function setHold(id, on) {
    document.querySelectorAll(`[data-tmdb="${id}"] .act-wl`).forEach(b => {
      b.classList.toggle("is-on", on);
      b.innerHTML = on ? "&#9733; On Hold" : "Place Hold";
    });
  }

  document.addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-act]");
    if (!btn) return;
    e.preventDefault();
    if (btn.dataset.act === "sync") {
      btn.disabled = true; const orig = btn.innerHTML; btn.innerHTML = "SYNCING…";
      try {
        const d = await (await fetch("/api/sync", { method: "POST" })).json();
        if (d.ok) { toast(d.new ? `PULLED ${d.new} NEW ENTR${d.new === 1 ? "Y" : "IES"}` : "ALREADY UP TO DATE"); if (d.new) setTimeout(() => location.reload(), 800); }
        else toast("SYNC: " + (d.reason || "failed"));
      } catch (_) { toast("SYNC FAILED"); }
      finally { btn.disabled = false; btn.innerHTML = orig; }
      return;
    }
    const host = btn.closest("[data-tmdb]");
    const id = host && host.dataset.tmdb;
    if (!id) return;
    btn.disabled = true;
    try {
      if (btn.dataset.act === "watchlist") {
        const res = await post(`/api/film/${id}/watchlist`);
        setHold(id, res.in_watchlist);
        toast(res.in_watchlist ? "HOLD PLACED" : "HOLD CANCELLED");
      } else if (btn.dataset.act === "radarr") {
        toast("TRANSMITTING ORDER TO RADARR…");
        const res = await post(`/api/film/${id}/radarr`);
        if (res.ok) { renderStatus(id, res.status); setHold(id, true); toast("ORDER PLACED — ACQUIRING"); startPolling(); }
        else toast("RADARR ERROR: " + (res.error || "FAILED"));
      }
    } catch (err) { toast("REQUEST FAILED"); }
    finally { btn.disabled = false; }
  });

  // --- live acquisition polling ---
  let pollTimer = null;
  const prev = {};
  function ids() {
    const set = new Set();
    document.querySelectorAll("[data-status]").forEach(n => {
      const h = n.closest("[data-tmdb]"); if (h) set.add(h.dataset.tmdb);
    });
    return [...set];
  }
  async function pollOnce() {
    const list = ids();
    if (!list.length) return false;
    let inFlight = false;
    try {
      const data = await (await fetch(`/api/status?ids=${list.join(",")}`)).json();
      list.forEach(id => {
        const st = data[id]; if (!st) return;
        if (st.state === "grabbing" || st.state === "wanted") inFlight = true;
        if (prev[id] && prev[id] !== "available" && st.state === "available") toast("RECEIVED — FILED TO LIBRARY");
        prev[id] = st.state;
        renderStatus(id, st);
      });
    } catch (e) {}
    return inFlight;
  }
  function startPolling() {
    if (pollTimer) return;
    const tick = async () => { if (!(await pollOnce())) { clearInterval(pollTimer); pollTimer = null; } };
    pollTimer = setInterval(tick, 4000); tick();
  }

  function liveSearch(inputSel, gridSel) {
    const input = document.querySelector(inputSel), grid = document.querySelector(gridSel);
    if (!input || !grid) return;
    let t = null;
    input.addEventListener("input", () => {
      clearTimeout(t);
      const q = input.value.trim();
      t = setTimeout(async () => {
        try {
          const { results } = await (await fetch(`/api/search?q=${encodeURIComponent(q)}`)).json();
          grid.innerHTML = results.length ? results.map(recHTML).join("") : '<p class="empty">NO RECORDS MATCH QUERY.</p>';
          startPolling();
          history.replaceState(null, "", q ? `/discover?q=${encodeURIComponent(q)}` : "/discover");
        } catch (e) {}
      }, 260);
    });
  }
  function pollFilm() { startPolling(); }

  document.addEventListener("DOMContentLoaded", () => {
    // deadpan terminal clock
    const clk = document.getElementById("clk");
    if (clk) {
      const tick = () => {
        const d = new Date(), p = n => String(n).padStart(2, "0");
        clk.textContent = `TERMINAL 01  ·  ${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}  ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
      };
      tick(); setInterval(tick, 1000);
    }
    // last-synced indicator
    const sn = document.getElementById("syncnote");
    if (sn) {
      const last = parseFloat(sn.dataset.last);
      if (last) {
        const s = Math.max(0, Date.now() / 1000 - last);
        const rel = s < 90 ? "just now" : s < 5400 ? Math.round(s / 60) + "m ago" : s < 172800 ? Math.round(s / 3600) + "h ago" : Math.round(s / 86400) + "d ago";
        sn.textContent = "last synced " + rel;
      } else { sn.textContent = "auto-syncs hourly"; }
    }
    if ([...document.querySelectorAll(".stamp,.bigstamp")].some(b =>
      /grabbing|wanted|order|recv|receiving/i.test(b.className + b.textContent))) startPolling();
  });

  return { liveSearch, pollFilm, startPolling };
})();
