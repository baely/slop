// Package server exposes the HTTP API and embedded UI for the migrator.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"time"

	"github.com/baileybutler/traccar-migrate/internal/traccar"
)

//go:embed web
var webFS embed.FS

type Server struct {
	mux *http.ServeMux
}

func New() *Server {
	s := &Server{mux: http.NewServeMux()}

	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	s.mux.HandleFunc("/api/connect", s.handleConnect)
	s.mux.HandleFunc("/api/probe", s.handleProbe)
	s.mux.HandleFunc("/api/preview", s.handlePreview)
	s.mux.HandleFunc("/api/create-devices", s.handleCreateDevices)
	s.mux.HandleFunc("/api/migrate", s.handleMigrate)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

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

// --- connect: validate a Traccar connection and list its devices -----------

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

// --- probe: confirm the destination OsmAnd endpoint is reachable -----------

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	o := traccar.NewOsmAnd(body.Endpoint)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := o.Reachable(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoint": o.Endpoint(), "reachable": true})
}

// --- preview: count positions and return a decimated track per device ------

type mapping struct {
	SourceDeviceID int    `json:"sourceDeviceId"`
	SourceName     string `json:"sourceName"`
	TargetUniqueID string `json:"targetUniqueId"`
}

type previewReq struct {
	Source   traccar.Conn `json:"source"`
	Mappings []mapping    `json:"mappings"`
	From     time.Time    `json:"from"`
	To       time.Time    `json:"to"`
}

const maxTrackPoints = 800

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req previewReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c := traccar.NewClient(req.Source)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	type devPreview struct {
		SourceDeviceID int         `json:"sourceDeviceId"`
		SourceName     string      `json:"sourceName"`
		TargetUniqueID string      `json:"targetUniqueId"`
		Count          int         `json:"count"`
		First          *time.Time  `json:"first"`
		Last           *time.Time  `json:"last"`
		Track          [][2]float64 `json:"track"`
	}

	out := make([]devPreview, 0, len(req.Mappings))
	total := 0
	for _, m := range req.Mappings {
		dp := devPreview{SourceDeviceID: m.SourceDeviceID, SourceName: m.SourceName, TargetUniqueID: m.TargetUniqueID}
		var all []traccar.Position
		err := c.RouteChunked(ctx, m.SourceDeviceID, req.From, req.To, 14*24*time.Hour, func(batch []traccar.Position) error {
			all = append(all, batch...)
			return nil
		})
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("%s: %w", m.SourceName, err))
			return
		}
		dp.Count = len(all)
		total += len(all)
		if len(all) > 0 {
			f := all[0].Time()
			l := all[len(all)-1].Time()
			dp.First, dp.Last = &f, &l
			dp.Track = decimate(all, maxTrackPoints)
		}
		out = append(out, dp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out, "total": total})
}

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
	// Always include the final point so the track terminates correctly.
	last := ps[len(ps)-1]
	if n := len(track); n == 0 || track[n-1] != [2]float64{last.Latitude, last.Longitude} {
		track = append(track, [2]float64{last.Latitude, last.Longitude})
	}
	return track
}

// --- create-devices: register missing target uniqueIds on the destination --

func (s *Server) handleCreateDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Dest    traccar.Conn `json:"dest"`
		Devices []struct {
			Name     string `json:"name"`
			UniqueID string `json:"uniqueId"`
		} `json:"devices"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c := traccar.NewClient(body.Dest)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	created := []traccar.Device{}
	for _, d := range body.Devices {
		dev, err := c.CreateDevice(ctx, d.Name, d.UniqueID)
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("create %q: %w", d.UniqueID, err))
			return
		}
		created = append(created, *dev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created})
}
