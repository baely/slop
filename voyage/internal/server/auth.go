package server

import (
	"crypto/subtle"
	"net/http"
	"net/url"
)

const sessionCookie = "voyage_session"

// auth wraps owner handlers. When AdminToken is unset, auth is disabled so the
// app is usable for local development; in production ADMIN_TOKEN must be set.
func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.AdminToken == "" || s.authed(r) {
			h(w, r)
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
	})
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.opts.AdminToken)) == 1
}

type loginPage struct {
	Title string
	Next  string
	Error bool
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.opts.AdminToken == "" || s.authed(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "login", loginPage{Title: "Sign in · " + s.opts.Title, Next: r.URL.Query().Get("next")})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}
	if s.opts.AdminToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.AdminToken)) != 1 {
		s.render(w, "login", loginPage{Title: "Sign in · " + s.opts.Title, Next: next, Error: true})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.opts.AdminToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 90,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
