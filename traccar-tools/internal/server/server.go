// Package server hosts the Traccar Tools site: a landing page plus the
// migrate and GPX-export tools, sharing one Traccar connection endpoint.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

//go:embed web
var webFS embed.FS

type Server struct{ mux *http.ServeMux }

func New() *Server {
	s := &Server{mux: http.NewServeMux()}

	// Pages.
	s.page("/", "web/index.html")
	s.page("/migrate", "web/migrate.html")
	s.page("/export", "web/export.html")
	s.asset("/app.css", "web/app.css", "text/css; charset=utf-8")

	// Shared.
	s.mux.HandleFunc("/api/connect", s.handleConnect)

	// Migrate tool.
	s.mux.HandleFunc("/api/migrate/probe", s.handleProbe)
	s.mux.HandleFunc("/api/migrate/preview", s.handleMigratePreview)
	s.mux.HandleFunc("/api/migrate/create-devices", s.handleCreateDevices)
	s.mux.HandleFunc("/api/migrate/run", s.handleMigrateRun)

	// Export tool.
	s.mux.HandleFunc("/api/export/preview", s.handleExportPreview)
	s.mux.HandleFunc("/api/export/file", s.handleExportFile)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// page serves a single HTML file at an exact path (so /migrate works without a
// .html suffix and without FileServer's directory behaviour).
func (s *Server) page(path, file string) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		b, err := webFS.ReadFile(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}

func (s *Server) asset(path, file, ctype string) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		b, err := webFS.ReadFile(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Write(b)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// handleConnect validates a Traccar connection and lists its devices. Shared by
// both tools (each posts a traccar.Conn).
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var conn traccar.Conn
	if err := decode(r, &conn); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c := traccar.NewClient(conn)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	info, err := c.Server(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	devices, err := c.Devices(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"server": info, "devices": devices})
}

// decimate reduces a position list to at most max [lat,lon] pairs for map
// previews, always keeping the final point. Shared by both tools.
func decimate(ps []traccar.Position, max int) [][2]float64 {
	if max < 2 {
		max = 2
	}
	step := 1
	if len(ps) > max {
		step = (len(ps) + max - 1) / max
	}
	track := make([][2]float64, 0, max)
	for i := 0; i < len(ps); i += step {
		track = append(track, [2]float64{ps[i].Latitude, ps[i].Longitude})
	}
	last := ps[len(ps)-1]
	if n := len(track); n == 0 || track[n-1] != [2]float64{last.Latitude, last.Longitude} {
		track = append(track, [2]float64{last.Latitude, last.Longitude})
	}
	return track
}

const maxTrackPoints = 800
