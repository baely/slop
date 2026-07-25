package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s, dir
}

func TestNewIDShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := NewID(binIDLen)
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != binIDLen {
			t.Fatalf("len(%q) = %d, want %d", id, len(id), binIDLen)
		}
		for _, c := range id {
			if !strings.ContainsRune(idAlphabet, c) {
				t.Fatalf("id %q contains %q, outside the alphabet", id, c)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d draws", id, i)
		}
		seen[id] = true
	}
	// 20 chars over a 32-symbol alphabet is 100 bits.
	if binIDLen*5 < 96 {
		t.Fatalf("bin ids carry %d bits, want at least 96", binIDLen*5)
	}
}

func TestValidID(t *testing.T) {
	good, _ := NewID(binIDLen)
	cases := []struct {
		id   string
		want bool
	}{
		{good, true},
		{"", false},
		{"short", false},
		{strings.Repeat("a", binIDLen-1), false},
		{strings.Repeat("a", binIDLen+1), false},
		{strings.Repeat("i", binIDLen), false}, // ambiguous char, not in alphabet
		{strings.Repeat("A", binIDLen), false}, // uppercase
		{"../../etc/passwd0000", false},
	}
	for _, c := range cases {
		if got := validID(c.id); got != c.want {
			t.Errorf("validID(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestCreateAndGet(t *testing.T) {
	s, _ := testStore(t)
	now := time.Now()
	bin, err := s.Create("bssid-reporter", 202, now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if bin.Status != 202 || bin.Label != "bssid-reporter" {
		t.Fatalf("bin = %+v", bin)
	}
	got, ok := s.Get(bin.ID)
	if !ok {
		t.Fatal("Get: bin missing")
	}
	if got.ID != bin.ID {
		t.Fatalf("Get id = %q, want %q", got.ID, bin.ID)
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get: unknown id returned a bin")
	}
	// Out-of-range statuses fall back to 200 rather than erroring.
	b2, err := s.Create("", 9000, now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b2.Status != 200 {
		t.Fatalf("status = %d, want 200", b2.Status)
	}
}

func TestRetentionCap(t *testing.T) {
	s, _ := testStore(t)
	base := time.Now()
	bin, err := s.Create("cap", 200, base)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < maxRequestsPerBin+15; i++ {
		id, _ := NewID(reqIDLen)
		if _, err := s.Capture(bin.ID, &Request{
			ID: id, At: base.Add(time.Duration(i) * time.Second), Method: "POST",
			SubPath: "/" + id,
		}); err != nil {
			t.Fatalf("Capture: %v", err)
		}
	}
	got, _ := s.Get(bin.ID)
	if len(got.Requests) != maxRequestsPerBin {
		t.Fatalf("held %d requests, want %d", len(got.Requests), maxRequestsPerBin)
	}
	// Newest first, and the oldest 15 are gone.
	for i := 1; i < len(got.Requests); i++ {
		if got.Requests[i-1].At.Before(got.Requests[i].At) {
			t.Fatalf("requests not newest-first at index %d", i)
		}
	}
	if !got.Requests[0].At.Equal(base.Add(time.Duration(maxRequestsPerBin+14) * time.Second)) {
		t.Fatalf("newest request is %v", got.Requests[0].At)
	}
}

func TestCaptureUnknownBin(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.Capture("missing", &Request{ID: "x"}); err != errNotFound {
		t.Fatalf("Capture into missing bin: err = %v, want errNotFound", err)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	s, dir := testStore(t)
	now := time.Now().Truncate(time.Millisecond)
	bin, err := s.Create("traccar", 204, now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := []byte("{\"id\":\"860906\",\"lat\":-37.81}")
	if _, err := s.Capture(bin.ID, &Request{
		ID: "req0001", At: now, Method: "PUT", Path: "/b/" + bin.ID + "/hit",
		SubPath: "/hit", Query: "a=1&b=2",
		Headers: []Header{{Name: "Content-Type", Values: []string{"application/json"}}},
		Body:    body, BodySize: int64(len(body)), RemoteIP: "203.0.113.9",
		ContentType: "application/json",
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// No temp files left behind by the atomic write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	files := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
		files++
	}
	if files != 1 {
		t.Fatalf("wrote %d files, want 1", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin-"+bin.ID+".json")); err != nil {
		t.Fatalf("bin file: %v", err)
	}

	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get(bin.ID)
	if !ok {
		t.Fatal("bin did not survive the restart")
	}
	if got.Label != "traccar" || got.Status != 204 {
		t.Fatalf("bin = %+v", got)
	}
	if len(got.Requests) != 1 {
		t.Fatalf("held %d requests, want 1", len(got.Requests))
	}
	r := got.Requests[0]
	if string(r.Body) != string(body) {
		t.Fatalf("body = %q, want %q", r.Body, body)
	}
	if r.Method != "PUT" || r.SubPath != "/hit" || r.Query != "a=1&b=2" || r.RemoteIP != "203.0.113.9" {
		t.Fatalf("request = %+v", r)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "Content-Type" {
		t.Fatalf("headers = %+v", r.Headers)
	}
	if !r.At.Equal(now) {
		t.Fatalf("timestamp = %v, want %v", r.At, now)
	}
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := writeFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writeFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files in dir, want 1", len(entries))
	}
}

func TestSweepExpiry(t *testing.T) {
	s, dir := testStore(t)
	now := time.Now()
	cases := []struct {
		name    string
		age     time.Duration
		survive bool
	}{
		{"fresh", 0, true},
		{"29 days", 29 * 24 * time.Hour, true},
		{"just under ttl", binTTL - time.Minute, true},
		{"just over ttl", binTTL + time.Minute, false},
		{"ancient", 400 * 24 * time.Hour, false},
	}
	ids := map[string]string{}
	for _, c := range cases {
		b, err := s.Create(c.name, 200, now.Add(-c.age))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids[c.name] = b.ID
	}
	swept := s.Sweep(now)
	if swept != 2 {
		t.Fatalf("swept %d bins, want 2", swept)
	}
	for _, c := range cases {
		_, ok := s.Get(ids[c.name])
		if ok != c.survive {
			t.Errorf("%s: present = %v, want %v", c.name, ok, c.survive)
		}
		_, err := os.Stat(filepath.Join(dir, "bin-"+ids[c.name]+".json"))
		if (err == nil) != c.survive {
			t.Errorf("%s: file present = %v, want %v", c.name, err == nil, c.survive)
		}
	}
}

func TestTouchKeepsBinAlive(t *testing.T) {
	s, _ := testStore(t)
	now := time.Now()
	b, _ := s.Create("kept", 200, now.Add(-29*24*time.Hour))
	s.Touch(b.ID, now)
	if n := s.Sweep(now); n != 0 {
		t.Fatalf("swept %d bins after a touch, want 0", n)
	}
	got, _ := s.Get(b.ID)
	if !got.Expires().After(now.Add(29 * 24 * time.Hour)) {
		t.Fatalf("expiry not extended: %v", got.Expires())
	}
}

func TestClearAndDelete(t *testing.T) {
	s, dir := testStore(t)
	now := time.Now()
	b, _ := s.Create("clearme", 200, now)
	if _, err := s.Capture(b.ID, &Request{ID: "r1", At: now, Method: "GET"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if err := s.Clear(b.ID, now); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, _ := s.Get(b.ID)
	if len(got.Requests) != 0 {
		t.Fatalf("held %d requests after Clear", len(got.Requests))
	}
	if !got.LastSeen.IsZero() {
		t.Fatalf("LastSeen = %v after Clear, want zero", got.LastSeen)
	}
	if err := s.Clear("missing", now); err != errNotFound {
		t.Fatalf("Clear missing: %v", err)
	}

	if err := s.Delete(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get(b.ID); ok {
		t.Fatal("bin still present after Delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "bin-"+b.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("bin file still on disk: %v", err)
	}
	if err := s.Delete(b.ID); err != errNotFound {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestUpdate(t *testing.T) {
	s, _ := testStore(t)
	now := time.Now()
	b, _ := s.Create("old", 200, now)
	if err := s.Update(b.ID, "new", 418, now); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := s.Get(b.ID)
	if got.Label != "new" || got.Status != 418 {
		t.Fatalf("bin = %+v", got)
	}
	if err := s.Update(b.ID, "x", 99, now); err == nil {
		t.Fatal("Update accepted status 99")
	}
	if err := s.Update("missing", "x", 200, now); err != errNotFound {
		t.Fatalf("Update missing: %v", err)
	}
}

func TestStats(t *testing.T) {
	s, _ := testStore(t)
	now := time.Now()
	a, _ := s.Create("a", 200, now)
	if _, err := s.Create("b", 200, now); err != nil { // second bin stays empty
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < 3; i++ {
		id, _ := NewID(reqIDLen)
		if _, err := s.Capture(a.ID, &Request{ID: id, At: now}); err != nil {
			t.Fatalf("Capture: %v", err)
		}
	}
	bins, reqs := s.Stats()
	if bins != 2 || reqs != 3 {
		t.Fatalf("Stats = %d bins, %d requests; want 2, 3", bins, reqs)
	}
}
