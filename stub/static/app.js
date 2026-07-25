// stub — two small progressive enhancements. The page is fully usable without
// this file: short URLs are selectable text, delete is a plain form POST.
(function () {
  'use strict';

  // Copy buttons are rendered hidden and only revealed when we can actually
  // copy. No point showing a button that does nothing.
  var canCopy = !!(navigator.clipboard && navigator.clipboard.writeText);
  var buttons = document.querySelectorAll('button.copy');
  for (var i = 0; i < buttons.length; i++) {
    (function (btn) {
      if (!canCopy) return;
      btn.hidden = false;
      btn.addEventListener('click', function () {
        navigator.clipboard.writeText(btn.getAttribute('data-copy')).then(
          function () {
            var original = btn.textContent;
            btn.textContent = 'Copied';
            btn.disabled = true;
            window.setTimeout(function () {
              btn.textContent = original;
              btn.disabled = false;
            }, 1200);
          },
          function () {
            btn.textContent = 'Copy Failed';
          }
        );
      });
    })(buttons[i]);
  }

  // Destructive forms ask once. Without JS the POST just goes through, which
  // is the same behaviour every other admin table in the estate has.
  var forms = document.querySelectorAll('form[data-confirm]');
  for (var j = 0; j < forms.length; j++) {
    (function (form) {
      form.addEventListener('submit', function (e) {
        if (!window.confirm(form.getAttribute('data-confirm'))) {
          e.preventDefault();
        }
      });
    })(forms[j]);
  }
})();
