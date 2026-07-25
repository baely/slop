package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testToken = "test-token-9f2a"

func newTestApp(t *testing.T) (*App, *httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app := &App{
		cfg:     Config{DataDir: dir, BaseURL: "http://example.test"},
		store:   s,
		auth:    NewAuth(testToken),
		checker: NewChecker(s, logger, 2),
		logger:  logger,
		secure:  false,
	}
	if err := app.parseTemplates(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(srv.Close)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return app, srv, client
}

func get(t *testing.T, c *http.Client, url string, hdr map[string]string) (int, string, *http.Response) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp
}

func post(t *testing.T, c *http.Client, u string, form url.Values, hdr map[string]string) (int, string, *http.Response) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp
}

func TestHealthzIsOpen(t *testing.T) {
	_, srv, c := newTestApp(t)
	code, body, _ := get(t, c, srv.URL+"/healthz", nil)
	if code != http.StatusOK || body != "ok" {
		t.Fatalf("healthz = %d %q, want 200 \"ok\"", code, body)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	_, srv, c := newTestApp(t)
	for _, path := range []string{"/", "/healthz", "/login", "/static/style.css"} {
		_, _, resp := get(t, c, srv.URL+path, nil)
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: nosniff = %q", path, got)
		}
		if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("%s: referrer-policy = %q", path, got)
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "https://fonts.gstatic.com") {
			t.Fatalf("%s: csp = %q", path, csp)
		}
	}
}

func TestPublicStatusPageIsEmptyAndHonest(t *testing.T) {
	_, srv, c := newTestApp(t)
	code, body, _ := get(t, c, srv.URL+"/", nil)
	if code != http.StatusOK {
		t.Fatalf("status page = %d", code)
	}
	if !strings.Contains(body, "0 targets.") {
		t.Fatalf("empty status page does not say \"0 targets.\":\n%s", body)
	}
	if strings.Contains(body, "100%") {
		t.Fatal("empty status page claims 100% uptime")
	}
}

func TestTokenGating(t *testing.T) {
	_, srv, c := newTestApp(t)

	// Browser-ish request is redirected to the login form.
	code, _, resp := get(t, c, srv.URL+"/admin", map[string]string{"Accept": "text/html"})
	if code != http.StatusSeeOther {
		t.Fatalf("unauthenticated /admin = %d, want 303", code)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?next=") {
		t.Fatalf("redirect location = %q", loc)
	}

	// API-ish request gets a flat 401.
	if code, _, _ := get(t, c, srv.URL+"/admin", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /admin (no html accept) = %d, want 401", code)
	}

	// Wrong token: 401.
	if code, _, _ := get(t, c, srv.URL+"/admin", map[string]string{"Authorization": "Bearer wrong"}); code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer = %d, want 401", code)
	}

	// Right token: 200.
	code, body, _ := get(t, c, srv.URL+"/admin", map[string]string{"Authorization": "Bearer " + testToken})
	if code != http.StatusOK {
		t.Fatalf("correct bearer = %d, want 200", code)
	}
	if !strings.Contains(body, "Add Target") {
		t.Fatal("admin page missing the add form")
	}

	// Mutating endpoints are gated too.
	if code, _, _ := post(t, c, srv.URL+"/admin/targets", url.Values{"name": {"x"}, "url": {"https://x.example/"}}, nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated add = %d, want 401", code)
	}
}

func TestLoginFlowAndBruteForceLimit(t *testing.T) {
	app, srv, c := newTestApp(t)

	code, body, _ := post(t, c, srv.URL+"/login", url.Values{"token": {"nope"}}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", code)
	}
	if !strings.Contains(body, "Wrong token.") {
		t.Fatal("bad login did not say so")
	}

	code, _, resp := post(t, c, srv.URL+"/login", url.Values{"token": {testToken}, "next": {"/admin"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("good login = %d, want 303", code)
	}
	var cookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags wrong: %+v", cookie)
	}
	if strings.Contains(cookie.Value, testToken) {
		t.Fatal("cookie contains the token itself")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin", nil)
	req.AddCookie(cookie)
	resp2, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("cookie auth = %d, want 200", resp2.StatusCode)
	}

	// Burn the failure budget; the next attempt is refused outright.
	for i := 0; i < failLimit+2; i++ {
		post(t, c, srv.URL+"/login", url.Values{"token": {"nope"}}, nil)
	}
	code, _, _ = post(t, c, srv.URL+"/login", url.Values{"token": {testToken}}, nil)
	if code != http.StatusTooManyRequests {
		t.Fatalf("after %d failures login = %d, want 429", failLimit+2, code)
	}
	_ = app
}

func TestCrossOriginPostRejected(t *testing.T) {
	_, srv, c := newTestApp(t)
	code, _, _ := post(t, c, srv.URL+"/admin/targets",
		url.Values{"name": {"x"}, "url": {"https://x.example/"}},
		map[string]string{"Authorization": "Bearer " + testToken, "Origin": "https://evil.example"})
	if code != http.StatusForbidden {
		t.Fatalf("cross-origin post = %d, want 403", code)
	}
}

func TestAddTargetEndToEnd(t *testing.T) {
	app, srv, c := newTestApp(t)
	bearer := map[string]string{"Authorization": "Bearer " + testToken}

	code, body, _ := post(t, c, srv.URL+"/admin/targets", url.Values{
		"name": {"index"}, "url": {"https://index.baileys.dev/"},
		"interval": {"60"}, "timeout": {"10"}, "expect": {"2xx"},
	}, bearer)
	if code != http.StatusSeeOther {
		t.Fatalf("add target = %d, want 303\n%s", code, body)
	}
	if app.store.Count() != 1 {
		t.Fatalf("store holds %d targets", app.store.Count())
	}
	tg := app.store.Targets()[0]

	// Rejected input re-renders the form with a 400 and an explanation.
	code, body, _ = post(t, c, srv.URL+"/admin/targets", url.Values{"name": {"bad"}, "url": {"ftp://nope/"}}, bearer)
	if code != http.StatusBadRequest {
		t.Fatalf("bad target = %d, want 400", code)
	}
	if !strings.Contains(body, "url must be http or https") {
		t.Fatalf("no explanation in the response:\n%s", body)
	}

	// Public page names the target and its state, and nothing more.
	code, body, _ = get(t, c, srv.URL+"/", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "index") || !strings.Contains(body, "Unchecked") {
		t.Fatalf("status page missing name or state:\n%s", body)
	}
	if strings.Contains(body, "index.baileys.dev") {
		t.Fatal("public status page leaks the target URL")
	}
	if strings.Contains(body, tg.ID) {
		t.Fatal("public status page leaks the target id")
	}

	// The detail page is gated.
	if code, _, _ := get(t, c, srv.URL+"/t/"+tg.ID, nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated detail = %d, want 401", code)
	}
	code, body, _ = get(t, c, srv.URL+"/t/"+tg.ID, bearer)
	if code != http.StatusOK {
		t.Fatalf("detail = %d, want 200", code)
	}
	if !strings.Contains(body, "index.baileys.dev") {
		t.Fatal("detail page should show the URL to an authenticated caller")
	}
	if !strings.Contains(body, "Unchecked.") {
		t.Fatal("a never-checked target must read Unchecked.")
	}

	// Unknown target is a 404, not a 500.
	if code, _, _ := get(t, c, srv.URL+"/t/aaaaaaaaaaaaaaaaaaaa", bearer); code != http.StatusNotFound {
		t.Fatalf("unknown target = %d, want 404", code)
	}

	// Delete.
	code, _, _ = post(t, c, srv.URL+"/admin/targets/"+tg.ID+"/delete", nil, bearer)
	if code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", code)
	}
	if app.store.Count() != 0 {
		t.Fatal("target survived deletion")
	}
}

func TestCheckNowAndPersistenceAcrossRestart(t *testing.T) {
	app, srv, c := newTestApp(t)
	bearer := map[string]string{"Authorization": "Bearer " + testToken}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "estate ok")
	}))
	defer upstream.Close()

	post(t, c, srv.URL+"/admin/targets", url.Values{
		"name": {"local"}, "url": {upstream.URL + "/"}, "interval": {"3600"},
		"timeout": {"5"}, "keyword": {"estate"},
	}, bearer)
	tg := app.store.Targets()[0]

	code, _, _ := post(t, c, srv.URL+"/admin/targets/"+tg.ID+"/check", nil, bearer)
	if code != http.StatusSeeOther {
		t.Fatalf("check now = %d, want 303", code)
	}
	checks := app.store.Checks(tg.ID)
	if len(checks) != 1 || !checks[0].OK || checks[0].Status != 200 {
		t.Fatalf("manual check did not record a pass: %+v", checks)
	}

	if err := app.store.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(app.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Count() != 1 {
		t.Fatalf("restart lost targets: %d", reloaded.Count())
	}
	if got := reloaded.Checks(tg.ID); len(got) != 1 || !got[0].OK {
		t.Fatalf("restart lost checks: %+v", got)
	}
	u := reloaded.Uptime(tg.ID, 24*time.Hour, time.Now())
	if !u.Known || u.OK != 1 {
		t.Fatalf("uptime after restart = %+v", u)
	}
}

func TestStatusPageRendersStateAndIncidents(t *testing.T) {
	app, srv, c := newTestApp(t)
	now := time.Now()
	tg := mustTarget(t, app.store, TargetInput{Name: "trackui", URL: "https://trackui.baileys.dev/"}, now)
	app.store.Record(tg.ID, Check{At: now.Add(-10 * time.Minute), Status: 200, LatencyMs: 30, OK: true})
	app.store.Record(tg.ID, Check{At: now.Add(-5 * time.Minute), Status: 0, LatencyMs: 5000, Err: "timeout"})

	code, body, _ := get(t, c, srv.URL+"/", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"trackui", "Down", "1 Down", "Ongoing", "50.0%"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "timeout") {
		t.Fatal("public status page leaks the failure detail")
	}
	if !strings.Contains(body, "<svg") {
		t.Fatal("no sparkline rendered")
	}

	code, body, _ = get(t, c, srv.URL+"/t/"+tg.ID, map[string]string{"Authorization": "Bearer " + testToken})
	if code != http.StatusOK {
		t.Fatalf("detail = %d", code)
	}
	if !strings.Contains(body, "timeout") {
		t.Fatal("detail page should show the failure detail to an authenticated caller")
	}
}
