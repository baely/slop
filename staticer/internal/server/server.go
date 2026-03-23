package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/baely/staticer/internal/storage"
	"github.com/baely/staticer/internal/temporal"
	"github.com/baely/staticer/web"
)

// Config holds server configuration
type Config struct {
	Port             string
	Host             string
	SitesDir         string
	DatabasePath     string
	UploadSecret     string
	AdminSecret      string
	MaxUploadSize    int64
	MaxExtractedSize int64
	MaxFilesPerSite  int
	RateLimitUploads int
	TLSEnabled       bool
	TLSCertCache     string
	TLSEmail         string
}

// Server is the HTTP server for staticer
type Server struct {
	config   *Config
	store    storage.Storage
	logger   *slog.Logger
	temporal *temporal.Client
	server   *http.Server
}

// New creates a new Server
func New(config *Config, store storage.Storage, logger *slog.Logger, temporalClient *temporal.Client) *Server {
	return &Server{
		config:   config,
		store:    store,
		logger:   logger,
		temporal: temporalClient,
	}
}

// Run starts the server and blocks until shutdown
func (s *Server) Run() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/deploy", s.handleDeploy)
	mux.HandleFunc("/api/sites", s.handleSites)
	mux.HandleFunc("/api/sites/", s.handleSiteAction)
	mux.HandleFunc("/api/public/sites", s.handlePublicSites)
	mux.HandleFunc("/api/admin/sites", s.handleAdminSites)
	mux.HandleFunc("/api/admin/sites/", s.handleAdminSiteAction)
	mux.HandleFunc("/api/admin/stats", s.handleAdminStats)

	// Dashboard
	dashboardFS, err := fs.Sub(web.DashboardFS, "dashboard")
	if err != nil {
		return fmt.Errorf("failed to create dashboard filesystem: %w", err)
	}
	dashboardFileServer := http.FileServer(http.FS(dashboardFS))
	mux.Handle("/dashboard/", http.StripPrefix("/dashboard/", dashboardFileServer))

	// Serve dashboard at root
	mux.Handle("/", dashboardFileServer)

	// Root handler: serve sites by subdomain, or redirect to dashboard
	handler := s.subdomainRouter(mux)

	s.server = &http.Server{
		Addr:         ":" + s.config.Port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		s.logger.Info("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}()

	s.logger.Info("Starting server", "port", s.config.Port, "host", s.config.Host)
	return s.server.ListenAndServe()
}

// subdomainRouter routes requests based on the Host header
func (s *Server) subdomainRouter(apiMux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		// Strip port if present
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}

		// Check if this is a custom domain
		if host != s.config.Host && !strings.HasSuffix(host, "."+s.config.Host) {
			site, err := s.store.GetSiteByCustomDomain(host)
			if err == nil {
				s.serveSite(w, r, site.Subdomain)
				return
			}
		}

		// Check if this is a subdomain request
		if strings.HasSuffix(host, "."+s.config.Host) {
			subdomain := strings.TrimSuffix(host, "."+s.config.Host)
			if subdomain != "" && subdomain != "www" {
				s.serveSite(w, r, subdomain)
				return
			}
		}

		// Only serve API/dashboard on the main host
		if host == s.config.Host {
			apiMux.ServeHTTP(w, r)
			return
		}

		// Unknown host
		http.NotFound(w, r)
	})
}

// serveSite serves static files for a deployed site
func (s *Server) serveSite(w http.ResponseWriter, r *http.Request, subdomain string) {
	sitePath := s.store.GetSitePath(subdomain)
	if _, err := os.Stat(sitePath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	http.FileServer(http.Dir(sitePath)).ServeHTTP(w, r)
}
