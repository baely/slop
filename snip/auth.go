package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "snip_session"

// sessionTTL is how long a browser login lasts.
const sessionTTL = 30 * 24 * time.Hour

type authResult int

const (
	authNone      authResult = iota // no credential presented
	authOK                          // valid token or session
	authBad                         // credential presented and wrong
	authThrottled                   // too many failures from this client
)

// Auth checks the shared token, issues browser sessions and rate limits
// failures per client so the token cannot be ground down.
type Auth struct {
	token []byte
	rl    *rateLimiter
	now   func() time.Time
}

func NewAuth(token string) *Auth {
	return &Auth{
		token: []byte(token),
		rl:    newRateLimiter(10, time.Minute),
		now:   time.Now,
	}
}

// CheckToken compares in constant time.
func (a *Auth) CheckToken(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), a.token) == 1
}

// Check inspects an incoming request for a bearer token or a session cookie.
func (a *Auth) Check(r *http.Request) authResult {
	header := r.Header.Get("Authorization")
	cookie, cookieErr := r.Cookie(sessionCookie)
	if header == "" && cookieErr != nil {
		return authNone
	}
	ip := ClientIP(r)
	if a.rl.blocked(ip, a.now()) {
		return authThrottled
	}
	if header != "" {
		tok, ok := cutBearer(header)
		if ok && a.CheckToken(tok) {
			return authOK
		}
		a.rl.fail(ip, a.now())
		return authBad
	}
	if a.checkSession(cookie.Value) {
		return authOK
	}
	a.rl.fail(ip, a.now())
	return authBad
}

// Login validates a form-supplied token, honouring the rate limit.
func (a *Auth) Login(r *http.Request, candidate string) authResult {
	ip := ClientIP(r)
	if a.rl.blocked(ip, a.now()) {
		return authThrottled
	}
	if a.CheckToken(candidate) {
		return authOK
	}
	a.rl.fail(ip, a.now())
	return authBad
}

func cutBearer(h string) (string, bool) {
	const p = "bearer "
	if len(h) < len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(h[len(p):]), true
}

// NewSession mints "<random id>.<HMAC(id) keyed by the token>". The token
// itself never goes into a cookie, and the value is unforgeable without it.
func (a *Auth) NewSession() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	return id + "." + a.sign(id), nil
}

func (a *Auth) sign(id string) string {
	m := hmac.New(sha256.New, a.token)
	m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) checkSession(v string) bool {
	id, mac, ok := strings.Cut(v, ".")
	if !ok || id == "" {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(a.sign(id)))
}

// SetSessionCookie writes the session cookie. Secure is dropped for plain
// http on localhost so the app is usable when run locally for testing.
func (a *Auth) SetSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ClientIP returns the caller's address. Traefik appends the peer address to
// X-Forwarded-For, so the *last* element is the one hop we can trust; earlier
// elements may have been supplied by the client.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		last := strings.TrimSpace(parts[len(parts)-1])
		if last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// TruncateIP reduces an address to a network prefix for logging: /24 for IPv4,
// /48 for IPv6.
func TruncateIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPv4(v4[0], v4[1], v4[2], 0).String() + "/24"
	}
	masked := ip.Mask(net.CIDRMask(48, 128))
	return masked.String() + "/48"
}

// rateLimiter counts failures per key inside a sliding window.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[string][]time.Time{}, limit: limit, window: window}
}

func (rl *rateLimiter) blocked(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.pruneLocked(now)
	return len(rl.hits[key]) >= rl.limit
}

func (rl *rateLimiter) fail(key string, now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.pruneLocked(now)
	// Bound memory: if a flood creates too many distinct keys, start over.
	if len(rl.hits) > 10000 {
		rl.hits = map[string][]time.Time{}
	}
	rl.hits[key] = append(rl.hits[key], now)
}

func (rl *rateLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-rl.window)
	for k, ts := range rl.hits {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(rl.hits, k)
		} else {
			rl.hits[k] = kept
		}
	}
}
