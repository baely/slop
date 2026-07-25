// Progressive enhancement only. Every one of these is optional: the numeric
// x/y/w/h fields, the font and size selects and the plain forms are the source
// of truth, and the page is completely usable with this file blocked.
(function () {
  'use strict';

  // ---------------------------------------------------------------- snippet
  var pre = document.getElementById('snippet');
  if (pre && navigator.clipboard) {
    var bar = document.createElement('div');
    bar.className = 'snippet-bar';
    var copy = document.createElement('button');
    copy.type = 'button';
    copy.className = 'btn';
    copy.textContent = 'Copy Snippet';
    copy.addEventListener('click', function () {
      navigator.clipboard.writeText(pre.textContent).then(function () {
        copy.textContent = 'Copied.';
      }, function () {
        copy.textContent = 'Copy Failed.';
      });
    });
    bar.appendChild(copy);
    pre.parentNode.insertBefore(bar, pre);
  }

  // ------------------------------------------------------------ font rules
  // The same numbers the server validates against, handed over as JSON so the
  // size control can reconfigure itself the moment the font changes.
  var rules = {};
  var rulesEl = document.getElementById('font-rules');
  if (rulesEl) {
    try {
      JSON.parse(rulesEl.textContent).forEach(function (r) { rules[r.id] = r; });
    } catch (e) { /* leave the server-rendered controls alone */ }
  }

  function advise(rule, size) {
    if (!rule) return '';
    if (rule.bitmap) {
      var legal = rule.sizes.join(', ');
      if (rule.sizes.indexOf(size) >= 0) return '';
      if (size < rule.sizes[0]) {
        return rule.name + ' is a bitmap face: ' + size + 'px is below its native ' +
          rule.sizes[0] + 'px and it cannot be scaled down. Use ' + legal + '.';
      }
      if (size > rule.sizes[rule.sizes.length - 1]) {
        return rule.name + ' stops at ' + rule.sizes[rule.sizes.length - 1] +
          'px. Use ' + legal + ', or an outline face for anything larger.';
      }
      return rule.name + ' has no ' + size + 'px size: a bitmap face only scales in whole multiples of ' +
        rule.sizes[0] + 'px. Use ' + legal + '.';
    }
    if (size < rule.min) {
      var alt = rules[rule.alt];
      return rule.name + ' at ' + size + 'px thins out on e-ink. Try ' +
        (alt ? alt.name : rule.alt) + ' at ' + rule.altAt + 'px.';
    }
    if (size > rule.max) return rule.name + ' stops at ' + rule.max + 'px.';
    return '';
  }

  function snap(rule, size) {
    if (!rule) return size;
    if (!rule.bitmap) return Math.min(rule.max, Math.max(rule.min, size));
    var best = rule.sizes[0];
    rule.sizes.forEach(function (s) {
      if (Math.abs(s - size) < Math.abs(best - size)) best = s;
    });
    return best;
  }

  // Rebuild the size control for the face now selected, then refresh the
  // sample strip and the advice line beneath it.
  function retuneType(i) {
    var fontSel = document.querySelector('[data-font="' + i + '"]');
    var wrap = document.querySelector('[data-sizewrap="' + i + '"]');
    if (!fontSel || !wrap) return;
    var rule = rules[fontSel.value];
    if (!rule) return;

    var current = wrap.querySelector('[data-size="' + i + '"]');
    var size = parseInt(current && current.value, 10) || (rule.bitmap ? rule.sizes[0] : rule.min);
    var wantSelect = !!rule.bitmap;
    var isSelect = current && current.tagName === 'SELECT';

    if (wantSelect !== isSelect) {
      var el;
      if (wantSelect) {
        el = document.createElement('select');
        rule.sizes.forEach(function (s) {
          var o = document.createElement('option');
          o.value = String(s);
          o.textContent = s + ' px';
          el.appendChild(o);
        });
      } else {
        el = document.createElement('input');
        el.type = 'number';
        el.inputMode = 'numeric';
        el.min = String(rule.min);
        el.max = String(rule.max);
        el.step = '1';
      }
      el.id = 'b' + i + '-size';
      el.name = 'b' + i + '.size';
      el.setAttribute('data-size', String(i));
      el.value = String(snap(rule, size));
      current.parentNode.replaceChild(el, current);
      current = el;
      bindSize(i);
    } else if (wantSelect) {
      current.innerHTML = '';
      rule.sizes.forEach(function (s) {
        var o = document.createElement('option');
        o.value = String(s);
        o.textContent = s + ' px';
        current.appendChild(o);
      });
      current.value = String(snap(rule, size));
    } else {
      current.min = String(rule.min);
      current.max = String(rule.max);
    }
    refreshSample(i);
  }

  function refreshSample(i) {
    var fontSel = document.querySelector('[data-font="' + i + '"]');
    var sizeEl = document.querySelector('[data-size="' + i + '"]');
    var img = document.querySelector('[data-sample="' + i + '"]');
    var hint = document.querySelector('[data-advice="' + i + '"]');
    if (!fontSel || !sizeEl) return;
    var rule = rules[fontSel.value];
    var size = parseInt(sizeEl.value, 10) || 0;
    if (img) {
      img.src = '/sample.png?font=' + encodeURIComponent(fontSel.value) + '&size=' + size;
    }
    if (hint) {
      var msg = advise(rule, size);
      var note = rule ? rule.note : '';
      if (rule && rule.cap && size > rule.cap) {
        note += ' Sample shown at ' + rule.cap + 'px; this block renders at ' + size + 'px.';
      }
      hint.textContent = msg || note;
      hint.className = msg ? 'hint bad' : 'hint';
    }
  }

  function bindSize(i) {
    var sizeEl = document.querySelector('[data-size="' + i + '"]');
    if (sizeEl && !sizeEl.dataset.bound) {
      sizeEl.dataset.bound = '1';
      sizeEl.addEventListener('change', function () { refreshSample(i); });
      sizeEl.addEventListener('input', function () { refreshSample(i); });
    }
  }

  Array.prototype.forEach.call(document.querySelectorAll('[data-font]'), function (sel) {
    var i = sel.getAttribute('data-font');
    sel.addEventListener('change', function () { retuneType(i); });
    bindSize(i);
  });

  // -------------------------------------------------------- drag and resize
  var canvas = document.getElementById('canvas');
  var editor = document.getElementById('editor');
  if (!canvas || !editor) return;

  var canvasW = parseInt(canvas.getAttribute('data-w'), 10);
  var canvasH = parseInt(canvas.getAttribute('data-h'), 10);
  var GRID = 8;

  function fieldOf(i, which) { return document.getElementById('b' + i + '-' + which); }

  function readBox(i) {
    return {
      x: parseInt(fieldOf(i, 'x').value, 10) || 0,
      y: parseInt(fieldOf(i, 'y').value, 10) || 0,
      w: parseInt(fieldOf(i, 'w').value, 10) || 1,
      h: parseInt(fieldOf(i, 'h').value, 10) || 1
    };
  }

  function writeBox(i, box, el) {
    fieldOf(i, 'x').value = box.x;
    fieldOf(i, 'y').value = box.y;
    fieldOf(i, 'w').value = box.w;
    fieldOf(i, 'h').value = box.h;
    el.style.left = box.x + 'px';
    el.style.top = box.y + 'px';
    el.style.width = box.w + 'px';
    el.style.height = box.h + 'px';
  }

  function clampBox(box) {
    box.w = Math.max(1, Math.min(box.w, canvasW));
    box.h = Math.max(1, Math.min(box.h, canvasH));
    box.x = Math.max(0, Math.min(box.x, canvasW - box.w));
    box.y = Math.max(0, Math.min(box.y, canvasH - box.h));
    return box;
  }

  function round(v, free) { return free ? Math.round(v) : Math.round(v / GRID) * GRID; }

  var drag = null;

  function start(ev, i, mode, el) {
    ev.preventDefault();
    drag = { i: i, mode: mode, el: el, startX: ev.clientX, startY: ev.clientY, box: readBox(i) };
    el.setPointerCapture(ev.pointerId);
    Array.prototype.forEach.call(document.querySelectorAll('.blk.on'), function (o) { o.classList.remove('on'); });
    el.classList.add('on');
  }

  function move(ev) {
    if (!drag) return;
    ev.preventDefault();
    var dx = ev.clientX - drag.startX;
    var dy = ev.clientY - drag.startY;
    var free = ev.shiftKey;
    var b = drag.box;
    var next;
    if (drag.mode === 'move') {
      next = { x: round(b.x + dx, free), y: round(b.y + dy, free), w: b.w, h: b.h };
    } else {
      next = { x: b.x, y: b.y, w: round(b.w + dx, free), h: round(b.h + dy, free) };
    }
    writeBox(drag.i, clampBox(next), drag.el);
  }

  function end(ev) {
    if (!drag) return;
    var el = drag.el;
    try { el.releasePointerCapture(ev.pointerId); } catch (e) { /* already gone */ }
    drag = null;
    // Save on release so the preview underneath is the real render again,
    // rather than an outline floating over a stale picture.
    if (editor.requestSubmit) editor.requestSubmit();
    else editor.submit();
  }

  Array.prototype.forEach.call(canvas.querySelectorAll('.blk'), function (el) {
    var i = el.getAttribute('data-blk');
    if (!fieldOf(i, 'x')) return;
    el.addEventListener('pointerdown', function (ev) {
      if (ev.target && ev.target.hasAttribute('data-handle')) return;
      start(ev, i, 'move', el);
    });
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', end);
    el.addEventListener('pointercancel', end);

    var handle = el.querySelector('[data-handle]');
    if (handle) {
      handle.addEventListener('pointerdown', function (ev) {
        ev.stopPropagation();
        start(ev, i, 'resize', el);
      });
    }
  });

  // Typing in a number field moves the outline, so the two views never
  // disagree while you work.
  Array.prototype.forEach.call(editor.querySelectorAll('[data-geom]'), function (input) {
    input.addEventListener('input', function () {
      var i = input.getAttribute('data-i');
      var el = canvas.querySelector('.blk[data-blk="' + i + '"]');
      if (!el) return;
      var box = readBox(i);
      el.style.left = box.x + 'px';
      el.style.top = box.y + 'px';
      el.style.width = box.w + 'px';
      el.style.height = box.h + 'px';
    });
  });
})();
