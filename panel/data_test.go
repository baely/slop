package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestExtractPath(t *testing.T) {
	const doc = `{
		"current": {"temp_c": 14.5, "cond": "rain", "ok": true, "nothing": null},
		"list": [{"name": "first"}, {"name": "second"}],
		"count": 42
	}`
	var v any
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}

	ok := map[string]string{
		"current.temp_c": "14.5",
		"current.cond":   "rain",
		"current.ok":     "true",
		"list.0.name":    "first",
		"list.1.name":    "second",
		"count":          "42",
	}
	for path, want := range ok {
		got, err := extractPath(v, path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}

	bad := map[string]string{
		"current.missing":       `no "missing" at current.missing`,
		"nope":                  `no "nope" at nope`,
		"current.temp_c.deeper": "current.temp_c is not an object or array",
		"list.9.name":           "index 9 is outside the 2-item array",
		"list.name":             `"name" is not an index`,
		"current.nothing":       "holds null",
		"current":               "holds an object",
		"list":                  "holds an array",
	}
	for path, want := range bad {
		_, err := extractPath(v, path)
		if err == nil {
			t.Errorf("%s: expected an error", path)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %q missing %q", path, err, want)
		}
	}
}

func TestApplyTemplate(t *testing.T) {
	tests := []struct{ tpl, v, want string }{
		{"{{v}}°C", "14.5", "14.5°C"},
		{"{{value}} kWh", "3", "3 kWh"},
		{"", "x", "x"},
		{"{{v}} / {{v}}", "2", "2 / 2"},
	}
	for _, tc := range tests {
		if got := applyTemplate(tc.tpl, tc.v); got != tc.want {
			t.Errorf("applyTemplate(%q, %q) = %q, want %q", tc.tpl, tc.v, got, tc.want)
		}
	}
}

// A data block whose URL is unreachable renders its fallback, and the render
// itself never waits for the network.
func TestUnreachableURLRendersFallbackAndDoesNotHang(t *testing.T) {
	d := newDataStore(false)
	b := Block{Type: BlockData, X: 0, Y: 0, W: 300, H: 60, Font: "go-bold", Size: 32,
		// TEST-NET-1, port 81: routable in principle, answers never.
		URL: "http://192.0.2.1:81/none.json", Path: "a", Text: "{{v}}", Fallback: "no data",
		Interval: 60, Timeout: 1}.Normalize()

	sc := Screen{Name: "t", W: 400, H: 200, Blocks: []Block{b}}
	ctx := RenderCtx{Now: fixedNow, Data: func(bb Block) (string, bool) { return d.Lookup(bb, time.Now()) }}

	d.RefreshNow(context.Background(), []Block{b})

	start := time.Now()
	img := Render(sc, paramsFor(sc), ctx)
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("render took %v; it waited on the network", el)
	}
	ink := 0
	for _, v := range img.Pix {
		if v != idxBlack && v != idxWhite {
			t.Fatalf("index %d escaped", v)
		}
		if v == idxBlack {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("the fallback text did not render")
	}

	// And it is still the fallback once the fetch has actually failed.
	waitFor(t, 5*time.Second, func() bool { return d.Status(b).Err != "" })
	text, stale := d.Lookup(b, time.Now())
	if text != "no data" || !stale {
		t.Fatalf("after a failed fetch: text=%q stale=%v, want the fallback and stale", text, stale)
	}
}

// A slow endpoint must not slow a frame down. Every render happens while the
// server is still sitting on the request.
func TestSlowEndpointDoesNotBlockTheFrame(t *testing.T) {
	release := make(chan struct{})
	hits := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- struct{}{}
		<-release
		_, _ = w.Write([]byte(`{"v": 99}`))
	}))
	defer srv.Close()
	defer close(release)

	d := newDataStore(true) // httptest is on loopback
	b := Block{Type: BlockData, X: 0, Y: 0, W: 300, H: 60, Font: "go-bold", Size: 32,
		URL: srv.URL, Path: "v", Text: "{{v}}", Fallback: "-", Interval: 60, Timeout: 15}.Normalize()
	sc := Screen{Name: "t", W: 400, H: 200, Blocks: []Block{b}}
	ctx := RenderCtx{Now: fixedNow, Data: func(bb Block) (string, bool) { return d.Lookup(bb, time.Now()) }}

	d.RefreshNow(context.Background(), []Block{b})
	select {
	case <-hits:
	case <-time.After(3 * time.Second):
		t.Fatal("the fetch never reached the server")
	}

	// The endpoint is mid-request. Frames still come out immediately.
	for i := 0; i < 5; i++ {
		start := time.Now()
		Render(sc, paramsFor(sc), ctx)
		if el := time.Since(start); el > 300*time.Millisecond {
			t.Fatalf("frame %d took %v while the endpoint was hanging", i, el)
		}
	}
	if text, stale := d.Lookup(b, time.Now()); text != "-" || !stale {
		t.Fatalf("while waiting: text=%q stale=%v, want the fallback", text, stale)
	}
}

func TestSuccessfulFetchLandsAndTemplates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current": {"temp_c": 14.5}}`))
	}))
	defer srv.Close()

	d := newDataStore(true)
	b := Block{Type: BlockData, URL: srv.URL, Path: "current.temp_c", Text: "{{v}}°C",
		Fallback: "-", Interval: 60, Timeout: 5, W: 100, H: 40, Font: "go", Size: 20}.Normalize()

	d.RefreshNow(context.Background(), []Block{b})
	if !waitFor(t, 5*time.Second, func() bool { return d.Status(b).OK }) {
		t.Fatalf("value never arrived: %+v", d.Status(b))
	}
	text, stale := d.Lookup(b, time.Now())
	if text != "14.5°C" {
		t.Fatalf("text = %q, want 14.5°C", text)
	}
	if stale {
		t.Error("a value fetched a moment ago is not stale")
	}
	// Two intervals later it is.
	if _, stale := d.Lookup(b, time.Now().Add(3*time.Minute)); !stale {
		t.Error("an old value should read stale")
	}
}

func TestFetchErrorsAreShortAndSpecific(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"HTTP 500": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"not JSON": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>hi")) },
		`no "missing"`: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"other": 1}`))
		},
	}
	for want, h := range cases {
		srv := httptest.NewServer(h)
		d := newDataStore(true)
		b := Block{Type: BlockData, URL: srv.URL, Path: "missing", Text: "{{v}}", Fallback: "-",
			Interval: 60, Timeout: 5, W: 100, H: 40, Font: "go", Size: 20}.Normalize()
		d.RefreshNow(context.Background(), []Block{b})
		if !waitFor(t, 5*time.Second, func() bool { return d.Status(b).Err != "" }) {
			t.Errorf("%s: no error recorded", want)
			srv.Close()
			continue
		}
		if got := d.Status(b).Err; !strings.Contains(got, want) {
			t.Errorf("error %q missing %q", got, want)
		}
		srv.Close()
	}
}

// A user-supplied URL is fetched by the server, which sits on a home LAN. The
// dialer refuses anything that is not a public destination.
func TestSSRFGuard(t *testing.T) {
	d := newDataStore(false)
	blocked := []string{
		"127.0.0.1:80", "127.0.0.1:443", "[::1]:443",
		"10.1.2.3:80", "192.168.0.82:80", "172.16.9.9:443",
		"169.254.169.254:80", "100.64.1.1:80", "0.0.0.0:80",
		"224.0.0.1:80", "[fe80::1]:443",
	}
	for _, addr := range blocked {
		if err := d.checkAddr("tcp", addr); err == nil {
			t.Errorf("%s was allowed", addr)
		}
	}
	// Non-web ports are refused even on a public address.
	for _, addr := range []string{"93.184.216.34:22", "93.184.216.34:6379", "93.184.216.34:25"} {
		if err := d.checkAddr("tcp", addr); err == nil {
			t.Errorf("%s was allowed", addr)
		} else if !strings.Contains(err.Error(), "refused port") {
			t.Errorf("%s: %v", addr, err)
		}
	}
	// And a genuinely public one is fine.
	for _, addr := range []string{"93.184.216.34:443", "1.1.1.1:80", "[2606:4700::1111]:443"} {
		if err := d.checkAddr("tcp", addr); err != nil {
			t.Errorf("%s was refused: %v", addr, err)
		}
	}

	// The guard runs on the resolved address, so a hostname pointing at
	// loopback is stopped too.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"v":1}`))
	}))
	defer srv.Close()
	b := Block{Type: BlockData, URL: srv.URL, Path: "v", Text: "{{v}}", Fallback: "-",
		Interval: 60, Timeout: 5, W: 100, H: 40, Font: "go", Size: 20}.Normalize()
	d.RefreshNow(context.Background(), []Block{b})
	if !waitFor(t, 5*time.Second, func() bool { return d.Status(b).Err != "" }) {
		t.Fatal("a loopback URL was not refused")
	}
	if d.Status(b).OK {
		t.Fatal("a loopback URL returned a value")
	}
}

// The response body is capped so a hostile endpoint cannot stream forever.
func TestOversizedResponseIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"v": "`))
		chunk := strings.Repeat("x", 64<<10)
		for i := 0; i < 32; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	d := newDataStore(true)
	b := Block{Type: BlockData, URL: srv.URL, Path: "v", Text: "{{v}}", Fallback: "-",
		Interval: 60, Timeout: 10, W: 100, H: 40, Font: "go", Size: 20}.Normalize()
	d.RefreshNow(context.Background(), []Block{b})
	if !waitFor(t, 10*time.Second, func() bool { return d.Status(b).Err != "" || d.Status(b).OK }) {
		t.Fatal("no outcome recorded")
	}
	if d.Status(b).OK {
		t.Fatal("a 2 MiB response was accepted; the body cap is not working")
	}
}

// Refresh honours the interval instead of hammering an endpoint every tick.
func TestRefreshRespectsInterval(t *testing.T) {
	var hits int32
	done := make(chan struct{}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"v":1}`))
		done <- struct{}{}
	}))
	defer srv.Close()

	d := newDataStore(true)
	b := Block{Type: BlockData, URL: srv.URL, Path: "v", Text: "{{v}}", Fallback: "-",
		Interval: 300, Timeout: 5, W: 100, H: 40, Font: "go", Size: 20}.Normalize()

	now := time.Now()
	d.Refresh(context.Background(), []Block{b}, now)
	<-done
	for i := 0; i < 5; i++ {
		d.Refresh(context.Background(), []Block{b}, now.Add(time.Duration(i)*time.Second))
	}
	time.Sleep(120 * time.Millisecond)
	if hits != 1 {
		t.Fatalf("%d fetches inside one interval, want 1", hits)
	}
	// Past the interval it goes again.
	d.Refresh(context.Background(), []Block{b}, now.Add(400*time.Second))
	<-done
	if hits != 2 {
		t.Fatalf("%d fetches after the interval elapsed, want 2", hits)
	}
}
