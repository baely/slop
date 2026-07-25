package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

type config struct {
	Addr    string
	DataDir string
	BaseURL string
	Token   string
}

type server struct {
	cfg   config
	store *Store
	auth  *Auth
	mux   *http.ServeMux
	tpl   map[string]*template.Template
	now   func() time.Time
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	cfg := config{
		Addr:    env("ADDR", ":8080"),
		DataDir: env("DATA_DIR", "/data"),
		BaseURL: strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		Token:   os.Getenv("APP_TOKEN"),
	}
	if cfg.Token == "" {
		log.Println("APP_TOKEN is empty: refusing to start. Set APP_TOKEN to a long random secret.")
		os.Exit(1)
	}

	srv, err := newServer(cfg)
	if err != nil {
		log.Printf("startup failed: %v", err)
		os.Exit(1)
	}

	if n := srv.store.Sweep(time.Now()); n > 0 {
		log.Printf("swept %d expired bin(s) at startup", n)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.Default(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if n := srv.store.Sweep(now); n > 0 {
					log.Printf("swept %d expired bin(s)", n)
				}
			}
		}
	}()

	go func() {
		log.Printf("catch listening on %s data=%s base=%s", cfg.Addr, cfg.DataDir, cfg.BaseURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("listen failed: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func newServer(cfg config) (*server, error) {
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	s := &server{
		cfg:   cfg,
		store: store,
		auth:  NewAuth(cfg.Token),
		mux:   http.NewServeMux(),
		tpl:   tpl,
		now:   time.Now,
	}
	s.routes()
	return s, nil
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFor(time.Hour, http.FileServer(http.FS(static)))))

	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("POST /bins", s.handleCreateBin)
	s.mux.HandleFunc("GET /bin/{id}", s.handleBin)
	s.mux.HandleFunc("GET /bin/{id}/rows", s.handleRows)
	s.mux.HandleFunc("GET /bin/{id}/r/{rid}/raw", s.handleRawBody)
	s.mux.HandleFunc("POST /bin/{id}/settings", s.handleBinSettings)
	s.mux.HandleFunc("POST /bin/{id}/clear", s.handleClearBin)
	s.mux.HandleFunc("POST /bin/{id}/delete", s.handleDeleteBin)
}

// ServeHTTP wraps every response with the security headers and one log line.
// Capture traffic is routed before ServeMux so that odd paths reach the bin
// verbatim instead of being cleaned or redirected.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	setSecurityHeaders(rec.Header())

	if strings.HasPrefix(r.URL.Path, "/b/") {
		s.handleCapture(rec, r)
	} else {
		s.mux.ServeHTTP(rec, r)
	}

	// Path only: query strings on this service routinely carry other people's
	// secrets, and bodies never go anywhere near the log.
	log.Printf("%s %s %d %s ip=%s", r.Method, r.URL.Path, rec.status,
		time.Since(start).Round(time.Millisecond), truncateIP(clientIP(r)))
}

func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy",
		"default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
			"font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'")
}

func cacheFor(d time.Duration, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(d.Seconds())))
		h.ServeHTTP(w, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *recorder) WriteHeader(code int) {
	if r.written {
		return
	}
	r.written = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

// clientIP trusts only the first hop of X-Forwarded-For (Traefik appends the
// real peer there; anything further left is attacker-controlled).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// truncateIP drops the host part so logs keep the network, not the person.
func truncateIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "unknown"
	}
	if v4 := parsed.To4(); v4 != nil {
		return net.IP(append(append([]byte{}, v4[:3]...), 0)).String() + "/24"
	}
	masked := parsed.Mask(net.CIDRMask(48, 128))
	return masked.String() + "/48"
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
