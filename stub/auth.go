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
	sessionCookie = "stub_session"
	sessionIDLen  = 20 // 20 chars of a 31-symbol alphabet ~= 99 bits
	maxFailures   = 10
	failureWindow = time.Minute
	sessionMaxAge = 30 * 24 * time.Hour
)

// authenticator holds the shared token and the brute-force counter. The token
// itself is never written to a cookie: the cookie carries a random session id
// plus an HMAC of that id keyed by the token, so stealing the cookie does not
// reveal APP_TOKEN and rotating APP_TOKEN invalidates every session.
type authenticator struct {
	tokenHash [32]byte
	key       []byte

	mu    sync.Mutex
	fails map[string][]time.Time
}

func newAuthenticator(token string) *authenticator {
	return &authenticator{
		tokenHash: sha256.Sum256([]byte(token)),
		key:       []byte(token),
		fails:     map[string][]time.Time{},
	}
}

// checkToken compares in constant time. Both sides are hashed first so the
// comparison length never leaks the real token length.
func (a *authenticator) checkToken(candidate string) bool {
	h := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(h[:], a.tokenHash[:]) == 1
}

func (a *authenticator) mac(msg string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// allow reports whether ip may attempt another token check right now.
func (a *authenticator) allow(ip string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune(now)
	return len(a.fails[ip]) < maxFailures
}

// fail records a rejected credential for ip.
func (a *authenticator) fail(ip string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune(now)
	a.fails[ip] = append(a.fails[ip], now)
}

func (a *authenticator) prune(now time.Time) {
	cutoff := now.Add(-failureWindow)
	for ip, ts := range a.fails {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(a.fails, ip)
		} else {
			a.fails[ip] = kept
		}
	}
}

// newSessionValue mints a cookie value of the form "<sid>.<hmac(sid)>".
func (a *authenticator) newSessionValue() (string, error) {
	sid, err := randomID(sessionIDLen)
	if err != nil {
		return "", err
	}
	return sid + "." + a.mac("sess|"+sid), nil
}

// parseSessionValue verifies the cookie signature and returns the session id.
func (a *authenticator) parseSessionValue(v string) (string, bool) {
	i := strings.IndexByte(v, '.')
	if i <= 0 || i == len(v)-1 {
		return "", false
	}
	sid, sig := v[:i], v[i+1:]
	if len(sid) != sessionIDLen {
		return "", false
	}
	want := a.mac("sess|" + sid)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return "", false
	}
	return sid, true
}

// csrfToken is bound to the session, so a token lifted from one browser is
// useless in another session.
func (a *authenticator) csrfToken(sid string) string {
	return a.mac("csrf|" + sid)[:32]
}

func (a *authenticator) checkCSRF(sid, got string) bool {
	if sid == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a.csrfToken(sid)), []byte(got)) == 1
}

// session is the result of authenticating a request.
type session struct {
	ok  bool
	sid string // empty for bearer-token (API) callers; form CSRF only applies to cookie sessions
}

// authResult distinguishes "no credentials" from "bad credentials" so we only
// rate-limit the latter.
type authResult int

const (
	authNone authResult = iota
	authOK
	authBad
	authLimited
)

// authenticate inspects Authorization and the session cookie.
func (s *server) authenticate(r *http.Request, now time.Time) (session, authResult) {
	ip := clientIP(r)

	if h := r.Header.Get("Authorization"); h != "" {
		if !s.auth.allow(ip, now) {
			return session{}, authLimited
		}
		const prefix = "Bearer "
		if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
			if s.auth.checkToken(strings.TrimSpace(h[len(prefix):])) {
				return session{ok: true}, authOK
			}
		}
		s.auth.fail(ip, now)
		return session{}, authBad
	}

	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if !s.auth.allow(ip, now) {
			return session{}, authLimited
		}
		if sid, ok := s.auth.parseSessionValue(c.Value); ok {
			return session{ok: true, sid: sid}, authOK
		}
		s.auth.fail(ip, now)
		return session{}, authBad
	}

	return session{}, authNone
}

func setSessionCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge / time.Second),
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
