package main

import (
	"strings"
	"testing"
	"time"
)

func TestHumanize(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "just now"},
		{3 * time.Second, "just now"},
		{42 * time.Second, "42 seconds ago"},
		{90 * time.Second, "a minute ago"},
		{4 * time.Minute, "4 minutes ago"},
		{90 * time.Minute, "an hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{30 * time.Hour, "yesterday"},
		{3 * 24 * time.Hour, "3 days ago"},
		{21 * 24 * time.Hour, "3 weeks ago"},
		{90 * 24 * time.Hour, "3 months ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, tc := range cases {
		if got := humanize(now.Add(-tc.d), now); got != tc.want {
			t.Fatalf("humanize(-%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	if got := humanize(time.Time{}, now); got != "never" {
		t.Fatalf("zero time = %q, want never", got)
	}
	if got := humanize(now.Add(time.Hour), now); got != "just now" {
		t.Fatalf("future time = %q", got)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{2 * time.Minute, "2m 00s"},
		{135 * time.Second, "2m 15s"},
		{time.Hour + 7*time.Minute, "1h 07m"},
		{50 * time.Hour, "2d 02h"},
	}
	for _, tc := range cases {
		if got := humanDuration(tc.d); got != tc.want {
			t.Fatalf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFmtPctNeverRoundsUpToAHundred(t *testing.T) {
	cases := []struct {
		u    Uptime
		want string
	}{
		{Uptime{}, "—"},
		{Uptime{Total: 10, OK: 10, Known: true}, "100%"},
		{Uptime{Total: 10000, OK: 9999, Known: true}, "99.9%"},   // 99.99 must not read 100
		{Uptime{Total: 100000, OK: 99999, Known: true}, "99.9%"}, // 99.999 must not read 100
		{Uptime{Total: 2, OK: 1, Known: true}, "50.0%"},
		{Uptime{Total: 3, OK: 0, Known: true}, "0.0%"},
	}
	for _, tc := range cases {
		if got := fmtPct(tc.u); got != tc.want {
			t.Fatalf("fmtPct(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}

func TestBuildSpark(t *testing.T) {
	now := time.Now()
	mk := func(ms int, ok bool) Check {
		return Check{At: now, LatencyMs: ms, OK: ok}
	}

	if s := BuildSpark(nil, 60); s.Has {
		t.Fatal("an empty history produced a sparkline")
	}

	s := BuildSpark([]Check{mk(10, true), mk(200, true), mk(50, true)}, 60)
	if !s.Has || s.Count != 3 || s.CountWord != "checks" {
		t.Fatalf("unexpected spark: %+v", s)
	}
	if !s.HasOK {
		t.Fatal("a healthy history should report a usable peak")
	}
	if one := BuildSpark([]Check{mk(10, true)}, 60); one.CountWord != "check" {
		t.Fatalf("singular caption = %q, want \"check\"", one.CountWord)
	}
	if none := BuildSpark([]Check{mk(0, false)}, 60); none.HasOK {
		t.Fatal("a history with no passes must not claim a latency peak")
	}
	if s.MaxMs != 200 {
		t.Fatalf("peak = %d, want 200", s.MaxMs)
	}
	if len(s.Paths) != 1 {
		t.Fatalf("got %d paths, want a single unbroken series", len(s.Paths))
	}
	if !strings.HasPrefix(s.Paths[0], "M") || strings.Count(s.Paths[0], "L") != 2 {
		t.Fatalf("path is malformed: %q", s.Paths[0])
	}
	if len(s.Marks) != 0 || s.Fails != 0 {
		t.Fatalf("healthy history produced failure marks: %+v", s.Marks)
	}

	// Failures break the line and become marks with a word beside them.
	s = BuildSpark([]Check{mk(10, true), mk(0, false), mk(20, true), mk(0, false)}, 60)
	if s.Fails != 2 || len(s.Marks) != 2 {
		t.Fatalf("failures = %d, marks = %d, want 2 and 2", s.Fails, len(s.Marks))
	}
	if s.Window != "2 failed" {
		t.Fatalf("caption = %q, want \"2 failed\"", s.Window)
	}
	if len(s.Paths) != 2 {
		t.Fatalf("got %d path segments, want the line broken at each failure", len(s.Paths))
	}
	// Failure latency must not set the scale.
	s = BuildSpark([]Check{mk(30, true), mk(60000, false)}, 60)
	if s.MaxMs != 30 {
		t.Fatalf("a failed check set the peak to %d ms", s.MaxMs)
	}

	// Only the most recent n are drawn.
	many := make([]Check, 200)
	for i := range many {
		many[i] = mk(i+1, true)
	}
	s = BuildSpark(many, 60)
	if s.Count != 60 {
		t.Fatalf("drew %d points, want the last 60", s.Count)
	}
	if s.MaxMs != 200 {
		t.Fatalf("peak = %d, want the most recent window's 200", s.MaxMs)
	}
}
