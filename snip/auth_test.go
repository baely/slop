package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "s3cret-token-for-tests-0123456789"

func TestCheckToken(t *testing.T) {
	a := NewAuth(testToken)
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"exact", testToken, true},
		{"empty", "", false},
		{"prefix", testToken[:len(testToken)-1], false},
		{"suffix added", testToken + "x", false},
		{"case changed", strings.ToUpper(testToken), false},
		{"whitespace", " " + testToken, false},
	}
	for _, c := range cases {
		if got := a.CheckToken(c.in); got != c.want {
			t.Errorf("%s: CheckToken = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckBearer(t *testing.T) {
	a := NewAuth(testToken)
	cases := []struct {
		name   string
		header string
		want   authResult
	}{
		{"absent", "", authNone},
		{"correct", "Bearer " + testToken, authOK},
		{"lowercase scheme", "bearer " + testToken, authOK},
		{"wrong token", "Bearer nope", authBad},
		{"no scheme", testToken, authBad},
		{"basic auth", "Basic " + testToken, authBad},
	}
	for _, c := range cases {
		a.rl = newRateLimiter(10, time.Minute) // isolate from the rate limit
		r := httptest.NewRequest(http.MethodGet, "/pastes", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		if got := a.Check(r); got != c.want {
			t.Errorf("%s: Check = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	a := NewAuth(testToken)
	sess, err := a.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sess, testToken) {
		t.Fatal("session value contains the token itself")
	}
	if !a.checkSession(sess) {
		t.Fatal("freshly minted session did not verify")
	}

	id, _, _ := strings.Cut(sess, ".")
	bad := []string{
		"",
		"garbage",
		id,                     // no mac
		id + ".",               // empty mac
		id + ".deadbeef",       // wrong mac
		"." + a.sign(id),       // empty id
		id + "x." + a.sign(id), // tampered id
	}
	for _, v := range bad {
		if a.checkSession(v) {
			t.Errorf("checkSession(%q) accepted a forged value", v)
		}
	}

	// A session minted under a different token must not verify.
	other := NewAuth("a-completely-different-token")
	if other.checkSession(sess) {
		t.Error("session verified under the wrong token")
	}
}

func TestSessionCookieAuthenticates(t *testing.T) {
	a := NewAuth(testToken)
	sess, err := a.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/pastes", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	if got := a.Check(r); got != authOK {
		t.Errorf("Check with a valid session = %v, want authOK", got)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/pastes", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess + "x"})
	if got := a.Check(r2); got != authBad {
		t.Errorf("Check with a tampered session = %v, want authBad", got)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	a := NewAuth(testToken)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	a.SetSessionCookie(w, r, "abc.def")

	res := w.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if !c.Secure {
		t.Error("cookie is not Secure behind https")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q", c.Path)
	}
}

func TestRateLimitBlocksBruteForce(t *testing.T) {
	a := NewAuth(testToken)
	base := time.Now()
	a.now = func() time.Time { return base }

	newAttempt := func(tok string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/pastes", nil)
		r.RemoteAddr = "198.51.100.7:5000"
		r.Header.Set("Authorization", "Bearer "+tok)
		return r
	}

	for i := 0; i < 10; i++ {
		if got := a.Check(newAttempt("wrong")); got != authBad {
			t.Fatalf("attempt %d = %v, want authBad", i, got)
		}
	}
	if got := a.Check(newAttempt("wrong")); got != authThrottled {
		t.Fatalf("11th attempt = %v, want authThrottled", got)
	}
	// Even the correct token is refused while throttled.
	if got := a.Check(newAttempt(testToken)); got != authThrottled {
		t.Fatalf("correct token while throttled = %v, want authThrottled", got)
	}
	// A different client is unaffected.
	other := newAttempt(testToken)
	other.RemoteAddr = "203.0.113.9:5000"
	if got := a.Check(other); got != authOK {
		t.Fatalf("other client = %v, want authOK", got)
	}
	// The window slides.
	a.now = func() time.Time { return base.Add(61 * time.Second) }
	if got := a.Check(newAttempt(testToken)); got != authOK {
		t.Fatalf("after the window = %v, want authOK", got)
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		xff  string
		addr string
		want string
	}{
		{"no proxy", "", "192.0.2.44:1234", "192.0.2.44"},
		{"one proxy hop", "203.0.113.9", "10.0.0.2:1234", "203.0.113.9"},
		// Only the hop Traefik appended is trusted: a client-supplied prefix
		// must not be able to spoof the address we rate limit on.
		{"spoofed prefix", "1.2.3.4, 203.0.113.9", "10.0.0.2:1234", "203.0.113.9"},
		{"padded", " 203.0.113.9 ", "10.0.0.2:1234", "203.0.113.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.addr
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r); got != c.want {
			t.Errorf("%s: ClientIP = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTruncateIP(t *testing.T) {
	cases := []struct{ in, want string }{
		{"203.0.113.9", "203.0.113.0/24"},
		{"10.0.0.255", "10.0.0.0/24"},
		{"2001:db8:1234:5678::1", "2001:db8:1234::/48"},
		{"not-an-ip", "unknown"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := TruncateIP(c.in); got != c.want {
			t.Errorf("TruncateIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
