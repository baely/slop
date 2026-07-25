package main

import (
	"strings"
	"testing"
	"time"
)

func TestCheckToken(t *testing.T) {
	a := newAuthenticator("s3cret-token")
	tests := []struct {
		candidate string
		want      bool
	}{
		{"s3cret-token", true},
		{"", false},
		{"s3cret-toke", false},
		{"s3cret-tokenn", false},
		{"S3CRET-TOKEN", false},
		{" s3cret-token", false},
		{"wrong", false},
	}
	for _, tc := range tests {
		if got := a.checkToken(tc.candidate); got != tc.want {
			t.Errorf("checkToken(%q) = %v, want %v", tc.candidate, got, tc.want)
		}
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	a := newAuthenticator("token-a")
	v, err := a.newSessionValue()
	if err != nil {
		t.Fatal(err)
	}
	sid, ok := a.parseSessionValue(v)
	if !ok {
		t.Fatalf("freshly minted session %q did not verify", v)
	}
	if len(sid) != sessionIDLen {
		t.Fatalf("session id length = %d, want %d", len(sid), sessionIDLen)
	}
	if strings.Contains(v, "token-a") {
		t.Fatal("the cookie contains the token itself")
	}
}

func TestSessionCookieRejectsTampering(t *testing.T) {
	a := newAuthenticator("token-a")
	v, _ := a.newSessionValue()
	parts := strings.SplitN(v, ".", 2)
	sid, sig := parts[0], parts[1]

	bad := []string{
		"",
		sid,
		sid + ".",
		"." + sig,
		sid + ".deadbeef",
		"aaaaaaaaaaaaaaaaaaaa." + sig, // different sid, original signature
		sid + "x." + sig,
	}
	for _, v := range bad {
		if _, ok := a.parseSessionValue(v); ok {
			t.Errorf("parseSessionValue(%q) accepted a forged cookie", v)
		}
	}
}

func TestRotatingTheTokenInvalidatesSessions(t *testing.T) {
	a := newAuthenticator("token-a")
	v, _ := a.newSessionValue()
	b := newAuthenticator("token-b")
	if _, ok := b.parseSessionValue(v); ok {
		t.Fatal("a session minted under the old token still verifies after rotation")
	}
}

func TestCSRFTokenIsBoundToSession(t *testing.T) {
	a := newAuthenticator("token-a")
	c1 := a.csrfToken("session-one")
	c2 := a.csrfToken("session-two")
	if c1 == c2 {
		t.Fatal("csrf token is not bound to the session")
	}
	if !a.checkCSRF("session-one", c1) {
		t.Fatal("matching csrf token was rejected")
	}
	if a.checkCSRF("session-one", c2) {
		t.Fatal("another session's csrf token was accepted")
	}
	if a.checkCSRF("session-one", "") || a.checkCSRF("", c1) {
		t.Fatal("empty csrf inputs were accepted")
	}
}

func TestRateLimitBlocksBruteForce(t *testing.T) {
	a := newAuthenticator("token-a")
	now := time.Now()
	const ip = "198.51.100.20"
	for i := 0; i < maxFailures; i++ {
		if !a.allow(ip, now) {
			t.Fatalf("blocked after only %d failures, expected %d", i, maxFailures)
		}
		a.fail(ip, now)
	}
	if a.allow(ip, now) {
		t.Fatalf("still allowed after %d failures", maxFailures)
	}
	// Another address is unaffected.
	if !a.allow("198.51.100.21", now) {
		t.Fatal("rate limit leaked across client addresses")
	}
	// And the window expires.
	if !a.allow(ip, now.Add(failureWindow+time.Second)) {
		t.Fatal("rate limit never expires")
	}
}
