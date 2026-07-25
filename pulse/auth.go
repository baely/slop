package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "pulse_session"
	sessionMaxAge = 30 * 24 * time.Hour

	failWindow = time.Minute
	failLimit  = 10 // failed token checks per IP per minute
)

// Auth holds the shared secret and derives session cookies from it.
// The cookie never carries the token: it is a random session id plus an HMAC
// of that id keyed by the token.
type Auth struct {
	token []byte

	mu    sync.Mutex
	fails map[string]*failCount
	now   func() time.Time
}

type failCount struct {
	n     int
	until time.Time
}

// NewAuth builds an Auth for the given shared secret.
func NewAuth(token string) *Auth {
	return &Auth{
		token: []byte(token),
		fails: map[string]*failCount{},
		now:   time.Now,
	}
}

// TokenOK compares a presented token in constant time.
func (a *Auth) TokenOK(presented string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), a.token) == 1
}

func (a *Auth) sign(sid string) string {
	m := hmac.New(sha256.New, a.token)
	m.Write([]byte(sid))
	return hex.EncodeToString(m.Sum(nil))
}

// NewSessionValue mints a cookie value: <random id>.<hmac>.
func (a *Auth) NewSessionValue() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("pulse: crypto/rand unavailable: " + err.Error())
	}
	sid := hex.EncodeToString(b)
	return sid + "." + a.sign(sid)
}

// SessionOK verifies a cookie value.
func (a *Auth) SessionOK(v string) bool {
	sid, mac, ok := strings.Cut(v, ".")
	if !ok || sid == "" || mac == "" {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(a.sign(sid)))
}

// Throttled reports whether this IP has burned through its failure budget.
func (a *Auth) Throttled(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fails[ip]
	if !ok {
		return false
	}
	if a.now().After(f.until) {
		delete(a.fails, ip)
		return false
	}
	return f.n >= failLimit
}

// Fail records a failed token check for an IP.
func (a *Auth) Fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	f, ok := a.fails[ip]
	if !ok || now.After(f.until) {
		a.fails[ip] = &failCount{n: 1, until: now.Add(failWindow)}
	} else {
		f.n++
	}
	// Opportunistic sweep; the map is per-IP and tiny.
	if len(a.fails) > 4096 {
		for k, v := range a.fails {
			if now.After(v.until) {
				delete(a.fails, k)
			}
		}
	}
}

// Authed reports whether a request carries a valid bearer token or session.
func (a *Auth) Authed(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := strings.CutPrefix(h, "Bearer "); ok && a.TokenOK(strings.TrimSpace(tok)) {
			return true
		}
		return false
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return a.SessionOK(c.Value)
	}
	return false
}

// SetSession writes the session cookie.
func (a *Auth) SetSession(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    a.NewSessionValue(),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge / time.Second),
	})
}

// ClearSession expires the session cookie.
func ClearSession(w http.ResponseWriter, secure bool) {
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

// clientIP returns the caller's address, trusting X-Forwarded-For only for the
// single hop in front of us (Traefik). Traefik appends the peer it saw, so the
// rightmost entry is the only one a client cannot forge.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		last := strings.TrimSpace(parts[len(parts)-1])
		if ip := net.ParseIP(last); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// truncIP drops host precision for logging: IPv4 to /24, IPv6 to /48.
func truncIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return "-"
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPv4(v4[0], v4[1], v4[2], 0).String()
	}
	m := ip.Mask(net.CIDRMask(48, 128))
	return m.String() + "/48"
}
