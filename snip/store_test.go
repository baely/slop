package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "pastes"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestNewSlug(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		slug, err := newSlug()
		if err != nil {
			t.Fatalf("newSlug: %v", err)
		}
		if len(slug) != slugLen {
			t.Fatalf("slug %q has length %d, want %d", slug, len(slug), slugLen)
		}
		if !validSlug(slug) {
			t.Fatalf("slug %q failed validSlug", slug)
		}
		for _, r := range slug {
			if strings.ContainsRune("ilou", r) {
				t.Fatalf("slug %q contains an ambiguous character", slug)
			}
		}
		if seen[slug] {
			t.Fatalf("duplicate slug %q after %d draws", slug, i)
		}
		seen[slug] = true
	}
}

func TestValidSlug(t *testing.T) {
	good, err := newSlug()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		in   string
		want bool
	}{
		{good, true},
		{"", false},
		{"short", false},
		{strings.Repeat("a", slugLen+1), false},
		{strings.Repeat("a", slugLen-1), false},
		{strings.Repeat("i", slugLen), false}, // excluded letter
		{"../../etc/passwd0000", false},       // traversal
		{strings.Repeat("A", slugLen), false}, // uppercase
		{strings.Repeat("a", slugLen-1) + "!", false},
	}
	for _, c := range cases {
		if got := validSlug(c.in); got != c.want {
			t.Errorf("validSlug(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCreateFetchRoundTrip(t *testing.T) {
	s := newTestStore(t)
	body := "line one\nline two\n\tindented\n"
	p, err := s.Create("Notes", body, time.Hour, false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !validSlug(p.Slug) {
		t.Fatalf("bad slug %q", p.Slug)
	}
	if p.Size != len(body) {
		t.Errorf("Size = %d, want %d", p.Size, len(body))
	}
	got, err := s.Fetch(p.Slug)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
	if got.Title != "Notes" {
		t.Errorf("Title = %q", got.Title)
	}
	// A non-burn paste survives repeated reads.
	if _, err := s.Fetch(p.Slug); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pastes")
	s1, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s1.Create("Kept", "still here", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	burn, err := s1.Create("Burner", "one shot", 0, true)
	if err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if s2.Len() != 2 {
		t.Fatalf("after reload Len = %d, want 2", s2.Len())
	}
	got, err := s2.Fetch(p.Slug)
	if err != nil {
		t.Fatalf("Fetch after reload: %v", err)
	}
	if got.Body != "still here" || got.Title != "Kept" {
		t.Errorf("round trip lost data: %+v", got)
	}
	if !got.Expires.IsZero() {
		t.Errorf("Expires = %v, want zero for never", got.Expires)
	}
	meta, err := s2.Peek(burn.Slug)
	if err != nil {
		t.Fatalf("Peek after reload: %v", err)
	}
	if !meta.Burn {
		t.Error("burn flag lost across restart")
	}
	if meta.Body != "" {
		t.Error("Peek must not return the body")
	}
}

func TestWriteFileAtomicLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := writeFileAtomic(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want 1 (temp files must be cleaned up)", len(entries))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %v, want 0600", perm)
	}
}

func TestExpiryOnRead(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.now = func() time.Time { return base }

	p, err := s.Create("", "temporary", 10*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fetch(p.Slug); err != nil {
		t.Fatalf("Fetch before expiry: %v", err)
	}

	// The sweeper has not run; the read path must still refuse.
	s.now = func() time.Time { return base.Add(11 * time.Minute) }
	if _, err := s.Fetch(p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Fetch after expiry = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(s.path(p.Slug)); !os.IsNotExist(err) {
		t.Error("expired paste file still on disk after a read")
	}
	if s.Len() != 0 {
		t.Errorf("Len = %d, want 0", s.Len())
	}
}

func TestExpiryBoundary(t *testing.T) {
	base := time.Now()
	p := Paste{Expires: base.Add(time.Minute)}
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before", base, false},
		{"one nanosecond early", base.Add(time.Minute - 1), false},
		{"exactly at expiry", base.Add(time.Minute), true},
		{"after", base.Add(2 * time.Minute), true},
	}
	for _, c := range cases {
		if got := p.Expired(c.at); got != c.want {
			t.Errorf("%s: Expired = %v, want %v", c.name, got, c.want)
		}
	}
	never := Paste{}
	if never.Expired(base.Add(100 * 365 * 24 * time.Hour)) {
		t.Error("a paste with no expiry must never expire")
	}
}

func TestSweep(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.now = func() time.Time { return base }

	short, _ := s.Create("", "short", 10*time.Minute, false)
	long, _ := s.Create("", "long", 24*time.Hour, false)
	forever, _ := s.Create("", "forever", 0, false)

	s.now = func() time.Time { return base.Add(time.Hour) }
	if n := s.Sweep(); n != 1 {
		t.Fatalf("Sweep removed %d, want 1", n)
	}
	if _, err := s.Peek(short.Slug); !errors.Is(err, ErrNotFound) {
		t.Error("expired paste survived the sweep")
	}
	if _, err := s.Peek(long.Slug); err != nil {
		t.Errorf("unexpired paste was swept: %v", err)
	}
	if _, err := s.Peek(forever.Slug); err != nil {
		t.Errorf("never-expiring paste was swept: %v", err)
	}
	if n := s.Sweep(); n != 0 {
		t.Errorf("second Sweep removed %d, want 0", n)
	}
}

func TestBurnAfterReadingIsAtomicAndDurable(t *testing.T) {
	s := newTestStore(t)
	p, err := s.Create("Secret", "the only copy", 0, true)
	if err != nil {
		t.Fatal(err)
	}

	const readers = 64
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		bodies  []string
		misses  int
		others  []error
		onDiskA bool
	)
	start := make(chan struct{})
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := s.Fetch(p.Slug)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
				bodies = append(bodies, got.Body)
				// The destroy must already be durable by the time the body is
				// handed back, so the file cannot still be there.
				if _, statErr := os.Stat(s.path(p.Slug)); statErr == nil {
					onDiskA = true
				}
			case errors.Is(err, ErrNotFound):
				misses++
			default:
				others = append(others, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d readers got the body, want exactly 1", wins)
	}
	if bodies[0] != "the only copy" {
		t.Errorf("winner got %q", bodies[0])
	}
	if misses != readers-1 {
		t.Errorf("%d readers got ErrNotFound, want %d", misses, readers-1)
	}
	if len(others) > 0 {
		t.Errorf("unexpected errors: %v", others)
	}
	if onDiskA {
		t.Error("the paste file still existed when the body was returned; destroy was not durable first")
	}
	if _, err := os.Stat(s.path(p.Slug)); !os.IsNotExist(err) {
		t.Error("burned paste file still on disk")
	}
	if s.Len() != 0 {
		t.Errorf("Len = %d after burn, want 0", s.Len())
	}
	if _, err := s.Fetch(p.Slug); !errors.Is(err, ErrNotFound) {
		t.Errorf("Fetch after burn = %v, want ErrNotFound", err)
	}
}

func TestPeekDoesNotBurn(t *testing.T) {
	s := newTestStore(t)
	p, err := s.Create("", "still here", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Peek(p.Slug); err != nil {
			t.Fatalf("Peek %d: %v", i, err)
		}
	}
	if _, err := s.Fetch(p.Slug); err != nil {
		t.Fatalf("Fetch after peeks: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.Create("", "gone soon", 0, false)
	if err := s.Delete(p.Slug); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Fetch(p.Slug); !errors.Is(err, ErrNotFound) {
		t.Errorf("Fetch after delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(p.Slug); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete("not-a-valid-slug"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete with a bad slug = %v, want ErrNotFound", err)
	}
}

func TestFetchRejectsBadSlugWithoutTouchingDisk(t *testing.T) {
	s := newTestStore(t)
	for _, slug := range []string{"", "../../secret", "0000000000000000000", strings.Repeat("z", 64)} {
		if _, err := s.Fetch(slug); !errors.Is(err, ErrNotFound) {
			t.Errorf("Fetch(%q) = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestCreateRejectsOversizeBody(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create("", strings.Repeat("x", MaxPasteBytes+1), 0, false); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Create oversize = %v, want ErrTooLarge", err)
	}
	if _, err := s.Create("", strings.Repeat("x", MaxPasteBytes), 0, false); err != nil {
		t.Fatalf("Create at exactly the cap: %v", err)
	}
}

func TestListIsNewestFirstAndExcludesExpired(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.now = func() time.Time { return base }
	first, _ := s.Create("first", "a", 0, false)
	s.now = func() time.Time { return base.Add(time.Second) }
	dying, _ := s.Create("dying", "b", 10*time.Minute, false)
	s.now = func() time.Time { return base.Add(2 * time.Second) }
	last, _ := s.Create("last", "c", 0, false)

	s.now = func() time.Time { return base.Add(time.Hour) }
	got := s.List()
	if len(got) != 2 {
		t.Fatalf("List returned %d rows, want 2", len(got))
	}
	if got[0].Slug != last.Slug || got[1].Slug != first.Slug {
		t.Errorf("wrong order: %s then %s", got[0].Title, got[1].Title)
	}
	for _, p := range got {
		if p.Slug == dying.Slug {
			t.Error("expired paste appeared in the listing")
		}
		if p.Body != "" {
			t.Error("List must not carry bodies")
		}
	}
}

func TestStoreIgnoresJunkFilesOnLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pastes")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Create("", "real", 0, false)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0000000000000000000x.json"), []byte("{ truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if s2.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s2.Len())
	}
	if _, err := s2.Fetch(p.Slug); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}
