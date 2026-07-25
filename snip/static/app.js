/* snip — copy buttons and the soft-wrap toggle.
   Both controls ship hidden and are revealed here, so a browser without
   JavaScript never sees a dead button. Everything else works without this
   file: the page is server-rendered and the Raw link is a plain link. */
(function () {
  'use strict';

  function textOf(el) {
    var lines = el.querySelectorAll('.l');
    if (lines.length) {
      var out = [];
      for (var i = 0; i < lines.length; i++) out.push(lines[i].textContent);
      return out.join('\n');
    }
    return el.textContent;
  }

  function copy(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(function () { done(true); },
        function () { done(fallback(text)); });
      return;
    }
    done(fallback(text));
  }

  function fallback(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }

  var buttons = document.querySelectorAll('[data-copy-target]');
  for (var i = 0; i < buttons.length; i++) {
    (function (btn) {
      var target = document.querySelector(btn.getAttribute('data-copy-target'));
      if (!target) return;
      btn.hidden = false;
      var label = btn.textContent;
      btn.addEventListener('click', function () {
        copy(textOf(target), function (ok) {
          btn.textContent = ok ? 'Copied.' : 'Copy Failed.';
          window.setTimeout(function () { btn.textContent = label; }, 2000);
        });
      });
    })(buttons[i]);
  }

  var code = document.getElementById('code');
  var toggle = document.querySelector('[data-wrap-toggle]');
  if (code && toggle) {
    toggle.hidden = false;
    var stored = null;
    try { stored = window.localStorage.getItem('snip.wrap'); } catch (e) { stored = null; }
    var on = stored === '1';
    apply(on);
    toggle.addEventListener('click', function () {
      on = !on;
      apply(on);
      try { window.localStorage.setItem('snip.wrap', on ? '1' : '0'); } catch (e) { /* ignore */ }
    });
  }

  function apply(on) {
    if (on) { code.classList.add('wrap'); } else { code.classList.remove('wrap'); }
    toggle.setAttribute('aria-pressed', on ? 'true' : 'false');
  }
})();
