package main

import "html/template"

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>hop</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,800&family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
:root{--font-display:'Bricolage Grotesque',system-ui,sans-serif;--font-body:'Inter',system-ui,sans-serif;--font-mono:'JetBrains Mono',ui-monospace,monospace;--accent:#0891b2;--accent-deep:#0e7490;--accent-ink:#fff;--r-ctl:3px;--r-card:4px;--sheen:linear-gradient(180deg,rgb(255 255 255/.12),rgb(0 0 0/.08));--bg:#fafafa;--surface:#f1f1f3;--surface-2:#e9e9ed;--text:#131316;--text-2:#55555e;--muted:#9b9ba6;--line:#e4e4e9;--accent-text:#0e7490;--accent-soft:#e2f0f4;color-scheme:light dark}
@media(prefers-color-scheme:dark){:root:not([data-theme=light]){--bg:#0b0b0d;--surface:#17171a;--surface-2:#212126;--text:#f2f2f4;--text-2:#a3a3ad;--muted:#5b5b66;--line:#26262c;--accent-text:#3aa8c4;--accent-soft:#0c2229}}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:var(--font-body);font-size:14.5px;line-height:1.55;color:var(--text);background:var(--bg);padding:32px 24px 64px}
main{max-width:860px;margin:0 auto}
h1{font-family:var(--font-display);font-weight:800;letter-spacing:-.03em;line-height:1.1;font-size:clamp(28px,6vw,44px);margin-bottom:24px}
input[type=text],input[type=url],input[type=number]{font:inherit;color:var(--text);background:var(--surface);border:0;border-radius:var(--r-ctl);padding:8px 10px;width:100%}
input:focus{outline:2px solid var(--accent);outline-offset:0}
button{font:inherit;font-weight:500;border:0;border-radius:var(--r-ctl);padding:8px 14px;cursor:pointer;white-space:nowrap}
.primary{color:var(--accent-ink);background:var(--accent) var(--sheen)}
.primary:hover{background-color:var(--accent-deep)}
.quiet{color:var(--text-2);background:var(--surface-2)}
.quiet:hover{color:var(--text)}
.add{background:var(--surface);border-radius:var(--r-card);padding:16px;margin-bottom:32px}
.add input[type=text],.add input[type=url]{background:var(--bg)}
.row{display:flex;gap:8px;align-items:end}
.row label{display:flex;flex-direction:column;gap:4px;font-size:13px;color:var(--text-2)}
.key{flex:0 0 200px}
.url{flex:1}
.rules{display:flex;flex-wrap:wrap;gap:8px 18px;align-items:center;margin-top:14px;font-size:13px;color:var(--text-2)}
.rules input[type=number]{width:64px;background:var(--bg);padding:4px 8px;margin-left:6px}
.rules label{display:inline-flex;align-items:center;gap:6px}
.rules input[type=checkbox]{accent-color:var(--accent)}
.error{color:#b91c1c;font-size:13px;margin-bottom:12px}
.link{display:grid;grid-template-columns:minmax(160px,1fr) 2fr auto auto;gap:8px;align-items:center;padding:8px 0;border-top:1px solid var(--line)}
.link:last-child{border-bottom:1px solid var(--line)}
.short{font-family:var(--font-mono);font-size:13px;color:var(--accent-text);text-decoration:none;overflow-wrap:anywhere}
.short:hover{text-decoration:underline}
.empty{color:var(--muted)}
.count{font-family:var(--font-mono);font-size:12px;color:var(--muted);margin-bottom:8px}
@media(max-width:640px){.row{flex-wrap:wrap}.key,.url{flex:1 1 100%}.link{grid-template-columns:1fr auto auto}.link input{grid-column:1/-1}}
.b-glyph{position:fixed;bottom:10px;right:10px;z-index:90;font-family:var(--font-display);font-weight:800;font-size:13px;line-height:1;color:var(--text-2);background:var(--surface);border-radius:var(--r-ctl);padding:6px 8px;text-decoration:none}
.b-glyph:hover{color:var(--accent-text)}
</style>
<main>
<h1>hop.</h1>

<form method="post" action="/add" class="add" id="add">
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<div class="row">
<label class="key">Key<input type="text" name="key" value="{{.Form.Key}}" placeholder="blank to generate" autocomplete="off" spellcheck="false" autocapitalize="off"></label>
<button type="button" class="quiet" id="gen">Generate</button>
<label class="url">URL<input type="url" name="url" value="{{.Form.URL}}" placeholder="https://" required spellcheck="false"></label>
<button class="primary">Add Link</button>
</div>
<div class="rules">
<label>Length<input type="number" name="len" min="1" max="32" value="{{.Form.Rules.Length}}"></label>
<label><input type="checkbox" name="lower" value="1"{{if .Form.Rules.Lower}} checked{{end}}>a–z</label>
<label><input type="checkbox" name="upper" value="1"{{if .Form.Rules.Upper}} checked{{end}}>A–Z</label>
<label><input type="checkbox" name="digits" value="1"{{if .Form.Rules.Digits}} checked{{end}}>0–9</label>
<label><input type="checkbox" name="unambiguous" value="1"{{if .Form.Rules.Unambiguous}} checked{{end}}>Skip 0 O 1 l I</label>
</div>
</form>

{{if .Links}}<p class="count">{{len .Links}} links</p>{{end}}
<section>
{{range .Links}}<form method="post" action="/update" class="link">
<input type="hidden" name="key" value="{{.Key}}">
<a class="short" href="{{.Short}}">{{.Display}}</a>
<input type="url" name="url" value="{{.URL}}" required spellcheck="false">
<button class="quiet">Save</button>
<button class="quiet" formaction="/delete" formnovalidate>Delete</button>
</form>
{{else}}<p class="empty">No links yet.</p>
{{end}}
</section>
</main>
<a class="b-glyph" href="https://index.baileys.app" title="A Bailey App">b.</a>
<script>
document.getElementById('gen').addEventListener('click', async () => {
  const f = document.getElementById('add');
  const q = new URLSearchParams();
  for (const n of ['len','lower','upper','digits','unambiguous']) if (f.elements[n].type === 'checkbox' ? f.elements[n].checked : true) q.set(n, f.elements[n].value);
  const r = await fetch('/generate?' + q);
  const t = await r.text();
  if (r.ok) f.elements.key.value = t; else alert(t);
});
</script>
`))
