package proxy

import "bytes"

// readOnlyAssets is spliced into every HTML page before </body>. It runs after
// the page's own scripts and makes the UI read-only without altering the
// layout: write controls are disabled and management nav links are hidden.
//
// Selectors map directly to officetracker's write surfaces:
//   - #notes textarea     -> blur triggers PUT /api/v1/note/...      (form page)
//   - #calendar .day cells -> click/right-click cycle + PUT state     (form page)
//   - #export-csv/-pdf      -> report endpoints reject token auth      (form page)
//   - .settings-section controls -> PUT /api/v1/settings/{theme,calendar}
//   - #schedule-calendar .schedule-day cells -> PUT /api/v1/settings/schedule
//   - /logout, /developer nav links and #assoc-uri (link an account) -> hidden
const readOnlyAssets = `<style>
#calendar .day, #schedule-calendar .schedule-day { cursor: default !important; }
nav .nav-links a[href="/logout"], nav .nav-links a[href="/developer"] { display: none !important; }
#assoc-uri { display: none !important; }
#export-csv, #export-pdf { opacity: .5; cursor: not-allowed; }
</style>
<script>
(function () {
  // Block state-changing interactions on calendar / weekly-schedule cells.
  // The listener is bound to document (capture phase) rather than the #calendar
  // container because form.js rebuilds and replaces the #calendar element on
  // every month navigation, which would detach a container-bound listener.
  // Matching the event target against the cell selectors survives any re-render.
  function blockCellWrite(e) {
    var t = e.target;
    if (t && t.closest && t.closest('#calendar .day, #schedule-calendar .schedule-day')) {
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  }
  ['click', 'contextmenu', 'mousedown'].forEach(function (ev) {
    document.addEventListener(ev, blockCellWrite, true);
  });

  function lock() {
    // Disable note editing (blur -> PUT).
    document.querySelectorAll('#notes').forEach(function (el) { el.readOnly = true; el.disabled = true; });
    // Disable report export buttons (report API rejects token auth).
    ['export-csv', 'export-pdf'].forEach(function (id) { var b = document.getElementById(id); if (b) b.disabled = true; });
    // Disable settings write controls (theme / schedule / tracking-year -> PUT).
    document.querySelectorAll('.settings-section select, .settings-section input, .settings-section textarea').forEach(function (el) { el.disabled = true; });
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', lock);
  else lock();
})();
</script>`

// injectReadOnly splices the read-only assets in just before the closing
// </body> tag, or appends them if no such tag exists.
func injectReadOnly(body []byte) []byte {
	marker := []byte("</body>")
	idx := bytes.LastIndex(body, marker)
	if idx == -1 {
		return append(body, readOnlyAssets...)
	}

	out := make([]byte, 0, len(body)+len(readOnlyAssets))
	out = append(out, body[:idx]...)
	out = append(out, readOnlyAssets...)
	out = append(out, body[idx:]...)
	return out
}
