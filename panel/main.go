package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // so the container does not have to be trusted for zoneinfo
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

var pages = map[string]*template.Template{}

func init() {
	for _, name := range []string{"preview.html", "edit.html", "login.html"} {
		pages[name] = template.Must(template.ParseFS(tmplFS, "templates/base.html", "templates/"+name))
	}
}

// renderPage executes a page against the shared base layout.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, name string, data any) {
	t, ok := pages[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(0)

	token := strings.TrimSpace(os.Getenv("APP_TOKEN"))
	if token == "" {
		log.Println("fatal: APP_TOKEN is empty. panel will not start without one; set it to a long random string.")
		os.Exit(1)
	}

	addr := env("ADDR", ":8080")
	dataDir := env("DATA_DIR", "/data")
	baseURL := strings.TrimRight(env("BASE_URL", "https://panel.baileys.dev"), "/")

	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		log.Printf("warn: could not load Australia/Melbourne (%v), falling back to local time", err)
		loc = time.Local
	}

	store, err := NewStore(dataDir)
	if err != nil {
		log.Printf("fatal: could not open data dir %s: %v", dataDir, err)
		os.Exit(1)
	}

	srv := &Server{
		auth:    NewAuth(token),
		store:   store,
		cache:   newRenderCache(),
		loc:     loc,
		baseURL: baseURL,
		now:     time.Now,
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(srv.Routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(idle)
	}()

	log.Printf("panel listening on %s data=%s base=%s tz=%s", addr, dataDir, baseURL, loc)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
	<-idle
}

// Routes builds the full handler, security headers included.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /screen.png", s.handleScreen(false))
	mux.HandleFunc("GET /screen.bin", s.handleScreen(true))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.requireToken(s.handlePreview)(w, r)
	})
	mux.HandleFunc("GET /edit", s.requireToken(s.handleEdit))
	mux.HandleFunc("POST /edit", s.requireToken(s.handleSave))
	mux.HandleFunc("POST /key/rotate", s.requireToken(s.handleRotateKey))
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	if static, err := fs.Sub(staticFS, "static"); err == nil {
		fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
		mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			fileServer.ServeHTTP(w, r)
		})
	} else {
		log.Printf("warn: static assets unavailable: %v", err)
	}

	return securityHeaders(mux)
}

// securityHeaders applies the estate-wide headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// logRequests writes one line per request: no bodies, no query string (the
// device key rides in it), and only a truncated client address.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s %s", r.Method, r.URL.Path, sw.status,
			time.Since(start).Round(100*time.Microsecond), truncIP(clientIP(r)))
	})
}
