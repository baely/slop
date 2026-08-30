package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/baileybutler/traccar-tools/internal/export"
	"github.com/baileybutler/traccar-tools/internal/traccar"
)

type deviceRef struct {
	DeviceID int    `json:"deviceId"`
	Name     string `json:"name"`
}

type exportReq struct {
	Source  traccar.Conn `json:"source"`
	Devices []deviceRef  `json:"devices"`
	From    time.Time    `json:"from"`
	To      time.Time    `json:"to"`
	GapMin  int          `json:"gapMin"`
	Format  string       `json:"format"` // gpx | kml | geojson
}

func (s *Server) handleExportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req exportReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c := traccar.NewClient(req.Source)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	type devPreview struct {
		DeviceID int          `json:"deviceId"`
		Name     string       `json:"name"`
		Count    int          `json:"count"`
		First    *time.Time   `json:"first"`
		Last     *time.Time   `json:"last"`
		Track    [][2]float64 `json:"track"`
	}
	out := make([]devPreview, 0, len(req.Devices))
	total := 0
	for _, d := range req.Devices {
		dp := devPreview{DeviceID: d.DeviceID, Name: d.Name}
		var all []traccar.Position
		err := c.RouteChunked(ctx, d.DeviceID, req.From, req.To, 14*24*time.Hour, func(b []traccar.Position) error {
			all = append(all, b...)
			return nil
		})
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("%s: %w", d.Name, err))
			return
		}
		dp.Count = len(all)
		total += len(all)
		if len(all) > 0 {
			f, l := all[0].Time(), all[len(all)-1].Time()
			dp.First, dp.Last = &f, &l
			dp.Track = decimate(all, maxTrackPoints)
		}
		out = append(out, dp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out, "total": total})
}

func (s *Server) handleExportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req exportReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Devices) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no devices selected"))
		return
	}

	// Build the encoder up front so an unknown format is a clean JSON error
	// before any file headers are written.
	enc, err := export.New(strings.ToLower(req.Format), w)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	c := traccar.NewClient(req.Source)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	gap := time.Duration(req.GapMin) * time.Minute
	name, filename := exportNames(req, enc.Ext())
	w.Header().Set("Content-Type", enc.ContentType())
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	enc.Header(name)
	for _, d := range req.Devices {
		enc.BeginTrack(d.Name)
		segOpen := false
		var last time.Time
		ferr := c.RouteChunked(ctx, d.DeviceID, req.From, req.To, 14*24*time.Hour, func(batch []traccar.Position) error {
			for _, p := range batch {
				t := p.Time()
				if segOpen && gap > 0 && !last.IsZero() && t.Sub(last) > gap {
					enc.EndSeg()
					segOpen = false
				}
				if !segOpen {
					enc.BeginSeg()
					segOpen = true
				}
				enc.Point(p)
				last = t
			}
			return enc.Err()
		})
		if segOpen {
			enc.EndSeg()
		}
		enc.EndTrack()
		if ferr != nil {
			enc.Footer()
			_ = enc.Flush()
			return
		}
	}
	enc.Footer()
	_ = enc.Flush()
}

func exportNames(req exportReq, ext string) (title, filename string) {
	stamp := req.From.UTC().Format("20060102") + "-" + req.To.UTC().Format("20060102")
	if len(req.Devices) == 1 {
		return req.Devices[0].Name, slug(req.Devices[0].Name) + "_" + stamp + "." + ext
	}
	return fmt.Sprintf("Traccar export (%d devices)", len(req.Devices)), "traccar-export_" + stamp + "." + ext
}

func slug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "track"
	}
	return out
}
