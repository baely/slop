package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "panel_session"

// Auth carries the app token and the failure limiter.
type Auth struct {
	token   string
	limiter *limiter
}

func NewAuth(token string) *Auth {
	return &Auth{token: token, limiter: newLimiter(10, time.Minute)}
}

func (a *Auth) tokenMatches(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(a.token)) == 1
}

// NewSession mints "<id>.<hmac>" where the MAC is keyed by the app token. The
// cookie never carries the token itself, so a leaked cookie cannot be replayed
// as an Authorization header or survive a token rotation.
func (a *Auth) NewSession() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	return id + "." + a.sign(id), nil
}

func (a *Auth) sign(id string) string {
	m := hmac.New(sha256.New, []byte(a.token))
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil))
}

func (a *Auth) validSession(v string) bool {
	i := strings.LastIndexByte(v, '.')
	if i <= 0 || i == len(v)-1 {
		return false
	}
	return hmac.Equal([]byte(a.sign(v[:i])), []byte(v[i+1:]))
}

// Authed reports whether the request carries the app token, either as a bearer
// header or as a valid session cookie.
func (a *Auth) Authed(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if a.tokenMatches(strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))) {
			return true
		}
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if a.validSession(c.Value) {
			return true
		}
	}
	return false
}

func (a *Auth) SetSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   14 * 24 * 3600,
	})
}

func (a *Auth) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// limiter counts failures per client IP inside a fixed window.
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	seen   map[string]*bucket
}

type bucket struct {
	n     int
	start time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, seen: map[string]*bucket{}}
}

// Blocked reports whether this IP has spent its failure budget.
func (l *limiter) Blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.seen[ip]
	if !ok || now.Sub(b.start) > l.window {
		return false
	}
	return b.n >= l.limit
}

// Fail records a failed check and returns true when the caller is now blocked.
func (l *limiter) Fail(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) > 4096 {
		for k, v := range l.seen {
			if now.Sub(v.start) > l.window {
				delete(l.seen, k)
			}
		}
	}
	b, ok := l.seen[ip]
	if !ok || now.Sub(b.start) > l.window {
		b = &bucket{start: now}
		l.seen[ip] = b
	}
	b.n++
	return b.n >= l.limit
}

// clientIP trusts only the first hop of X-Forwarded-For (Traefik) and is used
// for logging and rate limiting, never for authorisation.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := net.ParseIP(strings.TrimSpace(xff)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// truncIP drops the host part of the address so the log keeps the network but
// not the individual: 203.0.113.7 -> 203.0.113.x, 2001:db8::1 -> 2001:db8::/32.
func truncIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return "?"
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IP{v4[0], v4[1], v4[2], 0}.String() + "/24"
	}
	masked := ip.Mask(net.CIDRMask(32, 128))
	return masked.String() + "/32"
}
