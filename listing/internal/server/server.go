package server

import (
	"context"
	"embed"
	"html/template"
	"net/http"

	"github.com/baely/listing/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Server handles HTTP requests for the listing service
type Server struct {
	store    *store.ContainerStore
	template *template.Template
	server   *http.Server
}

// New creates a new Server instance
func New(addr string, s *store.ContainerStore) (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	srv := &Server{
		store:    s,
		template: tmpl,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/health", srv.handleHealth)

	srv.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return srv, nil
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
