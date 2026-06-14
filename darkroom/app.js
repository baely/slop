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
  const slidersEl  = $("sliders");
  const iterInput  = $("iterInput");
  const iterUp     = $("iterUp");
  const iterDown   = $("iterDown");

  // ---- state ----
  let width = 0, height = 0;
  let origData = null;     // ImageData of the loaded photo (the source of truth)
  let resultData = null;   // ImageData after the last develop
  let busy = false;
  let rerunPending = false; // a slider moved mid-run; re-develop when free

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
  // editing the code by hand clears the active chip; either way, re-scan sliders
  let discTimer = null;
  editor.on("change", (_, ch) => {
    if (ch.origin && ch.origin !== "setValue") setActivePreset(-1);
    clearTimeout(discTimer);
    discTimer = setTimeout(discoverSliders, 250);
  });

  // ---- status helpers ----
  function status(html, state = "idle") {
    statusEl.dataset.state = state;
    statusText.innerHTML = html;
  }

  // ================= sliders =================
  // Users declare a control inline:  const t = slider("amount", 1, 0, 4)
  // We run the code in a discovery pass to find every slider() call, build the
  // UI, then feed the live values back in on every develop.
  const sliderState = new Map();   // name -> { value, def, min, max, step, seen }
  let discoverGen = 0;
  let lastSliderSig = "";

  function makeSlider(register) {
    return function slider(name, def = 0, min, max, step) {
      if (typeof name !== "string") throw new Error("slider() needs a name as its first argument.");
      if (min === undefined) min = 0;
      if (max === undefined) max = def > min ? def * 2 : min + 1;
      if (step === undefined) {
        const span = max - min;
        step = (Number.isInteger(min) && Number.isInteger(max) && Number.isInteger(def) && span >= 4)
          ? 1 : span / 100;
      }
      return register(name, def, min, max, step);
    };
  }

  // registers/updates a slider and returns its current value
  function registerSlider(name, def, min, max, step) {
    let s = sliderState.get(name);
    if (!s) { s = { value: def }; sliderState.set(name, s); }
    s.def = def; s.min = min; s.max = max; s.step = step; s.seen = discoverGen;
    s.value = Math.min(max, Math.max(min, s.value));
    return s.value;
  }

  // Run the program once (without touching the image) to learn its sliders.
  function discoverSliders() {
    discoverGen++;
    const slider = makeSlider(registerSlider);
    const probe = () => [0, 0, 0, 255];
    const get = origData
      ? (nx, ny) => {
          const d = origData.data, W = width, H = height;
          nx = nx < 0 ? 0 : (nx >= W ? W - 1 : nx | 0);
          ny = ny < 0 ? 0 : (ny >= H ? H - 1 : ny | 0);
          const i = (ny * W + nx) << 2;
          return [d[i], d[i + 1], d[i + 2], d[i + 3]];
        }
      : probe;
    try {
      const pixel = makeFactory()(get, width || 1, height || 1, slider);
      if (typeof pixel === "function") pixel(0, 0); // trip any slider() calls inside pixel()
    } catch (_) { /* mid-typing errors are fine; the real run reports them */ }
    for (const [name, s] of sliderState) if (s.seen !== discoverGen) sliderState.delete(name);
    renderSliders();
  }

  function renderSliders() {
    const entries = [...sliderState.entries()];
    slidersEl.hidden = entries.length === 0;
    const sig = entries.map(([n, s]) => `${n}|${s.min}|${s.max}|${s.step}`).join("§");
    if (sig === lastSliderSig) return; // same controls — don't rebuild mid-drag
    lastSliderSig = sig;

    slidersEl.innerHTML = "";
    for (const [name, s] of entries) {
      const row = document.createElement("div");
      row.className = "slider-row";
      const head = document.createElement("div");
      head.className = "slider-head";
      const nm = document.createElement("span");
      nm.className = "slider-name"; nm.textContent = name;
      const val = document.createElement("span");
      val.className = "slider-val"; val.textContent = fmt(s.value);
      head.append(nm, val);
      const range = document.createElement("input");
      range.type = "range";
      range.min = s.min; range.max = s.max; range.step = s.step; range.value = s.value;
      range.addEventListener("input", () => {
        s.value = parseFloat(range.value);
        val.textContent = fmt(s.value);
        requestDevelop();
      });
      row.append(head, range);
      slidersEl.appendChild(row);
    }
  }

  function fmt(v) {
    return Number.isInteger(v) ? String(v) : (Math.round(v * 1000) / 1000).toString();
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
  function makeFactory() {
    return new Function(
      "get", "width", "height", "slider",
      `"use strict";\n${editor.getValue()}\n;
       if (typeof pixel !== "function") throw new Error("Define a function called pixel(x, y).");
       return pixel;`
    );
  }

  function getIterations() {
    let n = parseInt(iterInput.value, 10);
    if (!Number.isFinite(n)) n = 1;
    n = Math.min(50, Math.max(1, n));
    iterInput.value = n;
    return n;
  }

  // schedule a develop, coalescing rapid slider input into one re-run at a time
  function requestDevelop() {
    if (!origData) return;
    if (busy) { rerunPending = true; return; }
    develop();
  }

  function develop() {
    if (busy || !origData) return;
    busy = true;
    rerunPending = false;
    runBtn.disabled = true;
    status("running…", "idle");

    discoverSliders(); // keep the controls + live values in sync with the code
    const passes = getIterations();
    const len = width * height * 4;
    // ping-pong buffers: read from one, write to the other, swap each pass
    let readBuf = new Uint8ClampedArray(origData.data); // copy of the original
    let writeBuf = new Uint8ClampedArray(len);

    // get() reads the CURRENT source (the original on pass 1, the previous
    // pass's output afterwards), with edge clamping
    const W = width, H = height;
    function get(nx, ny) {
      nx = nx < 0 ? 0 : (nx >= W ? W - 1 : nx | 0);
      ny = ny < 0 ? 0 : (ny >= H ? H - 1 : ny | 0);
      const i = (ny * W + nx) << 2;
      return [readBuf[i], readBuf[i + 1], readBuf[i + 2], readBuf[i + 3]];
    }
    const slider = makeSlider(registerSlider);

    let pixel;
    try {
      pixel = makeFactory()(get, W, H, slider);
    } catch (err) {
      finishError(err);
      return;
    }

    developing.hidden = false;
    barFill.style.width = "0%";

    const t0 = performance.now();
    const ROWS_PER_FRAME = Math.max(1, Math.floor(40000 / width)); // ~40k px/chunk
    let pass = 0, y = 0;

    function chunk() {
      const yEnd = Math.min(height, y + ROWS_PER_FRAME);
      try {
        for (; y < yEnd; y++) {
          for (let x = 0; x < width; x++) {
            const i = (y * width + x) << 2;
            const res = pixel(x, y);
            if (res == null) { // unchanged
              writeBuf[i] = readBuf[i]; writeBuf[i+1] = readBuf[i+1];
              writeBuf[i+2] = readBuf[i+2]; writeBuf[i+3] = readBuf[i+3];
              continue;
            }
            writeBuf[i]   = clamp(res[0]);
            writeBuf[i+1] = clamp(res[1]);
            writeBuf[i+2] = clamp(res[2]);
            writeBuf[i+3] = res.length > 3 ? clamp(res[3]) : 255;
          }
        }
      } catch (err) {
        developing.hidden = true;
        finishError(err, y);
        return;
      }

      barFill.style.width = ((pass * height + y) / (passes * height) * 100).toFixed(1) + "%";
      developLabel.textContent = passes > 1 ? `developing… pass ${pass + 1}/${passes}` : "developing…";

      if (y < height) {
        requestAnimationFrame(chunk);
        return;
      }

      // finished a pass: the output becomes the input for the next one
      pass++;
      if (pass < passes) {
        const tmp = readBuf; readBuf = writeBuf; writeBuf = tmp;
        y = 0;
        requestAnimationFrame(chunk);
        return;
      }

      // all passes done
      const ms = performance.now() - t0;
      const out = new ImageData(width, height);
      out.data.set(writeBuf);
      resultData = out;
      ctx.putImageData(out, 0, 0);
      developing.hidden = true;
      const xN = passes > 1 ? ` ·×${passes}` : "";
      timingEl.textContent = `${(width*height/1e6).toFixed(2)} MP${xN} · ${ms < 1000 ? ms.toFixed(0)+" ms" : (ms/1000).toFixed(2)+" s"}`;
      status(passes > 1 ? `Developed (${passes} passes).` : "Developed.", "ok");
      busy = false;
      runBtn.disabled = false;
      if (rerunPending) requestDevelop(); // a slider moved while we were busy
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
    rerunPending = false;
    runBtn.disabled = false;
    // leave the original on screen
    if (origData) ctx.putImageData(origData, 0, 0);
  }

  // ================= wiring =================
  runBtn.addEventListener("click", develop);

  // iterations stepper
  iterUp.addEventListener("click", () => { iterInput.value = getIterations() + 1; requestDevelop(); });
  iterDown.addEventListener("click", () => { iterInput.value = getIterations() - 1; requestDevelop(); });
  iterInput.addEventListener("change", () => { getIterations(); requestDevelop(); });
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
    iterInput.value = 1;
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
