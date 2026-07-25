package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenMatch(t *testing.T) {
	a := NewAuth("correct-horse-battery-staple")
	tests := []struct {
		name string
		try  string
		want bool
	}{
		{"exact", "correct-horse-battery-staple", true},
		{"empty", "", false},
		{"prefix", "correct-horse", false},
		{"extra", "correct-horse-battery-staple ", false},
		{"case", "Correct-Horse-Battery-Staple", false},
	}
	for _, tc := range tests {
		if got := a.tokenMatches(tc.try); got != tc.want {
			t.Errorf("%s: tokenMatches(%q) = %v, want %v", tc.name, tc.try, got, tc.want)
		}
	}
}

func TestSessionCookieIsNotTheToken(t *testing.T) {
	a := NewAuth("s3cret")
	sess, err := a.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if !a.validSession(sess) {
		t.Fatal("freshly minted session rejected")
	}
	if sess == "s3cret" || len(sess) < 40 {
		t.Fatalf("session value looks wrong: %q", sess)
	}
	for _, bad := range []string{"", "nodot", "id.", ".mac", "id.deadbeef", sess + "x"} {
		if a.validSession(bad) {
			t.Errorf("accepted forged session %q", bad)
		}
	}
	// A session minted under one token must not validate under another: the
	// MAC is keyed by the token, so rotating it logs everyone out.
	other := NewAuth("different")
	if other.validSession(sess) {
		t.Error("session survived a token change")
	}
}

func TestAuthedAcceptsBearerAndCookie(t *testing.T) {
	a := NewAuth("tok")
	sess, err := a.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	bearer := httptest.NewRequest(http.MethodGet, "/", nil)
	bearer.Header.Set("Authorization", "Bearer tok")
	if !a.Authed(bearer) {
		t.Error("bearer token rejected")
	}

	wrong := httptest.NewRequest(http.MethodGet, "/", nil)
	wrong.Header.Set("Authorization", "Bearer nope")
	if a.Authed(wrong) {
		t.Error("wrong bearer token accepted")
	}

	basic := httptest.NewRequest(http.MethodGet, "/", nil)
	basic.Header.Set("Authorization", "Basic tok")
	if a.Authed(basic) {
		t.Error("non-bearer scheme accepted")
	}

	cookied := httptest.NewRequest(http.MethodGet, "/", nil)
	cookied.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	if !a.Authed(cookied) {
		t.Error("valid session cookie rejected")
	}

	forged := httptest.NewRequest(http.MethodGet, "/", nil)
	forged.AddCookie(&http.Cookie{Name: sessionCookie, Value: "abc.def"})
	if a.Authed(forged) {
		t.Error("forged session cookie accepted")
	}

	none := httptest.NewRequest(http.MethodGet, "/", nil)
	if a.Authed(none) {
		t.Error("anonymous request accepted")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	a := NewAuth("tok")
	rec := httptest.NewRecorder()
	a.SetSessionCookie(rec, "value")
	res := rec.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("cookie flags wrong: %+v", c)
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(10, time.Minute)
	now := time.Now()
	if l.Blocked("1.2.3.4", now) {
		t.Fatal("blocked before any failure")
	}
	for i := 0; i < 9; i++ {
		if l.Fail("1.2.3.4", now) {
			t.Fatalf("blocked after %d failures", i+1)
		}
	}
	if !l.Fail("1.2.3.4", now) {
		t.Fatal("not blocked after 10 failures")
	}
	if !l.Blocked("1.2.3.4", now) {
		t.Fatal("Blocked disagrees with Fail")
	}
	if l.Blocked("5.6.7.8", now) {
		t.Fatal("a different address was caught in the blast radius")
	}
	if l.Blocked("1.2.3.4", now.Add(61*time.Second)) {
		t.Fatal("window never expires")
	}
}

func TestClientIPAndTruncation(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.9:5555"
	if got := clientIP(r); got != "10.0.0.9" {
		t.Errorf("clientIP = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("first hop not used: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := clientIP(r); got != "10.0.0.9" {
		t.Errorf("garbage XFF should fall back to RemoteAddr, got %q", got)
	}

	tests := map[string]string{
		"203.0.113.7":                            "203.0.113.0/24",
		"2001:db8:1234:5678:9abc:def0:1234:5678": "2001:db8::/32",
		"garbage":                                "?",
	}
	for in, want := range tests {
		if got := truncIP(in); got != want {
			t.Errorf("truncIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIfNoneMatchParsing(t *testing.T) {
	const etag = `"abc123"`
	tests := []struct {
		header string
		want   bool
	}{
		{"", false},
		{`"abc123"`, true},
		{`W/"abc123"`, true},
		{`"other", "abc123"`, true},
		{` "abc123" `, true},
		{"*", true},
		{`"other"`, false},
		{`"abc12"`, false},
	}
	for _, tc := range tests {
		if got := ifNoneMatch(tc.header, etag); got != tc.want {
			t.Errorf("ifNoneMatch(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestSafeNext(t *testing.T) {
	tests := map[string]string{
		"":                    "/",
		"/edit":               "/edit",
		"/edit?flash=x":       "/edit?flash=x",
		"//evil.example.com":  "/",
		"https://evil.test":   "/",
		"javascript:alert(1)": "/",
	}
	for in, want := range tests {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanize(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "just now"},
		{90 * time.Second, "1 minute ago"},
		{20 * time.Minute, "20 minutes ago"},
		{90 * time.Minute, "1 hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{30 * time.Hour, "yesterday"},
		{72 * time.Hour, "3 days ago"},
		{40 * 24 * time.Hour, "5 weeks ago"},
	}
	for _, tc := range tests {
		if got := humanize(tc.d); got != tc.want {
			t.Errorf("humanize(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
