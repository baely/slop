package traccar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OsmAnd sends positions to a Traccar instance via the OsmAnd HTTP protocol
// (default port 5055). This is the supported way to backfill historical data:
// devices are addressed by uniqueId and the original timestamp is preserved.
type OsmAnd struct {
	base string
	http *http.Client
}

// NewOsmAnd builds a sender for an endpoint such as "http://host:5055".
// A scheme is assumed http:// if omitted; any trailing path is dropped.
func NewOsmAnd(endpoint string) *OsmAnd {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" && !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	if u, err := url.Parse(endpoint); err == nil {
		endpoint = u.Scheme + "://" + u.Host
	}
	return &OsmAnd{
		base: strings.TrimRight(endpoint, "/"),
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// Endpoint returns the normalized base URL.
func (o *OsmAnd) Endpoint() string { return o.base }

func (o *OsmAnd) query(uniqueID string, p Position) url.Values {
	q := url.Values{}
	q.Set("id", uniqueID)
	q.Set("timestamp", strconv.FormatInt(p.Time().Unix(), 10))
	q.Set("lat", strconv.FormatFloat(p.Latitude, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(p.Longitude, 'f', -1, 64))
	if p.Speed != 0 {
		q.Set("speed", strconv.FormatFloat(p.Speed, 'f', -1, 64)) // knots, matched to Traccar storage
	}
	if p.Course != 0 {
		q.Set("bearing", strconv.FormatFloat(p.Course, 'f', -1, 64))
	}
	if p.Altitude != 0 {
		q.Set("altitude", strconv.FormatFloat(p.Altitude, 'f', -1, 64))
	}
	if p.Accuracy != 0 {
		q.Set("accuracy", strconv.FormatFloat(p.Accuracy, 'f', -1, 64))
	}
	// Battery, if the source recorded it. Traccar stores both as percentages.
	if b, ok := attrFloat(p.Attributes, "batteryLevel"); ok {
		q.Set("batt", strconv.FormatFloat(b, 'f', -1, 64))
	} else if b, ok := attrFloat(p.Attributes, "battery"); ok {
		q.Set("batt", strconv.FormatFloat(b, 'f', -1, 64))
	}
	return q
}

// Send replays a single position for the given destination uniqueId.
func (o *OsmAnd) Send(ctx context.Context, uniqueID string, p Position) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/?"+o.query(uniqueID, p).Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusBadRequest, http.StatusNotFound:
		// Traccar rejects positions for unknown uniqueIds with 400/404.
		return fmt.Errorf("device %q not accepted (is it registered on the destination?)", uniqueID)
	default:
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("osmand %d: %s", resp.StatusCode, msg)
	}
}

// Reachable checks that the OsmAnd endpoint is listening without writing any
// data: it posts a position for a uniqueId that cannot exist, so a 400/404
// response still proves the protocol handler is up.
func (o *OsmAnd) Reachable(ctx context.Context) error {
	probe := Position{Latitude: 0, Longitude: 0, FixTime: time.Now()}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.base+"/?"+o.query("__traccar_migrate_probe__", probe).Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach OsmAnd endpoint %s: %w", o.base, err)
	}
	resp.Body.Close()
	return nil
}

func attrFloat(attrs map[string]any, key string) (float64, bool) {
	if attrs == nil {
		return 0, false
	}
	switch v := attrs[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}
