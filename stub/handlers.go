package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- templates ----

var funcMap = template.FuncMap{
	"humanize": humanizeAny,
	"exact":    exactAny,
	"truncate": truncateString,
}

// humanizeAny / exactAny accept time.Time or *time.Time so templates can use
// the optional first/last click fields without extra plumbing.
func humanizeAny(v any) string {
	t, ok := asTime(v)
	if !ok {
		return "never"
	}
	return humanizeTime(t)
}

func exactAny(v any) string {
	t, ok := asTime(v)
	if !ok {
		return "never"
	}
	return t.Format("2006-01-02 15:04:05 MST")
}

func asTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, !t.IsZero()
	case *time.Time:
		if t == nil || t.IsZero() {
			return time.Time{}, false
		}
		return *t, true
	}
	return time.Time{}, false
}

var pageFiles = []string{"index.html", "login.html", "stats.html", "message.html"}

func parseTemplates() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range pageFiles {
		t, err := template.New("base.html").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

func (s *server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		log.Printf("stub: unknown template %q", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "base.html", data); err != nil {
		// Headers are already out; log and stop.
		log.Printf("stub: render %s: %v", page, err)
	}
}

// ---- view models ----

type baseData struct {
	Title   string
	Authed  bool
	CSRF    string
	BaseURL string
}

type rowView struct {
	Slug        string
	ShortURL    string
	Target      string
	TargetShort string
	Clicks      int
	Human       int
	Bots        int
	Uniques     int
	Created     time.Time
	State       string
	Disabled    bool
	Expires     string
	ExpiresHum  string
	MaxClicks   int
	Remaining   string
}

func (s *server) row(l *Link, now time.Time) rowView {
	r := rowView{
		Slug:        l.Slug,
		ShortURL:    s.baseURL + "/" + l.Slug,
		Target:      l.Target,
		TargetShort: truncateString(l.Target, 56),
		Clicks:      l.Clicks,
		Human:       l.HumanClicks(),
		Bots:        l.BotClicks,
		Uniques:     l.UniqueCount(),
		Created:     l.Created,
		State:       l.State(now).String(),
		Disabled:    l.Disabled,
		MaxClicks:   l.MaxClicks,
	}
	if l.ExpiresAt != nil {
		r.Expires = l.ExpiresAt.Format("2006-01-02 15:04")
		r.ExpiresHum = humanizeTime(*l.ExpiresAt)
	}
	if l.MaxClicks > 0 {
		left := l.MaxClicks - l.Clicks
		if left < 0 {
			left = 0
		}
		r.Remaining = fmt.Sprintf("%d of %d left", left, l.MaxClicks)
	}
	return r
}

type indexData struct {
	baseData
	Rows      []rowView
	Sort      string
	Dir       string
	SortLinks map[string]string
	Error     string
	Notice    string
	NewLink   *rowView
	Count     int
	Max       int
	Form      formValues
}

type formValues struct {
	Target    string
	Slug      string
	Expires   string
	MaxClicks string
}

type statsData struct {
	baseData
	Link     rowView
	Chart    Chart
	Refs     []RefRow
	First    *time.Time
	Last     *time.Time
	Capped   bool
	BotShare int
	BotDays  int
}

// ---- pages ----

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	sess, res := s.authenticate(r, now)
	if res == authLimited {
		s.tooMany(w)
		return
	}
	if !sess.ok {
		s.renderLogin(w, http.StatusOK, "")
		return
	}
	q := r.URL.Query()
	s.renderIndex(w, http.StatusOK, sess, indexData{Sort: q.Get("sort"), Dir: q.Get("dir")})
}

func (s *server) renderLogin(w http.ResponseWriter, status int, errMsg string) {
	s.render(w, status, "login.html", struct {
		baseData
		Error string
	}{
		baseData: baseData{Title: "stub", BaseURL: s.baseURL},
		Error:    errMsg,
	})
}

// renderIndex fills in everything that does not depend on the specific action.
func (s *server) renderIndex(w http.ResponseWriter, status int, sess session, d indexData) {
	now := s.now()
	sortKey := d.Sort
	if sortKey != "clicks" {
		sortKey = "created"
	}
	dir := d.Dir
	if dir != "asc" {
		dir = "desc"
	}

	links := s.store.List()
	rows := make([]rowView, 0, len(links))
	for _, l := range links {
		rows = append(rows, s.row(l, now))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		var less bool
		if sortKey == "clicks" {
			if rows[i].Clicks == rows[j].Clicks {
				less = rows[i].Created.Before(rows[j].Created)
			} else {
				less = rows[i].Clicks < rows[j].Clicks
			}
		} else {
			less = rows[i].Created.Before(rows[j].Created)
		}
		if dir == "desc" {
			return !less
		}
		return less
	})

	// Absolute so the header links still work when the index is re-rendered
	// from a POST to /admin/*.
	flip := func(col string) string {
		if sortKey == col && dir == "desc" {
			return "/?sort=" + col + "&dir=asc"
		}
		return "/?sort=" + col + "&dir=desc"
	}

	d.baseData = baseData{
		Title:   "stub",
		Authed:  true,
		CSRF:    s.auth.csrfToken(sess.sid),
		BaseURL: s.baseURL,
	}
	d.Rows = rows
	d.Sort = sortKey
	d.Dir = dir
	d.SortLinks = map[string]string{"clicks": flip("clicks"), "created": flip("created")}
	d.Count = len(rows)
	d.Max = maxLinks
	s.render(w, status, "index.html", d)
}

func (s *server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	sess, res := s.authenticate(r, now)
	if res == authLimited {
		s.tooMany(w)
		return
	}
	if !sess.ok {
		s.renderLogin(w, http.StatusUnauthorized, "Sign in to view stats.")
		return
	}
	l, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		s.message(w, http.StatusNotFound, "No Such Link", "That slug is not in the table.")
		return
	}
	row := s.row(l, now)
	refs := topReferrers(l.Referrers, 12)
	botDays := 0
	for _, v := range l.BotDays {
		botDays += v
	}
	share := 0
	if l.Clicks > 0 {
		share = int(float64(l.BotClicks)*100/float64(l.Clicks) + 0.5)
	}
	s.render(w, http.StatusOK, "stats.html", statsData{
		baseData: baseData{Title: "stub / " + l.Slug, Authed: true, CSRF: s.auth.csrfToken(sess.sid), BaseURL: s.baseURL},
		Link:     row,
		Chart:    buildChart(l.Days, now),
		Refs:     refs,
		First:    l.FirstClick,
		Last:     l.LastClick,
		Capped:   l.UniquesCapped,
		BotShare: share,
		BotDays:  botDays,
	})
}

// ---- auth endpoints ----

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	ip := clientIP(r)
	if !s.auth.allow(ip, now) {
		s.tooMany(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, http.StatusBadRequest, "Could not read the form.")
		return
	}
	if !s.auth.checkToken(strings.TrimSpace(r.PostFormValue("token"))) {
		s.auth.fail(ip, now)
		s.renderLogin(w, http.StatusUnauthorized, "Wrong token.")
		return
	}
	value, err := s.auth.newSessionValue()
	if err != nil {
		log.Printf("stub: session mint: %v", err)
		s.message(w, http.StatusInternalServerError, "Error", "Could not start a session.")
		return
	}
	setSessionCookie(w, value, s.secure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, s.secure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- form actions ----

// requireForm authenticates a browser POST and validates the CSRF token.
func (s *server) requireForm(w http.ResponseWriter, r *http.Request) (session, bool) {
	now := s.now()
	sess, res := s.authenticate(r, now)
	if res == authLimited {
		s.tooMany(w)
		return sess, false
	}
	if !sess.ok {
		s.renderLogin(w, http.StatusUnauthorized, "Sign in first.")
		return sess, false
	}
	if err := r.ParseForm(); err != nil {
		s.message(w, http.StatusBadRequest, "Bad Request", "Could not read the form.")
		return sess, false
	}
	if sess.sid != "" && !s.auth.checkCSRF(sess.sid, r.PostFormValue("csrf")) {
		s.message(w, http.StatusForbidden, "Bad Request", "Form token did not match. Reload the page and try again.")
		return sess, false
	}
	return sess, true
}

func (s *server) handleCreateForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireForm(w, r)
	if !ok {
		return
	}
	form := formValues{
		Target:    strings.TrimSpace(r.PostFormValue("target")),
		Slug:      strings.TrimSpace(r.PostFormValue("slug")),
		Expires:   strings.TrimSpace(r.PostFormValue("expires")),
		MaxClicks: strings.TrimSpace(r.PostFormValue("max_clicks")),
	}

	p := CreateParams{Target: form.Target, Slug: form.Slug}
	if form.Expires != "" {
		t, err := parseLocalTime(form.Expires)
		if err != nil {
			s.renderIndex(w, http.StatusBadRequest, sess, indexData{Error: err.Error(), Form: form})
			return
		}
		p.ExpiresAt = &t
	}
	if form.MaxClicks != "" {
		n, err := strconv.Atoi(form.MaxClicks)
		if err != nil || n < 0 {
			s.renderIndex(w, http.StatusBadRequest, sess, indexData{Error: "Max clicks must be a whole number of 0 or more (0 means unlimited).", Form: form})
			return
		}
		p.MaxClicks = n
	}

	l, err := s.store.Create(p, s.now())
	if err != nil {
		status := http.StatusBadRequest
		var ce conflictError
		if errors.As(err, &ce) {
			status = http.StatusConflict
		}
		s.renderIndex(w, status, sess, indexData{Error: err.Error(), Form: form})
		return
	}
	row := s.row(l, s.now())
	s.renderIndex(w, http.StatusOK, sess, indexData{Notice: "Created.", NewLink: &row})
}

func (s *server) handleDeleteForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireForm(w, r)
	if !ok {
		return
	}
	slug := r.PostFormValue("slug")
	if err := s.store.Delete(slug); err != nil {
		s.renderIndex(w, http.StatusNotFound, sess, indexData{Error: "No such slug."})
		return
	}
	s.renderIndex(w, http.StatusOK, sess, indexData{Notice: "Deleted " + slug + "."})
}

func (s *server) handleStateForm(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireForm(w, r)
	if !ok {
		return
	}
	slug := r.PostFormValue("slug")
	disable := r.PostFormValue("disabled") == "1"
	if err := s.store.SetDisabled(slug, disable); err != nil {
		s.renderIndex(w, http.StatusNotFound, sess, indexData{Error: "No such slug."})
		return
	}
	word := "Enabled "
	if disable {
		word = "Disabled "
	}
	s.renderIndex(w, http.StatusOK, sess, indexData{Notice: word + slug + "."})
}

// ---- the public redirect ----

// handleRedirect is the only public, high-traffic path. No template, no body,
// no allocation beyond the click record.
func (s *server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" || reservedSlugs[strings.ToLower(slug)] {
		s.message(w, http.StatusNotFound, "Not Found", "No link with that slug.")
		return
	}

	target, state, found := s.store.Resolve(slug, Visit{
		IP:       clientIP(r),
		UA:       r.UserAgent(),
		Referer:  r.Referer(),
		Prefetch: isPrefetch(r.Header),
		Now:      s.now(),
	})
	if !found {
		s.message(w, http.StatusNotFound, "Not Found", "No link with that slug.")
		return
	}
	if state != stateActive {
		s.gone(w, slug, state)
		return
	}

	h := w.Header()
	h.Set("Location", target)
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	h.Set("Content-Length", "0")
	w.WriteHeader(http.StatusFound)
}

func (s *server) gone(w http.ResponseWriter, slug string, state linkState) {
	var detail string
	switch state {
	case stateDisabled:
		detail = "This link has been turned off."
	case stateExpired:
		detail = "This link passed its expiry time."
	case stateExhausted:
		detail = "This link hit its click limit."
	default:
		detail = "This link no longer resolves."
	}
	s.message(w, http.StatusGone, "Gone", detail+" It existed; it does not resolve any more.")
}

func (s *server) message(w http.ResponseWriter, status int, heading, detail string) {
	s.render(w, status, "message.html", struct {
		baseData
		Heading string
		Detail  string
		Status  int
	}{
		baseData: baseData{Title: "stub / " + heading, BaseURL: s.baseURL},
		Heading:  heading,
		Detail:   detail,
		Status:   status,
	})
}

func (s *server) tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	s.message(w, http.StatusTooManyRequests, "Slow Down", "Too many failed token checks from this address. Try again in a minute.")
}

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=600")
	s.static.ServeHTTP(w, r)
}

// ---- JSON API ----

type apiLink struct {
	Slug          string         `json:"slug"`
	ShortURL      string         `json:"short_url"`
	Target        string         `json:"target"`
	Created       time.Time      `json:"created"`
	ExpiresAt     *time.Time     `json:"expires_at,omitempty"`
	MaxClicks     int            `json:"max_clicks,omitempty"`
	Disabled      bool           `json:"disabled"`
	State         string         `json:"state"`
	Clicks        int            `json:"clicks"`
	HumanClicks   int            `json:"human_clicks"`
	BotClicks     int            `json:"bot_clicks"`
	UnknownClicks int            `json:"unknown_clicks"`
	UniqueClicks  int            `json:"unique_clicks"`
	FirstClick    *time.Time     `json:"first_click,omitempty"`
	LastClick     *time.Time     `json:"last_click,omitempty"`
	Days          map[string]int `json:"days,omitempty"`
	BotDays       map[string]int `json:"bot_days,omitempty"`
	Referrers     map[string]int `json:"referrers,omitempty"`
}

func (s *server) apiView(l *Link, now time.Time, detail bool) apiLink {
	v := apiLink{
		Slug:          l.Slug,
		ShortURL:      s.baseURL + "/" + l.Slug,
		Target:        l.Target,
		Created:       l.Created,
		ExpiresAt:     l.ExpiresAt,
		MaxClicks:     l.MaxClicks,
		Disabled:      l.Disabled,
		State:         strings.ToLower(l.State(now).String()),
		Clicks:        l.Clicks,
		HumanClicks:   l.HumanClicks(),
		BotClicks:     l.BotClicks,
		UnknownClicks: l.UnknownClicks,
		UniqueClicks:  l.UniqueCount(),
		FirstClick:    l.FirstClick,
		LastClick:     l.LastClick,
	}
	if detail {
		v.Days = l.Days
		v.BotDays = l.BotDays
		v.Referrers = l.Referrers
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// requireAPI insists on a valid credential and answers in JSON.
func (s *server) requireAPI(w http.ResponseWriter, r *http.Request) bool {
	now := s.now()
	sess, res := s.authenticate(r, now)
	switch res {
	case authLimited:
		w.Header().Set("Retry-After", "60")
		writeAPIError(w, http.StatusTooManyRequests, "Too many failed token checks from this address.")
		return false
	case authOK:
		_ = sess
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="stub"`)
	writeAPIError(w, http.StatusUnauthorized, "Authorization: Bearer <APP_TOKEN> required.")
	return false
}

type createRequest struct {
	Target    string `json:"target"`
	Slug      string `json:"slug"`
	ExpiresAt string `json:"expires_at"`
	ExpiresIn string `json:"expires_in"`
	MaxClicks int    `json:"max_clicks"`
}

func (s *server) handleAPICreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "Request body too large.")
		return
	}
	var req createRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Body must be JSON: {\"target\":\"https://…\",\"slug\":\"optional\"}")
			return
		}
	}
	now := s.now()
	p := CreateParams{Target: req.Target, Slug: req.Slug, MaxClicks: req.MaxClicks}
	switch {
	case req.ExpiresAt != "":
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "expires_at must be RFC3339, e.g. 2026-08-01T09:00:00+10:00.")
			return
		}
		p.ExpiresAt = &t
	case req.ExpiresIn != "":
		d, err := time.ParseDuration(req.ExpiresIn)
		if err != nil || d <= 0 {
			writeAPIError(w, http.StatusBadRequest, "expires_in must be a positive Go duration, e.g. 72h.")
			return
		}
		t := now.Add(d)
		p.ExpiresAt = &t
	}

	l, err := s.store.Create(p, now)
	if err != nil {
		var ce conflictError
		if errors.As(err, &ce) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Location", s.baseURL+"/"+l.Slug)
	writeJSON(w, http.StatusCreated, s.apiView(l, now, false))
}

func (s *server) handleAPIList(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	now := s.now()
	links := s.store.List()
	out := make([]apiLink, 0, len(links))
	for _, l := range links {
		out = append(out, s.apiView(l, now, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "links": out})
}

func (s *server) handleAPIGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	l, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "No link with that slug.")
		return
	}
	writeJSON(w, http.StatusOK, s.apiView(l, s.now(), true))
}

func (s *server) handleAPIDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	if err := s.store.Delete(r.PathValue("slug")); err != nil {
		writeAPIError(w, http.StatusNotFound, "No link with that slug.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- small helpers ----

// truncateString counts runes, not bytes, so a target with non-ASCII in it
// cannot be cut mid-character and land in the page as a replacement glyph.
func truncateString(s string, n int) string {
	if n <= 1 {
		if s == "" {
			return ""
		}
		return "…"
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// parseLocalTime accepts what <input type="datetime-local"> submits.
func parseLocalTime(v string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("Expiry %q is not a time this understands. Use the date picker, or YYYY-MM-DDTHH:MM.", v)
}

// humanizeTime renders a coarse relative time. Exact values go in title
// attributes, per house style.
func humanizeTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	future := d < 0
	if future {
		d = -d
	}
	var s string
	switch {
	case d < 45*time.Second:
		if future {
			return "in a moment"
		}
		return "just now"
	case d < 90*time.Second:
		s = "a minute"
	case d < time.Hour:
		s = fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		s = "an hour"
	case d < 24*time.Hour:
		s = fmt.Sprintf("%d hours", int(d.Hours()))
	case d < 48*time.Hour:
		if future {
			return "tomorrow"
		}
		return "yesterday"
	case d < 14*24*time.Hour:
		s = fmt.Sprintf("%d days", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		s = fmt.Sprintf("%d weeks", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		s = fmt.Sprintf("%d months", int(d.Hours()/24/30))
	default:
		s = fmt.Sprintf("%d years", int(d.Hours()/24/365))
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}
