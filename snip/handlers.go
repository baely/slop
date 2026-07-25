package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// formLimit is generous because percent-encoding inflates the body; the
// decoded paste is still checked against MaxPasteBytes.
const formLimit = 4*MaxPasteBytes + 64*1024

const maxTitleRunes = 120

type base struct {
	Title  string
	Authed bool
	Nav    string
	Script bool
}

type loginData struct {
	base
	Err  string
	Next string
}

type newData struct {
	base
	Err        string
	Body       string
	PasteTitle string
	Expiry     string
	Burn       bool
	Options    []expiryOption
	MaxSize    string
	Curl       string
	Live       int
	Cap        int
}

type doneData struct {
	base
	Slug       string
	PasteTitle string
	URL        string
	RawURL     string
	Expiry     string
	Size       string
	Burn       bool
}

type pasteData struct {
	base
	Slug         string
	Heading      string
	HasTitle     bool
	Lines        []string
	Size         string
	Created      string
	CreatedExact string
	Expiry       string
	Burn         bool
	RawURL       string
}

type indexRow struct {
	Slug         string
	PasteTitle   string
	URL          string
	Size         string
	Created      string
	CreatedExact string
	Expiry       string
}

type indexData struct {
	base
	Rows  []indexRow
	Count int
	Cap   int
}

type messageData struct {
	base
	Heading string
	Detail  string
}

// ---------- auth plumbing ----------

// requireAuth resolves the caller. It returns false once it has already
// written a response.
func (s *server) requireAuth(w http.ResponseWriter, r *http.Request, next string) bool {
	switch s.auth.Check(r) {
	case authOK:
		return true
	case authThrottled:
		s.tooManyAttempts(w, r)
		return false
	default:
		s.renderLogin(w, r, http.StatusUnauthorized, "", next)
		return false
	}
}

// requireAuthAPI is the same check for machine callers: plain text, no HTML.
func (s *server) requireAuthAPI(w http.ResponseWriter, r *http.Request) bool {
	switch s.auth.Check(r) {
	case authOK:
		return true
	case authThrottled:
		w.Header().Set("Retry-After", "60")
		plain(w, http.StatusTooManyRequests, "Too many failed attempts. Wait a minute.")
		return false
	default:
		w.Header().Set("WWW-Authenticate", `Bearer realm="snip"`)
		plain(w, http.StatusUnauthorized, "Creating a paste requires the token. Send Authorization: Bearer <token>.")
		return false
	}
}

func (s *server) authed(r *http.Request) bool {
	return s.auth.Check(r) == authOK
}

func (s *server) tooManyAttempts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "60")
	s.render(w, http.StatusTooManyRequests, "message.html", messageData{
		base:    base{Title: "Slow Down", Authed: false},
		Heading: "Too many attempts.",
		Detail:  "Wait a minute and try again.",
	})
}

func plain(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, msg)
}

// ---------- pages ----------

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "/") {
		return
	}
	s.renderNew(w, http.StatusOK, newData{Expiry: defaultExpiry})
}

func (s *server) renderNew(w http.ResponseWriter, status int, d newData) {
	d.base = base{Title: "New Paste", Authed: true, Nav: "new", Script: true}
	d.Options = expiryOptions
	d.MaxSize = HumanSize(MaxPasteBytes)
	d.Curl = s.curlLine()
	d.Live = s.store.Len()
	d.Cap = MaxPastes
	if d.Expiry == "" {
		d.Expiry = defaultExpiry
	}
	s.render(w, status, "new.html", d)
}

func (s *server) curlLine() string {
	return fmt.Sprintf(`curl -sS -H "Authorization: Bearer $SNIP_TOKEN" --data-binary @notes.txt "%s/api/pastes?title=Notes&expiry=1d"`, s.cfg.BaseURL)
}

func (s *server) renderLogin(w http.ResponseWriter, r *http.Request, status int, errMsg, next string) {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	s.render(w, status, "login.html", loginData{
		base: base{Title: "Log In", Authed: false, Nav: "login"},
		Err:  errMsg,
		Next: next,
	})
}

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.authed(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, r, http.StatusOK, "", r.URL.Query().Get("next"))
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, r, http.StatusBadRequest, "Could not read that form.", "/")
		return
	}
	next := r.PostFormValue("next")
	switch s.auth.Login(r, r.PostFormValue("token")) {
	case authOK:
		sess, err := s.auth.NewSession()
		if err != nil {
			log.Printf("snip: session: %v", err)
			s.renderLogin(w, r, http.StatusInternalServerError, "Could not start a session.", next)
			return
		}
		s.auth.SetSessionCookie(w, r, sess)
		if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
			next = "/"
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	case authThrottled:
		s.tooManyAttempts(w, r)
	default:
		s.renderLogin(w, r, http.StatusUnauthorized, "Wrong token.", next)
	}
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ClearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------- create ----------

func (s *server) handleCreateForm(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "/") {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, formLimit)
	if err := r.ParseForm(); err != nil {
		s.renderNew(w, http.StatusRequestEntityTooLarge, newData{
			Err:    fmt.Sprintf("That paste is too big. The cap is %s.", HumanSize(MaxPasteBytes)),
			Expiry: defaultExpiry,
		})
		return
	}

	body := strings.ReplaceAll(r.PostFormValue("body"), "\r\n", "\n")
	title := cleanTitle(r.PostFormValue("title"))
	expiry := r.PostFormValue("expiry")
	burn := r.PostFormValue("burn") != ""

	form := newData{Body: body, PasteTitle: title, Expiry: expiry, Burn: burn}

	if strings.TrimSpace(body) == "" {
		form.Err = "Nothing to paste."
		s.renderNew(w, http.StatusBadRequest, form)
		return
	}
	if len(body) > MaxPasteBytes {
		form.Body = ""
		form.Err = fmt.Sprintf("That paste is %s. The cap is %s.", HumanSize(len(body)), HumanSize(MaxPasteBytes))
		s.renderNew(w, http.StatusRequestEntityTooLarge, form)
		return
	}
	ttl, err := ParseExpiry(expiry)
	if err != nil {
		form.Err = "Pick an expiry from the list."
		s.renderNew(w, http.StatusBadRequest, form)
		return
	}

	p, err := s.store.Create(title, body, ttl, burn)
	if err != nil {
		form.Err = createErrMessage(err)
		s.renderNew(w, createErrStatus(err), form)
		return
	}
	http.Redirect(w, r, "/done/"+p.Slug, http.StatusSeeOther)
}

func createErrMessage(err error) string {
	switch {
	case errors.Is(err, ErrFull):
		return fmt.Sprintf("The store is full at %d pastes. Delete something first.", MaxPastes)
	case errors.Is(err, ErrTooLarge):
		return fmt.Sprintf("That paste is too big. The cap is %s.", HumanSize(MaxPasteBytes))
	default:
		log.Printf("snip: create: %v", err)
		return "Could not save that paste."
	}
}

func createErrStatus(err error) int {
	switch {
	case errors.Is(err, ErrFull):
		return http.StatusInsufficientStorage
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

// handleAPICreate takes the raw request body as the paste and the options as
// query parameters, so a single curl line does the job.
func (s *server) handleAPICreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthAPI(w, r) {
		return
	}
	q := r.URL.Query()
	ttl, err := ParseExpiry(q.Get("expiry"))
	if err != nil {
		plain(w, http.StatusBadRequest, "Unknown expiry. Use one of: "+ExpiryValues()+".")
		return
	}
	burn := isTrue(q.Get("burn"))

	r.Body = http.MaxBytesReader(w, r.Body, MaxPasteBytes+1)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		plain(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("That paste is too big. The cap is %s.", HumanSize(MaxPasteBytes)))
		return
	}
	body := string(raw)
	if strings.TrimSpace(body) == "" {
		plain(w, http.StatusBadRequest, "Nothing to paste. Send the paste as the request body.")
		return
	}
	if len(body) > MaxPasteBytes {
		plain(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("That paste is too big. The cap is %s.", HumanSize(MaxPasteBytes)))
		return
	}

	p, err := s.store.Create(cleanTitle(q.Get("title")), body, ttl, burn)
	if err != nil {
		plain(w, createErrStatus(err), createErrMessage(err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Location", s.cfg.BaseURL+"/p/"+p.Slug)
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "%s/p/%s\n", s.cfg.BaseURL, p.Slug)
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func cleanTitle(t string) string {
	t = strings.TrimSpace(t)
	t = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, t)
	runes := []rune(t)
	if len(runes) > maxTitleRunes {
		t = string(runes[:maxTitleRunes])
	}
	return strings.TrimSpace(t)
}

// ---------- read ----------

func (s *server) handleDone(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !s.requireAuth(w, r, "/done/"+slug) {
		return
	}
	p, err := s.store.Peek(slug)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "done.html", doneData{
		base:       base{Title: "Paste Created", Authed: true, Nav: "new", Script: true},
		Slug:       p.Slug,
		PasteTitle: p.Title,
		URL:        s.cfg.BaseURL + "/p/" + p.Slug,
		RawURL:     s.cfg.BaseURL + "/raw/" + p.Slug,
		Expiry:     ExpiryLine(p, s.now()),
		Size:       HumanSize(p.Size),
		Burn:       p.Burn,
	})
}

func (s *server) handleView(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")

	if r.Method == http.MethodHead {
		s.headPaste(w, slug)
		return
	}
	p, err := s.store.Fetch(slug)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			log.Printf("snip: fetch %s: %v", slug, err)
		}
		s.notFound(w, r)
		return
	}
	heading := p.Title
	hasTitle := heading != ""
	if !hasTitle {
		heading = p.Slug
	}
	s.render(w, http.StatusOK, "paste.html", pasteData{
		base:         base{Title: heading, Authed: s.authed(r), Nav: "", Script: true},
		Slug:         p.Slug,
		Heading:      heading,
		HasTitle:     hasTitle,
		Lines:        SplitLines(p.Body),
		Size:         HumanSize(p.Size),
		Created:      HumanAgo(p.Created, s.now()),
		CreatedExact: ExactTime(p.Created),
		Expiry:       ExpiryLine(p, s.now()),
		Burn:         p.Burn,
		RawURL:       "/raw/" + p.Slug,
	})
}

// handleRaw serves the body as an attachment so a paste of HTML can never be
// rendered as a page on this domain.
func (s *server) handleRaw(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")

	if r.Method == http.MethodHead {
		s.headPaste(w, slug)
		return
	}
	p, err := s.store.Fetch(slug)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			log.Printf("snip: fetch %s: %v", slug, err)
		}
		s.notFoundPlain(w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", p.Slug+".txt"))
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, p.Body)
}

// headPaste answers a HEAD without consuming a burn-after-reading paste.
func (s *server) headPaste(w http.ResponseWriter, slug string) {
	if _, err := s.store.Peek(slug); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// notFound is the same page whether the slug never existed, expired or burned.
func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound.html", messageData{
		base:    base{Title: "Not Found", Authed: false, Nav: ""},
		Heading: "No such paste.",
		Detail:  "It never existed, it expired, or it burned after being read. There is no way to tell which, and that is deliberate.",
	})
}

func (s *server) notFoundPlain(w http.ResponseWriter) {
	plain(w, http.StatusNotFound, "No such paste.")
}

func (s *server) handleCatchAll(w http.ResponseWriter, r *http.Request) {
	s.notFound(w, r)
}

// ---------- token-gated index ----------

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "/pastes") {
		return
	}
	now := s.now()
	live := s.store.List()
	rows := make([]indexRow, 0, len(live))
	for _, p := range live {
		rows = append(rows, indexRow{
			Slug:         p.Slug,
			PasteTitle:   p.Title,
			URL:          "/p/" + p.Slug,
			Size:         HumanSize(p.Size),
			Created:      HumanAgo(p.Created, now),
			CreatedExact: ExactTime(p.Created),
			Expiry:       ExpiryLine(p, now),
		})
	}
	s.render(w, http.StatusOK, "index.html", indexData{
		base:  base{Title: "Pastes", Authed: true, Nav: "pastes"},
		Rows:  rows,
		Count: len(rows),
		Cap:   MaxPastes,
	})
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "/pastes") {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	slug := r.PostFormValue("slug")
	if err := s.store.Delete(slug); err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("snip: delete %s: %v", slug, err)
		http.Error(w, "could not delete", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/pastes", http.StatusSeeOther)
}
