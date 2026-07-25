/* swatch — palette extraction, entirely client-side. */
(function () {
  'use strict';

  // Long edge of the sampling canvas. 320px caps us at ~102k sample points, so
  // a 40-megapixel scan costs the same as a thumbnail once it has decoded.
  var MAX_DIM = 320;
  var STORE_KEY = 'swatch.settings.v1';
  var SVG_W = 640, SVG_H = 80;

  /* ------------------------------------------------------------------ colour */

  // Shared by OKLab and WCAG luminance — both want linear-light sRGB.
  function srgbToLinear(c) {
    var v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  }

  // Ottosson's OKLab matrices, then Lab -> LCh polar form.
  function rgbToOklch(r, g, b) {
    var lr = srgbToLinear(r), lg = srgbToLinear(g), lb = srgbToLinear(b);

    var l = 0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb;
    var m = 0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb;
    var s = 0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb;

    var l_ = Math.cbrt(l), m_ = Math.cbrt(m), s_ = Math.cbrt(s);

    var L = 0.2104542553 * l_ + 0.7936177850 * m_ - 0.0040720468 * s_;
    var A = 1.9779984951 * l_ - 2.4285922050 * m_ + 0.4505937099 * s_;
    var B = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.8086757660 * s_;

    var C = Math.sqrt(A * A + B * B);
    var H = Math.atan2(B, A) * 180 / Math.PI;
    if (H < 0) H += 360;
    if (C < 1e-6) H = 0; // hue is atan2 noise at zero chroma
    return { L: L, C: C, H: H };
  }

  function rgbToHsl(r, g, b) {
    var rn = r / 255, gn = g / 255, bn = b / 255;
    var max = Math.max(rn, gn, bn), min = Math.min(rn, gn, bn);
    var l = (max + min) / 2, h = 0, s = 0, d = max - min;
    if (d > 1e-9) {
      s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
      if (max === rn) h = ((gn - bn) / d) % 6;
      else if (max === gn) h = (bn - rn) / d + 2;
      else h = (rn - gn) / d + 4;
      h *= 60;
      if (h < 0) h += 360;
    }
    return { h: h, s: s, l: l };
  }

  // WCAG 2.x relative luminance and contrast ratio.
  function relativeLuminance(r, g, b) {
    return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b);
  }
  function contrastRatio(a, b) {
    var la = relativeLuminance(a[0], a[1], a[2]);
    var lb = relativeLuminance(b[0], b[1], b[2]);
    var hi = Math.max(la, lb), lo = Math.min(la, lb);
    return (hi + 0.05) / (lo + 0.05);
  }

  function verdict(ratio) {
    if (ratio >= 7) return 'passes AAA for text';
    if (ratio >= 4.5) return 'passes AA for text';
    if (ratio >= 3) return 'passes AA for large text only';
    return 'fails AA';
  }

  // Truncate rather than round, so a displayed 4.5 always means at least 4.5
  // and never contradicts the verdict beside it.
  function ratioText(ratio) {
    return (Math.floor(ratio * 10) / 10).toFixed(1) + ':1';
  }

  function hex2(v) { return v.toString(16).padStart(2, '0'); }
  function toHex(r, g, b) { return '#' + hex2(r) + hex2(g) + hex2(b); }

  /* ------------------------------------------------------- median cut (own) */
  /* Median cut rather than k-means: it is deterministic (same image, same
     palette, every time), needs no seeding or convergence loop, and splitting
     on pixel population means the shares fall out of the algorithm instead of
     being counted afterwards. k-means gives marginally tighter clusters but
     can drift between runs, which is wrong for a tool that emits design
     tokens someone will paste into a stylesheet. */

  function buildHistogram(data) {
    // Fully transparent pixels read back as (0,0,0,0); counting them would
    // stain every palette with black.
    var hist = new Map();
    var kept = 0, dropped = 0;
    for (var i = 0; i < data.length; i += 4) {
      if (data[i + 3] === 0) { dropped++; continue; }
      var key = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
      hist.set(key, (hist.get(key) || 0) + 1);
      kept++;
    }
    var entries = [];
    hist.forEach(function (n, key) {
      entries.push({ r: (key >> 16) & 255, g: (key >> 8) & 255, b: key & 255, n: n });
    });
    return { entries: entries, kept: kept, dropped: dropped };
  }

  function makeBox(entries) {
    var rmin = 255, rmax = 0, gmin = 255, gmax = 0, bmin = 255, bmax = 0, count = 0;
    for (var i = 0; i < entries.length; i++) {
      var e = entries[i];
      if (e.r < rmin) rmin = e.r; if (e.r > rmax) rmax = e.r;
      if (e.g < gmin) gmin = e.g; if (e.g > gmax) gmax = e.g;
      if (e.b < bmin) bmin = e.b; if (e.b > bmax) bmax = e.b;
      count += e.n;
    }
    return { entries: entries, count: count, rr: rmax - rmin, gr: gmax - gmin, br: bmax - bmin };
  }

  function splitBox(box) {
    // Widest channel takes the cut. Ranges are compared raw, not luma-weighted:
    // weighting biases every split toward green and flattens the blues a film
    // scan actually contains.
    var ch = box.rr >= box.gr && box.rr >= box.br ? 'r' : box.gr >= box.br ? 'g' : 'b';
    var es = box.entries.slice().sort(function (a, b) { return a[ch] - b[ch]; });
    var half = box.count / 2, acc = 0, cut = 0;
    for (var i = 0; i < es.length; i++) {
      acc += es[i].n;
      if (acc >= half) { cut = i + 1; break; }
    }
    // Never hand back an empty side: one colour holding >50% of the pixels
    // would otherwise swallow the whole box at i = 0.
    if (cut < 1) cut = 1;
    if (cut > es.length - 1) cut = es.length - 1;
    return [makeBox(es.slice(0, cut)), makeBox(es.slice(cut))];
  }

  function medianCut(entries, k) {
    if (!entries.length) return [];
    var boxes = [makeBox(entries)];
    while (boxes.length < k) {
      var idx = -1, best = -1;
      for (var i = 0; i < boxes.length; i++) {
        var b = boxes[i];
        if (b.entries.length < 2) continue; // one distinct colour, atomic
        var score = b.count * Math.max(b.rr, b.gr, b.br);
        if (score > best) { best = score; idx = i; }
      }
      if (idx < 0) break; // fewer distinct colours in the image than requested
      var parts = splitBox(boxes[idx]);
      boxes.splice(idx, 1, parts[0], parts[1]);
    }
    return boxes;
  }

  function quantize(data, k) {
    var h = buildHistogram(data);
    if (!h.kept) return { swatches: [], kept: 0, dropped: h.dropped, distinct: 0 };

    var boxes = medianCut(h.entries, k);
    var byHex = new Map();

    for (var i = 0; i < boxes.length; i++) {
      var box = boxes[i], r = 0, g = 0, b = 0;
      for (var j = 0; j < box.entries.length; j++) {
        var e = box.entries[j];
        r += e.r * e.n; g += e.g * e.n; b += e.b * e.n;
      }
      r = Math.round(r / box.count); g = Math.round(g / box.count); b = Math.round(b / box.count);
      var hex = toHex(r, g, b);
      // Two buckets can average to the same rounded colour; collapse them
      // instead of emitting a duplicate swatch.
      var prev = byHex.get(hex);
      if (prev) prev.count += box.count;
      else byHex.set(hex, { hex: hex, r: r, g: g, b: b, count: box.count });
    }

    var swatches = [];
    byHex.forEach(function (s) {
      s.share = s.count / h.kept * 100;
      s.hsl = rgbToHsl(s.r, s.g, s.b);
      s.oklch = rgbToOklch(s.r, s.g, s.b);
      s.cWhite = contrastRatio([s.r, s.g, s.b], [255, 255, 255]);
      s.cBlack = contrastRatio([s.r, s.g, s.b], [0, 0, 0]);
      swatches.push(s);
    });

    return { swatches: swatches, kept: h.kept, dropped: h.dropped, distinct: h.entries.length };
  }

  /* ------------------------------------------------------------- formatting */

  function fmtRgb(s) { return 'rgb(' + s.r + ' ' + s.g + ' ' + s.b + ')'; }
  function fmtHsl(s) {
    return 'hsl(' + Math.round(s.hsl.h) + ' ' + Math.round(s.hsl.s * 100) + '% ' +
      Math.round(s.hsl.l * 100) + '%)';
  }
  function fmtOklch(s) {
    return 'oklch(' + s.oklch.L.toFixed(3) + ' ' + s.oklch.C.toFixed(3) + ' ' +
      s.oklch.H.toFixed(1) + ')';
  }
  function fmtShare(v) { return v.toFixed(v < 1 ? 2 : 1) + '%'; }

  function sortSwatches(list, mode) {
    var out = list.slice();
    if (mode === 'lightness') {
      out.sort(function (a, b) { return a.oklch.L - b.oklch.L || b.share - a.share; });
    } else if (mode === 'hue') {
      // Near-achromatic colours have no meaningful hue, so they go to the end
      // ordered by lightness rather than being scattered through the wheel.
      out.sort(function (a, b) {
        var ga = a.oklch.C < 0.02 ? 1 : 0, gb = b.oklch.C < 0.02 ? 1 : 0;
        if (ga !== gb) return ga - gb;
        if (ga === 1) return a.oklch.L - b.oklch.L;
        return a.oklch.H - b.oklch.H || b.share - a.share;
      });
    } else {
      out.sort(function (a, b) { return b.share - a.share || a.hex.localeCompare(b.hex); });
    }
    return out;
  }

  function buildCss(list) {
    var lines = [':root {'];
    for (var i = 0; i < list.length; i++) {
      lines.push('  --swatch-' + (i + 1) + ': ' + list[i].hex + '; /* ' + fmtShare(list[i].share) + ' */');
    }
    lines.push('}');
    return lines.join('\n');
  }

  function buildJson(list) {
    // Hand-assembled one object per line: JSON.stringify's indent mode explodes
    // every small array onto five lines and makes this unreadable.
    var rows = list.map(function (s, i) {
      return '  {"name": "swatch-' + (i + 1) + '", "hex": "' + s.hex +
        '", "rgb": [' + s.r + ', ' + s.g + ', ' + s.b + ']' +
        ', "hsl": [' + Math.round(s.hsl.h) + ', ' + Math.round(s.hsl.s * 100) + ', ' + Math.round(s.hsl.l * 100) + ']' +
        ', "oklch": [' + s.oklch.L.toFixed(4) + ', ' + s.oklch.C.toFixed(4) + ', ' + s.oklch.H.toFixed(2) + ']' +
        ', "share": ' + (Math.round(s.share * 100) / 100) +
        ', "contrast": {"white": ' + (Math.round(s.cWhite * 100) / 100) +
        ', "black": ' + (Math.round(s.cBlack * 100) / 100) + '}}';
    });
    return '[\n' + rows.join(',\n') + '\n]';
  }

  function buildSvg(list) {
    var n = list.length, x = 0, cum = 0, parts = [];
    for (var i = 0; i < n; i++) {
      cum += list[i].share;
      var end = i === n - 1 ? SVG_W : Math.round(cum * SVG_W / 100);
      if (end < x + 1) end = x + 1;                      // every swatch stays visible
      if (end > SVG_W - (n - 1 - i)) end = SVG_W - (n - 1 - i); // leave room for the rest
      parts.push('  <rect x="' + x + '" y="0" width="' + (end - x) + '" height="' + SVG_H +
        '" fill="' + list[i].hex + '"/>');
      x = end;
    }
    return '<svg xmlns="http://www.w3.org/2000/svg" width="' + SVG_W + '" height="' + SVG_H +
      '" viewBox="0 0 ' + SVG_W + ' ' + SVG_H + '" role="img" aria-label="Palette strip">\n' +
      parts.join('\n') + '\n</svg>';
  }

  /* --------------------------------------------------------------- elements */

  var $ = function (id) { return document.getElementById(id); };
  var el = {
    dropZone: $('dropZone'), pickBtn: $('pickBtn'), clearBtn: $('clearBtn'),
    fileInput: $('fileInput'), fileError: $('fileError'),
    source: $('source'), thumb: $('thumb'), sourceMeta: $('sourceMeta'),
    countRange: $('countRange'), countOut: $('countOut'),
    status: $('status'),
    results: $('results'), emptyState: $('emptyState'),
    resultsMeta: $('resultsMeta'), noticeLine: $('noticeLine'), swatchGrid: $('swatchGrid'),
    surfaces: $('surfaces'), lightStrip: $('lightStrip'), lightRows: $('lightRows'),
    darkStrip: $('darkStrip'), darkRows: $('darkRows'),
    exports: $('exports'), cssOut: $('cssOut'), jsonOut: $('jsonOut'), svgOut: $('svgOut')
  };
  var sortBtns = Array.prototype.slice.call(document.querySelectorAll('[data-sort]'));
  var copyBtns = Array.prototype.slice.call(document.querySelectorAll('[data-target]'));

  var state = { count: 6, sort: 'share', sample: null, result: null, objectUrl: null };

  /* ----------------------------------------------------------- persistence */

  function loadSettings() {
    try {
      var raw = localStorage.getItem(STORE_KEY);
      if (!raw) return;
      var s = JSON.parse(raw);
      if (typeof s.count === 'number' && s.count >= 3 && s.count <= 12) state.count = s.count;
      if (s.sort === 'share' || s.sort === 'lightness' || s.sort === 'hue') state.sort = s.sort;
    } catch (err) { /* private mode or corrupt value: defaults are fine */ }
  }
  function saveSettings() {
    try {
      localStorage.setItem(STORE_KEY, JSON.stringify({ count: state.count, sort: state.sort }));
    } catch (err) { /* quota or private mode: not worth surfacing */ }
  }

  /* --------------------------------------------------------------- helpers */

  function say(msg) { el.status.textContent = msg; }
  function showError(msg) { el.fileError.textContent = msg; }
  function clearError() { el.fileError.textContent = ''; }

  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    // file:// and older browsers have no async clipboard.
    return new Promise(function (resolve, reject) {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.top = '-1000px';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (err) { ok = false; }
      document.body.removeChild(ta);
      ok ? resolve() : reject(new Error('execCommand failed'));
    });
  }

  /* ----------------------------------------------------------- file intake */

  function handleFile(file) {
    clearError();
    if (!file) return;
    if (!file.type || file.type.indexOf('image/') !== 0) {
      showError('Not an image file. ' + (file.name || 'That file') + ' is ' +
        (file.type || 'an unrecognised type') + '.');
      say('Rejected.');
      return;
    }

    var url = URL.createObjectURL(file);
    var img = new Image();

    img.onload = function () {
      if (!img.naturalWidth || !img.naturalHeight) {
        URL.revokeObjectURL(url);
        showError('Could not read the dimensions of that image.');
        return;
      }
      if (state.objectUrl) URL.revokeObjectURL(state.objectUrl);
      state.objectUrl = url;
      el.thumb.src = url;
      el.thumb.alt = 'Preview of ' + (file.name || 'the loaded image');
      try {
        sample(img, file);
      } catch (err) {
        showError('Could not sample that image: ' + (err && err.message ? err.message : 'unknown error') + '.');
      }
    };

    img.onerror = function () {
      URL.revokeObjectURL(url);
      showError('Could not decode that image. It may be corrupt or an unsupported format.');
      say('Failed.');
    };

    img.src = url;
  }

  function sample(img, file) {
    var w = img.naturalWidth, h = img.naturalHeight;
    var scale = Math.min(1, MAX_DIM / Math.max(w, h));
    var sw = Math.max(1, Math.round(w * scale));
    var sh = Math.max(1, Math.round(h * scale));

    var canvas = document.createElement('canvas');
    canvas.width = sw; canvas.height = sh;
    var ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) throw new Error('canvas 2d context unavailable');

    // Nearest-neighbour on purpose: smoothing would blend neighbouring pixels
    // into colours the photograph never contained.
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(img, 0, 0, sw, sh);

    state.sample = {
      data: ctx.getImageData(0, 0, sw, sh).data,
      w: sw, h: sh,
      srcW: w, srcH: h,
      name: file.name || 'pasted image',
      type: file.type,
      size: file.size
    };

    el.source.hidden = false;
    el.clearBtn.hidden = false;
    renderSourceMeta();
    recompute();
  }

  function fmtBytes(n) {
    if (!n && n !== 0) return '—';
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    return (n / 1024 / 1024).toFixed(1) + ' MB';
  }

  function renderSourceMeta() {
    var s = state.sample;
    var rows = [
      ['File', s.name],
      ['Source', s.srcW + ' × ' + s.srcH + ' px'],
      ['Sampled', s.w + ' × ' + s.h + ' px'],
      ['Size', fmtBytes(s.size)]
    ];
    el.sourceMeta.replaceChildren();
    rows.forEach(function (r) {
      var dt = document.createElement('dt'); dt.textContent = r[0];
      var dd = document.createElement('dd'); dd.textContent = r[1];
      el.sourceMeta.append(dt, dd);
    });
  }

  function clearImage() {
    if (state.objectUrl) { URL.revokeObjectURL(state.objectUrl); state.objectUrl = null; }
    state.sample = null;
    state.result = null;
    el.thumb.removeAttribute('src');
    el.source.hidden = true;
    el.clearBtn.hidden = true;
    el.fileInput.value = '';
    clearError();
    renderEmpty();
    say('Cleared.');
  }

  /* ---------------------------------------------------------------- render */

  function renderEmpty() {
    el.emptyState.hidden = false;
    el.resultsMeta.hidden = true;
    el.noticeLine.hidden = true;
    el.swatchGrid.replaceChildren();
    el.surfaces.hidden = true;
    el.exports.hidden = true;
  }

  function recompute() {
    if (!state.sample) { renderEmpty(); return; }
    state.result = quantize(state.sample.data, state.count);
    if (!state.result.kept) {
      renderEmpty();
      showError('Every pixel in that image is fully transparent. Nothing to sample.');
      say('No opaque pixels.');
      return;
    }
    render();
  }

  function render() {
    var q = state.result;
    var list = sortSwatches(q.swatches, state.sort);
    var n = list.length;

    el.emptyState.hidden = true;
    el.resultsMeta.hidden = false;
    el.resultsMeta.textContent = n + (n === 1 ? ' colour · ' : ' colours · ') +
      q.kept.toLocaleString() + ' px sampled · ' + q.distinct.toLocaleString() + ' distinct in source' +
      (q.dropped ? ' · ' + q.dropped.toLocaleString() + ' transparent px ignored' : '');

    if (n < state.count) {
      el.noticeLine.hidden = false;
      el.noticeLine.textContent = q.distinct <= state.count
        ? 'Only ' + n + (n === 1 ? ' distinct colour found.' : ' distinct colours found.')
        : 'Merged to ' + n + ' swatches. Some buckets averaged to the same colour.';
    } else {
      el.noticeLine.hidden = true;
    }

    renderGrid(list);
    renderSurfaces(list);
    renderExports(list);
  }

  function renderGrid(list) {
    var frag = document.createDocumentFragment();

    list.forEach(function (s, i) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'swatch';
      btn.dataset.hex = s.hex;
      btn.title = 'Copy ' + s.hex;
      btn.setAttribute('aria-label', 'Swatch ' + (i + 1) + ', ' + s.hex + ', ' +
        fmtShare(s.share) + ' of the image. Copy hex.');

      var block = document.createElement('span');
      block.className = 'swatch-block';
      block.style.backgroundColor = s.hex;

      var body = document.createElement('span');
      body.className = 'swatch-body';

      var share = document.createElement('span');
      share.className = 'swatch-share';
      share.textContent = fmtShare(s.share);

      var shareLabel = document.createElement('span');
      shareLabel.className = 'swatch-share-label';
      shareLabel.textContent = 'Image Share';

      var vals = document.createElement('span');
      vals.className = 'swatch-vals';
      [['hex', s.hex], ['', fmtRgb(s)], ['', fmtHsl(s)], ['', fmtOklch(s)]].forEach(function (v) {
        var sp = document.createElement('span');
        if (v[0]) sp.className = v[0];
        sp.textContent = v[1];
        vals.appendChild(sp);
      });

      body.append(share, shareLabel, vals);
      btn.append(block, body);
      frag.appendChild(btn);
    });

    el.swatchGrid.replaceChildren(frag);
  }

  function renderSurfaces(list) {
    el.surfaces.hidden = false;

    [[el.lightStrip, el.lightRows, 'cWhite'], [el.darkStrip, el.darkRows, 'cBlack']].forEach(function (cfg) {
      var strip = cfg[0], rows = cfg[1], key = cfg[2];

      var sfrag = document.createDocumentFragment();
      list.forEach(function (s) {
        var sp = document.createElement('span');
        sp.style.backgroundColor = s.hex;
        sp.style.width = (100 / list.length) + '%';
        sfrag.appendChild(sp);
      });
      strip.replaceChildren(sfrag);

      var rfrag = document.createDocumentFragment();
      list.forEach(function (s) {
        var li = document.createElement('li');

        var sample = document.createElement('span');
        sample.className = 'sample num';
        sample.style.color = s.hex;
        sample.textContent = s.hex;

        var ratio = document.createElement('span');
        ratio.className = 'ratio';
        ratio.textContent = ratioText(s[key]);

        var word = document.createElement('span');
        word.className = 'verdict';
        word.textContent = '· ' + verdict(s[key]);

        li.append(sample, ratio, word);
        rfrag.appendChild(li);
      });
      rows.replaceChildren(rfrag);
    });
  }

  function renderExports(list) {
    el.exports.hidden = false;
    el.cssOut.textContent = buildCss(list);
    el.jsonOut.textContent = buildJson(list);
    el.svgOut.textContent = buildSvg(list);
  }

  /* ---------------------------------------------------------------- events */

  el.pickBtn.addEventListener('click', function () { el.fileInput.click(); });
  el.clearBtn.addEventListener('click', clearImage);

  el.fileInput.addEventListener('change', function () {
    if (el.fileInput.files && el.fileInput.files[0]) handleFile(el.fileInput.files[0]);
    // Reset so re-picking the same file still fires a change event.
    el.fileInput.value = '';
  });

  el.countRange.addEventListener('input', function () {
    state.count = parseInt(el.countRange.value, 10) || 6;
    el.countOut.textContent = String(state.count);
    saveSettings();
    recompute();
  });

  sortBtns.forEach(function (btn) {
    btn.addEventListener('click', function () {
      state.sort = btn.dataset.sort;
      sortBtns.forEach(function (b) {
        b.setAttribute('aria-pressed', String(b === btn));
      });
      saveSettings();
      if (state.result && state.result.kept) render();
    });
  });

  el.swatchGrid.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('.swatch') : null;
    if (!btn) return;
    copyText(btn.dataset.hex).then(function () {
      say('Copied.');
    }, function () {
      say('Copy failed. Select ' + btn.dataset.hex + ' and copy it manually.');
    });
  });

  copyBtns.forEach(function (btn) {
    btn.addEventListener('click', function () {
      var target = document.getElementById(btn.dataset.target);
      if (!target || !target.textContent) { say('Nothing to copy.'); return; }
      copyText(target.textContent).then(function () {
        say('Copied.');
      }, function () {
        say('Copy failed. Select the text and copy it manually.');
      });
    });
  });

  // Drag and drop anywhere on the page. Depth counter keeps the highlight
  // stable while the pointer crosses child elements.
  var dragDepth = 0;
  document.addEventListener('dragenter', function (e) {
    e.preventDefault();
    dragDepth++;
    el.dropZone.classList.add('over');
  });
  document.addEventListener('dragover', function (e) { e.preventDefault(); });
  document.addEventListener('dragleave', function () {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) el.dropZone.classList.remove('over');
  });
  document.addEventListener('drop', function (e) {
    e.preventDefault();
    dragDepth = 0;
    el.dropZone.classList.remove('over');
    var dt = e.dataTransfer;
    if (dt && dt.files && dt.files[0]) handleFile(dt.files[0]);
    else showError('That drop carried no file.');
  });

  document.addEventListener('paste', function (e) {
    var items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    for (var i = 0; i < items.length; i++) {
      if (items[i].kind === 'file') {
        var f = items[i].getAsFile();
        if (f) { e.preventDefault(); handleFile(f); return; }
      }
    }
  });

  /* ------------------------------------------------------------------ boot */

  loadSettings();
  el.countRange.value = String(state.count);
  el.countOut.textContent = String(state.count);
  sortBtns.forEach(function (b) {
    b.setAttribute('aria-pressed', String(b.dataset.sort === state.sort));
  });
  renderEmpty();
})();
