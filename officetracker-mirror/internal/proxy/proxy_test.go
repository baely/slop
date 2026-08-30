package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func do(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Cookie", "user=attacker")
	req.Header.Set("Authorization", "Bearer client-supplied")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestGetInjectsTokenStripsClientCredsAndCookies(t *testing.T) {
	var lastReq *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReq = r.Clone(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Set-Cookie", "user=secret; Path=/")
		_, _ = w.Write([]byte("<html><body><div id=\"calendar\"></div></body></html>"))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := New(Config{Upstream: u, Token: "officetracker:testtoken"})

	resp := do(t, h, http.MethodGet, "/2026-06")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := lastReq.Header.Get("Authorization"); got != "Bearer officetracker:testtoken" {
		t.Errorf("upstream Authorization = %q, want injected token", got)
	}
	if got := lastReq.Header.Get("Cookie"); got != "" {
		t.Errorf("upstream Cookie = %q, want stripped", got)
	}
	if vals := resp.Header.Values("Set-Cookie"); len(vals) != 0 {
		t.Errorf("response Set-Cookie = %v, want stripped", vals)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "el.disabled = true") {
		t.Errorf("read-only assets not injected into HTML body")
	}
	if !strings.Contains(string(body), "<div id=\"calendar\"></div>") {
		t.Errorf("original body content missing")
	}
}

func TestWritesRejected(t *testing.T) {
	u, _ := url.Parse("http://example.invalid")
	h := New(Config{Upstream: u, Token: "t"})
	for _, m := range []string{http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch} {
		resp := do(t, h, m, "/api/v1/state/2026/6/19")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", m, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "read-only mirror") {
			t.Errorf("%s body = %q, want read-only message", m, string(body))
		}
	}
}

func TestSensitivePathsDenied(t *testing.T) {
	u, _ := url.Parse("http://example.invalid")
	h := New(Config{Upstream: u, Token: "t"})
	denied := []string{
		"/api/v1/developer/tokens",
		"/api/v1/developer/secret",
		"/api/v1/account/link",
		"/mcp/v1/",
		"/auth/callback",
		"/login",
		"/logout",
		"/developer",
	}
	for _, p := range denied {
		resp := do(t, h, http.MethodGet, p)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", p, resp.StatusCode)
		}
	}
}

func TestMetaEndpoint(t *testing.T) {
	u, _ := url.Parse("http://example.invalid")
	h := New(Config{Upstream: u, Token: "t"})

	// GET is public and returns the anonymous, read-only capability body.
	resp := do(t, h, http.MethodGet, "/api/v1/meta")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/meta status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := strings.TrimSpace(string(body)); got != `{"auth":"none","read_only":true}` {
		t.Errorf("body = %q, want anonymous read-only meta", got)
	}

	// Writes to meta are rejected like any other write.
	if w := do(t, h, http.MethodPost, "/api/v1/meta"); w.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/v1/meta status = %d, want 403", w.StatusCode)
	}
}

func TestCORSHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Upstream sends its own (restrictive) CORS policy, which we must override.
		w.Header().Set("Access-Control-Allow-Origin", "https://officetracker.com.au")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := New(Config{Upstream: u, Token: "t"})

	// Proxied GET: upstream's origin is replaced with the wildcard (exactly once).
	resp := do(t, h, http.MethodGet, "/api/v1/state/2026")
	if vals := resp.Header.Values("Access-Control-Allow-Origin"); len(vals) != 1 || vals[0] != "*" {
		t.Errorf("proxied Access-Control-Allow-Origin = %v, want [*]", vals)
	}

	// Preflight: 204 with the allowed methods advertised.
	pre := do(t, h, http.MethodOptions, "/api/v1/state/2026")
	if pre.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", pre.StatusCode)
	}
	if got := pre.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("OPTIONS Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := pre.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("OPTIONS Access-Control-Allow-Methods = %q, want GET listed", got)
	}

	// Local meta endpoint also carries CORS.
	meta := do(t, h, http.MethodGet, "/api/v1/meta")
	if got := meta.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("meta Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestReadPathsAllowed(t *testing.T) {
	for _, p := range []string{"/", "/2026-06", "/settings", "/api/v1/state/2026", "/api/v1/note/2026", "/api/v1/settings/", "/static/themes.css"} {
		if isDenied(p) {
			t.Errorf("path %s should be allowed", p)
		}
	}
}
