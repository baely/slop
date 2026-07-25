package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const testToken = "s3cret-token-for-tests-0123456789"

func TestCheckToken(t *testing.T) {
	a := NewAuth(testToken)
	cases := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"exact", testToken, true},
		{"empty", "", false},
		{"wrong", "nope", false},
		{"prefix", testToken[:len(testToken)-1], false},
		{"suffix added", testToken + "x", false},
		{"case changed", strings.ToUpper(testToken), false},
		{"whitespace", " " + testToken, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.CheckToken(c.candidate); got != c.want {
				t.Fatalf("CheckToken(%q) = %v, want %v", c.candidate, got, c.want)
			}
		})
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := NewAuth(testToken)
	value, err := a.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if !a.ValidSession(value) {
		t.Fatal("freshly minted session rejected")
	}
	// The cookie must not be the token, nor contain it.
	if strings.Contains(value, testToken) {
		t.Fatal("cookie value leaks the token")
	}

	sid, mac, _ := strings.Cut(value, ".")
	bad := []string{
		"",
		sid,
		sid + ".",
		"." + mac,
		sid + "." + strings.Repeat("0", len(mac)),
		sid + "x." + mac,
		sid + "." + mac[:len(mac)-1],
	}
	for _, v := range bad {
		if a.ValidSession(v) {
			t.Fatalf("forged session %q accepted", v)
		}
	}

	// A session minted under a different token must not verify.
	other := NewAuth("a-different-token")
	if other.ValidSession(value) {
		t.Fatal("session verified under the wrong token")
	}
}

func TestRateLimit(t *testing.T) {
	a := NewAuth(testToken)
	now := time.Now()
	for i := 0; i < maxFailures; i++ {
		if !a.Allow("198.51.100.7", now) {
			t.Fatalf("blocked after %d failures, want %d", i, maxFailures)
		}
		a.Fail("198.51.100.7", now)
	}
	if a.Allow("198.51.100.7", now) {
		t.Fatalf("still allowed after %d failures", maxFailures)
	}
	if !a.Allow("198.51.100.8", now) {
		t.Fatal("a different IP was caught in the limit")
	}
	if !a.Allow("198.51.100.7", now.Add(failWindow+time.Second)) {
		t.Fatal("limit did not lapse after the window")
	}
	// A successful login clears the counter.
	a.Fail("198.51.100.9", now)
	a.Reset("198.51.100.9")
	if !a.Allow("198.51.100.9", now) {
		t.Fatal("Reset did not clear the counter")
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header string
		want   string
		ok     bool
	}{
		{"", "", false},
		{"Bearer abc", "abc", true},
		{"bearer abc", "abc", true},
		{"BEARER  abc ", "abc", true},
		{"Basic abc", "", false},
		{"Bearer", "", false},
		{"Bearer ", "", false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodGet, "/", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		got, ok := bearerToken(r)
		if got != c.want || ok != c.ok {
			t.Errorf("bearerToken(%q) = (%q, %v), want (%q, %v)", c.header, got, ok, c.want, c.ok)
		}
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"/bin/abc":             "/bin/abc",
		"/bin/abc?x=1":         "/bin/abc?x=1",
		"//evil.example.com":   "",
		"https://evil.example": "",
		"javascript:alert(1)":  "",
		"bin/abc":              "",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		xff  string
		addr string
		want string
	}{
		{"no xff", "", "10.0.0.5:5555", "10.0.0.5"},
		{"single hop", "203.0.113.9", "10.0.0.5:5555", "203.0.113.9"},
		{"first hop only", "203.0.113.9, 10.0.0.1, 172.16.0.1", "10.0.0.5:5555", "203.0.113.9"},
		{"spacing", "  198.51.100.2 ,10.0.0.1", "10.0.0.5:5555", "198.51.100.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.addr
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := clientIP(r); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTruncateIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.9":     "203.0.113.0/24",
		"10.1.2.3":        "10.1.2.0/24",
		"2001:db8:1:2::5": "2001:db8:1::/48",
		"garbage":         "unknown",
	}
	for in, want := range cases {
		if got := truncateIP(in); got != want {
			t.Errorf("truncateIP(%q) = %q, want %q", in, got, want)
		}
	}
}
