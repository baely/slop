package main

import (
	"strconv"
	"strings"
)

// renderPage produces the single house-style page used for the index and for
// every error. It is baked at startup, so nothing here runs per request and
// no request data is ever reflected into it.
func renderPage(status int, heading, detail string) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>`)
	sb.WriteString(strings.TrimSuffix(heading, "."))
	sb.WriteString(`</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,800&family=Inter:wght@400&family=JetBrains+Mono:wght@400&display=swap" rel="stylesheet">
<style>
:root{--font-display:'Bricolage Grotesque',system-ui,sans-serif;--font-body:'Inter',system-ui,sans-serif;--font-mono:'JetBrains Mono',ui-monospace,monospace;--accent:#0891b2;--accent-deep:#0e7490;--accent-ink:#fff;--r-ctl:3px;--r-card:4px;--bg:#fafafa;--surface:#f1f1f3;--surface-2:#e9e9ed;--text:#131316;--text-2:#55555e;--muted:#9b9ba6;--line:#e4e4e9;--accent-text:#0e7490;--accent-soft:#e2f0f4;color-scheme:light dark}
@media(prefers-color-scheme:dark){:root:not([data-theme=light]){--bg:#0b0b0d;--surface:#17171a;--surface-2:#212126;--text:#f2f2f4;--text-2:#a3a3ad;--muted:#5b5b66;--line:#26262c;--accent-text:#3aa8c4;--accent-soft:#0c2229}}
*{box-sizing:border-box;margin:0;padding:0}
body{min-height:100vh;display:flex;align-items:center;padding:32px;font-family:var(--font-body);font-size:14.5px;line-height:1.55;color:var(--text);background:var(--bg)}
main{max-width:560px}
h1{font-family:var(--font-display);font-weight:800;letter-spacing:-.03em;line-height:1.1;font-size:clamp(28px,6vw,44px)}
p{margin-top:12px;color:var(--text-2)}
.code{font-family:var(--font-mono);font-size:13px;color:var(--muted);margin-top:24px}
.b-glyph{position:fixed;bottom:10px;right:10px;z-index:90;font-family:var(--font-display);font-weight:800;font-size:13px;line-height:1;color:var(--text-2);background:var(--surface);border-radius:var(--r-ctl);padding:6px 8px;text-decoration:none}
.b-glyph:hover{color:var(--accent-text)}
</style>
<main>
<h1>`)
	sb.WriteString(heading)
	sb.WriteString(`</h1>
<p>`)
	sb.WriteString(detail)
	sb.WriteString(`</p>
`)
	if status != 200 {
		sb.WriteString(`<p class="code">HTTP `)
		sb.WriteString(strconv.Itoa(status))
		sb.WriteString(`</p>
`)
	}
	sb.WriteString(`</main>
<a class="b-glyph" href="https://index.baileys.app" title="A Bailey App">b.</a>
`)
	return sb.String()
}
