package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/baileybutler/traccar-migrate/internal/traccar"
)

type migrateReq struct {
	Source       traccar.Conn `json:"source"`
	OsmAnd       string       `json:"osmand"`
	Mappings     []mapping    `json:"mappings"`
	From         time.Time    `json:"from"`
	To           time.Time    `json:"to"`
	ThrottleMS   int          `json:"throttleMs"` // delay between sends, 0 = full speed
	SkipInvalid  bool         `json:"skipInvalid"`
}

// event is one NDJSON line streamed back to the browser as the job runs.
type event struct {
	Type     string `json:"type"`           // start | device | progress | warn | done
	Message  string `json:"message,omitempty"`
	DeviceID int    `json:"deviceId,omitempty"`
	Device   string `json:"device,omitempty"`
	Target   string `json:"target,omitempty"`
	Sent     int    `json:"sent,omitempty"`
	Failed   int    `json:"failed,omitempty"`
	Total    int    `json:"total,omitempty"`
}

// handleMigrate streams newline-delimited JSON progress events while replaying
// positions. The client drives cancellation by aborting the fetch, which
// cancels r.Context() and unwinds the loops below.
func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req migrateReq
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
	emit := func(e event) {
		_ = enc.Encode(e)
		flusher.Flush()
	}

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
						emit(event{Type: "warn", DeviceID: m.SourceDeviceID, Device: m.SourceName,
							Message: err.Error()})
					}
				} else {
					sent++
				}
				if (sent+failed)%25 == 0 {
					emit(event{Type: "progress", DeviceID: m.SourceDeviceID, Device: m.SourceName,
						Target: m.TargetUniqueID, Sent: sent, Failed: failed})
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
		emit(event{Type: "progress", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID,
			Sent: sent, Failed: failed})
		if err != nil && ctx.Err() == nil {
			emit(event{Type: "warn", DeviceID: m.SourceDeviceID, Device: m.SourceName,
				Message: "fetch error: " + err.Error()})
		}
		emit(event{Type: "device", DeviceID: m.SourceDeviceID, Device: m.SourceName, Target: m.TargetUniqueID,
			Sent: sent, Failed: failed,
			Message: fmt.Sprintf("%s done: %d sent, %d failed", m.SourceName, sent, failed)})
	}

	status := "complete"
	if ctx.Err() != nil {
		status = "cancelled"
	}
	emit(event{Type: "done", Sent: grandSent, Failed: grandFailed,
		Message: fmt.Sprintf("Migration %s — %d positions sent, %d failed", status, grandSent, grandFailed)})
}
