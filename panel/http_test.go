package main

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testToken = "test-token-please-ignore"

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("Kitchen Panel", sampleConfig().Rows, "study panel"); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		auth:    NewAuth(testToken),
		store:   store,
		cache:   newRenderCache(),
		loc:     time.UTC,
		baseURL: "https://panel.baileys.dev",
		now:     func() time.Time { return fixedNow },
	}
	return s, logRequests(s.Routes())
}

func do(h http.Handler, method, target string, headers map[string]string, body io.Reader) *http.Response {
	r := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Result()
}

func TestHealthz(t *testing.T) {
	_, h := newTestServer(t)
	res := do(h, http.MethodGet, "/healthz", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "ok" {
		t.Fatalf("body = %q", b)
	}
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	_, h := newTestServer(t)
	for _, path := range []string{"/healthz", "/login", "/", "/screen.png"} {
		res := do(h, http.MethodGet, path, nil, nil)
		if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: nosniff = %q", path, got)
		}
		if got := res.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: referrer policy = %q", path, got)
		}
		if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("%s: csp = %q", path, csp)
		}
	}
}

func TestScreenRequiresDeviceKey(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey

	res := do(h, http.MethodGet, "/screen.png", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /screen.png = %d, want 401", res.StatusCode)
	}

	res = do(h, http.MethodGet, "/screen.png?k=wrong", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d, want 401", res.StatusCode)
	}

	res = do(h, http.MethodGet, "/screen.png?k="+key, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("correct key = %d, want 200", res.StatusCode)
	}

	res = do(h, http.MethodGet, "/screen.png", map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("app token = %d, want 200", res.StatusCode)
	}
}

func TestScreenPNGResponse(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey
	res := do(h, http.MethodGet, "/screen.png?w=400&h=300&k="+key, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if got, _ := strconv.Atoi(res.Header.Get("Content-Length")); got != len(body) {
		t.Fatalf("Content-Length %s != body %d", res.Header.Get("Content-Length"), len(body))
	}
	c := chunks(t, body)
	if c["IHDR"][8] != 1 {
		t.Fatalf("served PNG bit depth = %d, want 1", c["IHDR"][8])
	}
	if c["IHDR"][9] != 3 {
		t.Fatalf("served PNG colour type = %d, want 3 (paletted)", c["IHDR"][9])
	}
	// Scan what actually came down the wire, not just what the renderer made.
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a != 0xffff || !((r == 0 && g == 0 && bl == 0) || (r == 0xffff && g == 0xffff && bl == 0xffff)) {
				t.Fatalf("served pixel (%d,%d) is not pure black or white: %d %d %d %d", x, y, r, g, bl, a)
			}
		}
	}
	if res.Header.Get("ETag") == "" {
		t.Fatal("no ETag")
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("Cache-Control = %q; it must not stop revalidation", cc)
	}
}

func TestScreenBinLengthAndPacking(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey
	for _, p := range presets {
		q := url.Values{"w": {strconv.Itoa(p.W)}, "h": {strconv.Itoa(p.H)}, "k": {key}}
		res := do(h, http.MethodGet, "/screen.bin?"+q.Encode(), nil, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d", p.Name, res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		want := ((p.W + 7) / 8) * p.H
		if len(body) != want {
			t.Fatalf("%s: %d bytes, want ceil(%d/8)*%d = %d", p.Name, len(body), p.W, p.H, want)
		}
		if cl := res.Header.Get("Content-Length"); cl != strconv.Itoa(want) {
			t.Fatalf("%s: Content-Length = %q, want %d", p.Name, cl, want)
		}
		if ct := res.Header.Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("%s: content type = %q", p.Name, ct)
		}
		if got := res.Header.Get("X-Panel-Stride"); got != strconv.Itoa((p.W+7)/8) {
			t.Fatalf("%s: stride header = %q", p.Name, got)
		}
		if got := res.Header.Get("X-Panel-Format"); got != "MONO_HLSB" {
			t.Fatalf("%s: format header = %q", p.Name, got)
		}
	}
}

// A battery panel polling every minute must get 304, not the same bytes again.
func TestNotModified(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey
	for _, path := range []string{"/screen.png", "/screen.bin"} {
		res := do(h, http.MethodGet, path+"?k="+key, nil, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: first fetch = %d", path, res.StatusCode)
		}
		etag := res.Header.Get("ETag")
		first, _ := io.ReadAll(res.Body)

		res = do(h, http.MethodGet, path+"?k="+key, map[string]string{"If-None-Match": etag}, nil)
		if res.StatusCode != http.StatusNotModified {
			t.Fatalf("%s: revalidation = %d, want 304", path, res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if len(body) != 0 {
			t.Fatalf("%s: 304 carried a %d byte body", path, len(body))
		}
		if res.Header.Get("ETag") != etag {
			t.Fatalf("%s: 304 changed the ETag", path)
		}

		// A stale validator still gets the bytes.
		res = do(h, http.MethodGet, path+"?k="+key, map[string]string{"If-None-Match": `"stale"`}, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: stale validator = %d, want 200", path, res.StatusCode)
		}
		again, _ := io.ReadAll(res.Body)
		if !bytes.Equal(first, again) {
			t.Fatalf("%s: two fetches in the same minute differ", path)
		}
	}
}

// Content changing must break the ETag, or the panel would never update.
func TestETagChangesWithContent(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey
	res := do(h, http.MethodGet, "/screen.png?k="+key, nil, nil)
	etag := res.Header.Get("ETag")

	if err := s.store.Save("Kitchen Panel", []Row{{"Bin Night", "Wednesday"}}, "study panel"); err != nil {
		t.Fatal(err)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+key, map[string]string{"If-None-Match": etag}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("after an edit the device still got %d", res.StatusCode)
	}
	if res.Header.Get("ETag") == etag {
		t.Fatal("ETag did not change with the content")
	}
}

func TestBadParamsAreExplained(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.Get().DeviceKey
	tests := map[string]string{
		"w=99999":       "between 64 and 2400",
		"w=2400&h=2400": "the cap is",
		"rotate=45":     "0, 90, 180 or 270",
		"preset=huge":   "unknown preset",
	}
	for q, want := range tests {
		res := do(h, http.MethodGet, "/screen.png?k="+key+"&"+q, nil, nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", q, res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), want) {
			t.Fatalf("%s: message %q does not explain the problem", q, bytes.TrimSpace(body))
		}
	}
}

func TestPreviewIsTokenGated(t *testing.T) {
	_, h := newTestServer(t)

	// A browser gets bounced to the login form.
	res := do(h, http.MethodGet, "/", map[string]string{"Accept": "text/html"}, nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous browser = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("redirected to %q", loc)
	}

	// Anything else gets a flat 401.
	res = do(h, http.MethodGet, "/", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous api client = %d, want 401", res.StatusCode)
	}

	res = do(h, http.MethodGet, "/", map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authorised = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	// The Copy button is injected by /static/app.js; the snippet itself is
	// plain selectable text, so the page works with JavaScript off.
	for _, want := range []string{"framebuf.MONO_HLSB", "/screen.bin", "/static/app.js", "b-glyph", "If-None-Match"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("preview page is missing %q", want)
		}
	}
}

func TestEditRoundTripOverHTTP(t *testing.T) {
	s, h := newTestServer(t)
	form := url.Values{
		"title":  {"Hallway"},
		"label":  {"Bin Night", "", "Battery"},
		"value":  {"Tuesday", "", "3.94 V"},
		"footer": {"kitchen"},
	}
	res := do(h, http.MethodPost, "/edit",
		map[string]string{
			"Authorization": "Bearer " + testToken,
			"Content-Type":  "application/x-www-form-urlencoded",
		},
		strings.NewReader(form.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save = %d, want 303", res.StatusCode)
	}
	got := s.store.Get()
	if got.Title != "Hallway" || got.Footer != "kitchen" || len(got.Rows) != 2 {
		t.Fatalf("save did not land: %+v", got)
	}

	// Unauthenticated saves are refused and change nothing.
	res = do(h, http.MethodPost, "/edit",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		strings.NewReader("title=hacked"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous save = %d, want 401", res.StatusCode)
	}
	if s.store.Get().Title != "Hallway" {
		t.Fatal("anonymous save modified the config")
	}
}

func TestLoginFlow(t *testing.T) {
	_, h := newTestServer(t)

	res := do(h, http.MethodPost, "/login",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		strings.NewReader("token=wrong"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", res.StatusCode)
	}

	res = do(h, http.MethodPost, "/login",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		strings.NewReader("token="+url.QueryEscape(testToken)+"&next=%2Fedit"))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("correct token = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/edit" {
		t.Fatalf("redirected to %q", loc)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie {
		t.Fatalf("no session cookie: %+v", cookies)
	}

	// And the cookie actually opens the door.
	r := httptest.NewRequest(http.MethodGet, "/edit", nil)
	r.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie session = %d, want 200", rec.Code)
	}
}

func TestBruteForceIsRateLimited(t *testing.T) {
	_, h := newTestServer(t)
	saw429 := false
	for i := 0; i < 15; i++ {
		res := do(h, http.MethodPost, "/login",
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			strings.NewReader("token=guess"+strconv.Itoa(i)))
		if res.StatusCode == http.StatusTooManyRequests {
			saw429 = true
			break
		}
	}
	if !saw429 {
		t.Fatal("15 wrong tokens never produced a 429")
	}
}

func TestRotateKeyKillsOldURLs(t *testing.T) {
	s, h := newTestServer(t)
	old := s.store.Get().DeviceKey
	res := do(h, http.MethodPost, "/key/rotate", map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("rotate = %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+old, nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old key still works: %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+s.store.Get().DeviceKey, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new key = %d", res.StatusCode)
	}
}

func TestMicropythonSnippetIsExact(t *testing.T) {
	p := Params{W: 800, H: 480}
	snip := micropython("https://panel.baileys.dev/", p, "abc123")
	for _, want := range []string{
		`URL    = "https://panel.baileys.dev/screen.bin?h=480&w=800&k=abc123"`,
		"W, H   = 800, 480",
		"STRIDE = 100",
		"BUF    = bytearray(STRIDE * H)   # 48000 bytes",
		"framebuf.MONO_HLSB",
		"If-None-Match",
		"r.status_code == 304",
		"r.raw.readinto(mv[n:])",
	} {
		if !strings.Contains(snip, want) {
			t.Errorf("snippet missing %q\n---\n%s", want, snip)
		}
	}
	if got := strings.Count(snip, "48000"); got < 2 {
		t.Errorf("byte count not stated clearly (%d mentions)", got)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	_, h := newTestServer(t)
	for _, path := range []string{"/static/style.css", "/static/app.js"} {
		res := do(h, http.MethodGet, path, nil, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", path, res.StatusCode)
		}
	}
}
