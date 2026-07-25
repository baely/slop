package main

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// captureReadCap is the most we will read off the wire for one capture. The
// first bodyStoreCap bytes are kept; the rest is counted and discarded.
const captureReadCap = 8 << 20

// formReadCap caps our own forms.
const formReadCap = 64 << 10

type navData struct {
	Active string
}

type indexData struct {
	Nav      navData
	Bins     []*Bin
	NumBins  int
	NumReqs  int
	BaseURL  string
	Notice   string
	MaxReqs  int
	TTLDays  int
	Statuses []int
}

type reqView struct {
	Req  *Request
	Body BodyView
}

type binData struct {
	Nav      navData
	Bin      *Bin
	BinID    string
	Rows     []reqView
	URL      string
	MaxReqs  int
	TTLDays  int
	Statuses []int
}

type rowsData struct {
	BinID string
	Rows  []reqView
}

type loginData struct {
	Nav   navData
	Error string
	Next  string
}

type messageData struct {
	Nav     navData
	Heading string
	Body    string
	Back    string
}

var offeredStatuses = []int{200, 201, 202, 204, 301, 400, 401, 403, 404, 418, 429, 500, 502, 503}

func parseTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"since":   func(t time.Time) string { return humanizeAt(t, time.Now()) },
		"until":   func(t time.Time) string { return untilAt(t, time.Now()) },
		"exact":   exactTime,
		"size":    formatSize,
		"favicon": faviconURL,
	}
	out := map[string]*template.Template{}
	for _, page := range []string{"index.html", "bin.html", "login.html", "message.html"} {
		t, err := template.New("base").Funcs(funcs).ParseFS(templatesFS,
			"templates/base.html", "templates/rows.html", "templates/"+page)
		if err != nil {
			return nil, err
		}
		out[page] = t
	}
	t, err := template.New("rows").Funcs(funcs).ParseFS(templatesFS, "templates/rows.html")
	if err != nil {
		return nil, err
	}
	out["rows"] = t
	return out, nil
}

func faviconURL() template.URL {
	return template.URL("data:image/svg+xml,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%2032%2032'%3E%3Crect%20width='32'%20height='32'%20rx='4'%20fill='%230891b2'/%3E%3Crect%20x='8'%20y='14'%20width='16'%20height='4'%20fill='%23ffffff'/%3E%3C/svg%3E")
}

func (s *server) render(w http.ResponseWriter, name string, code int, data any) {
	t, ok := s.tpl[name]
	if !ok {
		log.Printf("render: unknown template %q", name)
		http.Error(w, "Internal error.", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Internal error.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) message(w http.ResponseWriter, code int, heading, body, back string) {
	s.render(w, "message.html", code, messageData{
		Nav:     navData{},
		Heading: heading,
		Body:    body,
		Back:    back,
	})
}

// ---------- capture (the only public write path) ----------

func (s *server) handleCapture(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/b/")
	id, sub, _ := strings.Cut(rest, "/")
	if sub != "" {
		sub = "/" + sub
	}
	if !validID(id) {
		plain(w, http.StatusNotFound, "No such bin.\n")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, captureReadCap)
	body, total, overflow := readCapped(r.Body)

	reqID, err := NewID(reqIDLen)
	if err != nil {
		log.Printf("capture: id: %v", err)
		plain(w, http.StatusInternalServerError, "Internal error.\n")
		return
	}

	req := &Request{
		ID:          reqID,
		At:          s.now(),
		Method:      r.Method,
		Path:        r.URL.EscapedPath(),
		SubPath:     sub,
		Query:       r.URL.RawQuery,
		Headers:     collectHeaders(r),
		Body:        body,
		BodySize:    total,
		Truncated:   overflow,
		RemoteIP:    clientIP(r),
		ContentType: r.Header.Get("Content-Type"),
	}

	status, err := s.store.Capture(id, req)
	if errors.Is(err, errNotFound) {
		plain(w, http.StatusNotFound, "No such bin.\n")
		return
	}
	if err != nil {
		log.Printf("capture %s: %v", id, err)
		plain(w, http.StatusInternalServerError, "Internal error.\n")
		return
	}

	w.Header().Set("X-Catch-Request-Id", reqID)
	if noBodyStatus(status) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		return
	}
	plain(w, status, "caught "+reqID+"\n")
}

// readCapped keeps the first bodyStoreCap bytes and counts the rest.
func readCapped(rc io.Reader) (body []byte, total int64, truncated bool) {
	buf := make([]byte, bodyStoreCap)
	n, err := io.ReadFull(rc, buf)
	body = buf[:n]
	total = int64(n)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return body, total, false
	}
	if err != nil {
		return body, total, true
	}
	extra, cerr := io.Copy(io.Discard, rc)
	total += extra
	return body, total, extra > 0 || cerr != nil
}

func collectHeaders(r *http.Request) []Header {
	out := make([]Header, 0, len(r.Header)+1)
	// Host never appears in r.Header; an inspector should still show it.
	out = append(out, Header{Name: "Host", Values: []string{r.Host}})
	names := make([]string, 0, len(r.Header))
	for k := range r.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		vals := append([]string(nil), r.Header[k]...)
		out = append(out, Header{Name: k, Values: vals})
	}
	return out
}

func noBodyStatus(status int) bool {
	return status == http.StatusNoContent || status == http.StatusNotModified ||
		(status >= 100 && status < 200)
}

func validID(id string) bool {
	if len(id) != binIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !strings.ContainsRune(idAlphabet, rune(id[i])) {
			return false
		}
	}
	return true
}

// ---------- auth plumbing ----------

func (s *server) secureCookies() bool {
	return strings.HasPrefix(s.cfg.BaseURL, "https://")
}

func (s *server) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	now := s.now()

	if tok, ok := bearerToken(r); ok {
		if !s.auth.Allow(ip, now) {
			plain(w, http.StatusTooManyRequests, "Too many failed token checks. Wait a minute.\n")
			return false
		}
		if s.auth.CheckToken(tok) {
			return true
		}
		s.auth.Fail(ip, now)
		plain(w, http.StatusUnauthorized, "Unauthorized.\n")
		return false
	}

	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if !s.auth.Allow(ip, now) {
			plain(w, http.StatusTooManyRequests, "Too many failed token checks. Wait a minute.\n")
			return false
		}
		if s.auth.ValidSession(c.Value) {
			return true
		}
		s.auth.Fail(ip, now)
		clearSessionCookie(w, s.secureCookies())
	}

	// No credential offered: not a brute-force attempt, so no penalty.
	if wantsHTML(r) && r.Method == http.MethodGet {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return false
	}
	plain(w, http.StatusUnauthorized, "Unauthorized. Send Authorization: Bearer <token>.\n")
	return false
}

// checkOrigin rejects cross-site form posts. SameSite=Lax already blocks the
// cookie on cross-site POSTs; this is the belt to that pair of braces.
func (s *server) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		plain(w, http.StatusForbidden, "Bad origin.\n")
		return false
	}
	base, err := url.Parse(s.cfg.BaseURL)
	if err == nil && u.Host == base.Host {
		return true
	}
	if u.Host == r.Host {
		return true
	}
	plain(w, http.StatusForbidden, "Cross-site request refused.\n")
	return false
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func plain(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

// ---------- login ----------

func (s *server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login.html", http.StatusOK, loginData{
		Nav:  navData{Active: "login"},
		Next: safeNext(r.URL.Query().Get("next")),
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, formReadCap)
	if err := r.ParseForm(); err != nil {
		s.message(w, http.StatusBadRequest, "Bad request.", "The form could not be read.", "/login")
		return
	}
	ip := clientIP(r)
	now := s.now()
	next := safeNext(r.PostFormValue("next"))

	if !s.auth.Allow(ip, now) {
		s.render(w, "login.html", http.StatusTooManyRequests, loginData{
			Nav:   navData{Active: "login"},
			Error: "Too many failed attempts. Wait a minute.",
			Next:  next,
		})
		return
	}
	if !s.auth.CheckToken(r.PostFormValue("token")) {
		s.auth.Fail(ip, now)
		s.render(w, "login.html", http.StatusUnauthorized, loginData{
			Nav:   navData{Active: "login"},
			Error: "Wrong token.",
			Next:  next,
		})
		return
	}
	value, err := s.auth.NewSession()
	if err != nil {
		log.Printf("session: %v", err)
		s.message(w, http.StatusInternalServerError, "Internal error.", "Try again.", "/login")
		return
	}
	s.auth.Reset(ip)
	setSessionCookie(w, value, s.secureCookies())
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	clearSessionCookie(w, s.secureCookies())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// safeNext only allows same-site absolute paths.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	return next
}

// ---------- bins ----------

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	bins := s.store.List()
	nb, nr := s.store.Stats()
	s.render(w, "index.html", http.StatusOK, indexData{
		Nav:      navData{Active: "bins"},
		Bins:     bins,
		NumBins:  nb,
		NumReqs:  nr,
		BaseURL:  s.cfg.BaseURL,
		MaxReqs:  maxRequestsPerBin,
		TTLDays:  int(binTTL / (24 * time.Hour)),
		Statuses: offeredStatuses,
	})
}

func (s *server) handleCreateBin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	if !s.checkOrigin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, formReadCap)
	if err := r.ParseForm(); err != nil {
		s.message(w, http.StatusBadRequest, "Bad request.", "The form could not be read.", "/")
		return
	}
	status, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("status")))
	if err != nil || status < 100 || status > 599 {
		status = 200
	}
	bin, err := s.store.Create(r.PostFormValue("label"), status, s.now())
	if err != nil {
		log.Printf("create bin: %v", err)
		s.message(w, http.StatusInternalServerError, "Internal error.", "The bin was not created.", "/")
		return
	}
	http.Redirect(w, r, "/bin/"+bin.ID, http.StatusSeeOther)
}

func (s *server) handleBin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	bin, ok := s.store.Get(id)
	if !ok {
		s.message(w, http.StatusNotFound, "No such bin.", "It was deleted, expired, or never existed.", "/")
		return
	}
	s.store.Touch(id, s.now())
	s.render(w, "bin.html", http.StatusOK, binData{
		Nav:      navData{Active: "bins"},
		Bin:      bin,
		BinID:    bin.ID,
		Rows:     buildRows(bin),
		URL:      s.cfg.BaseURL + "/b/" + bin.ID,
		MaxReqs:  maxRequestsPerBin,
		TTLDays:  int(binTTL / (24 * time.Hour)),
		Statuses: offeredStatuses,
	})
}

func (s *server) handleRows(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	bin, ok := s.store.Get(id)
	if !ok {
		plain(w, http.StatusNotFound, "No such bin.\n")
		return
	}
	s.store.Touch(id, s.now())
	var buf bytes.Buffer
	if err := s.tpl["rows"].ExecuteTemplate(&buf, "rows", rowsData{BinID: bin.ID, Rows: buildRows(bin)}); err != nil {
		log.Printf("rows: %v", err)
		plain(w, http.StatusInternalServerError, "Internal error.\n")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func buildRows(bin *Bin) []reqView {
	rows := make([]reqView, 0, len(bin.Requests))
	for _, req := range bin.Requests {
		rows = append(rows, reqView{Req: req, Body: viewBody(req)})
	}
	return rows
}

// handleRawBody serves a captured body as an inert download. Captured bodies
// are attacker-supplied and this host shares a cookie domain with the rest of
// the estate, so: text/plain, nosniff, attachment. Always.
func (s *server) handleRawBody(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	bin, ok := s.store.Get(r.PathValue("id"))
	if !ok {
		plain(w, http.StatusNotFound, "No such bin.\n")
		return
	}
	rid := r.PathValue("rid")
	for _, req := range bin.Requests {
		if req.ID != rid {
			continue
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="catch-`+bin.ID+`-`+req.ID+`.txt"`)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(req.Body)
		return
	}
	plain(w, http.StatusNotFound, "No such request.\n")
}

func (s *server) handleBinSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	if !s.checkOrigin(w, r) {
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, formReadCap)
	if err := r.ParseForm(); err != nil {
		s.message(w, http.StatusBadRequest, "Bad request.", "The form could not be read.", "/bin/"+id)
		return
	}
	status, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("status")))
	if err != nil || status < 100 || status > 599 {
		s.message(w, http.StatusBadRequest, "Bad status.", "Pick a status between 100 and 599.", "/bin/"+id)
		return
	}
	if err := s.store.Update(id, r.PostFormValue("label"), status, s.now()); err != nil {
		s.message(w, http.StatusNotFound, "No such bin.", "It was deleted or expired.", "/")
		return
	}
	http.Redirect(w, r, "/bin/"+id, http.StatusSeeOther)
}

func (s *server) handleClearBin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	if !s.checkOrigin(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.store.Clear(id, s.now()); err != nil {
		s.message(w, http.StatusNotFound, "No such bin.", "It was deleted or expired.", "/")
		return
	}
	http.Redirect(w, r, "/bin/"+id, http.StatusSeeOther)
}

func (s *server) handleDeleteBin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	if !s.checkOrigin(w, r) {
		return
	}
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		s.message(w, http.StatusNotFound, "No such bin.", "It was deleted or expired.", "/")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
