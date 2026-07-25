// catch — progressive enhancement only. The page works with this file absent:
// reload to see new requests, forms post normally.
(function () {
  'use strict';

  // Confirm destructive posts.
  document.addEventListener('click', function (ev) {
    var el = ev.target.closest('[data-confirm]');
    if (el && !window.confirm(el.getAttribute('data-confirm'))) {
      ev.preventDefault();
    }
  });

  // Copy the capture URL.
  document.addEventListener('click', function (ev) {
    var el = ev.target.closest('[data-copy]');
    if (!el) return;
    var target = document.querySelector(el.getAttribute('data-copy'));
    if (!target || !navigator.clipboard) return;
    navigator.clipboard.writeText(target.textContent.trim()).then(function () {
      var was = el.textContent;
      el.textContent = 'Copied.';
      window.setTimeout(function () { el.textContent = was; }, 1500);
    });
  });

  // Poll the request list. Server-rendered fragment, already escaped.
  var list = document.getElementById('reqs');
  var toggle = document.getElementById('live-toggle');
  if (!list || !list.dataset.rows) return;

  var url = list.dataset.rows;
  var live = true;
  var timer = null;
  var last = null;

  function tick() {
    if (!live || document.hidden) return;
    fetch(url, { headers: { 'Accept': 'text/html' }, credentials: 'same-origin' })
      .then(function (res) { return res.ok ? res.text() : null; })
      .then(function (html) {
        if (html === null || html === last) return;
        last = html;
        list.innerHTML = html;
      })
      .catch(function () { /* offline; the next tick tries again */ });
  }

  function start() {
    if (timer) return;
    timer = window.setInterval(tick, 4000);
  }

  if (toggle) {
    toggle.hidden = false;
    toggle.addEventListener('click', function () {
      live = !live;
      toggle.textContent = live ? 'Pause Live' : 'Resume Live';
      if (live) tick();
    });
  }
  start();
})();
