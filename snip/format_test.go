package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestParseExpiry(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"10m", 10 * time.Minute, false},
		{"1h", time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"never", 0, false},
		{"", 7 * 24 * time.Hour, false}, // default
		{"1y", 0, true},
		{"forever", 0, true},
		{"0", 0, true},
	}
	for _, c := range cases {
		got, err := ParseExpiry(c.in)
		if c.wantErr {
			if !errors.Is(err, errBadExpiry) {
				t.Errorf("ParseExpiry(%q) err = %v, want errBadExpiry", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseExpiry(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseExpiry(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0 bytes"},
		{1, "1 byte"},
		{999, "999 bytes"},
		{1000, "1.0 kB"},
		{12345, "12.3 kB"},
		{999999, "1000.0 kB"},
		{1000000, "1.00 MB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanAgo(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "just now"},
		{30 * time.Second, "just now"},
		{60 * time.Second, "a minute ago"},
		{4 * time.Minute, "4 minutes ago"},
		{90 * time.Minute, "an hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{30 * time.Hour, "yesterday"},
		{5 * 24 * time.Hour, "5 days ago"},
		{21 * 24 * time.Hour, "3 weeks ago"},
		{90 * 24 * time.Hour, "3 months ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, c := range cases {
		if got := HumanAgo(now.Add(-c.ago), now); got != c.want {
			t.Errorf("HumanAgo(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := HumanAgo(now.Add(time.Hour), now); got != "just now" {
		t.Errorf("a future timestamp should clamp, got %q", got)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Hour, "0 seconds"},
		{9 * time.Second, "9 seconds"},
		{70 * time.Second, "a minute"},
		{9 * time.Minute, "9 minutes"},
		{75 * time.Minute, "an hour"},
		{5 * time.Hour, "5 hours"},
		// A freshly created paste must read back as the expiry that was picked,
		// not one unit short of it.
		{24 * time.Hour, "a day"},
		{24*time.Hour - time.Millisecond, "a day"},
		{7*24*time.Hour - time.Millisecond, "7 days"},
		{30*24*time.Hour - time.Millisecond, "30 days"},
		{3 * 24 * time.Hour, "3 days"},
		{90 * 24 * time.Hour, "3 months"},
	}
	for _, c := range cases {
		if got := HumanDuration(c.in); got != c.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExpiryLine(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		p    Paste
		want string
	}{
		{"never", Paste{}, "Expires never."},
		{"in three days", Paste{Expires: now.Add(3 * 24 * time.Hour)}, "Expires in 3 days."},
		{"in ten minutes", Paste{Expires: now.Add(10 * time.Minute)}, "Expires in 10 minutes."},
		{"burn only", Paste{Burn: true}, "Burns after reading."},
		{"burn with expiry", Paste{Burn: true, Expires: now.Add(time.Hour)}, "Burns after reading. Expires in an hour."},
	}
	for _, c := range cases {
		if got := ExpiryLine(c.p, now); got != c.want {
			t.Errorf("%s: ExpiryLine = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{""}},
		{"single", "one", []string{"one"}},
		{"trailing newline dropped", "one\n", []string{"one"}},
		{"two lines", "one\ntwo", []string{"one", "two"}},
		{"blank line kept", "one\n\ntwo\n", []string{"one", "", "two"}},
		{"crlf normalised", "one\r\ntwo\r\n", []string{"one", "two"}},
		{"only newlines", "\n\n", []string{"", ""}},
	}
	for _, c := range cases {
		if got := SplitLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: SplitLines(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Notes  ", "Notes"},
		{"line\nbreak", "line break"},
		{"nul\x00byte", "nulbyte"},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanTitle(c.in); got != c.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := make([]rune, 500)
	for i := range long {
		long[i] = 'x'
	}
	if got := cleanTitle(string(long)); len([]rune(got)) != maxTitleRunes {
		t.Errorf("long title trimmed to %d runes, want %d", len([]rune(got)), maxTitleRunes)
	}
}

func TestIsTrue(t *testing.T) {
	for _, s := range []string{"1", "true", "TRUE", "yes", "on", " 1 "} {
		if !isTrue(s) {
			t.Errorf("isTrue(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "0", "false", "no", "off", "maybe"} {
		if isTrue(s) {
			t.Errorf("isTrue(%q) = true, want false", s)
		}
	}
}
