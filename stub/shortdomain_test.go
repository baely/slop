package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newSplitServer wires an admin host and a separate short host, the way the
// deployment does (admin on stub.baileys.dev, short links on baely.sh).
func newSplitServer(t *testing.T) (*server, http.Handler, *Store) {
	t.Helper()
	store, _ := newTestStore(t)
	s, err := newServer(store, testToken, "https://admin.example", "https://short.example")
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	s.secure = false
	return s, s.handler(), store
}

func TestShortDomainServesRedirectsOnly(t *testing.T) {
	s, h, store := newSplitServer(t)
	if s.shortHost != "short.example" {
		t.Fatalf("shortHost = %q, want short.example", s.shortHost)
	}
	l, err := store.Create(CreateParams{Target: "https://example.com/deep/path", Slug: "abc123x"}, time.Now())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The slug resolves on the short host.
	req := httptest.NewRequest("GET", "http://short.example/"+l.Slug, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("short host redirect = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://example.com/deep/path" {
		t.Fatalf("Location = %q", got)
	}

	// Everything that is not a redirect is a flat 404 there — no login form,
	// no admin, no API, and no hint that any of them exist.
	for _, path := range []string{"/", "/login", "/logout", "/admin/link/abc123x", "/api/links", "/static/style.css"} {
		req := httptest.NewRequest("GET", "http://short.example"+path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET short.example%s = %d, want 404", path, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(strings.ToLower(body), "token") ||
			strings.Contains(strings.ToLower(body), "sign in") {
			t.Errorf("GET short.example%s leaked admin wording: %q", path, body)
		}
	}

	// A POST to the short host is refused even on an allowed-looking path.
	req = httptest.NewRequest("POST", "http://short.example/login", strings.NewReader("token="+testToken))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST short.example/login = %d, want 404", rec.Code)
	}

	// healthz still answers, so the container stays monitorable on either host.
	req = httptest.NewRequest("GET", "http://short.example/healthz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("short host /healthz = %d, want 200", rec.Code)
	}
}

func TestAdminHostKeepsFullSurface(t *testing.T) {
	_, h, _ := newSplitServer(t)
	// The admin host still serves the login page rather than a 404.
	req := httptest.NewRequest("GET", "http://admin.example/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin host / = %d, want 200", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "token") {
		t.Error("admin host / did not render the sign-in form")
	}
}

func TestShortLinksAreBuiltFromShortURL(t *testing.T) {
	s, _, store := newSplitServer(t)
	l, err := store.Create(CreateParams{Target: "https://example.com/", Slug: "zzz999q"}, time.Now())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view := s.apiView(l, time.Now(), false)
	if view.ShortURL != "https://short.example/zzz999q" {
		t.Fatalf("ShortURL = %q, want https://short.example/zzz999q", view.ShortURL)
	}
}

// When SHORT_URL is unset the two collapse to one host and nothing is guarded.
func TestSingleHostUnguarded(t *testing.T) {
	s, h, _ := newTestServer(t)
	if s.shortHost != "" {
		t.Fatalf("shortHost = %q, want empty when both URLs match", s.shortHost)
	}
	req := httptest.NewRequest("GET", "http://stub.example/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("single-host / = %d, want 200", rec.Code)
	}
}
