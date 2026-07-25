package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "catch_session"
	maxFailures   = 10
	failWindow    = time.Minute
)

// Auth holds the shared token, mints session cookies keyed by it, and rate
// limits failed token checks per IP.
type Auth struct {
	token []byte

	mu    sync.Mutex
	fails map[string]*failCount
}

type failCount struct {
	n     int
	start time.Time
}

func NewAuth(token string) *Auth {
	return &Auth{token: []byte(token), fails: map[string]*failCount{}}
}

// CheckToken compares in constant time.
func (a *Auth) CheckToken(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), a.token) == 1
}

// NewSession returns a cookie value: a random session id plus an HMAC of that
// id keyed by the token. The token itself never leaves the server.
func (a *Auth) NewSession() (string, error) {
	sid, err := NewID(26)
	if err != nil {
		return "", err
	}
	return sid + "." + a.sign(sid), nil
}

// ValidSession verifies a cookie value against the token.
func (a *Auth) ValidSession(value string) bool {
	sid, mac, ok := strings.Cut(value, ".")
	if !ok || sid == "" || mac == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(mac), []byte(a.sign(sid))) == 1
}

func (a *Auth) sign(sid string) string {
	m := hmac.New(sha256.New, a.token)
	m.Write([]byte("catch-session\x00"))
	m.Write([]byte(sid))
	return hex.EncodeToString(m.Sum(nil))
}

// Allow reports whether ip has failures left in the current window.
func (a *Auth) Allow(ip string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fails[ip]
	if !ok || now.Sub(f.start) > failWindow {
		return true
	}
	return f.n < maxFailures
}

// Fail records a rejected credential from ip.
func (a *Auth) Fail(ip string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fails[ip]
	if !ok || now.Sub(f.start) > failWindow {
		a.fails[ip] = &failCount{n: 1, start: now}
		return
	}
	f.n++
	// Opportunistic cleanup so the map cannot grow without bound.
	if len(a.fails) > 4096 {
		for k, v := range a.fails {
			if now.Sub(v.start) > failWindow {
				delete(a.fails, k)
			}
		}
	}
}

// Reset clears the failure counter after a successful login.
func (a *Auth) Reset(ip string) {
	a.mu.Lock()
	delete(a.fails, ip)
	a.mu.Unlock()
}

func setSessionCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// bearerToken pulls the token out of an Authorization header, if present.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	scheme, rest, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}
