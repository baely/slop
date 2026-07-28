// Package web is the server-rendered UI: receipts grouped by AU financial
// year, and a detail view with the email body and PDF attachment side by
// side. Optional single-password auth via AUTH_PASSWORD.
package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/baileybutler/shoebox/internal/ingest"
	"github.com/baileybutler/shoebox/internal/store"
)

//go:embed templates
var tplFS embed.FS

type Web struct {
	st     *store.Store
	pw     string
	tok    string
	ingest string
	loc    *time.Location
	pages  map[string]*template.Template
}

type base struct {
	Ingest string
}

func New(st *store.Store, password, ingestAddr string, loc *time.Location) http.Handler {
	w := &Web{st: st, pw: password, ingest: ingestAddr, loc: loc}
	if password != "" {
		m := hmac.New(sha256.New, []byte(password))
		m.Write([]byte("shoebox-session-v1"))
		w.tok = hex.EncodeToString(m.Sum(nil))
	}

	funcs := template.FuncMap{
		"money": money,
		"size":  humanSize,
		"rel":   func(t time.Time) string { return relTime(time.Since(t)) },
		"exact": func(t time.Time) string { return t.In(loc).Format("Mon 2 Jan 2006 15:04") },
		"day":   func(t time.Time) string { return t.In(loc).Format("2 Jan 2006") },
	}
	w.pages = map[string]*template.Template{}
	for _, p := range []string{"index", "detail", "login"} {
		w.pages[p] = template.Must(template.New("layout.html").Funcs(funcs).
			ParseFS(tplFS, "templates/layout.html", "templates/"+p+".html"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", w.index)
	mux.HandleFunc("GET /login", w.loginForm)
	mux.HandleFunc("POST /login", w.login)
	mux.HandleFunc("GET /r/{id}", w.detail)
	mux.HandleFunc("POST /r/{id}", w.update)
	mux.HandleFunc("POST /r/{id}/delete", w.del)
	mux.HandleFunc("GET /r/{id}/body", w.body)
	mux.HandleFunc("GET /r/{id}/raw", w.raw)
	mux.HandleFunc("GET /r/{id}/att/{file}", w.att)
	mux.HandleFunc("POST /upload", w.upload)
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, _ *http.Request) { io.WriteString(rw, "ok") })
	return w.auth(mux)
}

func (w *Web) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if w.pw == "" || p == "/login" || p == "/healthz" {
			next.ServeHTTP(rw, r)
			return
		}
		if c, err := r.Cookie("shoebox_auth"); err == nil &&
			subtle.ConstantTimeCompare([]byte(c.Value), []byte(w.tok)) == 1 {
			next.ServeHTTP(rw, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
	})
}

func (w *Web) render(rw http.ResponseWriter, page string, data any) {
	var buf bytes.Buffer
	if err := w.pages[page].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		log.Printf("web: render %s: %v", page, err)
		http.Error(rw, "template error", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(rw)
}

// --- index ---

type fyGroup struct {
	Label   string
	End     int
	Total   int64
	Missing int
	Count   int
	Rows    []store.Receipt
}

func (w *Web) index(rw http.ResponseWriter, r *http.Request) {
	all := w.st.List()
	byEnd := map[int]*fyGroup{}
	for _, rec := range all {
		end := fyEnd(rec.Date.In(w.loc))
		g := byEnd[end]
		if g == nil {
			g = &fyGroup{End: end, Label: fmt.Sprintf("FY%d–%02d", end-1, end%100)}
			byEnd[end] = g
		}
		g.Rows = append(g.Rows, rec)
		g.Count++
		if rec.AmountCents >= 0 {
			g.Total += rec.AmountCents
		} else {
			g.Missing++
		}
	}
	years := make([]*fyGroup, 0, len(byEnd))
	for _, g := range byEnd {
		years = append(years, g)
	}
	sort.Slice(years, func(i, j int) bool { return years[i].End > years[j].End })
	w.render(rw, "index", struct {
		base
		Years []*fyGroup
	}{base{w.ingest}, years})
}

// fyEnd is the calendar year an AU financial year (Jul–Jun) ends in.
func fyEnd(d time.Time) int {
	if d.Month() >= time.July {
		return d.Year() + 1
	}
	return d.Year()
}

// --- detail ---

func (w *Web) detail(rw http.ResponseWriter, r *http.Request) {
	rec, ok := w.st.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(rw, r)
		return
	}
	tabs := displayAtts(rec)
	sel := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("att")); err == nil && v >= 0 && v < len(tabs) {
		sel = v
	}
	var selAtt *store.Attachment
	kind := ""
	if len(tabs) > 0 {
		selAtt = &tabs[sel]
		switch {
		case selAtt.ContentType == "application/pdf" || strings.HasSuffix(strings.ToLower(selAtt.Name), ".pdf"):
			kind = "pdf"
		case strings.HasPrefix(selAtt.ContentType, "image/"):
			kind = "image"
		default:
			kind = "other"
		}
	}
	amount := ""
	if rec.AmountCents >= 0 {
		amount = fmt.Sprintf("%.2f", float64(rec.AmountCents)/100)
	}
	hasBody := rec.HasHTML || rec.HasText
	w.render(rw, "detail", struct {
		base
		R       store.Receipt
		Tabs    []store.Attachment
		Sel     int
		SelAtt  *store.Attachment
		Kind    string
		HasBody bool
		Amount  string
		TwoPane bool
	}{base{w.ingest}, rec, tabs, sel, selAtt, kind, hasBody, amount, hasBody && len(tabs) > 0})
}

// displayAtts picks what the attachment pane offers: proper attachments, or —
// only when there is no HTML body to render them in — inline non-text parts.
func displayAtts(r store.Receipt) []store.Attachment {
	var out []store.Attachment
	for _, a := range r.Attachments {
		if !a.Inline {
			out = append(out, a)
		}
	}
	if len(out) == 0 && !r.HasHTML {
		for _, a := range r.Attachments {
			out = append(out, a)
		}
	}
	return out
}

func (w *Web) update(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := w.st.Get(id); !ok {
		http.NotFound(rw, r)
		return
	}
	cents := int64(-1)
	if s := normalizeAmount(r.FormValue("amount")); s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 || f >= 1e7 {
			http.Error(rw, "bad amount", http.StatusBadRequest)
			return
		}
		cents = int64(math.Round(f * 100))
	}
	if err := w.st.Update(id, cents, strings.TrimSpace(r.FormValue("notes"))); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(rw, r, "/r/"+id, http.StatusSeeOther)
}

func normalizeAmount(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"AUD", "aud", "A$", "$"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.ReplaceAll(s, ",", "")
	return strings.TrimSpace(s)
}

func (w *Web) del(rw http.ResponseWriter, r *http.Request) {
	if err := w.st.Delete(r.PathValue("id")); err != nil {
		http.NotFound(rw, r)
		return
	}
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

// --- content endpoints ---

func (w *Web) body(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := w.st.Get(id)
	if !ok {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	// Rendered inside a sandboxed iframe; belt and braces via CSP: no
	// scripts, no remote loads (tracking pixels stay dead), cid images only.
	rw.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'")
	if rec.HasHTML {
		http.ServeFile(rw, r, w.st.BodyPath(id, true))
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if rec.HasText {
		b, err := os.ReadFile(w.st.BodyPath(id, false))
		if err != nil {
			http.Error(rw, "missing body", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(rw, `<!doctype html><meta charset="utf-8"><body style="margin:0;background:#fff"><pre style="font:13px/1.5 ui-monospace,monospace;white-space:pre-wrap;padding:16px;margin:0;color:#1a1a1a">%s</pre>`, html.EscapeString(string(b)))
		return
	}
	io.WriteString(rw, `<!doctype html><body style="font:13px sans-serif;color:#666;padding:16px">no body.`)
}

func (w *Web) raw(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := w.st.Get(id)
	if !ok || !rec.HasRaw {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "message/rfc822")
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+".eml"))
	http.ServeFile(rw, r, w.st.RawPath(id))
}

func (w *Web) att(rw http.ResponseWriter, r *http.Request) {
	id, file := r.PathValue("id"), r.PathValue("file")
	rec, ok := w.st.Get(id)
	if !ok {
		http.NotFound(rw, r)
		return
	}
	var meta *store.Attachment
	for i := range rec.Attachments {
		if rec.Attachments[i].File == file {
			meta = &rec.Attachments[i]
			break
		}
	}
	if meta == nil {
		http.NotFound(rw, r)
		return
	}
	ct := meta.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := "attachment" // html/unknown types never render in-origin
	if ct == "application/pdf" || strings.HasPrefix(ct, "image/") || ct == "text/plain" {
		disp = "inline"
	}
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Content-Type", ct)
	rw.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disp, meta.Name))
	http.ServeFile(rw, r, w.st.AttPath(id, file))
}

// --- upload ---

func (w *Web) upload(rw http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(rw, "bad upload", http.StatusBadRequest)
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(rw, "missing file", http.StatusBadRequest)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil || len(data) == 0 {
		http.Error(rw, "empty file", http.StatusBadRequest)
		return
	}
	var bundle *store.Bundle
	if bytes.HasPrefix(data, []byte("%PDF")) || strings.HasSuffix(strings.ToLower(fh.Filename), ".pdf") {
		bundle = ingest.FromPDF(fh.Filename, data, time.Now())
	} else {
		bundle = ingest.FromEmail(data, "", time.Now(), w.loc)
		bundle.Meta.Source = "upload"
	}
	rec, err := w.st.Save(bundle)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(rw, r, "/r/"+rec.ID, http.StatusSeeOther)
}

// --- auth ---

func (w *Web) loginForm(rw http.ResponseWriter, r *http.Request) {
	if w.pw == "" {
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	w.render(rw, "login", struct {
		base
		Bad bool
	}{base{w.ingest}, r.URL.Query().Get("bad") == "1"})
}

func (w *Web) login(rw http.ResponseWriter, r *http.Request) {
	if w.pw == "" {
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(w.pw)) != 1 {
		http.Redirect(rw, r, "/login?bad=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(rw, &http.Cookie{
		Name:     "shoebox_auth",
		Value:    w.tok,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 90,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

// --- formatting ---

func money(cents int64) string {
	if cents < 0 {
		return "—"
	}
	whole := strconv.FormatInt(cents/100, 10)
	var sb strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return fmt.Sprintf("$%s.%02d", sb.String(), cents%100)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func relTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 31*24*time.Hour:
		return plural(int(d.Hours()/24/7), "week")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	}
	return plural(int(d.Hours()/24/365), "year")
}

func plural(n int, unit string) string {
	if n <= 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
