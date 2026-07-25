package main

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// contentSecurityPolicy allows the Google Fonts stylesheet and nothing else
// off-origin. Scripts are same-origin only, so a paste body can never execute.
const contentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'"

type server struct {
	cfg   config
	store *Store
	auth  *Auth
	pages map[string]*template.Template
	now   func() time.Time
}

func newServer(cfg config, store *Store) (*server, error) {
	s := &server{
		cfg:   cfg,
		store: store,
		auth:  NewAuth(cfg.Token),
		pages: map[string]*template.Template{},
		now:   time.Now,
	}
	for _, name := range []string{"login.html", "new.html", "done.html", "paste.html", "index.html", "notfound.html", "message.html"} {
		t, err := template.New("layout.html").ParseFS(templateFS, "templates/layout.html", "templates/"+name)
		if err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	return s, nil
}

func (s *server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	mux.Handle("GET /static/", cacheable(fileServer))

	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("POST /{$}", s.handleCreateForm)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /pastes", s.handleIndex)
	mux.HandleFunc("POST /pastes/delete", s.handleDelete)
	mux.HandleFunc("GET /done/{slug}", s.handleDone)
	mux.HandleFunc("GET /p/{slug}", s.handleView)
	mux.HandleFunc("GET /raw/{slug}", s.handleRaw)
	mux.HandleFunc("POST /api/pastes", s.handleAPICreate)
	mux.HandleFunc("/", s.handleCatchAll)

	return logRequests(securityHeaders(mux))
}

func cacheable(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()
		head.Set("X-Content-Type-Options", "nosniff")
		head.Set("Referrer-Policy", "no-referrer")
		head.Set("Content-Security-Policy", contentSecurityPolicy)
		head.Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logRequests emits one line per request. No query strings, no bodies, no
// headers: a paste title or a token must never reach the log.
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		log.Printf("%s %s %d %s %s",
			r.Method, r.URL.Path, sw.status,
			time.Since(start).Round(time.Microsecond),
			TruncateIP(ClientIP(r)))
	})
}

// render buffers the page so a template error cannot half-write a response.
func (s *server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		log.Printf("snip: unknown page %q", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("snip: render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}
