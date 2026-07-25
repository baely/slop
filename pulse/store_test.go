package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTarget(t *testing.T, s *Store, in TargetInput, now time.Time) Target {
	t.Helper()
	tg, err := in.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	tg, err = s.AddTarget(tg, now)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return tg
}

func TestNewIDShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := newID()
		if len(id) != 20 {
			t.Fatalf("id %q has length %d, want 20 (96 bits of base32)", id, len(id))
		}
		for _, r := range id {
			if !strings.ContainsRune(idAlphabet, r) {
				t.Fatalf("id %q contains %q, outside the unambiguous alphabet", id, r)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d draws", id, i)
		}
		seen[id] = true
	}
}

func TestTargetInputValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      TargetInput
		wantErr bool
		check   func(*testing.T, Target)
	}{
		{
			name: "defaults applied",
			in:   TargetInput{Name: "covers", URL: "https://covers.baileys.dev/"},
			check: func(t *testing.T, tg Target) {
				if tg.Interval != 60 || tg.Timeout != 10 || tg.Expect != "2xx" || tg.Method != "GET" {
					t.Fatalf("defaults wrong: %+v", tg)
				}
			},
		},
		{
			name: "private address allowed",
			in:   TargetInput{Name: "registry", URL: "http://192.168.0.82:5000/v2/"},
			check: func(t *testing.T, tg Target) {
				if tg.URL != "http://192.168.0.82:5000/v2/" {
					t.Fatalf("url mangled: %q", tg.URL)
				}
			},
		},
		{name: "no name", in: TargetInput{URL: "https://a.example/"}, wantErr: true},
		{name: "no url", in: TargetInput{Name: "x"}, wantErr: true},
		{name: "bad scheme", in: TargetInput{Name: "x", URL: "ftp://a.example/"}, wantErr: true},
		{name: "file scheme", in: TargetInput{Name: "x", URL: "file:///etc/passwd"}, wantErr: true},
		{name: "no host", in: TargetInput{Name: "x", URL: "https:///path"}, wantErr: true},
		{name: "interval too small", in: TargetInput{Name: "x", URL: "https://a.example/", Interval: "5"}, wantErr: true},
		{name: "interval too large", in: TargetInput{Name: "x", URL: "https://a.example/", Interval: "99999999"}, wantErr: true},
		{name: "timeout above interval", in: TargetInput{Name: "x", URL: "https://a.example/", Interval: "10", Timeout: "30"}, wantErr: true},
		{name: "bad expect", in: TargetInput{Name: "x", URL: "https://a.example/", Expect: "okay"}, wantErr: true},
		{name: "keyword with HEAD", in: TargetInput{Name: "x", URL: "https://a.example/", Method: "HEAD", Keyword: "hi"}, wantErr: true},
		{name: "long name", in: TargetInput{Name: strings.Repeat("n", 65), URL: "https://a.example/"}, wantErr: true},
		{
			name: "explicit values",
			in:   TargetInput{Name: " voyage ", URL: "https://voyage.baileys.dev/", Method: "head", Interval: "300", Timeout: "5", Expect: "200,301"},
			check: func(t *testing.T, tg Target) {
				if tg.Name != "voyage" || tg.Method != "HEAD" || tg.Interval != 300 || tg.Timeout != 5 || tg.Expect != "200,301" {
					t.Fatalf("unexpected: %+v", tg)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg, err := tc.in.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", tg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, tg)
			}
		})
	}
}

func TestPersistenceRoundTripAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	tg := mustTarget(t, s, TargetInput{Name: "index", URL: "https://index.baileys.dev/", Keyword: "bailey"}, now)

	s.Record(tg.ID, Check{At: now.Add(-2 * time.Minute), Status: 200, LatencyMs: 41, OK: true})
	s.Record(tg.ID, Check{At: now.Add(-time.Minute), Status: 0, LatencyMs: 12, Err: "dns: no such host"})
	s.Record(tg.ID, Check{At: now, Status: 200, LatencyMs: 44, OK: true})

	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	// A second save must overwrite cleanly and leave no temp files behind.
	if err := s.Save(); err != nil {
		t.Fatalf("save again: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file %q survived the atomic write", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pulse.json")); err != nil {
		t.Fatalf("state file missing: %v", err)
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := s2.Target(tg.ID)
	if !ok {
		t.Fatal("target did not survive the restart")
	}
	if got.Name != "index" || got.Keyword != "bailey" || got.Interval != 60 {
		t.Fatalf("target changed across restart: %+v", got)
	}
	checks := s2.Checks(tg.ID)
	if len(checks) != 3 {
		t.Fatalf("got %d checks after restart, want 3", len(checks))
	}
	if checks[1].Err != "dns: no such host" || checks[1].OK {
		t.Fatalf("failed check did not round-trip: %+v", checks[1])
	}
	incs := s2.Incidents(tg.ID)
	if len(incs) != 1 || incs[0].Open() {
		t.Fatalf("incident did not round-trip closed: %+v", incs)
	}
}

func TestRetentionCapAndRollup(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tg := mustTarget(t, s, TargetInput{Name: "gpxer", URL: "https://gpxer.baileys.dev/"}, now)

	const total = 600
	for i := 0; i < total; i++ {
		at := now.Add(time.Duration(i-total+1) * time.Minute)
		s.Record(tg.ID, Check{At: at, Status: 200, LatencyMs: 100, OK: i >= 100})
	}
	checks := s.Checks(tg.ID)
	if len(checks) != MaxChecksPerTarget {
		t.Fatalf("kept %d checks, want the cap of %d", len(checks), MaxChecksPerTarget)
	}
	if checks[0].At.After(checks[len(checks)-1].At) {
		t.Fatal("checks are not oldest-first")
	}

	days := s.Days(tg.ID)
	rolled := 0
	for _, d := range days {
		rolled += d.Total
	}
	if rolled != total-MaxChecksPerTarget {
		t.Fatalf("rolled up %d checks, want %d", rolled, total-MaxChecksPerTarget)
	}
	// Rollups and the rolling window must be disjoint: 600 in, 600 accounted for.
	if rolled+len(checks) != total {
		t.Fatalf("accounted for %d checks, want %d", rolled+len(checks), total)
	}

	u := s.Uptime(tg.ID, 24*time.Hour, now)
	if !u.Rolled {
		t.Fatal("expected the 24h window to reach into the day rollups")
	}
	if u.Total != total || u.OK != 500 {
		t.Fatalf("uptime counted %d/%d, want 500/%d", u.OK, u.Total, total)
	}
	if got := fmtPct(u); got != "83.3%" {
		t.Fatalf("uptime reads %q, want 83.3%%", got)
	}
}

func TestUptimeWindowBoundaries(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tg := mustTarget(t, s, TargetInput{Name: "voyage", URL: "https://voyage.baileys.dev/"}, now)

	// 10 failures three days ago, 10 successes in the last hour.
	for i := 0; i < 10; i++ {
		s.Record(tg.ID, Check{At: now.Add(-72*time.Hour + time.Duration(i)*time.Minute), Status: 502, LatencyMs: 9})
	}
	for i := 0; i < 10; i++ {
		s.Record(tg.ID, Check{At: now.Add(-time.Hour + time.Duration(i)*time.Minute), Status: 200, LatencyMs: 30, OK: true})
	}

	u24 := s.Uptime(tg.ID, 24*time.Hour, now)
	if u24.Total != 10 || u24.OK != 10 {
		t.Fatalf("24h window saw %d/%d, want 10/10 — the old failures are outside it", u24.OK, u24.Total)
	}
	if got := fmtPct(u24); got != "100%" {
		t.Fatalf("24h uptime reads %q, want 100%%", got)
	}
	if u24.Rolled {
		t.Fatal("24h window should be exact, nothing was rolled up")
	}

	u7 := s.Uptime(tg.ID, 7*24*time.Hour, now)
	if u7.Total != 20 || u7.OK != 10 {
		t.Fatalf("7d window saw %d/%d, want 10/20", u7.OK, u7.Total)
	}
	if got := fmtPct(u7); got != "50.0%" {
		t.Fatalf("7d uptime reads %q, want 50.0%%", got)
	}

	// A window that excludes everything reports unknown, not 100%.
	u1m := s.Uptime(tg.ID, time.Minute, now)
	if u1m.Known {
		t.Fatalf("1m window claims to know something: %+v", u1m)
	}
	if got := fmtPct(u1m); got != "—" {
		t.Fatalf("unknown uptime reads %q, want an em dash", got)
	}
}

func TestUnknownTargetIsNotOneHundredPercent(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tg := mustTarget(t, s, TargetInput{Name: "fresh", URL: "https://fresh.example/"}, now)
	u := s.Uptime(tg.ID, 24*time.Hour, now)
	if u.Known || fmtPct(u) != "—" {
		t.Fatalf("a never-checked target reported %q (%+v)", fmtPct(u), u)
	}
}

func TestDeleteTargetRemovesHistory(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tg := mustTarget(t, s, TargetInput{Name: "gone", URL: "https://gone.example/"}, now)
	s.Record(tg.ID, Check{At: now, Status: 500, LatencyMs: 3})
	if err := s.DeleteTarget(tg.ID); err != nil {
		t.Fatal(err)
	}
	if s.Count() != 0 {
		t.Fatalf("count is %d after delete", s.Count())
	}
	if got := s.AllIncidents(10); len(got) != 0 {
		t.Fatalf("incidents survived the delete: %+v", got)
	}
	if err := s.DeleteTarget(tg.ID); err == nil {
		t.Fatal("deleting twice should fail")
	}
}

func TestDueReservesTargets(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	mustTarget(t, s, TargetInput{Name: "a", URL: "https://a.example/", Interval: "60"}, now)

	if got := s.Due(now); len(got) != 1 {
		t.Fatalf("first pass returned %d due targets, want 1", len(got))
	}
	if got := s.Due(now.Add(time.Second)); len(got) != 0 {
		t.Fatalf("target was dispatched twice inside its interval: %d", len(got))
	}
	if got := s.Due(now.Add(61 * time.Second)); len(got) != 1 {
		t.Fatalf("target was not due again after its interval: %d", len(got))
	}
}
