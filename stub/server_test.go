package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testToken = "test-token-please-ignore"

func newTestServer(t *testing.T) (*server, http.Handler, *Store) {
	t.Helper()
	store, _ := newTestStore(t)
	s, err := newServer(store, testToken, "https://stub.example")
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	s.secure = false // httptest speaks plain http
	return s, s.handler(), store
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func bearer(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestHealthz(t *testing.T) {
	_, h, _ := newTestServer(t)
	w := do(h, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "ok" {
		t.Fatalf("GET /healthz body = %q, want ok", w.Body.String())
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	_, h, store := newTestServer(t)
	store.Create(CreateParams{Slug: "hh", Target: "https://example.com"}, time.Now())

	for _, path := range []string{"/", "/healthz", "/hh", "/nope", "/static/style.css"} {
		w := do(h, httptest.NewRequest("GET", path, nil))
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q", path, got)
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: CSP = %q", path, csp)
		}
	}
}

func TestUnauthenticatedCannotListOrCreate(t *testing.T) {
	_, h, _ := newTestServer(t)

	// The HTML index shows a login form, never the table.
	w := do(h, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 login page", w.Code)
	}
	if strings.Contains(w.Body.String(), "Create Link") {
		t.Fatal("GET / exposed the create form without a token")
	}
	if !strings.Contains(w.Body.String(), "Sign In") {
		t.Fatal("GET / did not render the login form")
	}

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/links", ""},
		{"POST", "/api/links", `{"target":"https://example.com"}`},
		{"GET", "/api/links/anything", ""},
		{"DELETE", "/api/links/anything", ""},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := do(h, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", tc.method, tc.path, w.Code)
		}
	}

	// Wrong token is also 401.
	r := httptest.NewRequest("GET", "/api/links", nil)
	r.Header.Set("Authorization", "Bearer definitely-not-the-token")
	if w := do(h, r); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer token = %d, want 401", w.Code)
	}

	// Admin form endpoints refuse too.
	for _, path := range []string{"/admin/create", "/admin/delete", "/admin/state"} {
		w := do(h, httptest.NewRequest("POST", path, strings.NewReader("target=https://example.com")))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a token = %d, want 401", path, w.Code)
		}
	}
	w = do(h, httptest.NewRequest("GET", "/admin/link/whatever", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /admin/link/... without a token = %d, want 401", w.Code)
	}
}

func TestAPICreateAndRedirect(t *testing.T) {
	_, h, _ := newTestServer(t)

	w := do(h, bearer("POST", "/api/links", `{"target":"https://example.com/deep/path?x=1","slug":"demo"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/links = %d, want 201. body: %s", w.Code, w.Body.String())
	}
	var created apiLink
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if created.Slug != "demo" {
		t.Fatalf("slug = %q, want demo", created.Slug)
	}
	if created.ShortURL != "https://stub.example/demo" {
		t.Fatalf("short_url = %q", created.ShortURL)
	}

	// The redirect is public: no token.
	rw := do(h, httptest.NewRequest("GET", "/demo", nil))
	if rw.Code != http.StatusFound {
		t.Fatalf("GET /demo = %d, want 302", rw.Code)
	}
	if got := rw.Header().Get("Location"); got != "https://example.com/deep/path?x=1" {
		t.Fatalf("Location = %q", got)
	}
	if rw.Body.Len() != 0 {
		t.Fatalf("redirect wrote a %d-byte body; the fast path should write none", rw.Body.Len())
	}
	if !strings.Contains(rw.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("redirect Cache-Control = %q, want no-store so clicks are not cached away", rw.Header().Get("Cache-Control"))
	}

	// Slug lookup is case-insensitive.
	if w := do(h, httptest.NewRequest("GET", "/DEMO", nil)); w.Code != http.StatusFound {
		t.Fatalf("GET /DEMO = %d, want 302", w.Code)
	}
}

func TestAPICreateRejectsDangerousSchemes(t *testing.T) {
	_, h, _ := newTestServer(t)
	for _, target := range []string{
		"javascript:alert(document.domain)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"file:///etc/passwd",
		"//evil.example.com",
		"not-a-url",
	} {
		body, _ := json.Marshal(map[string]string{"target": target})
		w := do(h, bearer("POST", "/api/links", string(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("POST target=%q = %d, want 400", target, w.Code)
		}
		var e map[string]string
		json.Unmarshal(w.Body.Bytes(), &e)
		if e["error"] == "" {
			t.Fatalf("target=%q produced no error message", target)
		}
	}
}

func TestAPICreateSlugConflictIs409(t *testing.T) {
	_, h, _ := newTestServer(t)
	do(h, bearer("POST", "/api/links", `{"target":"https://example.com/1","slug":"taken"}`))
	w := do(h, bearer("POST", "/api/links", `{"target":"https://example.com/2","slug":"taken"}`))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate slug = %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "already taken") {
		t.Fatalf("409 body = %s", w.Body.String())
	}
}

// A link must never be able to shadow the app's own routes.
func TestReservedPathsCannotBeClaimed(t *testing.T) {
	_, h, _ := newTestServer(t)
	// Dotless names are refused by the reserved list and say so.
	for _, slug := range []string{"healthz", "static", "admin", "api", "login", "logout"} {
		body, _ := json.Marshal(map[string]string{"target": "https://evil.example/", "slug": slug})
		w := do(h, bearer("POST", "/api/links", string(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("creating slug %q = %d, want 400", slug, w.Code)
		}
		if !strings.Contains(w.Body.String(), "reserved") {
			t.Fatalf("slug %q rejection did not say it is reserved: %s", slug, w.Body.String())
		}
	}
	// These are unreachable for a second reason (the dot is not a legal slug
	// character), which is fine — what matters is that they are refused.
	for _, slug := range []string{"robots.txt", "favicon.ico", "sitemap.xml", ".well-known"} {
		body, _ := json.Marshal(map[string]string{"target": "https://evil.example/", "slug": slug})
		w := do(h, bearer("POST", "/api/links", string(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("creating slug %q = %d, want 400", slug, w.Code)
		}
		var e map[string]string
		json.Unmarshal(w.Body.Bytes(), &e)
		if e["error"] == "" {
			t.Fatalf("slug %q was refused without an explanation", slug)
		}
	}
	// And the real routes still behave.
	if w := do(h, httptest.NewRequest("GET", "/healthz", nil)); w.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d after the attempts", w.Code)
	}
	if w := do(h, httptest.NewRequest("GET", "/static/style.css", nil)); w.Code != http.StatusOK {
		t.Fatalf("GET /static/style.css = %d", w.Code)
	}
}

func TestMaxClicksReturns410AfterExhaustion(t *testing.T) {
	_, h, _ := newTestServer(t)
	w := do(h, bearer("POST", "/api/links", `{"target":"https://example.com/limited","slug":"lim","max_clicks":2}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	for i := 1; i <= 2; i++ {
		rw := do(h, httptest.NewRequest("GET", "/lim", nil))
		if rw.Code != http.StatusFound {
			t.Fatalf("click %d = %d, want 302", i, rw.Code)
		}
	}
	rw := do(h, httptest.NewRequest("GET", "/lim", nil))
	if rw.Code != http.StatusGone {
		t.Fatalf("click 3 = %d, want 410 Gone", rw.Code)
	}
	if rw.Header().Get("Location") != "" {
		t.Fatal("exhausted link still sent a Location header")
	}
	if !strings.Contains(rw.Body.String(), "click limit") {
		t.Fatalf("410 page does not explain why: %s", rw.Body.String())
	}
}

func TestDisabledAndExpiredReturn410(t *testing.T) {
	s, h, store := newTestServer(t)
	now := time.Now()
	store.Create(CreateParams{Slug: "off", Target: "https://example.com"}, now)
	store.SetDisabled("off", true)
	if w := do(h, httptest.NewRequest("GET", "/off", nil)); w.Code != http.StatusGone {
		t.Fatalf("disabled link = %d, want 410", w.Code)
	}

	exp := now.Add(time.Hour)
	store.Create(CreateParams{Slug: "old", Target: "https://example.com", ExpiresAt: &exp}, now)
	s.now = func() time.Time { return now.Add(2 * time.Hour) }
	if w := do(h, httptest.NewRequest("GET", "/old", nil)); w.Code != http.StatusGone {
		t.Fatalf("expired link = %d, want 410", w.Code)
	}
}

func TestUnknownSlugIs404(t *testing.T) {
	_, h, _ := newTestServer(t)
	w := do(h, httptest.NewRequest("GET", "/never-existed", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown slug = %d, want 404", w.Code)
	}
}

func TestBotClicksShowSeparatelyOnTheStatsPage(t *testing.T) {
	_, h, _ := newTestServer(t)
	do(h, bearer("POST", "/api/links", `{"target":"https://example.com/s","slug":"seen"}`))

	human := httptest.NewRequest("GET", "/seen", nil)
	human.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Chrome/120")
	human.Header.Set("Referer", "https://news.example/thread")
	do(h, human)

	bot := httptest.NewRequest("GET", "/seen", nil)
	bot.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1)")
	do(h, bot)

	w := do(h, bearer("GET", "/api/links/seen", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("stats = %d", w.Code)
	}
	var v apiLink
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.Clicks != 2 || v.HumanClicks != 1 || v.BotClicks != 1 {
		t.Fatalf("clicks=%d human=%d bot=%d, want 2/1/1", v.Clicks, v.HumanClicks, v.BotClicks)
	}
	if v.Referrers["https://news.example"] != 1 {
		t.Fatalf("referrers = %v", v.Referrers)
	}

	// The HTML stats page renders and mentions both numbers.
	page := do(h, bearer("GET", "/admin/link/seen", ""))
	if page.Code != http.StatusOK {
		t.Fatalf("stats page = %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{"Bots", "Unique-ish", "Clicks Per Day", "Referrers", "<svg", "news.example"} {
		if !strings.Contains(body, want) {
			t.Errorf("stats page is missing %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("stats page has an inline script, which the CSP would block")
	}
}

func TestLoginFlowAndCSRF(t *testing.T) {
	_, h, _ := newTestServer(t)

	// Wrong token.
	w := do(h, httptest.NewRequest("POST", "/login", strings.NewReader("token=nope")))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", w.Code)
	}

	// Right token sets a hardened cookie.
	r := httptest.NewRequest("POST", "/login", strings.NewReader("token="+url.QueryEscape(testToken)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = do(h, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie is not hardened: %+v", c)
	}
	if strings.Contains(c.Value, testToken) {
		t.Fatal("the session cookie contains APP_TOKEN")
	}

	// The cookie gets you the table.
	idx := httptest.NewRequest("GET", "/", nil)
	idx.AddCookie(c)
	iw := do(h, idx)
	if iw.Code != http.StatusOK || !strings.Contains(iw.Body.String(), "Create Link") {
		t.Fatalf("authenticated index = %d, create form present = %v", iw.Code, strings.Contains(iw.Body.String(), "Create Link"))
	}

	csrf := extractCSRF(t, iw.Body.String())

	// A form POST without the CSRF field is refused.
	bad := httptest.NewRequest("POST", "/admin/create", strings.NewReader("target=https://example.com"))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bad.AddCookie(c)
	if bw := do(h, bad); bw.Code != http.StatusForbidden {
		t.Fatalf("form POST without csrf = %d, want 403", bw.Code)
	}

	// With it, the link is created.
	good := httptest.NewRequest("POST", "/admin/create",
		strings.NewReader("csrf="+url.QueryEscape(csrf)+"&target="+url.QueryEscape("https://example.com/ui")+"&slug=viaui"))
	good.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	good.AddCookie(c)
	gw := do(h, good)
	if gw.Code != http.StatusOK {
		t.Fatalf("form create = %d: %s", gw.Code, gw.Body.String())
	}
	if !strings.Contains(gw.Body.String(), "Created.") {
		t.Fatal("form create did not confirm")
	}
	if rw := do(h, httptest.NewRequest("GET", "/viaui", nil)); rw.Code != http.StatusFound {
		t.Fatalf("link created via the UI = %d, want 302", rw.Code)
	}

	// A tampered cookie is rejected.
	tampered := *c
	tampered.Value = strings.Replace(c.Value, ".", ".0", 1)
	tr := httptest.NewRequest("GET", "/api/links", nil)
	tr.AddCookie(&tampered)
	if tw := do(h, tr); tw.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie = %d, want 401", tw.Code)
	}
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatal("no csrf field in the rendered page")
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatal("malformed csrf field")
	}
	return rest[:j]
}

func TestRateLimitOnRepeatedBadTokens(t *testing.T) {
	_, h, _ := newTestServer(t)
	var last int
	for i := 0; i < maxFailures+2; i++ {
		r := httptest.NewRequest("GET", "/api/links", nil)
		r.Header.Set("Authorization", "Bearer wrong")
		last = do(h, r).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d bad tokens the status is %d, want 429", maxFailures+2, last)
	}
}

func TestAPIListAndDelete(t *testing.T) {
	_, h, _ := newTestServer(t)
	do(h, bearer("POST", "/api/links", `{"target":"https://example.com/a","slug":"aa"}`))
	do(h, bearer("POST", "/api/links", `{"target":"https://example.com/b","slug":"bb"}`))

	w := do(h, bearer("GET", "/api/links", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d", w.Code)
	}
	var list struct {
		Count int       `json:"count"`
		Links []apiLink `json:"links"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Count != 2 || len(list.Links) != 2 {
		t.Fatalf("list count = %d", list.Count)
	}

	if dw := do(h, bearer("DELETE", "/api/links/aa", "")); dw.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", dw.Code)
	}
	if rw := do(h, httptest.NewRequest("GET", "/aa", nil)); rw.Code != http.StatusNotFound {
		t.Fatalf("deleted slug = %d, want 404", rw.Code)
	}
	if dw := do(h, bearer("DELETE", "/api/links/aa", "")); dw.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", dw.Code)
	}
}

func TestExpiresInAndExpiresAt(t *testing.T) {
	_, h, _ := newTestServer(t)
	w := do(h, bearer("POST", "/api/links", `{"target":"https://example.com","slug":"dur","expires_in":"72h"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("expires_in = %d: %s", w.Code, w.Body.String())
	}
	var v apiLink
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.ExpiresAt == nil {
		t.Fatal("expires_in did not set an expiry")
	}

	w = do(h, bearer("POST", "/api/links", `{"target":"https://example.com","slug":"bad","expires_in":"soon"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad expires_in = %d, want 400", w.Code)
	}
	w = do(h, bearer("POST", "/api/links", `{"target":"https://example.com","slug":"bad2","expires_at":"tomorrow"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad expires_at = %d, want 400", w.Code)
	}
}

// The target is attacker-influenced text; it must land in the page escaped.
func TestTargetIsEscapedInHTML(t *testing.T) {
	_, h, store := newTestServer(t)
	store.Create(CreateParams{Slug: "xss", Target: `https://example.com/"><script>alert(1)</script>`}, time.Now())

	w := do(h, bearer("GET", "/admin/link/xss", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("stats page = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("the target was rendered unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, "%3Cscript%3E") {
		t.Fatalf("expected the target to appear escaped; body did not contain it in escaped form")
	}
}

func TestBodyCapIsEnforced(t *testing.T) {
	_, h, _ := newTestServer(t)
	huge := `{"target":"https://example.com/` + strings.Repeat("a", maxBodyBytes+1024) + `"}`
	w := do(h, bearer("POST", "/api/links", huge))
	if w.Code == http.StatusCreated {
		t.Fatal("an oversized body was accepted")
	}
}

func TestClientIPTrustsOnlyTheLastForwardedHop(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	if got := clientIP(r); got != "10.0.0.1" {
		t.Fatalf("clientIP without XFF = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(r); got != "1.2.3.4" {
		t.Fatalf("clientIP with one XFF hop = %q", got)
	}
	// A client-supplied prefix must not win over Traefik's appended value.
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 203.0.113.5")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP with a spoofed prefix = %q, want the last hop", got)
	}
}

func TestMaskIP(t *testing.T) {
	tests := []struct{ in, want string }{
		{"203.0.113.42", "203.0.113.x"},
		{"10.0.0.1", "10.0.0.x"},
		{"garbage", "?"},
	}
	for _, tc := range tests {
		if got := maskIP(tc.in); got != tc.want {
			t.Errorf("maskIP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := maskIP("2001:db8::1"); !strings.HasPrefix(got, "2001:0db8") {
		t.Errorf("maskIP(ipv6) = %q", got)
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	_, h, _ := newTestServer(t)
	for _, path := range []string{"/static/style.css", "/static/app.js"} {
		w := do(h, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		b, _ := io.ReadAll(w.Body)
		if len(b) == 0 {
			t.Fatalf("GET %s served an empty file", path)
		}
	}
}

func TestStaticServesNoDirectoryListing(t *testing.T) {
	_, h, _ := newTestServer(t)
	for _, path := range []string{"/static/", "/static"} {
		w := do(h, httptest.NewRequest("GET", path, nil))
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "style.css") {
			t.Fatalf("GET %s served a directory listing", path)
		}
	}
}
