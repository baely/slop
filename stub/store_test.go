package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s, dir
}

func mustCreate(t *testing.T, s *Store, p CreateParams, now time.Time) *Link {
	t.Helper()
	l, err := s.Create(p, now)
	if err != nil {
		t.Fatalf("Create(%+v): %v", p, err)
	}
	return l
}

func TestCreateGeneratesUsableSlug(t *testing.T) {
	s, _ := newTestStore(t)
	l := mustCreate(t, s, CreateParams{Target: "https://example.com/x"}, time.Now())
	if len(l.Slug) != defaultSlugLen {
		t.Fatalf("generated slug %q has length %d, want %d", l.Slug, len(l.Slug), defaultSlugLen)
	}
	if _, err := normalizeSlug(l.Slug); err != nil {
		t.Fatalf("generated slug %q fails its own validation: %v", l.Slug, err)
	}
}

func TestCreateRejectsCollision(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "docs", Target: "https://example.com/1"}, now)

	_, err := s.Create(CreateParams{Slug: "docs", Target: "https://example.com/2"}, now)
	if err == nil {
		t.Fatal("second create with the same slug succeeded")
	}
	var ce conflictError
	if !errors.As(err, &ce) {
		t.Fatalf("collision error is %T, want conflictError so the API can answer 409", err)
	}
	if !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("collision message %q does not say the slug is taken", err)
	}

	// Case-folded collision too: /Docs and /docs must not be two links.
	if _, err := s.Create(CreateParams{Slug: "DOCS", Target: "https://example.com/3"}, now); err == nil {
		t.Fatal("case-different slug was accepted as a separate link")
	}
}

func TestCreateRejectsReservedAndBadTarget(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	for _, slug := range []string{"admin", "api", "healthz", "static"} {
		if _, err := s.Create(CreateParams{Slug: slug, Target: "https://example.com"}, now); err == nil {
			t.Fatalf("reserved slug %q was accepted", slug)
		}
	}
	if _, err := s.Create(CreateParams{Target: "javascript:alert(1)"}, now); err == nil {
		t.Fatal("javascript: target was accepted")
	}
	past := now.Add(-time.Hour)
	if _, err := s.Create(CreateParams{Target: "https://example.com", ExpiresAt: &past}, now); err == nil {
		t.Fatal("expiry in the past was accepted")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	exp := now.Add(48 * time.Hour)
	mustCreate(t, s, CreateParams{Slug: "keep", Target: "https://example.com/keep", ExpiresAt: &exp, MaxClicks: 5}, now)
	s.Resolve("keep", Visit{IP: "203.0.113.9", UA: "Mozilla/5.0 (X11)", Referer: "https://ref.example/x", Now: now})
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	l, ok := reopened.Get("keep")
	if !ok {
		t.Fatal("link did not survive a restart")
	}
	if l.Target != "https://example.com/keep" {
		t.Fatalf("target = %q after restart", l.Target)
	}
	if l.Clicks != 1 || l.BrowserClicks != 1 {
		t.Fatalf("clicks = %d/%d after restart, want 1/1", l.Clicks, l.BrowserClicks)
	}
	if l.MaxClicks != 5 || l.ExpiresAt == nil {
		t.Fatalf("options lost across restart: max=%d expires=%v", l.MaxClicks, l.ExpiresAt)
	}
	if l.UniqueCount() != 1 {
		t.Fatalf("uniques = %d after restart, want 1", l.UniqueCount())
	}
	if got := l.Referrers["https://ref.example"]; got != 1 {
		t.Fatalf("referrers = %v after restart", l.Referrers)
	}
	// Salt must be stable, otherwise every restart resets uniqueness.
	if string(reopened.salt) != string(s.salt) {
		t.Fatal("salt changed across restart")
	}
}

func TestAtomicWriteLeavesNoDebris(t *testing.T) {
	s, dir := newTestStore(t)
	now := time.Now()
	for i := 0; i < 20; i++ {
		mustCreate(t, s, CreateParams{Target: "https://example.com/x"}, now)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "stub.json" {
		t.Fatalf("data dir = %v, want exactly [stub.json] with no temp files left behind", names)
	}
	fi, err := os.Stat(filepath.Join(dir, "stub.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("stub.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

// The whole privacy claim rests on this: nothing that identifies a visitor is
// ever written to disk.
func TestNoRawIPOrUserAgentIsPersisted(t *testing.T) {
	s, dir := newTestStore(t)
	now := time.Now()
	const ip = "203.0.113.77"
	const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15"
	mustCreate(t, s, CreateParams{Slug: "p", Target: "https://example.com"}, now)
	s.Resolve("p", Visit{IP: ip, UA: ua, Now: now})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "stub.json"))
	if err != nil {
		t.Fatal(err)
	}
	blob := string(raw)
	if strings.Contains(blob, ip) {
		t.Fatal("the raw IP address was written to disk")
	}
	if strings.Contains(blob, ua) || strings.Contains(blob, "AppleWebKit") {
		t.Fatal("the raw User-Agent was written to disk")
	}
}

func TestVisitorHashIsSaltedAndStable(t *testing.T) {
	a, _ := newTestStore(t)
	b, _ := newTestStore(t)
	h1 := a.visitorHash("198.51.100.4", "UA")
	h2 := a.visitorHash("198.51.100.4", "UA")
	h3 := a.visitorHash("198.51.100.5", "UA")
	h4 := b.visitorHash("198.51.100.4", "UA")
	if h1 != h2 {
		t.Fatal("visitorHash is not stable for the same visitor")
	}
	if h1 == h3 {
		t.Fatal("visitorHash collides across different IPs")
	}
	if h1 == h4 {
		t.Fatal("visitorHash is identical across installs — the salt is not doing its job")
	}
	if strings.Contains(h1, "198.51.100.4") {
		t.Fatal("visitorHash leaked the input")
	}
}

func TestUniquesCountDistinctVisitorsOnly(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "u", Target: "https://example.com"}, now)
	for i := 0; i < 5; i++ {
		s.Resolve("u", Visit{IP: "198.51.100.1", UA: "Mozilla/5.0", Now: now})
	}
	s.Resolve("u", Visit{IP: "198.51.100.2", UA: "Mozilla/5.0", Now: now})
	l, _ := s.Get("u")
	if l.Clicks != 6 {
		t.Fatalf("clicks = %d, want 6", l.Clicks)
	}
	if l.UniqueCount() != 2 {
		t.Fatalf("uniques = %d, want 2", l.UniqueCount())
	}
}

func TestBotClicksAreCountedSeparatelyNotDropped(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "b", Target: "https://example.com"}, now)

	s.Resolve("b", Visit{IP: "1.1.1.1", UA: "Mozilla/5.0 (Windows NT 10.0) Chrome/120", Referer: "https://a.example/", Now: now})
	s.Resolve("b", Visit{IP: "2.2.2.2", UA: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", Referer: "https://a.example/", Now: now})
	s.Resolve("b", Visit{IP: "3.3.3.3", UA: "Mozilla/5.0", Prefetch: true, Now: now})
	s.Resolve("b", Visit{IP: "4.4.4.4", UA: "", Now: now})

	l, _ := s.Get("b")
	if l.Clicks != 4 {
		t.Fatalf("total clicks = %d, want 4 (nothing may be silently dropped)", l.Clicks)
	}
	if l.BotClicks != 2 {
		t.Fatalf("bot clicks = %d, want 2", l.BotClicks)
	}
	if l.BrowserClicks != 1 {
		t.Fatalf("browser clicks = %d, want 1", l.BrowserClicks)
	}
	if l.UnknownClicks != 1 {
		t.Fatalf("unknown clicks = %d, want 1", l.UnknownClicks)
	}
	if l.HumanClicks() != 2 {
		t.Fatalf("human clicks = %d, want 2 (browser + unknown)", l.HumanClicks())
	}
	day := now.Format("2006-01-02")
	if l.Days[day] != 2 {
		t.Fatalf("chart series for today = %d, want 2 non-bot clicks", l.Days[day])
	}
	if l.BotDays[day] != 2 {
		t.Fatalf("bot series for today = %d, want 2", l.BotDays[day])
	}
	if l.Referrers["https://a.example"] != 1 {
		t.Fatalf("referrers = %v, want the bot's referer excluded", l.Referrers)
	}
	if l.UniqueCount() != 2 {
		t.Fatalf("uniques = %d, want 2 (bots excluded)", l.UniqueCount())
	}
}

func TestMaxClicksExhaustsExactly(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "x", Target: "https://example.com", MaxClicks: 3}, now)
	for i := 1; i <= 3; i++ {
		target, st, found := s.Resolve("x", Visit{IP: "1.1.1.1", UA: "Mozilla/5.0", Now: now})
		if !found || st != stateActive || target == "" {
			t.Fatalf("click %d: state=%v found=%v, want an active redirect", i, st, found)
		}
	}
	target, st, found := s.Resolve("x", Visit{IP: "1.1.1.1", UA: "Mozilla/5.0", Now: now})
	if !found {
		t.Fatal("exhausted link reported as missing; it must be Gone, not 404")
	}
	if st != stateExhausted {
		t.Fatalf("state after exhaustion = %v, want stateExhausted", st)
	}
	if target != "" {
		t.Fatalf("exhausted link still returned a target %q", target)
	}
	l, _ := s.Get("x")
	if l.Clicks != 3 {
		t.Fatalf("clicks = %d, want 3 — the refused request must not count", l.Clicks)
	}
}

func TestExpiryAndDisable(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	exp := now.Add(time.Hour)
	mustCreate(t, s, CreateParams{Slug: "e", Target: "https://example.com", ExpiresAt: &exp}, now)
	mustCreate(t, s, CreateParams{Slug: "d", Target: "https://example.com"}, now)

	if _, st, _ := s.Resolve("e", Visit{Now: now}); st != stateActive {
		t.Fatalf("before expiry state = %v, want active", st)
	}
	if _, st, _ := s.Resolve("e", Visit{Now: now.Add(2 * time.Hour)}); st != stateExpired {
		t.Fatalf("after expiry state = %v, want expired", st)
	}
	// The boundary itself is expired: expiry is inclusive of "now".
	if _, st, _ := s.Resolve("e", Visit{Now: exp}); st != stateExpired {
		t.Fatalf("at exact expiry state = %v, want expired", st)
	}

	if err := s.SetDisabled("d", true); err != nil {
		t.Fatal(err)
	}
	if _, st, _ := s.Resolve("d", Visit{Now: now}); st != stateDisabled {
		t.Fatalf("disabled state = %v", st)
	}
	if err := s.SetDisabled("d", false); err != nil {
		t.Fatal(err)
	}
	if _, st, _ := s.Resolve("d", Visit{Now: now}); st != stateActive {
		t.Fatalf("re-enabled state = %v, want active", st)
	}
}

func TestDeleteRemovesFromDiskToo(t *testing.T) {
	s, dir := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "bye", Target: "https://example.com/secret-path"}, now)
	if err := s.Delete("bye"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("bye"); ok {
		t.Fatal("deleted link is still readable")
	}
	if err := s.Delete("bye"); !errors.Is(err, errNotFound) {
		t.Fatalf("second delete err = %v, want errNotFound", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "stub.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-path") {
		t.Fatal("deleted link is still on disk")
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get("bye"); ok {
		t.Fatal("deleted link came back after a restart")
	}
}

func TestDayCountersArePruned(t *testing.T) {
	s, _ := newTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	mustCreate(t, s, CreateParams{Slug: "r", Target: "https://example.com"}, base)
	for i := 0; i < dayRetention+40; i++ {
		s.Resolve("r", Visit{IP: "1.1.1.1", UA: "Mozilla/5.0", Now: base.AddDate(0, 0, i)})
	}
	l, _ := s.Get("r")
	if len(l.Days) > dayRetention+1 {
		t.Fatalf("kept %d day buckets, retention is %d", len(l.Days), dayRetention)
	}
	if l.Clicks != dayRetention+40 {
		t.Fatalf("total clicks = %d, want %d — pruning must not lose the total", l.Clicks, dayRetention+40)
	}
}

func TestReferrerBucketsAreCapped(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	mustCreate(t, s, CreateParams{Slug: "c", Target: "https://example.com"}, now)
	for i := 0; i < maxReferrerBuckets+50; i++ {
		s.Resolve("c", Visit{IP: "1.1.1.1", UA: "Mozilla/5.0", Referer: "https://r" + itoa(i) + ".example/", Now: now})
	}
	l, _ := s.Get("c")
	if len(l.Referrers) > maxReferrerBuckets+1 {
		t.Fatalf("%d referrer buckets, cap is %d", len(l.Referrers), maxReferrerBuckets)
	}
	if l.Referrers[otherReferrer] == 0 {
		t.Fatal("overflow referrers were dropped instead of being bucketed as (other)")
	}
	total := 0
	for _, v := range l.Referrers {
		total += v
	}
	if total != maxReferrerBuckets+50 {
		t.Fatalf("referrer counts sum to %d, want %d", total, maxReferrerBuckets+50)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestResolveMissingSlug(t *testing.T) {
	s, _ := newTestStore(t)
	if _, _, found := s.Resolve("nope", Visit{Now: time.Now()}); found {
		t.Fatal("Resolve found a slug that was never created")
	}
}

func TestOpenStoreRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stub.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("OpenStore accepted a corrupt file; it must refuse rather than silently start empty")
	}
}
