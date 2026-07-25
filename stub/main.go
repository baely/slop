package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const maxBodyBytes = 64 << 10

type server struct {
	store   *Store
	auth    *authenticator
	baseURL string
	// shortURL is the domain short links are built from — an apex like
	// https://baely.sh, kept separate from baseURL so the admin UI stays on
	// its own hostname. Equal to baseURL when SHORT_URL is unset.
	shortURL  string
	shortHost string // host part of shortURL, empty when it equals baseURL's
	secure    bool   // set Secure on cookies (false only for plain-http local runs)
	pages     map[string]*template.Template
	static    http.Handler
	now       func() time.Time
}

func main() {
	log.SetFlags(0)

	token := strings.TrimSpace(os.Getenv("APP_TOKEN"))
	if token == "" {
		log.Println("stub: APP_TOKEN is empty. Set it to a strong random secret; there is no default and none will be generated. Refusing to start.")
		os.Exit(1)
	}

	addr := envOr("ADDR", ":8080")
	dataDir := envOr("DATA_DIR", "/data")
	baseURL := strings.TrimRight(envOr("BASE_URL", "https://stub.baileys.dev"), "/")
	shortURL := strings.TrimRight(envOr("SHORT_URL", baseURL), "/")

	store, err := OpenStore(dataDir)
	if err != nil {
		log.Printf("stub: cannot open data dir %s: %v", dataDir, err)
		os.Exit(1)
	}

	srv, err := newServer(store, token, baseURL, shortURL)
	if err != nil {
		log.Printf("stub: startup failed: %v", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Clicks are the only high-frequency write. They mark the store dirty and
	// this ticker persists them, so a burst of traffic is one fsync every two
	// seconds instead of one per request. Creates and deletes still write
	// through synchronously.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := store.Flush(); err != nil {
					log.Printf("stub: flush failed: %v", err)
				}
			}
		}
	}()

	go func() {
		log.Printf("stub: listening on %s, data %s, base %s, %d links", addr, dataDir, baseURL, store.Count())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("stub: listen failed: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Println("stub: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("stub: shutdown: %v", err)
	}
	wg.Wait()
	if err := store.Flush(); err != nil {
		log.Printf("stub: final flush failed: %v", err)
	}
	log.Println("stub: stopped")
}

func newServer(store *Store, token, baseURL, shortURL string) (*server, error) {
	pages, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	if shortURL == "" {
		shortURL = baseURL
	}
	// Only guard by host when the two are genuinely different hostnames.
	var shortHost string
	if bu, err := url.Parse(baseURL); err == nil {
		if su, err := url.Parse(shortURL); err == nil && !strings.EqualFold(su.Host, bu.Host) {
			shortHost = su.Host
		}
	}
	return &server{
		store:     store,
		auth:      newAuthenticator(token),
		baseURL:   baseURL,
		shortURL:  shortURL,
		shortHost: shortHost,
		secure:    strings.HasPrefix(baseURL, "https://"),
		pages:     pages,
		static:    http.StripPrefix("/static/", http.FileServer(noDirFS{http.FS(sub)})),
		now:       time.Now,
	}, nil
}

// shortDomainOnly restricts the short domain to what a short domain is for.
// baely.sh is a public surface whose whole job is resolving slugs; it has no
// business serving a login form, the admin pages or the API, so anything else
// there is a flat 404. The admin host keeps the full surface.
func (s *server) shortDomainOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.shortHost == "" || !strings.EqualFold(hostOnly(r.Host), hostOnly(s.shortHost)) {
			next.ServeHTTP(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		allowed := p == "healthz" || p == "robots.txt" ||
			(r.Method == http.MethodGet && p != "" && !strings.Contains(p, "/") && !reservedSlugs[strings.ToLower(p)])
		if !allowed {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "Not found.", http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostOnly strips any :port so a Host header and a configured URL compare equal.
func hostOnly(h string) string {
	if i := strings.LastIndexByte(h, ':'); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}

// noDirFS makes http.FileServer serve files and nothing else — no directory
// index, so adding a subdirectory to static/ can never publish a listing.
type noDirFS struct{ fs http.FileSystem }

func (f noDirFS) Open(name string) (http.File, error) {
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if st.IsDir() {
		file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("GET /static/{path...}", s.handleStatic)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
	})

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("POST /admin/create", s.handleCreateForm)
	mux.HandleFunc("POST /admin/delete", s.handleDeleteForm)
	mux.HandleFunc("POST /admin/state", s.handleStateForm)
	mux.HandleFunc("GET /admin/link/{slug}", s.handleStatsPage)

	mux.HandleFunc("POST /api/links", s.handleAPICreate)
	mux.HandleFunc("GET /api/links", s.handleAPIList)
	mux.HandleFunc("GET /api/links/{slug}", s.handleAPIGet)
	mux.HandleFunc("DELETE /api/links/{slug}", s.handleAPIDelete)

	mux.HandleFunc("GET /{slug}", s.handleRedirect)

	return logging(securityHeaders(s.shortDomainOnly(http.MaxBytesHandler(mux, maxBodyBytes))))
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com; img-src 'self' data:; script-src 'self'")
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

// logging writes one line per request. No headers, no bodies, no full client
// IP — the last octet (or the low 80 bits of an IPv6 address) is masked.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s ip=%s",
			r.Method, truncatePath(r.URL.Path), sw.status,
			time.Since(start).Round(100*time.Microsecond), maskIP(clientIP(r)))
	})
}

func truncatePath(p string) string {
	if len(p) > 120 {
		return p[:120] + "…"
	}
	return p
}

// clientIP trusts exactly one hop. Traefik appends the real peer to
// X-Forwarded-For, so the LAST entry is the one it vouches for; anything
// earlier was supplied by the client and is not trustworthy.
func clientIP(r *http.Request) string {
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

// maskIP drops the identifying tail so logs cannot be joined back to a person.
func maskIP(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil {
		return "?"
	}
	if v4 := addr.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	return fmt.Sprintf("%02x%02x:%02x%02x::/32", addr[0], addr[1], addr[2], addr[3])
}
