package main

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"mime/multipart"
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
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	images, err := newImageStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		auth:    NewAuth(testToken),
		store:   store,
		images:  images,
		data:    newDataStore(true),
		cache:   newRenderCache(),
		loc:     time.UTC,
		baseURL: "https://panel.baileys.dev",
		now:     func() time.Time { return fixedNow },
		ctx:     context.Background(),
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

// authed is the header set a logged-in browser would send: the token plus the
// Sec-Fetch-Site a same-origin form post carries.
func authed(extra ...string) map[string]string {
	m := map[string]string{
		"Authorization":   "Bearer " + testToken,
		"Content-Type":    "application/x-www-form-urlencoded",
		"Sec-Fetch-Site":  "same-origin",
		"X-Test-Identity": "browser",
	}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i]] = extra[i+1]
	}
	return m
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

// The CSRF fix, pinned. Referrer-Policy must be same-origin, never no-referrer:
// under no-referrer Chrome sends "Origin: null" on same-origin form posts and
// any origin check falls over.
func TestSecurityHeadersEverywhere(t *testing.T) {
	_, h := newTestServer(t)
	for _, path := range []string{"/healthz", "/login", "/", "/screen.png"} {
		res := do(h, http.MethodGet, path, nil, nil)
		if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: nosniff = %q", path, got)
		}
		if got := res.Header.Get("Referrer-Policy"); got != "same-origin" {
			t.Errorf("%s: Referrer-Policy = %q, want same-origin", path, got)
		}
		csp := res.Header.Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'self'", "script-src 'self'", "form-action 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: csp %q missing %q", path, csp, want)
			}
		}
	}
}

// Sec-Fetch-Site is consulted first and is decisive.
func TestCSRFGate(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		allow   bool
	}{
		{"same-origin fetch metadata", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"direct navigation", map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"same-site sibling", map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		// Fetch metadata wins even when Origin disagrees: a cross-site page can
		// set neither, but it can get Origin to say whatever its own host is.
		{"cross-site claiming a good origin", map[string]string{
			"Sec-Fetch-Site": "cross-site", "Origin": "https://example.com"}, false},
		{"no metadata, good origin", map[string]string{"Origin": "http://example.com"}, true},
		{"no metadata, foreign origin", map[string]string{"Origin": "https://evil.test"}, false},
		{"no metadata, null origin", map[string]string{"Origin": "null", "Referer": "http://example.com/edit"}, true},
		{"no metadata, foreign referer", map[string]string{"Referer": "https://evil.test/x"}, false},
		{"nothing at all (curl)", map[string]string{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://example.com/edit", nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := sameSiteRequest(r); got != tc.allow {
				t.Fatalf("sameSiteRequest = %v, want %v", got, tc.allow)
			}
		})
	}

	// And it is actually wired to the POST routes.
	_, h := newTestServer(t)
	res := do(h, http.MethodPost, "/edit",
		map[string]string{
			"Authorization":  "Bearer " + testToken,
			"Content-Type":   "application/x-www-form-urlencoded",
			"Sec-Fetch-Site": "cross-site",
		},
		strings.NewReader("op=save"))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST = %d, want 403", res.StatusCode)
	}
}

func TestScreenRequiresDeviceKey(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.DefaultScreen().DeviceKey

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

// Per-screen endpoints, and the legacy paths still pointing at the first screen.
func TestPerScreenEndpoints(t *testing.T) {
	s, h := newTestServer(t)
	first := s.store.DefaultScreen()
	second, err := s.store.AddScreen("hallway", 400, 300)
	if err != nil {
		t.Fatal(err)
	}
	sc := second
	sc.Blocks = starterBlocks("clock-rows", sc)
	if err := s.store.SaveScreen("hallway", sc); err != nil {
		t.Fatal(err)
	}

	res := do(h, http.MethodGet, "/s/hallway/screen.png?k="+second.DeviceKey, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("named screen = %d", res.StatusCode)
	}
	if got := res.Header.Get("X-Panel-Screen"); got != "hallway" {
		t.Errorf("X-Panel-Screen = %q", got)
	}
	if got := res.Header.Get("X-Panel-Width"); got != "400" {
		t.Errorf("width header = %q, want 400", got)
	}

	// One screen's key does not open another's.
	res = do(h, http.MethodGet, "/s/hallway/screen.png?k="+first.DeviceKey, nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the wrong screen's key = %d, want 401", res.StatusCode)
	}

	// The legacy path is still the first screen.
	res = do(h, http.MethodGet, "/screen.png?k="+first.DeviceKey, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("legacy path = %d", res.StatusCode)
	}
	if got := res.Header.Get("X-Panel-Screen"); got != first.Name {
		t.Errorf("legacy path served %q", got)
	}

	res = do(h, http.MethodGet, "/s/nowhere/screen.png?k=x", nil, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown screen = %d, want 404", res.StatusCode)
	}
}

func TestScreenPNGResponse(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.DefaultScreen().DeviceKey
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
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a != 0xffff || !((r == 0 && g == 0 && bl == 0) || (r == 0xffff && g == 0xffff && bl == 0xffff)) {
				t.Fatalf("served pixel (%d,%d) is not pure black or white", x, y)
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
	key := s.store.DefaultScreen().DeviceKey
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

func TestNotModified(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.DefaultScreen().DeviceKey
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

// The ETag must move when the layout moves and sit still when it does not.
func TestETagTracksTheLayout(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.DefaultScreen().DeviceKey

	first := do(h, http.MethodGet, "/screen.png?k="+key, nil, nil).Header.Get("ETag")
	if first == "" {
		t.Fatal("no ETag")
	}
	// Unchanged layout, same minute: identical validator.
	for i := 0; i < 3; i++ {
		if got := do(h, http.MethodGet, "/screen.png?k="+key, nil, nil).Header.Get("ETag"); got != first {
			t.Fatalf("the ETag moved without the layout moving: %s != %s", got, first)
		}
	}

	// Move one block by a pixel.
	sc := s.store.DefaultScreen()
	sc.Blocks[1].X += 1
	if err := s.store.SaveScreen(sc.Name, sc); err != nil {
		t.Fatal(err)
	}
	res := do(h, http.MethodGet, "/screen.png?k="+key, map[string]string{"If-None-Match": first}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("after an edit the device still got %d", res.StatusCode)
	}
	moved := res.Header.Get("ETag")
	if moved == first {
		t.Fatal("the ETag did not change when a block moved")
	}

	// Change nothing but the block's text.
	sc = s.store.DefaultScreen()
	for i := range sc.Blocks {
		if sc.Blocks[i].Type == BlockText {
			sc.Blocks[i].Text = "something else"
			break
		}
	}
	if err := s.store.SaveScreen(sc.Name, sc); err != nil {
		t.Fatal(err)
	}
	if got := do(h, http.MethodGet, "/screen.png?k="+key, nil, nil).Header.Get("ETag"); got == moved {
		t.Fatal("the ETag did not change when the text changed")
	}
}

func TestBadParamsAreExplained(t *testing.T) {
	s, h := newTestServer(t)
	key := s.store.DefaultScreen().DeviceKey
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

	res := do(h, http.MethodGet, "/", map[string]string{"Accept": "text/html"}, nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous browser = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("redirected to %q", loc)
	}
	res = do(h, http.MethodGet, "/", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous api client = %d, want 401", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/", map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authorised = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{"framebuf.MONO_HLSB", "/screen.bin", "/static/app.js", "b-glyph", "If-None-Match"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("preview page is missing %q", want)
		}
	}
}

// The editor form works with JavaScript switched off: the numeric fields and
// the op buttons are the whole interface.
func TestEditorFormRoundTrip(t *testing.T) {
	s, h := newTestServer(t)
	name := s.store.DefaultScreen().Name

	form := url.Values{
		"op":            {"save"},
		"screen_name":   {name},
		"screen_w":      {"800"},
		"screen_h":      {"480"},
		"screen_rotate": {"0"},
		"b0.type":       {"text"},
		"b0.x":          {"24"}, "b0.y": {"16"}, "b0.w": {"400"}, "b0.h": {"48"},
		"b0.anchor": {"w"}, "b0.align": {"left"}, "b0.font": {"go-bold"}, "b0.size": {"32"},
		"b0.text": {"Hallway"},
		"b1.type": {"rows"},
		"b1.x":    {"24"}, "b1.y": {"96"}, "b1.w": {"600"}, "b1.h": {"200"},
		"b1.anchor": {"nw"}, "b1.align": {"left"}, "b1.font": {"pixel8x16"}, "b1.size": {"16"},
		"b1.rowlabel": {"Bin Night", "", "Battery"},
		"b1.rowvalue": {"Tuesday", "", "3.94 V"},
		"b1.rules":    {"1"},
	}
	res := do(h, http.MethodPost, "/edit?s="+name, authed(), strings.NewReader(form.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("save = %d, want 303\n%s", res.StatusCode, body)
	}
	got := s.store.DefaultScreen()
	if len(got.Blocks) != 2 {
		t.Fatalf("saved %d blocks: %+v", len(got.Blocks), got.Blocks)
	}
	if got.Blocks[0].Text != "Hallway" || got.Blocks[0].X != 24 || got.Blocks[0].W != 400 {
		t.Fatalf("text block wrong: %+v", got.Blocks[0])
	}
	if len(got.Blocks[1].Rows) != 2 || got.Blocks[1].Rows[1].Value != "3.94 V" {
		t.Fatalf("blank row not dropped / rows wrong: %+v", got.Blocks[1].Rows)
	}

	// Add, reorder, duplicate and delete. The browser resubmits every block on
	// every operation, so the form is rebuilt from the live screen each time —
	// which is also what proves an operation never eats the other blocks.
	post := func(op string, extra ...string) {
		t.Helper()
		f := formFor(s.store.DefaultScreen(), op)
		for i := 0; i+1 < len(extra); i += 2 {
			f.Set(extra[i], extra[i+1])
		}
		res := do(h, http.MethodPost, "/edit?s="+name, authed(), strings.NewReader(f.Encode()))
		if res.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("%s = %d\n%s", op, res.StatusCode, firstLines(string(body), 30))
		}
	}

	post("add", "addtype", "divider")
	if n := len(s.store.DefaultScreen().Blocks); n != 3 {
		t.Fatalf("after add: %d blocks", n)
	}
	post("up.1")
	if got := s.store.DefaultScreen(); got.Blocks[0].Type != BlockRows || len(got.Blocks) != 3 {
		t.Fatalf("move up did not reorder or lost blocks: %d %s", len(got.Blocks), got.Blocks[0].Type)
	}
	post("dup.0")
	if n := len(s.store.DefaultScreen().Blocks); n != 4 {
		t.Fatalf("after duplicate: %d blocks", n)
	}
	post("del.0")
	if n := len(s.store.DefaultScreen().Blocks); n != 3 {
		t.Fatalf("after delete: %d blocks", n)
	}
	// The two survivors of the original pair are still intact.
	found := 0
	for _, b := range s.store.DefaultScreen().Blocks {
		if b.Type == BlockText && b.Text == "Hallway" {
			found++
		}
		if b.Type == BlockRows && len(b.Rows) == 2 {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("an operation ate the other blocks: %+v", s.store.DefaultScreen().Blocks)
	}

	// Unauthenticated saves are refused and change nothing.
	before := len(s.store.DefaultScreen().Blocks)
	res = do(h, http.MethodPost, "/edit?s="+name,
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"},
		strings.NewReader("op=save"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous save = %d, want 401", res.StatusCode)
	}
	if n := len(s.store.DefaultScreen().Blocks); n != before {
		t.Fatal("an anonymous save modified the screen")
	}
}

// formFor rebuilds the whole editor form from a screen, the way the browser
// resubmits every field on every button press.
func formFor(sc Screen, op string) url.Values {
	f := url.Values{
		"op": {op}, "screen_name": {sc.Name},
		"screen_w": {itoa(sc.W)}, "screen_h": {itoa(sc.H)}, "screen_rotate": {itoa(sc.Rotate)},
	}
	if sc.Invert {
		f.Set("screen_invert", "1")
	}
	for i, b := range sc.Blocks {
		p := "b" + itoa(i) + "."
		f.Set(p+"type", b.Type)
		f.Set(p+"x", itoa(b.X))
		f.Set(p+"y", itoa(b.Y))
		f.Set(p+"w", itoa(b.W))
		f.Set(p+"h", itoa(b.H))
		f.Set(p+"anchor", b.Anchor)
		f.Set(p+"align", b.Align)
		f.Set(p+"font", b.Font)
		f.Set(p+"size", itoa(b.Size))
		f.Set(p+"text", b.Text)
		f.Set(p+"format", b.Format)
		f.Set(p+"orient", b.Orient)
		f.Set(p+"thickness", itoa(b.Thickness))
		f.Set(p+"label", b.Label)
		f.Set(p+"image", b.Image)
		f.Set(p+"dither", b.Dither)
		f.Set(p+"fit", b.Fit)
		f.Set(p+"url", b.URL)
		f.Set(p+"path", b.Path)
		f.Set(p+"fallback", b.Fallback)
		if b.Rules {
			f.Set(p+"rules", "1")
		}
		if b.Bullets {
			f.Set(p+"bullets", "1")
		}
		if b.Fill {
			f.Set(p+"fill", "1")
		}
		if b.Invert {
			f.Set(p+"invert", "1")
		}
		if b.Wrap {
			f.Set(p+"wrap", "1")
		}
		if b.Type == BlockProgress {
			f.Set(p+"value", strconv.Itoa(int(b.Value)))
		}
		if b.Type == BlockData {
			f.Set(p+"interval", itoa(b.Interval))
			f.Set(p+"timeout", itoa(b.Timeout))
		}
		for _, r := range b.Rows {
			f.Add(p+"rowlabel", r.Label)
			f.Add(p+"rowvalue", r.Value)
		}
		if len(b.Items) > 0 {
			f.Set(p+"items", strings.Join(b.Items, "\n"))
		}
	}
	return f
}

// A bad field comes back as a 400 with the offending block named, and the work
// still in the form.
func TestEditorRejectionKeepsTheWork(t *testing.T) {
	s, h := newTestServer(t)
	name := s.store.DefaultScreen().Name
	form := url.Values{
		"op": {"save"}, "screen_name": {name}, "screen_w": {"800"}, "screen_h": {"480"},
		"b0.type": {"text"}, "b0.x": {"700"}, "b0.y": {"0"}, "b0.w": {"400"}, "b0.h": {"40"},
		"b0.anchor": {"nw"}, "b0.align": {"left"}, "b0.font": {"go"}, "b0.size": {"20"},
		"b0.text": {"work in progress"},
	}
	res := do(h, http.MethodPost, "/edit?s="+name, authed(), strings.NewReader(form.Encode()))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("off-canvas block = %d, want 400", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "past the right edge") {
		t.Errorf("the page does not name the problem:\n%s", firstLines(string(body), 40))
	}
	if !strings.Contains(string(body), "work in progress") {
		t.Error("the rejected page lost the user's text")
	}
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}

func TestLayoutJSONEscapeHatch(t *testing.T) {
	s, h := newTestServer(t)
	name := s.store.DefaultScreen().Name

	// Read it back out.
	res := do(h, http.MethodGet, "/layout.json", map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /layout.json = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}

	good := `{"version":2,"screens":[{"name":"default","w":320,"h":240,"device_key":"",
	  "blocks":[{"type":"box","x":0,"y":0,"w":320,"h":40,"fill":true}],"updated_at":"2026-07-25T00:00:00Z"}],
	  "updated_at":"2026-07-25T00:00:00Z"}`
	res = do(h, http.MethodPost, "/layout", authed(),
		strings.NewReader(url.Values{"s": {name}, "json": {good}}.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("good paste = %d\n%s", res.StatusCode, firstLines(string(body), 40))
	}
	if got := s.store.DefaultScreen(); got.W != 320 || len(got.Blocks) != 1 {
		t.Fatalf("the paste did not land: %+v", got)
	}

	// Every malformed shape must name the problem.
	bad := map[string]string{
		`{"screens":[{"name":"default","w":320,"h":240,"blocks":[{"type":"sparkline","x":0,"y":0,"w":10,"h":10}]}]}`: `unknown block type "sparkline"`,
		`{"screens":[{"name":"default","w":320,"h":240,"blocks":[{"type":"box","x":0,"y":0,"w":-5,"h":10}]}]}`:       `block 1 (box): "w" must be at least 1 pixel, got -5`,
		`{"screens":[{"name":"default","w":320,"h":240,"blocks":[{"type":"box","x":300,"y":0,"w":100,"h":10}]}]}`:    "past the right edge",
		`{"screens":[{"name":"default","w":320,"h":240,"blocks":[{"type":"image","x":0,"y":0,"w":100,"h":10}]}]}`:    `"image" is required`,
		`{"screens":[]}`: "at least one screen is required",
		`{"screens":[{"name":"default","w":320,"h":240,"blocks":[{"type":"text","x":0,"y":0,"w":100,"h":30,"text":"x","font":"go","size":9}]}]}`: "thins out on e-ink",
		`{"screens":[{"name":"default"`: "a bracket or brace is unclosed",
		`{"screens": [ } ]}`:            "syntax error at byte",
		`{"screens":[{"name":"default","w":"wide","h":240,"blocks":[]}]}`: "wants a int",
		`{"screens":[{"name":"default","w":320,"h":240,"blox":[]}]}`:      "unknown field",
	}
	for body, want := range bad {
		res = do(h, http.MethodPost, "/layout", authed(),
			strings.NewReader(url.Values{"s": {name}, "json": {body}}.Encode()))
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%.50s: status = %d, want 400", body, res.StatusCode)
			continue
		}
		page, _ := io.ReadAll(res.Body)
		if !strings.Contains(unescape(string(page)), want) {
			t.Errorf("%.60s\n  did not produce %q", body, want)
		}
	}
	// None of that changed the stored layout.
	if got := s.store.DefaultScreen(); got.W != 320 || len(got.Blocks) != 1 {
		t.Fatalf("a rejected paste modified the state: %+v", got)
	}
}

// html/template escapes quotes in the flash message; compare against the text.
func unescape(s string) string {
	r := strings.NewReplacer("&#34;", `"`, "&quot;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">", "&#39;", "'", "&%23;", "#")
	return r.Replace(s)
}

func TestStarterLayoutsLoadInOneClick(t *testing.T) {
	s, h := newTestServer(t)
	name := s.store.DefaultScreen().Name
	for _, st := range starters {
		res := do(h, http.MethodPost, "/starter", authed(),
			strings.NewReader(url.Values{"s": {name}, "starter": {st.ID}}.Encode()))
		if res.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("%s = %d\n%s", st.ID, res.StatusCode, firstLines(string(body), 20))
		}
		sc := s.store.DefaultScreen()
		if len(sc.Blocks) == 0 {
			t.Fatalf("%s produced no blocks", st.ID)
		}
		// And it renders.
		res = do(h, http.MethodGet, "/screen.png?k="+sc.DeviceKey, nil, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: render = %d", st.ID, res.StatusCode)
		}
	}
	res := do(h, http.MethodPost, "/starter", authed(),
		strings.NewReader(url.Values{"s": {name}, "starter": {"nope"}}.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unknown starter = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("unknown starter produced no error: %q", loc)
	}
}

func TestScreensCRUDOverHTTP(t *testing.T) {
	s, h := newTestServer(t)
	res := do(h, http.MethodPost, "/screens", authed(),
		strings.NewReader(url.Values{"op": {"add"}, "name": {"Hallway"}, "w": {"400"}, "h": {"300"}}.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add screen = %d", res.StatusCode)
	}
	if _, ok := s.store.Screen("hallway"); !ok {
		t.Fatal("screen not created")
	}
	res = do(h, http.MethodPost, "/screens", authed(),
		strings.NewReader(url.Values{"op": {"duplicate"}, "s": {"hallway"}}.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("duplicate = %d", res.StatusCode)
	}
	if _, ok := s.store.Screen("hallway-copy"); !ok {
		t.Fatal("duplicate not created")
	}
	res = do(h, http.MethodPost, "/screens", authed(),
		strings.NewReader(url.Values{"op": {"delete"}, "s": {"hallway-copy"}}.Encode()))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	if _, ok := s.store.Screen("hallway-copy"); ok {
		t.Fatal("delete did not take")
	}
}

func TestImageUploadOverHTTP(t *testing.T) {
	s, h := newTestServer(t)
	name := s.store.DefaultScreen().Name

	upload := func(filename string, content []byte) *http.Response {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("s", name)
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(content)
		_ = mw.Close()
		return do(h, http.MethodPost, "/images?s="+name, map[string]string{
			"Authorization":  "Bearer " + testToken,
			"Content-Type":   mw.FormDataContentType(),
			"Sec-Fetch-Site": "same-origin",
		}, &buf)
	}

	res := upload("roll.png", pngBytes(t, photo(300, 200)))
	if res.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("upload = %d\n%s", res.StatusCode, body)
	}
	imgs := s.store.Images()
	if len(imgs) != 1 || imgs[0].Name != "roll.png" || imgs[0].W != 300 {
		t.Fatalf("stored: %+v", imgs)
	}

	// The thumbnail is served, and only to the token.
	res = do(h, http.MethodGet, "/images/"+imgs[0].ID+"/pic.png",
		map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail = %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/images/"+imgs[0].ID+"/pic.png", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous thumbnail = %d, want 401", res.StatusCode)
	}

	// A non-image is refused, and nothing is stored.
	res = upload("notes.txt", []byte("this is a shopping list, not a photograph"))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("bad upload = %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "err=") || !strings.Contains(loc, "PNG") {
		t.Errorf("a text file was not refused with a useful message: %q", loc)
	}
	if len(s.store.Images()) != 1 {
		t.Fatalf("a rejected upload was stored: %+v", s.store.Images())
	}

	// It renders through a block.
	sc := s.store.DefaultScreen()
	sc.Blocks = []Block{{Type: BlockImage, X: 0, Y: 0, W: 400, H: 300, Image: imgs[0].ID,
		Dither: "atkinson", Fit: "cover", Anchor: "c", Align: "left"}}
	if err := s.store.SaveScreen(sc.Name, sc); err != nil {
		t.Fatal(err)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+sc.DeviceKey, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("image render = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	c := chunks(t, body)
	if c["IHDR"][8] != 1 || c["IHDR"][9] != 3 {
		t.Fatalf("an image block did not come out 1-bit paletted: depth=%d type=%d", c["IHDR"][8], c["IHDR"][9])
	}
}

func TestSamplePNGIsOneBit(t *testing.T) {
	_, h := newTestServer(t)
	res := do(h, http.MethodGet, "/sample.png?font=pixel8x16&size=32",
		map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sample = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	c := chunks(t, body)
	if c["IHDR"][8] != 1 || c["IHDR"][9] != 3 {
		t.Fatalf("the sample strip is not 1-bit paletted")
	}
	// An illegal pairing still renders something, snapped, rather than erroring.
	res = do(h, http.MethodGet, "/sample.png?font=pixel8x16&size=21",
		map[string]string{"Authorization": "Bearer " + testToken}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("snapped sample = %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/sample.png?font=pixel8x16&size=32", nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous sample = %d, want 401", res.StatusCode)
	}
}

func TestEditPageHasTheControls(t *testing.T) {
	_, h := newTestServer(t)
	res := do(h, http.MethodGet, "/edit", map[string]string{
		"Authorization": "Bearer " + testToken, "Accept": "text/html"}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("edit = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	page := string(body)
	for _, want := range []string{
		`name="b0.x"`, `name="b0.y"`, `name="b0.w"`, `name="b0.h"`, // numbers work without JS
		`name="screen_w"`, `name="addtype"`, `value="save"`,
		`id="font-rules"`, `/sample.png?font=`, // the font teaching surface
		`action="/layout"`, `name="json"`, // the escape hatch
		`action="/starter"`, `action="/images?s=`, `action="/key/rotate"`,
		`class="blk"`, `data-handle=`, // the drag overlay
	} {
		if !strings.Contains(page, want) {
			t.Errorf("edit page is missing %q", want)
		}
	}
	// Every block type is offered.
	for _, typ := range blockTypes {
		if !strings.Contains(page, `<option value="`+typ+`">`) {
			t.Errorf("block type %q is not offered", typ)
		}
	}
	// Every font is offered.
	for _, f := range faces {
		if !strings.Contains(page, `value="`+f.ID+`"`) {
			t.Errorf("font %q is not offered", f.ID)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	_, h := newTestServer(t)

	res := do(h, http.MethodPost, "/login",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"},
		strings.NewReader("token=wrong"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", res.StatusCode)
	}

	res = do(h, http.MethodPost, "/login",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"},
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
			map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"},
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
	name := s.store.DefaultScreen().Name
	old := s.store.DefaultScreen().DeviceKey
	res := do(h, http.MethodPost, "/key/rotate", authed(), strings.NewReader("s="+name))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("rotate = %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+old, nil, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old key still works: %d", res.StatusCode)
	}
	res = do(h, http.MethodGet, "/screen.png?k="+s.store.DefaultScreen().DeviceKey, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new key = %d", res.StatusCode)
	}
}

func TestMicropythonSnippetIsExact(t *testing.T) {
	sc := Screen{Name: "hallway", W: 800, H: 480, DeviceKey: "abc123"}
	snip := micropython("https://panel.baileys.dev/", sc, paramsFor(sc))
	for _, want := range []string{
		`URL    = "https://panel.baileys.dev/s/hallway/screen.bin?h=480&w=800&k=abc123"`,
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

	// Rotation swaps the delivered dimensions, and the snippet has to say so.
	rot := Screen{Name: "tall", W: 800, H: 480, Rotate: 90, DeviceKey: "k"}
	snip = micropython("https://panel.baileys.dev", rot, paramsFor(rot))
	if !strings.Contains(snip, "W, H   = 480, 800") {
		t.Errorf("rotated snippet has the wrong dimensions:\n%s", firstLines(snip, 8))
	}
	if !strings.Contains(snip, "STRIDE = 60") {
		t.Errorf("rotated stride is wrong:\n%s", firstLines(snip, 8))
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
