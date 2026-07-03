package images

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/baileybutler/voyage/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResolveWikiCachesResult(t *testing.T) {
	calls := 0
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("gsrsearch"); got != "Fiji" {
			t.Errorf("unexpected search query %q", got)
		}
		_, _ = w.Write([]byte(`{"query":{"pages":[
			{"index":2,"title":"Fiji Airways","fullurl":"https://en.wikipedia.org/wiki/Fiji_Airways"},
			{"index":1,"title":"Fiji","fullurl":"https://en.wikipedia.org/wiki/Fiji",
			 "thumbnail":{"source":"https://upload.wikimedia.org/fiji.jpg"}}
		]}}`))
	}))
	defer wiki.Close()

	r := New(newTestStore(t), "")
	r.wikiBase = wiki.URL

	img := r.Resolve("  Fiji  ")
	if img == nil || img.URL != "https://upload.wikimedia.org/fiji.jpg" {
		t.Fatalf("unexpected image: %+v", img)
	}
	if img.Credit != "Fiji · Wikipedia" || img.CreditURL != "https://en.wikipedia.org/wiki/Fiji" {
		t.Fatalf("unexpected credit: %+v", img)
	}

	// Second resolve must come from the cache.
	if again := r.Resolve("fiji"); again == nil || again.URL != img.URL {
		t.Fatalf("cache miss: %+v", again)
	}
	if calls != 1 {
		t.Fatalf("want 1 upstream call, got %d", calls)
	}
}

func TestResolveFallsThroughQueries(t *testing.T) {
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("gsrsearch") == "Nowhereville" {
			_, _ = w.Write([]byte(`{"query":{"pages":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"query":{"pages":[
			{"index":1,"title":"Fiji","fullurl":"https://en.wikipedia.org/wiki/Fiji",
			 "thumbnail":{"source":"https://upload.wikimedia.org/fiji.jpg"}}]}}`))
	}))
	defer wiki.Close()

	r := New(newTestStore(t), "")
	r.wikiBase = wiki.URL

	img := r.Resolve("Nowhereville", "", "Fiji")
	if img == nil || img.URL != "https://upload.wikimedia.org/fiji.jpg" {
		t.Fatalf("expected fallback query to resolve, got %+v", img)
	}
	// The miss is cached too, so a page render doesn't refetch it.
	if cached, err := r.store.GetImage("wiki:nowhereville"); err != nil || cached.URL != "" {
		t.Fatalf("expected cached miss, got %+v (%v)", cached, err)
	}
}

func TestResolveUnsplashWhenKeySet(t *testing.T) {
	unsplash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Client-ID test-key" {
			t.Errorf("unexpected auth header %q", got)
		}
		_, _ = w.Write([]byte(`{"results":[{
			"urls":{"regular":"https://images.unsplash.com/fiji?w=1080"},
			"links":{"html":"https://unsplash.com/photos/abc"},
			"user":{"name":"Jane Doe"}}]}`))
	}))
	defer unsplash.Close()

	r := New(newTestStore(t), "test-key")
	r.unsplashBase = unsplash.URL

	img := r.Resolve("Fiji")
	if img == nil || img.URL != "https://images.unsplash.com/fiji?w=1080" {
		t.Fatalf("unexpected image: %+v", img)
	}
	if img.Credit != "Jane Doe · Unsplash" {
		t.Fatalf("unexpected credit: %q", img.Credit)
	}
}

func TestStripYears(t *testing.T) {
	cases := map[string]string{
		"Fiji 2026":        "Fiji",
		"2027 Japan trip":  "Japan trip",
		"Italy":            "Italy",
		"Euro summer 2026": "Euro summer",
	}
	for in, want := range cases {
		if got := StripYears(in); got != want {
			t.Errorf("StripYears(%q) = %q, want %q", in, got, want)
		}
	}
}
