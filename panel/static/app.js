// One progressive enhancement: a Copy button for the MicroPython snippet.
// The page is fully usable without it — the snippet is plain selectable text.
(function () {
  var pre = document.getElementById('snippet');
  if (!pre || !navigator.clipboard) return;

  var bar = document.createElement('div');
  bar.className = 'snippet-bar';

  var btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'btn';
  btn.textContent = 'Copy Snippet';
  btn.addEventListener('click', function () {
    navigator.clipboard.writeText(pre.textContent).then(function () {
      btn.textContent = 'Copied.';
    }, function () {
      btn.textContent = 'Copy Failed.';
    });
  });

  bar.appendChild(btn);
  pre.parentNode.insertBefore(bar, pre);
})();
