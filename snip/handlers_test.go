package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "pastes"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cfg := config{
		Addr:    ":0",
		DataDir: "unused",
		BaseURL: "https://snip.example",
		Token:   testToken,
	}
	srv, err := newServer(cfg, store)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	return srv
}

func do(t *testing.T, s *server, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func authed(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Bearer "+testToken)
	return r
}

// createPaste posts through the API and returns the slug.
func createPaste(t *testing.T, s *server, body, query string) string {
	t.Helper()
	r := authed(httptest.NewRequest(http.MethodPost, "/api/pastes?"+query, strings.NewReader(body)))
	w := do(t, s, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d, body %q", w.Code, w.Body.String())
	}
	url := strings.TrimSpace(w.Body.String())
	slug := url[strings.LastIndex(url, "/")+1:]
	if !validSlug(slug) {
		t.Fatalf("create returned %q, which does not end in a valid slug", url)
	}
	return slug
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t)
	w := do(t, s, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "ok" {
		t.Errorf("body %q, want %q", got, "ok")
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/healthz", "/", "/login", "/p/aaaaaaaaaaaaaaaaaaaa"} {
		w := do(t, s, httptest.NewRequest(http.MethodGet, path, nil))
		h := w.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", path)
		}
		if h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing Referrer-Policy", path)
		}
		if h.Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Errorf("%s: CSP = %q", path, h.Get("Content-Security-Policy"))
		}
	}
}

func TestCreateRequiresToken(t *testing.T) {
	s := newTestServer(t)

	// API without a token.
	r := httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader("hello"))
	r.RemoteAddr = "198.51.100.1:1000"
	if w := do(t, s, r); w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated API create = %d, want 401", w.Code)
	}

	// API with the wrong token.
	r = httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader("hello"))
	r.RemoteAddr = "198.51.100.2:1000"
	r.Header.Set("Authorization", "Bearer wrong")
	if w := do(t, s, r); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong-token API create = %d, want 401", w.Code)
	}

	// Browser form without a session.
	form := url.Values{"body": {"hello"}, "expiry": {"1d"}}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.3:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := do(t, s, r); w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated form create = %d, want 401", w.Code)
	}

	if n := s.store.Len(); n != 0 {
		t.Fatalf("%d pastes were created without a token", n)
	}

	// And with the token it works.
	if slug := createPaste(t, s, "hello", "expiry=1d"); slug == "" {
		t.Fatal("authenticated create produced no slug")
	}
}

func TestHomeAndIndexRequireToken(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/", "/pastes"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "198.51.100.20:1000"
		w := do(t, s, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated = %d, want 401", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "New Paste") {
			t.Errorf("GET %s leaked the authenticated page", path)
		}
		w = do(t, s, authed(httptest.NewRequest(http.MethodGet, path, nil)))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s authenticated = %d, want 200", path, w.Code)
		}
	}
}

func TestIndexListsAndDeletes(t *testing.T) {
	s := newTestServer(t)
	slug := createPaste(t, s, "listed content", "title=Listed&expiry=1d")

	w := do(t, s, authed(httptest.NewRequest(http.MethodGet, "/pastes", nil)))
	if w.Code != http.StatusOK {
		t.Fatalf("index status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, slug) || !strings.Contains(body, "Listed") {
		t.Error("index did not show the paste")
	}

	form := url.Values{"slug": {slug}}
	r := authed(httptest.NewRequest(http.MethodPost, "/pastes/delete", strings.NewReader(form.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := do(t, s, r); w.Code != http.StatusSeeOther {
		t.Fatalf("delete status %d, want 303", w.Code)
	}
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil)); w.Code != http.StatusNotFound {
		t.Errorf("after delete, GET /p = %d, want 404", w.Code)
	}

	// Deleting without a token must not work.
	r = httptest.NewRequest(http.MethodPost, "/pastes/delete", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.30:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := do(t, s, r); w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated delete = %d, want 401", w.Code)
	}
}

func TestReadIsPublic(t *testing.T) {
	s := newTestServer(t)
	slug := createPaste(t, s, "public body\nsecond line", "title=Open&expiry=1d")

	w := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /p unauthenticated = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"public body", "second line", "Open", "Expires in"} {
		if !strings.Contains(body, want) {
			t.Errorf("paste page missing %q", want)
		}
	}
	if !strings.Contains(body, `href="/raw/`+slug+`"`) {
		t.Error("paste page has no Raw link")
	}

	w = do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /raw unauthenticated = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "public body\nsecond line" {
		t.Errorf("raw body = %q", got)
	}
}

func TestRawIsNeverRenderable(t *testing.T) {
	s := newTestServer(t)
	const evil = "<script>alert(document.cookie)</script><h1>not a heading</h1>"
	slug := createPaste(t, s, evil, "expiry=1d")

	w := do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	h := w.Header()
	if got := h.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	if w.Body.String() != evil {
		t.Error("raw body was altered")
	}

	// The HTML view must escape it rather than execute it.
	w = do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil))
	page := w.Body.String()
	if strings.Contains(page, "<script>alert(") || strings.Contains(page, "<h1>not a heading</h1>") {
		t.Fatal("paste body was emitted as live HTML")
	}
	if !strings.Contains(page, "&lt;script&gt;alert(document.cookie)&lt;/script&gt;") {
		t.Error("paste body was not escaped as expected")
	}
}

func TestBurnAfterReadingOverHTTP(t *testing.T) {
	s := newTestServer(t)
	slug := createPaste(t, s, "one shot only", "expiry=never&burn=1")

	// A HEAD is not a read.
	if w := do(t, s, httptest.NewRequest(http.MethodHead, "/raw/"+slug, nil)); w.Code != http.StatusOK {
		t.Fatalf("HEAD before read = %d, want 200", w.Code)
	}

	w := do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("first raw read = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "one shot only" {
		t.Errorf("first read body = %q", got)
	}

	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil)); w.Code != http.StatusNotFound {
		t.Errorf("second raw read = %d, want 404", w.Code)
	}
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil)); w.Code != http.StatusNotFound {
		t.Errorf("html read after burn = %d, want 404", w.Code)
	}
	if w := do(t, s, httptest.NewRequest(http.MethodHead, "/raw/"+slug, nil)); w.Code != http.StatusNotFound {
		t.Errorf("HEAD after burn = %d, want 404", w.Code)
	}
}

func TestBurnPageHasNoRawLink(t *testing.T) {
	s := newTestServer(t)
	slug := createPaste(t, s, "gone in a flash", "burn=1&expiry=never")
	w := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	page := w.Body.String()
	if strings.Contains(page, "/raw/"+slug) {
		t.Error("a burned paste page offered a Raw link that cannot work")
	}
	if !strings.Contains(page, "burned after reading") {
		t.Error("page did not say the paste burned")
	}
}

func TestExpiredPasteIs404(t *testing.T) {
	s := newTestServer(t)
	base := time.Now()
	s.store.now = func() time.Time { return base }
	s.now = func() time.Time { return base }

	slug := createPaste(t, s, "ten minute paste", "expiry=10m")
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil)); w.Code != http.StatusOK {
		t.Fatalf("before expiry = %d, want 200", w.Code)
	}

	// No sweeper run: the read path alone must refuse.
	s.store.now = func() time.Time { return base.Add(11 * time.Minute) }
	s.now = s.store.now

	expired := do(t, s, httptest.NewRequest(http.MethodGet, "/p/"+slug, nil))
	if expired.Code != http.StatusNotFound {
		t.Fatalf("after expiry = %d, want 404", expired.Code)
	}
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil)); w.Code != http.StatusNotFound {
		t.Errorf("raw after expiry = %d, want 404", w.Code)
	}

	// The 404 for an expired paste is byte-identical to one that never existed.
	never := do(t, s, httptest.NewRequest(http.MethodGet, "/p/aaaaaaaaaaaaaaaaaaaa", nil))
	if never.Code != http.StatusNotFound {
		t.Fatalf("unknown slug = %d, want 404", never.Code)
	}
	if expired.Body.String() != never.Body.String() {
		t.Error("the expired 404 differs from the never-existed 404; that leaks existence")
	}
}

func TestOversizeBodyRejected(t *testing.T) {
	s := newTestServer(t)
	big := strings.Repeat("x", MaxPasteBytes+1)

	r := authed(httptest.NewRequest(http.MethodPost, "/api/pastes?expiry=1d", strings.NewReader(big)))
	w := do(t, s, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize API create = %d, want 413", w.Code)
	}
	if !strings.Contains(w.Body.String(), "too big") {
		t.Errorf("unhelpful message: %q", w.Body.String())
	}

	// Exactly at the cap is accepted.
	r = authed(httptest.NewRequest(http.MethodPost, "/api/pastes?expiry=1d", strings.NewReader(strings.Repeat("y", MaxPasteBytes))))
	if w := do(t, s, r); w.Code != http.StatusCreated {
		t.Fatalf("create at the cap = %d, want 201", w.Code)
	}

	// The browser form rejects it too.
	form := url.Values{"body": {big}, "expiry": {"1d"}}
	fr := authed(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode())))
	fr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := do(t, s, fr); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize form create = %d, want 413", w.Code)
	}
}

func TestAPIRejectsEmptyAndBadExpiry(t *testing.T) {
	s := newTestServer(t)
	r := authed(httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader("   \n ")))
	if w := do(t, s, r); w.Code != http.StatusBadRequest {
		t.Errorf("empty body = %d, want 400", w.Code)
	}
	r = authed(httptest.NewRequest(http.MethodPost, "/api/pastes?expiry=1y", strings.NewReader("hello")))
	w := do(t, s, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad expiry = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "10m") {
		t.Errorf("error should list the valid values, got %q", w.Body.String())
	}
}

func TestFormCreateRedirectsToDone(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{
		"body":   {"from the browser"},
		"title":  {"Browser Paste"},
		"expiry": {"1h"},
		"burn":   {"1"},
	}
	r := authed(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := do(t, s, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("form create = %d, want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/done/") {
		t.Fatalf("Location = %q", loc)
	}
	slug := strings.TrimPrefix(loc, "/done/")

	// The done page needs the token, and must not consume the burn paste.
	unauth := httptest.NewRequest(http.MethodGet, loc, nil)
	unauth.RemoteAddr = "198.51.100.40:1000"
	if w := do(t, s, unauth); w.Code != http.StatusUnauthorized {
		t.Errorf("done page unauthenticated = %d, want 401", w.Code)
	}
	w = do(t, s, authed(httptest.NewRequest(http.MethodGet, loc, nil)))
	if w.Code != http.StatusOK {
		t.Fatalf("done page = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "https://snip.example/p/"+slug) {
		t.Error("done page did not show the absolute paste URL")
	}
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil)); w.Code != http.StatusOK {
		t.Errorf("burn paste was consumed before anyone read it: %d", w.Code)
	}
}

func TestEmptyFormBodyRejected(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"body": {"   "}, "expiry": {"1d"}}
	r := authed(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := do(t, s, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty form body = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Nothing to paste.") {
		t.Error("missing the explanation")
	}
}

func TestLoginFlowIssuesUsableSession(t *testing.T) {
	s := newTestServer(t)

	form := url.Values{"token": {"wrong"}, "next": {"/pastes"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.50:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := do(t, s, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a failed login set a cookie")
	}

	form = url.Values{"token": {testToken}, "next": {"/pastes"}}
	r = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.51:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = do(t, s, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("good login = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/pastes" {
		t.Errorf("Location = %q, want /pastes", got)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies, want 1", len(cookies))
	}
	if strings.Contains(cookies[0].Value, testToken) {
		t.Fatal("the cookie carries the token")
	}

	r = httptest.NewRequest(http.MethodGet, "/pastes", nil)
	r.AddCookie(cookies[0])
	if w := do(t, s, r); w.Code != http.StatusOK {
		t.Fatalf("session request = %d, want 200", w.Code)
	}
}

func TestLoginRejectsOffsiteNext(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"token": {testToken}, "next": {"https://evil.example/steal"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.60:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := do(t, s, r)
	if got := w.Header().Get("Location"); got != "/" {
		t.Errorf("Location = %q, want / for an off-site next", got)
	}
	form = url.Values{"token": {testToken}, "next": {"//evil.example/steal"}}
	r = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.61:1000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = do(t, s, r)
	if got := w.Header().Get("Location"); got != "/" {
		t.Errorf("Location = %q, want / for a protocol-relative next", got)
	}
}

func TestUnknownPathIsTheSame404(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/nope", "/p/not-a-slug", "/raw/short"} {
		w := do(t, s, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/static/style.css", "/static/app.js"} {
		w := do(t, s, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		if w.Body.Len() == 0 {
			t.Errorf("%s is empty", path)
		}
	}
}

func TestDataSurvivesRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pastes")
	cfg := config{Addr: ":0", BaseURL: "https://snip.example", Token: testToken}

	store1, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := newServer(cfg, store1)
	if err != nil {
		t.Fatal(err)
	}
	slug := createPaste(t, s1, "survives a restart", "title=Durable&expiry=30d")

	store2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := newServer(cfg, store2)
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, s2, httptest.NewRequest(http.MethodGet, "/raw/"+slug, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("after restart = %d, want 200", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "survives a restart" {
		t.Errorf("body = %q", body)
	}
}
