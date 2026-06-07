(() => {
  "use strict";

  const MAX_EDGE = 1600; // downscale huge uploads so runs stay snappy

  // ---- elements ----
  const $ = id => document.getElementById(id);
  const dropzone   = $("dropzone");
  const fileInput  = $("fileInput");
  const browseBtn  = $("browseBtn");
  const sampleBtn  = $("sampleBtn");
  const canvasWrap = $("canvasWrap");
  const canvas     = $("canvas");
  const ctx        = canvas.getContext("2d", { willReadFrequently: true });
  const readout    = $("readout");
  const dimsEl     = $("dims");
  const timingEl   = $("timing");
  const developing = $("developing");
  const barFill    = $("barFill");
  const developLabel = $("developLabel");
  const runBtn     = $("runBtn");
  const downloadBtn= $("downloadBtn");
  const compareBtn = $("compareBtn");
  const resetBtn   = $("resetBtn");
  const newBtn     = $("newBtn");
  const statusEl   = $("status");
  const statusText = $("statusText");
  const presetsEl  = $("presets");

  // ---- state ----
  let width = 0, height = 0;
  let origData = null;     // ImageData of the loaded photo (the source of truth)
  let resultData = null;   // ImageData after the last develop
  let busy = false;

  // ---- editor ----
  const editor = CodeMirror($("editor"), {
    value: PRESETS[0].code,
    mode: "javascript",
    lineNumbers: true,
    indentUnit: 2,
    tabSize: 2,
    autoCloseBrackets: true,
    matchBrackets: true,
    theme: "default",
  });

  // ---- presets ----
  let activePreset = 0;
  PRESETS.forEach((p, i) => {
    const b = document.createElement("button");
    b.className = "preset" + (i === 0 ? " active" : "");
    b.textContent = p.name;
    b.addEventListener("click", () => {
      editor.setValue(p.code);
      editor.focus();
      setActivePreset(i);
      if (origData) develop();
    });
    presetsEl.appendChild(b);
  });
  function setActivePreset(i) {
    activePreset = i;
    [...presetsEl.children].forEach((c, j) => c.classList.toggle("active", j === i));
  }
  // editing the code by hand clears the active chip
  editor.on("change", (_, ch) => {
    if (ch.origin && ch.origin !== "setValue") setActivePreset(-1);
  });

  // ---- status helpers ----
  function status(html, state = "idle") {
    statusEl.dataset.state = state;
    statusText.innerHTML = html;
  }

  // ================= image loading =================
  function loadFromImage(img) {
    let w = img.naturalWidth || img.width;
    let h = img.naturalHeight || img.height;
    const scale = Math.min(1, MAX_EDGE / Math.max(w, h));
    const downscaled = scale < 1;
    w = Math.max(1, Math.round(w * scale));
    h = Math.max(1, Math.round(h * scale));

    width = w; height = h;
    canvas.width = w; canvas.height = h;
    ctx.clearRect(0, 0, w, h);
    ctx.drawImage(img, 0, 0, w, h);
    origData = ctx.getImageData(0, 0, w, h);
    resultData = null;

    dropzone.hidden = true;
    canvasWrap.hidden = false;
    readout.hidden = false;
    downloadBtn.disabled = false;
    runBtn.disabled = false;

    dimsEl.textContent = `${w} × ${h} px`;
    timingEl.textContent = `${(w * h / 1e6).toFixed(2)} MP`;
    status(
      downscaled
        ? `Loaded (scaled down to ${w}×${h} so it runs fast). Press <b>Develop</b>.`
        : `Loaded. Press <b>Develop</b> to run your function.`,
      "idle"
    );
    develop(); // run whatever is currently in the editor
  }

  function handleFile(file) {
    if (!file || !file.type.startsWith("image/")) {
      status("That doesn't look like an image file.", "error");
      return;
    }
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => { loadFromImage(img); URL.revokeObjectURL(url); };
    img.onerror = () => { status("Couldn't decode that image.", "error"); URL.revokeObjectURL(url); };
    img.src = url;
  }

  // procedural sample so you can try it without uploading
  function loadSample() {
    const w = 900, h = 600;
    const c = document.createElement("canvas");
    c.width = w; c.height = h;
    const g = c.getContext("2d");
    const grad = g.createLinearGradient(0, 0, w, h);
    grad.addColorStop(0, "#1b2a4a");
    grad.addColorStop(0.5, "#7d2b3a");
    grad.addColorStop(1, "#e0a44b");
    g.fillStyle = grad; g.fillRect(0, 0, w, h);
    // sun
    const sun = g.createRadialGradient(660, 180, 10, 660, 180, 150);
    sun.addColorStop(0, "rgba(255,240,200,1)");
    sun.addColorStop(1, "rgba(255,240,200,0)");
    g.fillStyle = sun; g.fillRect(0, 0, w, h);
    // hills
    for (let i = 0; i < 3; i++) {
      g.fillStyle = ["#241a2e", "#3a2236", "#522a39"][i];
      g.beginPath();
      const base = h - 60 - i * 70;
      g.moveTo(0, h);
      for (let x = 0; x <= w; x += 20) {
        g.lineTo(x, base + Math.sin(x / 90 + i * 2) * 40 + Math.cos(x / 35) * 12);
      }
      g.lineTo(w, h); g.closePath(); g.fill();
    }
    // type
    g.fillStyle = "rgba(255,255,255,.9)";
    g.font = "bold 64px 'JetBrains Mono', monospace";
    g.fillText("DARKROOM", 60, 110);
    const img = new Image();
    img.onload = () => loadFromImage(img);
    img.src = c.toDataURL();
  }

  // ================= develop =================
  function buildPixelFn(src) {
    // get() reads the ORIGINAL pixels, with edge clamping
    const W = width, H = height;
    function get(nx, ny) {
      nx = nx < 0 ? 0 : (nx >= W ? W - 1 : nx | 0);
      ny = ny < 0 ? 0 : (ny >= H ? H - 1 : ny | 0);
      const i = (ny * W + nx) << 2;
      return [src[i], src[i + 1], src[i + 2], src[i + 3]];
    }
    const factory = new Function(
      "get", "width", "height",
      `"use strict";\n${editor.getValue()}\n;
       if (typeof pixel !== "function") throw new Error("Define a function called pixel(x, y).");
       return pixel;`
    );
    const pixel = factory(get, W, H);
    return { pixel, get };
  }

  function develop() {
    if (busy || !origData) return;
    busy = true;
    runBtn.disabled = true;
    status("running…", "idle");

    const src = origData.data;            // never mutated during the pass
    const out = new ImageData(width, height);
    const dst = out.data;

    let pixel;
    try {
      pixel = buildPixelFn(src).pixel;
    } catch (err) {
      finishError(err);
      return;
    }

    developing.hidden = false;
    barFill.style.width = "0%";
    developLabel.textContent = "developing…";

    const t0 = performance.now();
    const ROWS_PER_FRAME = Math.max(1, Math.floor(40000 / width)); // ~40k px/chunk
    let y = 0;

    function chunk() {
      const yEnd = Math.min(height, y + ROWS_PER_FRAME);
      try {
        for (; y < yEnd; y++) {
          for (let x = 0; x < width; x++) {
            const res = pixel(x, y);
            const i = (y * width + x) << 2;
            if (res == null) { // treat as transparent / unchanged-ish
              dst[i] = src[i]; dst[i+1] = src[i+1]; dst[i+2] = src[i+2]; dst[i+3] = src[i+3];
              continue;
            }
            dst[i]   = clamp(res[0]);
            dst[i+1] = clamp(res[1]);
            dst[i+2] = clamp(res[2]);
            dst[i+3] = res.length > 3 ? clamp(res[3]) : 255;
          }
        }
      } catch (err) {
        developing.hidden = true;
        finishError(err, y);
        return;
      }

      barFill.style.width = (y / height * 100).toFixed(1) + "%";

      if (y < height) {
        requestAnimationFrame(chunk);
      } else {
        const ms = performance.now() - t0;
        resultData = out;
        ctx.putImageData(out, 0, 0);
        developing.hidden = true;
        timingEl.textContent = `${(width*height/1e6).toFixed(2)} MP · ${ms < 1000 ? ms.toFixed(0)+" ms" : (ms/1000).toFixed(2)+" s"}`;
        status("Developed.", "ok");
        busy = false;
        runBtn.disabled = false;
      }
    }
    requestAnimationFrame(chunk);
  }

  function clamp(v) {
    v = Math.round(v);
    return v < 0 ? 0 : v > 255 ? 255 : v;
  }

  function finishError(err, row) {
    const where = row != null ? ` (at row ${row})` : "";
    status("✕ " + (err && err.message ? err.message : String(err)) + where, "error");
    busy = false;
    runBtn.disabled = false;
    // leave the original on screen
    if (origData) ctx.putImageData(origData, 0, 0);
  }

  // ================= wiring =================
  runBtn.addEventListener("click", develop);
  downloadBtn.addEventListener("click", () => {
    if (!resultData && origData) ctx.putImageData(origData, 0, 0);
    const a = document.createElement("a");
    a.download = "darkroom.png";
    a.href = canvas.toDataURL("image/png");
    a.click();
  });

  resetBtn.addEventListener("click", () => {
    setActivePreset(0);
    editor.setValue(PRESETS[0].code);
    resultData = null;
    if (origData) ctx.putImageData(origData, 0, 0);
    status("Reset to the original.", "idle");
  });

  newBtn.addEventListener("click", () => {
    origData = resultData = null;
    width = height = 0;
    canvasWrap.hidden = true;
    readout.hidden = true;
    dropzone.hidden = false;
    downloadBtn.disabled = true;
    runBtn.disabled = true;
    status('Drop a photo to begin.', "idle");
  });

  // hold-to-compare
  function showOriginal() { if (origData && resultData) ctx.putImageData(origData, 0, 0); }
  function showResult()   { if (resultData) ctx.putImageData(resultData, 0, 0); }
  ["mousedown", "touchstart"].forEach(e => compareBtn.addEventListener(e, showOriginal));
  ["mouseup", "mouseleave", "touchend"].forEach(e => compareBtn.addEventListener(e, showResult));

  // upload triggers
  browseBtn.addEventListener("click", () => fileInput.click());
  sampleBtn.addEventListener("click", loadSample);
  fileInput.addEventListener("change", e => handleFile(e.target.files[0]));
  dropzone.addEventListener("click", e => { if (e.target === dropzone || e.target.classList.contains("dz-inner")) fileInput.click(); });

  // drag & drop (whole viewport)
  const vp = $("viewport");
  ["dragenter", "dragover"].forEach(ev => vp.addEventListener(ev, e => {
    e.preventDefault(); dropzone.classList.add("drag");
  }));
  ["dragleave", "drop"].forEach(ev => vp.addEventListener(ev, e => {
    e.preventDefault(); if (ev !== "dragover") dropzone.classList.remove("drag");
  }));
  vp.addEventListener("drop", e => {
    const f = e.dataTransfer.files && e.dataTransfer.files[0];
    if (f) handleFile(f);
  });

  // paste an image from clipboard
  window.addEventListener("paste", e => {
    const items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    for (const it of items) {
      if (it.type.startsWith("image/")) { handleFile(it.getAsFile()); break; }
    }
  });

  // ⌘/Ctrl + Enter to develop
  editor.setOption("extraKeys", {
    "Cmd-Enter": develop,
    "Ctrl-Enter": develop,
    "Tab": cm => cm.replaceSelection("  "),
  });
})();
