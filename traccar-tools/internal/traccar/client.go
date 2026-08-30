// Package traccar provides a small client for the Traccar REST API plus a
// sender for the OsmAnd ingestion protocol. It is deliberately dependency-free.
package traccar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Auth carries credentials for a Traccar instance. A Token (an API token
// generated in the Traccar UI) is preferred; otherwise Email/Password are sent
// as HTTP Basic auth, which Traccar accepts on /api endpoints.
type Auth struct {
	Token    string `json:"token"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Conn identifies a Traccar instance to talk to.
type Conn struct {
	BaseURL string `json:"baseUrl"` // e.g. https://track.example.com (with or without trailing /api)
	Auth    Auth   `json:"auth"`
}

// Client talks to a single Traccar REST API.
type Client struct {
	conn Conn
	http *http.Client
}

func NewClient(conn Conn) *Client {
	conn.BaseURL = normalizeBase(conn.BaseURL)
	return &Client{
		conn: conn,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// normalizeBase trims trailing slashes and any trailing /api so callers can
// paste either "https://host" or "https://host/api".
func normalizeBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, "/api")
	return base
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.conn.BaseURL+"/api"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.conn.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.conn.Auth.Token)
	} else if c.conn.Auth.Email != "" {
		req.SetBasicAuth(c.conn.Auth.Email, c.conn.Auth.Password)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("traccar %s: %s", req.URL.Path, msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Device is a subset of the Traccar device model.
type Device struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	UniqueID   string `json:"uniqueId"`
	Status     string `json:"status"`
	LastUpdate string `json:"lastUpdate"`
}

// ServerInfo is used purely to validate a connection.
type ServerInfo struct {
	ID       int    `json:"id"`
	Version  string `json:"version"`
	Map      string `json:"map"`
	Timezone string `json:"timezone"`
}

func (c *Client) Server(ctx context.Context) (*ServerInfo, error) {
	req, err := c.request(ctx, http.MethodGet, "/server", nil)
	if err != nil {
		return nil, err
	}
	var s ServerInfo
	if err := c.do(req, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	req, err := c.request(ctx, http.MethodGet, "/devices", nil)
	if err != nil {
		return nil, err
	}
	var d []Device
	if err := c.do(req, &d); err != nil {
		return nil, err
	}
	return d, nil
}

// CreateDevice registers a new device on the instance. uniqueId must be unique.
func (c *Client) CreateDevice(ctx context.Context, name, uniqueID string) (*Device, error) {
	payload, _ := json.Marshal(map[string]string{"name": name, "uniqueId": uniqueID})
	req, err := c.request(ctx, http.MethodPost, "/devices", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var d Device
	if err := c.do(req, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Position is a subset of the Traccar position model returned by reports/route.
type Position struct {
	ID         int            `json:"id"`
	DeviceID   int            `json:"deviceId"`
	Protocol   string         `json:"protocol"`
	FixTime    time.Time      `json:"fixTime"`
	DeviceTime time.Time      `json:"deviceTime"`
	ServerTime time.Time      `json:"serverTime"`
	Latitude   float64        `json:"latitude"`
	Longitude  float64        `json:"longitude"`
	Altitude   float64        `json:"altitude"`
	Speed      float64        `json:"speed"` // knots, as stored by Traccar
	Course     float64        `json:"course"`
	Accuracy   float64        `json:"accuracy"`
	Valid      bool           `json:"valid"`
	Address    string         `json:"address"`
	Attributes map[string]any `json:"attributes"`
}

// Time returns the best timestamp for a position.
func (p Position) Time() time.Time {
	if !p.FixTime.IsZero() {
		return p.FixTime
	}
	return p.DeviceTime
}

// Route fetches positions for a device within [from, to]. Traccar caps the
// window server-side, so callers should chunk large ranges via RouteChunked.
func (c *Client) Route(ctx context.Context, deviceID int, from, to time.Time) ([]Position, error) {
	q := url.Values{}
	q.Set("deviceId", fmt.Sprintf("%d", deviceID))
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	req, err := c.request(ctx, http.MethodGet, "/reports/route?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var p []Position
	if err := c.do(req, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// RouteChunked walks [from, to] in windows of size step, invoking fn with each
// batch in chronological order. It stops early if fn returns an error or the
// context is cancelled.
func (c *Client) RouteChunked(ctx context.Context, deviceID int, from, to time.Time, step time.Duration, fn func([]Position) error) error {
	if step <= 0 {
		step = 7 * 24 * time.Hour
	}
	for cur := from; cur.Before(to); cur = cur.Add(step) {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := cur.Add(step)
		if end.After(to) {
			end = to
		}
		batch, err := c.Route(ctx, deviceID, cur, end)
		if err != nil {
			return err
		}
		if err := fn(batch); err != nil {
			return err
		}
	}
	return nil
}
