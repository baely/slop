package main

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxFormBytes = 32 << 10

// App wires configuration, state and templates into the HTTP surface.
type App struct {
	cfg     Config
	store   *Store
	auth    *Auth
	checker *Checker
	logger  *log.Logger
	secure  bool
	pages   map[string]*template.Template
}

var pageNames = []string{"status", "login", "admin", "target"}

func (a *App) parseTemplates() error {
	a.pages = map[string]*template.Template{}
	for _, name := range pageNames {
		t, err := template.New("layout.html").ParseFS(templatesFS,
			"templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return err
		}
		a.pages[name] = t
	}
	return nil
}

// Handler builds the routed, wrapped HTTP handler.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("GET /{$}", a.handleStatus)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)

	mux.HandleFunc("GET /admin", a.requireToken(a.handleAdmin))
	mux.HandleFunc("POST /admin/targets", a.requireToken(a.handleAddTarget))
	mux.HandleFunc("POST /admin/targets/{id}/update", a.requireToken(a.handleUpdateTarget))
	mux.HandleFunc("POST /admin/targets/{id}/delete", a.requireToken(a.handleDeleteTarget))
	mux.HandleFunc("POST /admin/targets/{id}/check", a.requireToken(a.handleCheckNow))
	mux.HandleFunc("GET /t/{id}", a.requireToken(a.handleTarget))

	return a.logRequests(a.securityHeaders(a.capBody(mux)))
}

// ---------- middleware ----------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.logger.Printf("%s %s %d %s ip=%s",
			r.Method, r.URL.Path, rec.status,
			time.Since(start).Round(time.Millisecond), truncIP(clientIP(r)))
	})
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) capBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// requireToken gates a handler. Browsers get a redirect to the login form;
// anything else gets a bare 401.
func (a *App) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.auth.Authed(r) {
			next(w, r)
			return
		}
		ip := clientIP(r)
		if a.auth.Throttled(ip) {
			http.Error(w, "too many attempts", http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("Authorization") != "" {
			a.auth.Fail(ip)
		}
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="pulse"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// sameOrigin rejects cross-site form posts. SameSite=Lax already blocks the
// cookie; this covers the rest.
func (a *App) sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// ---------- rendering ----------

func (a *App) render(w http.ResponseWriter, r *http.Request, page string, code int, data any) {
	t, ok := a.pages[page]
	if !ok {
		a.fail(w, r, "unknown page "+page, nil)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		a.fail(w, r, "template", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(buf.Bytes())
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	a.logger.Printf("error %s %s: %s: %v", r.Method, r.URL.Path, what, err)
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}

// ---------- view models ----------

type baseView struct {
	Title  string
	Authed bool
	Nav    string
	Msg    string
	Err    string
	Caps   capsView
}

type capsView struct {
	Checks    int
	Days      int
	Targets   int
	Incidents int
}

var caps = capsView{
	Checks:    MaxChecksPerTarget,
	Days:      MaxDaySummaries,
	Targets:   MaxTargets,
	Incidents: MaxIncidentsPerTarget,
}

type targetView struct {
	ID        string
	Name      string
	URL       string // authed views only
	Method    string
	Expect    string
	Keyword   string
	Interval  int
	Timeout   int
	State     string
	StateKey  string
	Up24      string
	Up7d      string
	Rolled    bool
	LastAgo   string
	LastExact string
	Latency   string
	Spark     Spark
	Samples   int
	DownSince string
	LastErr   string // authed views only
}

type incidentView struct {
	TargetID   string
	Target     string
	Open       bool
	StartAgo   string
	StartExact string
	EndAgo     string
	EndExact   string
	Duration   string
	Err        string // authed views only
}

type statusView struct {
	baseView
	Targets    []targetView
	Incidents  []incidentView
	Up         int
	Down       int
	Unchecked  int
	Total      int
	Headline   string
	HeadlineOK bool
}

type adminView struct {
	baseView
	Targets []targetView
	Form    TargetInput
}

type targetPageView struct {
	baseView
	T         targetView
	Form      TargetInput
	Incidents []incidentView
	Checks    []checkRow
	Days      []dayRow
	Up30      string
}

type checkRow struct {
	Ago     string
	Exact   string
	Status  string
	Latency string
	Result  string
	Err     string
}

type dayRow struct {
	Day    string
	Total  int
	OK     int
	Uptime string
	AvgLat string
}

// buildTargetView assembles everything shown for one target. Fields that could
// leak internals (URL, error strings) are populated only when authed.
func (a *App) buildTargetView(t Target, authed bool, now time.Time) targetView {
	checks := a.store.Checks(t.ID)
	v := targetView{
		ID:       t.ID,
		Name:     t.Name,
		Method:   t.Method,
		Expect:   t.Expect,
		Keyword:  t.Keyword,
		Interval: t.Interval,
		Timeout:  t.Timeout,
		Samples:  len(checks),
		Spark:    BuildSpark(checks, 60),
		Latency:  "—",
		LastAgo:  "never",
		State:    "Unchecked",
		StateKey: "unknown",
	}
	if authed {
		v.URL = t.URL
	}

	u24 := a.store.Uptime(t.ID, 24*time.Hour, now)
	u7 := a.store.Uptime(t.ID, 7*24*time.Hour, now)
	v.Up24 = fmtPct(u24)
	v.Up7d = fmtPct(u7)
	v.Rolled = u24.Rolled || u7.Rolled

	if len(checks) > 0 {
		last := checks[len(checks)-1]
		v.LastAgo = humanize(last.At, now)
		v.LastExact = fmtExact(last.At)
		if last.OK {
			v.State, v.StateKey = "Up", "up"
		} else {
			v.State, v.StateKey = "Down", "down"
			if authed {
				v.LastErr = last.Err
				if v.LastErr == "" {
					v.LastErr = "check failed"
				}
			}
		}
		// Only a successful response has a latency worth quoting; the time
		// spent failing is in the history table, not on the headline.
		if last.OK {
			v.Latency = itoaMs(last.LatencyMs)
		}
	}

	incs := a.store.Incidents(t.ID)
	if len(incs) > 0 && incs[0].Open() {
		v.DownSince = humanize(incs[0].Start, now)
	}
	return v
}

func itoaMs(ms int) string {
	if ms < 0 {
		return "—"
	}
	return strconv.Itoa(ms) + " ms"
}

func (a *App) buildIncidentViews(incs []Incident, authed bool, now time.Time) []incidentView {
	names := map[string]string{}
	for _, t := range a.store.Targets() {
		names[t.ID] = t.Name
	}
	out := make([]incidentView, 0, len(incs))
	for _, in := range incs {
		iv := incidentView{
			TargetID:   in.TargetID,
			Target:     names[in.TargetID],
			Open:       in.Open(),
			StartAgo:   humanize(in.Start, now),
			StartExact: fmtExact(in.Start),
			Duration:   humanDuration(in.Duration(now)),
			EndAgo:     "ongoing",
			EndExact:   "",
		}
		if iv.Target == "" {
			iv.Target = "(removed)"
		}
		if in.End != nil {
			iv.EndAgo = humanize(*in.End, now)
			iv.EndExact = fmtExact(*in.End)
		}
		if authed {
			iv.Err = in.Err
			if iv.Err == "" {
				iv.Err = "check failed"
			}
		}
		out = append(out, iv)
	}
	return out
}

// ---------- handlers ----------

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	authed := a.auth.Authed(r)
	targets := a.store.Targets()

	v := statusView{
		baseView: baseView{Title: "pulse", Authed: authed, Nav: "status", Caps: caps,
			Msg: msgFor(r.URL.Query().Get("m"))},
		Total: len(targets),
	}
	for _, t := range targets {
		tv := a.buildTargetView(t, authed, now)
		switch tv.StateKey {
		case "up":
			v.Up++
		case "down":
			v.Down++
		default:
			v.Unchecked++
		}
		v.Targets = append(v.Targets, tv)
	}
	v.Incidents = a.buildIncidentViews(a.store.AllIncidents(20), authed, now)

	switch {
	case v.Total == 0:
		v.Headline = "0 targets."
	case v.Down > 0:
		v.Headline = strconv.Itoa(v.Down) + " Down"
	case v.Unchecked > 0 && v.Up == 0:
		v.Headline = "Unchecked."
	default:
		v.Headline = "All Up"
		v.HeadlineOK = true
	}
	a.render(w, r, "status", http.StatusOK, v)
}

func msgFor(code string) string {
	switch code {
	case "saved":
		return "Saved."
	case "deleted":
		return "Deleted."
	case "checked":
		return "Checked."
	case "updated":
		return "Updated."
	default:
		return ""
	}
}

func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if a.auth.Authed(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, http.StatusOK, "")
}

func (a *App) renderLogin(w http.ResponseWriter, r *http.Request, code int, errMsg string) {
	type loginView struct {
		baseView
		Next string
	}
	a.render(w, r, "login", code, loginView{
		baseView: baseView{Title: "Sign In — pulse", Nav: "login", Err: errMsg, Caps: caps},
		Next:     safeNext(r.URL.Query().Get("next")),
	})
}

// safeNext keeps redirects on this origin.
func safeNext(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return "/admin"
	}
	return v
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	if a.auth.Throttled(ip) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	next := safeNext(r.PostFormValue("next"))
	if !a.auth.TokenOK(r.PostFormValue("token")) {
		a.auth.Fail(ip)
		r.URL.RawQuery = "next=" + url.QueryEscape(next)
		a.renderLogin(w, r, http.StatusUnauthorized, "Wrong token.")
		return
	}
	a.auth.SetSession(w, a.secure)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	ClearSession(w, a.secure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	a.renderAdmin(w, r, http.StatusOK, TargetInput{}, "", msgFor(r.URL.Query().Get("m")))
}

func (a *App) renderAdmin(w http.ResponseWriter, r *http.Request, code int, form TargetInput, errMsg, msg string) {
	now := time.Now()
	v := adminView{
		baseView: baseView{Title: "Targets — pulse", Authed: true, Nav: "admin",
			Err: errMsg, Msg: msg, Caps: caps},
		Form: form,
	}
	for _, t := range a.store.Targets() {
		v.Targets = append(v.Targets, a.buildTargetView(t, true, now))
	}
	a.render(w, r, "admin", code, v)
}

func (a *App) handleAddTarget(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := formInput(r)
	t, err := in.Validate()
	if err != nil {
		a.renderAdmin(w, r, http.StatusBadRequest, in, err.Error(), "")
		return
	}
	if _, err := a.store.AddTarget(t, time.Now()); err != nil {
		a.renderAdmin(w, r, http.StatusBadRequest, in, err.Error(), "")
		return
	}
	if err := a.store.Save(); err != nil {
		a.fail(w, r, "persist", err)
		return
	}
	http.Redirect(w, r, "/admin?m=saved", http.StatusSeeOther)
}

func formInput(r *http.Request) TargetInput {
	return TargetInput{
		Name:     r.PostFormValue("name"),
		URL:      r.PostFormValue("url"),
		Method:   r.PostFormValue("method"),
		Interval: r.PostFormValue("interval"),
		Timeout:  r.PostFormValue("timeout"),
		Expect:   r.PostFormValue("expect"),
		Keyword:  r.PostFormValue("keyword"),
	}
}

func (a *App) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if _, ok := a.store.Target(id); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := formInput(r)
	t, err := in.Validate()
	if err != nil {
		a.renderTargetPage(w, r, id, http.StatusBadRequest, err.Error(), "", &in)
		return
	}
	if err := a.store.UpdateTarget(id, t); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := a.store.Save(); err != nil {
		a.fail(w, r, "persist", err)
		return
	}
	http.Redirect(w, r, "/t/"+url.PathEscape(id)+"?m=updated", http.StatusSeeOther)
}

func (a *App) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	if err := a.store.DeleteTarget(r.PathValue("id")); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := a.store.Save(); err != nil {
		a.fail(w, r, "persist", err)
		return
	}
	http.Redirect(w, r, "/admin?m=deleted", http.StatusSeeOther)
}

func (a *App) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	t, ok := a.store.Target(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(t.Timeout+2)*time.Second)
	defer cancel()
	a.checker.RunOne(ctx, t)
	a.store.Reschedule(id, time.Now().Add(time.Duration(t.Interval)*time.Second))
	if err := a.store.SaveIfDirty(); err != nil {
		a.logger.Printf("persist error: %v", err)
	}
	http.Redirect(w, r, "/t/"+url.PathEscape(id)+"?m=checked", http.StatusSeeOther)
}

func (a *App) handleTarget(w http.ResponseWriter, r *http.Request) {
	a.renderTargetPage(w, r, r.PathValue("id"), http.StatusOK, "", msgFor(r.URL.Query().Get("m")), nil)
}

func (a *App) renderTargetPage(w http.ResponseWriter, r *http.Request, id string, code int, errMsg, msg string, form *TargetInput) {
	t, ok := a.store.Target(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	now := time.Now()
	if form == nil {
		form = &TargetInput{
			Name:     t.Name,
			URL:      t.URL,
			Method:   t.Method,
			Interval: strconv.Itoa(t.Interval),
			Timeout:  strconv.Itoa(t.Timeout),
			Expect:   t.Expect,
			Keyword:  t.Keyword,
		}
	}
	v := targetPageView{
		baseView: baseView{Title: t.Name + " — pulse", Authed: true, Nav: "admin",
			Err: errMsg, Msg: msg, Caps: caps},
		T:    a.buildTargetView(t, true, now),
		Form: *form,
		Up30: fmtPct(a.store.Uptime(id, 30*24*time.Hour, now)),
	}
	v.Incidents = a.buildIncidentViews(a.store.Incidents(id), true, now)

	checks := a.store.Checks(id)
	for i := len(checks) - 1; i >= 0; i-- {
		c := checks[i]
		row := checkRow{
			Ago:     humanize(c.At, now),
			Exact:   fmtExact(c.At),
			Status:  "—",
			Latency: strconv.Itoa(c.LatencyMs),
			Result:  "Ok",
			Err:     c.Err,
		}
		if c.Status > 0 {
			row.Status = strconv.Itoa(c.Status)
		}
		if !c.OK {
			row.Result = "Failed"
			if row.Err == "" {
				row.Err = "check failed"
			}
		}
		v.Checks = append(v.Checks, row)
	}

	days := a.store.Days(id)
	for i := len(days) - 1; i >= 0; i-- {
		d := days[i]
		row := dayRow{Day: d.Day, Total: d.Total, OK: d.OK, Uptime: "—", AvgLat: "—"}
		if d.Total > 0 {
			row.Uptime = fmtPct(Uptime{Total: d.Total, OK: d.OK, Known: true})
			row.AvgLat = strconv.Itoa(int(d.LatencySum / int64(d.Total)))
		}
		v.Days = append(v.Days, row)
	}
	a.render(w, r, "target", code, v)
}
