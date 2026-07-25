package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T) *server {
	t.Helper()
	s, err := newServer(config{
		Addr:    ":0",
		DataDir: t.TempDir(),
		BaseURL: "http://catch.test",
		Token:   testToken,
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	return s
}

func newTestBin(t *testing.T, s *server, label string, status int) *Bin {
	t.Helper()
	b, err := s.store.Create(label, status, time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return b
}

func do(s *server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func authGet(t *testing.T, s *server, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	return do(s, r)
}

func TestHealthz(t *testing.T) {
	s := newTestApp(t)
	w := do(s, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("healthz is missing the nosniff header")
	}
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "hdrs", 200)
	paths := []string{"/", "/login", "/healthz", "/b/" + bin.ID, "/bin/" + bin.ID}
	for _, p := range paths {
		w := authGet(t, s, p)
		h := w.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: nosniff missing", p)
		}
		if h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: referrer policy missing", p)
		}
		csp := h.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: csp = %q", p, csp)
		}
	}
}

func TestIndexRequiresToken(t *testing.T) {
	s := newTestApp(t)

	// No credential, browser: bounced to the login form.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "text/html")
	w := do(s, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("browser without a cookie: %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("redirected to %q", loc)
	}

	// No credential, script: 401.
	w = do(s, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("script without a token: %d, want 401", w.Code)
	}

	// Wrong bearer token: 401 and no listing.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	w = do(s, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "Requests Held") {
		t.Fatal("wrong token still saw the index")
	}

	// Right bearer token: the listing.
	w = authGet(t, s, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("right token: %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Requests Held") {
		t.Fatal("index did not render")
	}
}

func TestEveryPrivilegedRouteRefusesAnonymous(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "gated", 200)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodPost, "/bins"},
		{http.MethodGet, "/bin/" + bin.ID},
		{http.MethodGet, "/bin/" + bin.ID + "/rows"},
		{http.MethodGet, "/bin/" + bin.ID + "/r/xyz/raw"},
		{http.MethodPost, "/bin/" + bin.ID + "/settings"},
		{http.MethodPost, "/bin/" + bin.ID + "/clear"},
		{http.MethodPost, "/bin/" + bin.ID + "/delete"},
	}
	for _, c := range cases {
		w := do(s, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", c.method, c.path, w.Code)
		}
	}
	// And the bin survived every one of those.
	if _, ok := s.store.Get(bin.ID); !ok {
		t.Fatal("an unauthenticated request deleted the bin")
	}
}

func TestBruteForceIsRateLimited(t *testing.T) {
	s := newTestApp(t)
	var last int
	for i := 0; i < maxFailures+2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer guess-"+string(rune('a'+i)))
		r.RemoteAddr = "198.51.100.44:1234"
		last = do(s, r).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last guess returned %d, want 429", last)
	}
	// Another address is unaffected.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.RemoteAddr = "198.51.100.45:1234"
	if code := do(s, r).Code; code != http.StatusOK {
		t.Fatalf("clean IP got %d, want 200", code)
	}
}

func TestLoginCookieFlow(t *testing.T) {
	s := newTestApp(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Wrong token: 401, no cookie.
	res, err := client.PostForm(ts.URL+"/login", url.Values{"token": {"wrong"}})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token login = %d, want 401", res.StatusCode)
	}
	base, _ := url.Parse(ts.URL)
	if len(jar.Cookies(base)) != 0 {
		t.Fatal("a failed login set a cookie")
	}

	// Right token: cookie set, index reachable.
	res, err = client.PostForm(ts.URL+"/login", url.Values{"token": {testToken}})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login landed on %d", res.StatusCode)
	}
	if !strings.Contains(string(body), "Requests Held") {
		t.Fatal("login did not land on the index")
	}
	cookies := jar.Cookies(base)
	if len(cookies) != 1 || cookies[0].Name != sessionCookie {
		t.Fatalf("cookies = %+v", cookies)
	}
	if strings.Contains(cookies[0].Value, testToken) {
		t.Fatal("the session cookie contains the token")
	}

	// A tampered cookie is refused.
	bad := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookies[0].Value + "0"})
	res, err = bad.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered cookie = %d, want 401", res.StatusCode)
	}
}

func TestCaptureAcceptsEveryMethod(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "methods", 200)
	ts := httptest.NewServer(s)
	defer ts.Close()
	client := &http.Client{}

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "FROB"}
	for _, m := range methods {
		req, err := http.NewRequest(m, ts.URL+"/b/"+bin.ID+"/hook/x", strings.NewReader("payload"))
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", m, res.StatusCode)
		}
		if m == "HEAD" {
			if len(body) != 0 {
				t.Errorf("HEAD returned a body: %q", body)
			}
		} else if !strings.HasPrefix(string(body), "caught ") {
			t.Errorf("%s ack = %q", m, body)
		}
	}

	got, _ := s.store.Get(bin.ID)
	if len(got.Requests) != len(methods) {
		t.Fatalf("captured %d requests, want %d", len(got.Requests), len(methods))
	}
	seen := map[string]bool{}
	for _, r := range got.Requests {
		seen[r.Method] = true
		if r.SubPath != "/hook/x" {
			t.Errorf("%s sub-path = %q, want /hook/x", r.Method, r.SubPath)
		}
	}
	for _, m := range methods {
		if !seen[m] {
			t.Errorf("method %s was not recorded", m)
		}
	}
}

func TestCaptureRecordsTheDetails(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "detail", 200)
	body := `{"bssid":"aa:bb:cc:dd:ee:ff"}`
	r := httptest.NewRequest(http.MethodPost, "/b/"+bin.ID+"/deep/path/here?x=1&y=two", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	r.Header.Set("User-Agent", "bssid-reporter/1.0")
	r.Host = "catch.baileys.dev"
	if code := do(s, r).Code; code != http.StatusOK {
		t.Fatalf("capture = %d, want 200", code)
	}

	got, _ := s.store.Get(bin.ID)
	if len(got.Requests) != 1 {
		t.Fatalf("captured %d requests", len(got.Requests))
	}
	req := got.Requests[0]
	checks := map[string][2]string{
		"method":       {req.Method, "POST"},
		"path":         {req.Path, "/b/" + bin.ID + "/deep/path/here"},
		"sub-path":     {req.SubPath, "/deep/path/here"},
		"query":        {req.Query, "x=1&y=two"},
		"remote ip":    {req.RemoteIP, "203.0.113.9"},
		"content type": {req.ContentType, "application/json"},
		"body":         {string(req.Body), body},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
	if req.BodySize != int64(len(body)) {
		t.Errorf("body size = %d, want %d", req.BodySize, len(body))
	}
	if req.Truncated {
		t.Error("small body reported as truncated")
	}
	if got.LastSeen.IsZero() {
		t.Error("LastSeen was not updated")
	}
	var host, ua bool
	for _, h := range req.Headers {
		if h.Name == "Host" && h.Values[0] == "catch.baileys.dev" {
			host = true
		}
		if h.Name == "User-Agent" && h.Values[0] == "bssid-reporter/1.0" {
			ua = true
		}
	}
	if !host || !ua {
		t.Errorf("headers = %+v", req.Headers)
	}
}

func TestCaptureUsesTheBinStatus(t *testing.T) {
	s := newTestApp(t)
	for _, status := range []int{200, 202, 204, 418, 500} {
		bin := newTestBin(t, s, "status", status)
		r := httptest.NewRequest(http.MethodPost, "/b/"+bin.ID, strings.NewReader("x"))
		w := do(s, r)
		if w.Code != status {
			t.Errorf("bin set to %d replied %d", status, w.Code)
		}
		if status == 204 && w.Body.Len() != 0 {
			t.Errorf("204 bin returned a body: %q", w.Body.String())
		}
		if status != 204 && !strings.HasPrefix(w.Body.String(), "caught ") {
			t.Errorf("%d bin ack = %q", status, w.Body.String())
		}
	}
}

func TestCaptureTruncatesLargeBodies(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "big", 200)
	payload := bytes.Repeat([]byte("A"), bodyStoreCap+5000)
	r := httptest.NewRequest(http.MethodPut, "/b/"+bin.ID, bytes.NewReader(payload))
	if code := do(s, r).Code; code != http.StatusOK {
		t.Fatalf("capture = %d", code)
	}
	got, _ := s.store.Get(bin.ID)
	req := got.Requests[0]
	if len(req.Body) != bodyStoreCap {
		t.Fatalf("stored %d bytes, want %d", len(req.Body), bodyStoreCap)
	}
	if !req.Truncated {
		t.Fatal("oversized body not flagged as truncated")
	}
	if req.BodySize != int64(len(payload)) {
		t.Fatalf("body size = %d, want %d", req.BodySize, len(payload))
	}
	w := authGet(t, s, "/bin/"+bin.ID)
	if !strings.Contains(w.Body.String(), truncatedNote) {
		t.Fatalf("bin page does not mention truncation")
	}
}

func TestCaptureUnknownBinIs404(t *testing.T) {
	s := newTestApp(t)
	for _, p := range []string{"/b/" + strings.Repeat("a", binIDLen), "/b/short", "/b/", "/b/x/y"} {
		w := do(s, httptest.NewRequest(http.MethodPost, p, strings.NewReader("x")))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, w.Code)
		}
	}
}

// TestCapturedContentIsEscaped is the one that matters: a captured body is
// attacker-controlled and this host shares a cookie domain with the rest of
// *.baileys.dev.
func TestCapturedContentIsEscaped(t *testing.T) {
	const payload = `<script>alert(1)</script>`
	s := newTestApp(t)
	bin := newTestBin(t, s, `<script>alert("label")</script>`, 200)

	r := httptest.NewRequest(http.MethodPost, "/b/"+bin.ID+"/x?q="+url.QueryEscape(payload), strings.NewReader(payload))
	r.Header.Set("Content-Type", "text/html")
	r.Header.Set("X-Evil", `<img src=x onerror=alert(1)>`)
	if code := do(s, r).Code; code != http.StatusOK {
		t.Fatalf("capture = %d", code)
	}

	for _, path := range []string{"/bin/" + bin.ID, "/bin/" + bin.ID + "/rows", "/"} {
		w := authGet(t, s, path)
		if w.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, w.Code)
		}
		page := w.Body.String()
		if strings.Contains(page, payload) {
			t.Fatalf("%s: the raw script tag survived into the HTML", path)
		}
		if strings.Contains(page, "<img src=x onerror=") {
			t.Fatalf("%s: a raw img tag from a header survived into the HTML", path)
		}
		if path != "/" && !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("%s: escaped body is missing; page did not render it", path)
		}
		if path == "/" && !strings.Contains(page, "&lt;script&gt;") {
			t.Fatalf("index: escaped label is missing")
		}
	}

	// And the raw body is served inert, byte for byte.
	got, _ := s.store.Get(bin.ID)
	rid := got.Requests[0].ID
	w := authGet(t, s, "/bin/"+bin.ID+"/r/"+rid+"/raw")
	if w.Code != http.StatusOK {
		t.Fatalf("raw = %d", w.Code)
	}
	if w.Body.String() != payload {
		t.Fatalf("raw body = %q, want %q", w.Body.String(), payload)
	}
	h := w.Header()
	if ct := h.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("raw content type = %q", ct)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("raw body is missing nosniff")
	}
	if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("raw content disposition = %q", cd)
	}
}

func TestJSONBodyIsPrettyPrintedInThePage(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "json", 200)
	r := httptest.NewRequest(http.MethodPost, "/b/"+bin.ID, strings.NewReader(`{"a":1,"b":[2,3]}`))
	r.Header.Set("Content-Type", "application/json")
	do(s, r)

	bad := httptest.NewRequest(http.MethodPost, "/b/"+bin.ID, strings.NewReader(`{"a":`))
	bad.Header.Set("Content-Type", "application/json")
	do(s, bad)

	page := authGet(t, s, "/bin/"+bin.ID).Body.String()
	if !strings.Contains(page, "&#34;a&#34;: 1") {
		t.Fatal("JSON body was not re-indented in the page")
	}
	if !strings.Contains(page, "Body is not valid JSON.") {
		t.Fatal("broken JSON did not get its note")
	}
}

func TestBinLifecycleOverHTTP(t *testing.T) {
	s := newTestApp(t)

	// Create.
	form := url.Values{"label": {"messenger"}, "status": {"202"}}
	r := httptest.NewRequest(http.MethodPost, "/bins", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := do(s, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create = %d, want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	id := strings.TrimPrefix(loc, "/bin/")
	if !validID(id) {
		t.Fatalf("created bin id %q is not well formed", id)
	}
	bin, ok := s.store.Get(id)
	if !ok || bin.Label != "messenger" || bin.Status != 202 {
		t.Fatalf("bin = %+v", bin)
	}

	// Capture something, then clear it.
	if code := do(s, httptest.NewRequest(http.MethodPost, "/b/"+id, strings.NewReader("x"))).Code; code != 202 {
		t.Fatalf("capture = %d, want 202", code)
	}
	r = httptest.NewRequest(http.MethodPost, "/bin/"+id+"/clear", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	if code := do(s, r).Code; code != http.StatusSeeOther {
		t.Fatalf("clear = %d", code)
	}
	bin, _ = s.store.Get(id)
	if len(bin.Requests) != 0 {
		t.Fatalf("clear left %d requests", len(bin.Requests))
	}

	// Settings.
	form = url.Values{"label": {"messenger-2"}, "status": {"204"}}
	r = httptest.NewRequest(http.MethodPost, "/bin/"+id+"/settings", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Authorization", "Bearer "+testToken)
	if code := do(s, r).Code; code != http.StatusSeeOther {
		t.Fatalf("settings = %d", code)
	}
	bin, _ = s.store.Get(id)
	if bin.Label != "messenger-2" || bin.Status != 204 {
		t.Fatalf("bin after settings = %+v", bin)
	}

	// Delete.
	r = httptest.NewRequest(http.MethodPost, "/bin/"+id+"/delete", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	if code := do(s, r).Code; code != http.StatusSeeOther {
		t.Fatalf("delete = %d", code)
	}
	if _, ok := s.store.Get(id); ok {
		t.Fatal("bin survived delete")
	}
	if code := do(s, httptest.NewRequest(http.MethodPost, "/b/"+id, nil)).Code; code != http.StatusNotFound {
		t.Fatalf("capture into a deleted bin = %d, want 404", code)
	}
}

func TestCrossSiteFormPostRefused(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "csrf", 200)
	r := httptest.NewRequest(http.MethodPost, "/bin/"+bin.ID+"/delete", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://evil.example")
	if code := do(s, r).Code; code != http.StatusForbidden {
		t.Fatalf("cross-site delete = %d, want 403", code)
	}
	if _, ok := s.store.Get(bin.ID); !ok {
		t.Fatal("cross-site delete went through")
	}
}

func TestPageWorksWithoutJavaScript(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "nojs", 200)
	do(s, httptest.NewRequest(http.MethodPost, "/b/"+bin.ID+"/hook", strings.NewReader("hello")))
	page := authGet(t, s, "/bin/"+bin.ID).Body.String()
	// The request list is server-rendered into the page, not fetched.
	if !strings.Contains(page, "/hook") || !strings.Contains(page, "hello") {
		t.Fatal("the request list is not present in the server-rendered HTML")
	}
	// And the destructive actions are plain forms.
	if !strings.Contains(page, `action="/bin/`+bin.ID+`/delete"`) {
		t.Fatal("delete is not a plain form post")
	}
}

func TestRetentionAndExpiryAreStated(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "copy", 200)
	for _, path := range []string{"/", "/bin/" + bin.ID} {
		page := authGet(t, s, path).Body.String()
		if !strings.Contains(page, "100") || !strings.Contains(page, "30 days") {
			t.Errorf("%s does not state the caps", path)
		}
	}
}

func TestRowsFragmentIsGated(t *testing.T) {
	s := newTestApp(t)
	bin := newTestBin(t, s, "rows", 200)
	do(s, httptest.NewRequest(http.MethodPost, "/b/"+bin.ID, strings.NewReader("secret-payload")))

	w := do(s, httptest.NewRequest(http.MethodGet, "/bin/"+bin.ID+"/rows", nil))
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "secret-payload") {
		t.Fatalf("rows leaked without a token: %d", w.Code)
	}
	w = authGet(t, s, "/bin/"+bin.ID+"/rows")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "secret-payload") {
		t.Fatalf("rows with a token = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("rows content type = %q", ct)
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	s := newTestApp(t)
	for _, p := range []string{"/static/style.css", "/static/app.js"} {
		w := do(s, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("%s = %d (%d bytes)", p, w.Code, w.Body.Len())
		}
	}
}
