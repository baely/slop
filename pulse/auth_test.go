package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenComparison(t *testing.T) {
	a := NewAuth("s3cret-token")
	cases := []struct {
		in   string
		want bool
	}{
		{"s3cret-token", true},
		{"s3cret-toke", false},
		{"s3cret-tokenn", false},
		{"", false},
		{"S3CRET-TOKEN", false},
	}
	for _, tc := range cases {
		if got := a.TokenOK(tc.in); got != tc.want {
			t.Fatalf("TokenOK(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSessionCookieIsNotTheToken(t *testing.T) {
	a := NewAuth("s3cret-token")
	v := a.NewSessionValue()
	if strings.Contains(v, "s3cret-token") {
		t.Fatalf("session value leaks the token: %q", v)
	}
	sid, mac, ok := strings.Cut(v, ".")
	if !ok || len(sid) != 32 || len(mac) != 64 {
		t.Fatalf("session value has the wrong shape: %q", v)
	}
	if !a.SessionOK(v) {
		t.Fatal("freshly minted session did not verify")
	}
	for _, bad := range []string{"", "nodot", sid + ".", "." + mac, sid + "." + strings.Repeat("0", 64), "x" + v} {
		if a.SessionOK(bad) {
			t.Fatalf("forged session accepted: %q", bad)
		}
	}
	// A session minted under a different token must not verify.
	other := NewAuth("another-token")
	if a.SessionOK(other.NewSessionValue()) {
		t.Fatal("session from a different token verified")
	}
}

func TestFailureRateLimit(t *testing.T) {
	a := NewAuth("tok")
	base := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	now := base
	a.now = func() time.Time { return now }

	for i := 0; i < failLimit-1; i++ {
		a.Fail("203.0.113.9")
	}
	if a.Throttled("203.0.113.9") {
		t.Fatalf("throttled after %d failures, the limit is %d", failLimit-1, failLimit)
	}
	a.Fail("203.0.113.9")
	if !a.Throttled("203.0.113.9") {
		t.Fatalf("not throttled after %d failures", failLimit)
	}
	if a.Throttled("198.51.100.4") {
		t.Fatal("throttling leaked to another address")
	}
	now = base.Add(2 * time.Minute)
	if a.Throttled("203.0.113.9") {
		t.Fatal("throttle did not expire after the window")
	}
}

func TestAuthedAcceptsBearerAndCookie(t *testing.T) {
	a := NewAuth("tok")
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if a.Authed(r) {
		t.Fatal("bare request considered authed")
	}
	r.Header.Set("Authorization", "Bearer tok")
	if !a.Authed(r) {
		t.Fatal("correct bearer token rejected")
	}
	r.Header.Set("Authorization", "Bearer nope")
	if a.Authed(r) {
		t.Fatal("wrong bearer token accepted")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: a.NewSessionValue()})
	if !a.Authed(r2) {
		t.Fatal("valid session cookie rejected")
	}
	r3 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r3.AddCookie(&http.Cookie{Name: sessionCookie, Value: "forged.deadbeef"})
	if a.Authed(r3) {
		t.Fatal("forged session cookie accepted")
	}
}

func TestClientIPAndTruncation(t *testing.T) {
	cases := []struct {
		name  string
		xff   string
		raddr string
		ip    string
		trunc string
	}{
		{name: "no header", raddr: "203.0.113.7:5321", ip: "203.0.113.7", trunc: "203.0.113.0"},
		{name: "traefik hop wins", xff: "1.2.3.4, 198.51.100.22", raddr: "10.0.0.1:5321", ip: "198.51.100.22", trunc: "198.51.100.0"},
		{name: "single hop", xff: "198.51.100.22", raddr: "10.0.0.1:5321", ip: "198.51.100.22", trunc: "198.51.100.0"},
		{name: "garbage falls back", xff: "not-an-ip", raddr: "203.0.113.7:5321", ip: "203.0.113.7", trunc: "203.0.113.0"},
		{name: "ipv6", raddr: "[2001:db8:1234:5678::1]:5321", ip: "2001:db8:1234:5678::1", trunc: "2001:db8:1234::/48"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.raddr
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(r); got != tc.ip {
				t.Fatalf("clientIP = %q, want %q", got, tc.ip)
			}
			if got := truncIP(tc.ip); got != tc.trunc {
				t.Fatalf("truncIP(%q) = %q, want %q", tc.ip, got, tc.trunc)
			}
		})
	}
}
