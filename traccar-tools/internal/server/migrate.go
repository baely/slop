package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/baileybutler/traccar-tools/internal/traccar"
)

type mapping struct {
	SourceDeviceID int    `json:"sourceDeviceId"`
	SourceName     string `json:"sourceName"`
	TargetUniqueID string `json:"targetUniqueId"`
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

type migratePreviewReq struct {
	Source   traccar.Conn `json:"source"`
	Mappings []mapping    `json:"mappings"`
	From     time.Time    `json:"from"`
	To       time.Time    `json:"to"`
}

func (s *Server) handleMigratePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req migratePreviewReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c := traccar.NewClient(req.Source)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	type devPreview struct {
		SourceDeviceID int          `json:"sourceDeviceId"`
		SourceName     string       `json:"sourceName"`
		TargetUniqueID string       `json:"targetUniqueId"`
		Count          int          `json:"count"`
		First          *time.Time   `json:"first"`
		Last           *time.Time   `json:"last"`
		Track          [][2]float64 `json:"track"`
	}
	out := make([]devPreview, 0, len(req.Mappings))
	total := 0
	for _, m := range req.Mappings {
		dp := devPreview{SourceDeviceID: m.SourceDeviceID, SourceName: m.SourceName, TargetUniqueID: m.TargetUniqueID}
		var all []traccar.Position
		err := c.RouteChunked(ctx, m.SourceDeviceID, req.From, req.To, 14*24*time.Hour, func(b []traccar.Position) error {
			all = append(all, b...)
			return nil
		})
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("%s: %w", m.SourceName, err))
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

// --- create-devices --------------------------------------------------------

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

// --- run: stream the replay as NDJSON --------------------------------------

type migrateRunReq struct {
	Source      traccar.Conn `json:"source"`
	OsmAnd      string       `json:"osmand"`
	Mappings    []mapping    `json:"mappings"`
	From        time.Time    `json:"from"`
	To          time.Time    `json:"to"`
	ThrottleMS  int          `json:"throttleMs"`
	SkipInvalid bool         `json:"skipInvalid"`
}

type event struct {
	Type     string `json:"type"`
	Message  string `json:"message,omitempty"`
	DeviceID int    `json:"deviceId,omitempty"`
	Device   string `json:"device,omitempty"`
	Target   string `json:"target,omitempty"`
	Sent     int    `json:"sent,omitempty"`
	Failed   int    `json:"failed,omitempty"`
}

func (s *Server) handleMigrateRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req migrateRunReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	emit := func(e event) { _ = enc.Encode(e); flusher.Flush() }

	ctx := r.Context()
	src := traccar.NewClient(req.Source)
	out := traccar.NewOsmAnd(req.OsmAnd)
	throttle := time.Duration(req.ThrottleMS) * time.Millisecond

	emit(event{Type: "start", Message: fmt.Sprintf("Replaying %d device(s) to %s", len(req.Mappings), out.Endpoint())})

	grandSent, grandFailed := 0, 0
	for _, m := range req.Mappings {
		if ctx.Err() != nil {
			emit(event{Type: "warn", Message: "cancelled"})
			break
		}
		emit(event{Type: "device", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID,
			Message: fmt.Sprintf("Starting %s → %s", m.SourceName, m.TargetUniqueID)})

		sent, failed := 0, 0
		err := src.RouteChunked(ctx, m.SourceDeviceID, req.From, req.To, 14*24*time.Hour, func(batch []traccar.Position) error {
			for _, p := range batch {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if req.SkipInvalid && !p.Valid {
					continue
				}
				if err := out.Send(ctx, m.TargetUniqueID, p); err != nil {
					failed++
					if failed <= 5 {
						emit(event{Type: "warn", DeviceID: m.SourceDeviceID, Device: m.SourceName, Message: err.Error()})
					}
				} else {
					sent++
				}
				if (sent+failed)%25 == 0 {
					emit(event{Type: "progress", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID, Sent: sent, Failed: failed})
				}
				if throttle > 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(throttle):
					}
				}
			}
			return nil
		})
		grandSent += sent
		grandFailed += failed
		emit(event{Type: "progress", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID, Sent: sent, Failed: failed})
		if err != nil && ctx.Err() == nil {
			emit(event{Type: "warn", DeviceID: m.SourceDeviceID, Device: m.SourceName, Message: "fetch error: " + err.Error()})
		}
		emit(event{Type: "device", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID, Sent: sent, Failed: failed,
			Message: fmt.Sprintf("%s done: %d sent, %d failed", m.SourceName, sent, failed)})
	}

	status := "complete"
	if ctx.Err() != nil {
		status = "cancelled"
	}
	emit(event{Type: "done", Sent: grandSent, Failed: grandFailed,
		Message: fmt.Sprintf("Migration %s — %d positions sent, %d failed", status, grandSent, grandFailed)})
}
