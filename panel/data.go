package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// retryInterval caps how long a never-yet-successful data block waits
	// before it is tried again, regardless of its configured interval.
	retryInterval = 60 * time.Second

	maxDataBody  = 512 << 10
	maxDataDepth = 24
	maxRedirects = 3
)

// dataValue is the last thing we know about one (url, path) pair.
type dataValue struct {
	Text    string
	OK      bool
	At      time.Time
	Err     string
	Fetched bool
}

// dataStore fetches JSON on a schedule and hands out the last good value.
//
// A render NEVER waits on the network. Lookup returns whatever is cached — the
// last good value, marked stale if it is past its interval — and the refresher
// goroutine is what actually goes out to the internet. A slow endpoint costs a
// stale reading, not a frame.
type dataStore struct {
	mu     sync.Mutex
	values map[string]dataValue
	busy   map[string]bool
	client *http.Client

	// allowPrivate opens up loopback and RFC1918 destinations. False in
	// production: this box sits on a home LAN behind Traefik and a data block
	// pointed at 192.168.x.x or 169.254.169.254 would be a server-side request
	// forgery with the app's own network position. Tests set it true so they
	// can point at httptest.
	allowPrivate bool
}

func newDataStore(allowPrivate bool) *dataStore {
	d := &dataStore{
		values:       map[string]dataValue{},
		busy:         map[string]bool{},
		allowPrivate: allowPrivate,
	}
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return d.checkAddr(network, address)
		},
	}
	d.client = &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			DisableKeepAlives:     false,
			MaxIdleConns:          8,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("refused redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
	return d
}

// checkAddr runs on the resolved address, after DNS, on every connection
// including redirects — so a hostname that resolves to a private address, or
// re-resolves to one on the second request, is still refused.
func (d *dataStore) checkAddr(network, address string) error {
	if network != "tcp4" && network != "tcp6" && network != "tcp" {
		return fmt.Errorf("refused network %q", network)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if d.allowPrivate {
		return nil
	}
	if port != "80" && port != "443" {
		return fmt.Errorf("refused port %s: data blocks may only use 80 and 443", port)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refused unresolvable address %q", host)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("refused %s: data blocks cannot reach private or loopback addresses", ip)
	}
	// IPv4-mapped IPv6 hides a private v4 address from the checks above.
	if v4 := ip.To4(); v4 != nil && (v4[0] == 0 || v4[0] == 127 || v4[0] == 10 ||
		(v4[0] == 172 && v4[1]&0xf0 == 16) || (v4[0] == 192 && v4[1] == 168) ||
		(v4[0] == 100 && v4[1]&0xc0 == 64) || (v4[0] == 169 && v4[1] == 254)) {
		return fmt.Errorf("refused %s: data blocks cannot reach private or loopback addresses", ip)
	}
	return nil
}

func dataKey(b Block) string { return b.URL + "\x00" + b.Path }

// Lookup returns the rendered text for a data block and whether it is stale.
// It never blocks and never returns an error: a panel needs a frame, not an
// exception.
func (d *dataStore) Lookup(b Block, now time.Time) (text string, stale bool) {
	key := dataKey(b)
	d.mu.Lock()
	v, ok := d.values[key]
	d.mu.Unlock()

	if !ok || !v.OK {
		return b.Fallback, true
	}
	interval := time.Duration(b.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval * time.Second
	}
	// Two intervals of silence is where a reading stops being worth showing.
	stale = now.Sub(v.At) > 2*interval
	return applyTemplate(b.Text, v.Text), stale
}

// Status is what the editor shows next to a data block.
func (d *dataStore) Status(b Block) dataValue {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.values[dataKey(b)]
}

// Refresh fetches every block whose value is missing or past its interval.
// Called on a ticker and immediately after a save, so by the time a device
// polls there is usually already a value.
func (d *dataStore) Refresh(ctx context.Context, blocks []Block, now time.Time) {
	for _, b := range blocks {
		if b.Type != BlockData || b.URL == "" {
			continue
		}
		key := dataKey(b)
		interval := time.Duration(b.Interval) * time.Second
		if interval < minInterval*time.Second {
			interval = defaultInterval * time.Second
		}

		d.mu.Lock()
		if d.busy[key] {
			d.mu.Unlock()
			continue
		}
		v, ok := d.values[key]
		wait := interval
		if ok && !v.OK {
			// Nothing good has ever come back from this one. Retry sooner than
			// the configured interval so a URL being typed into the editor
			// starts working within a minute rather than in five, but still
			// slowly enough not to hammer whatever is broken.
			if wait > retryInterval {
				wait = retryInterval
			}
		}
		if ok && v.Fetched && now.Sub(v.At) < wait {
			d.mu.Unlock()
			continue
		}
		d.busy[key] = true
		d.mu.Unlock()

		go d.fetch(ctx, b, key)
	}
}

// RefreshNow forces a fetch regardless of interval — used right after a save
// so the editor shows a real value on the next render.
func (d *dataStore) RefreshNow(ctx context.Context, blocks []Block) {
	for _, b := range blocks {
		if b.Type != BlockData || b.URL == "" {
			continue
		}
		key := dataKey(b)
		d.mu.Lock()
		if d.busy[key] {
			d.mu.Unlock()
			continue
		}
		d.busy[key] = true
		d.mu.Unlock()
		go d.fetch(ctx, b, key)
	}
}

func (d *dataStore) fetch(ctx context.Context, b Block, key string) {
	defer func() {
		d.mu.Lock()
		delete(d.busy, key)
		d.mu.Unlock()
	}()

	timeout := time.Duration(b.Timeout) * time.Second
	if timeout < minTimeout*time.Second || timeout > maxTimeout*time.Second {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	text, err := d.get(ctx, b)

	d.mu.Lock()
	defer d.mu.Unlock()
	prev := d.values[key]
	if err != nil {
		prev.Err = err.Error()
		prev.Fetched = true
		if !prev.OK {
			prev.At = time.Now()
		}
		d.values[key] = prev
		return
	}
	d.values[key] = dataValue{Text: text, OK: true, At: time.Now(), Fetched: true}
}

func (d *dataStore) get(ctx context.Context, b Block) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "panel/2 (+https://panel.baileys.dev)")

	res, err := d.client.Do(req)
	if err != nil {
		return "", trimErr(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
		res.Body.Close()
	}()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}

	var doc any
	dec := json.NewDecoder(io.LimitReader(res.Body, maxDataBody))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return "", errors.New("response was not JSON")
	}
	v, err := extractPath(doc, b.Path)
	if err != nil {
		return "", err
	}
	return v, nil
}

// trimErr keeps network errors short and free of internal detail.
func trimErr(err error) error {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s)-i < 60 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return errors.New(s)
}

// extractPath walks a dotted path through decoded JSON. Numeric segments index
// into arrays: `list.0.name`. It is deliberately not JSONPath — this is a
// panel, not a query engine.
func extractPath(doc any, path string) (string, error) {
	parts := strings.Split(path, ".")
	if len(parts) > maxDataDepth {
		return "", fmt.Errorf("path %q is too deep", path)
	}
	cur := doc
	for i, p := range parts {
		if p == "" {
			return "", fmt.Errorf("path %q has an empty segment", path)
		}
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[p]
			if !ok {
				return "", fmt.Errorf("no %q at %s", p, pathSoFar(parts, i))
			}
			cur = next
		case []any:
			n, err := strconv.Atoi(p)
			if err != nil {
				return "", fmt.Errorf("%s is an array; %q is not an index", pathSoFar(parts, i-1), p)
			}
			if n < 0 || n >= len(node) {
				return "", fmt.Errorf("index %d is outside the %d-item array at %s", n, len(node), pathSoFar(parts, i-1))
			}
			cur = node[n]
		default:
			return "", fmt.Errorf("%s is not an object or array", pathSoFar(parts, i-1))
		}
	}
	return scalarString(cur)
}

func pathSoFar(parts []string, i int) string {
	if i < 0 {
		return "the root"
	}
	if i >= len(parts) {
		i = len(parts) - 1
	}
	return strings.Join(parts[:i+1], ".")
}

func scalarString(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", errors.New("that path holds null")
	case string:
		return t, nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return t.String(), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case map[string]any:
		return "", errors.New("that path holds an object, not a value")
	case []any:
		return "", errors.New("that path holds an array, not a value")
	default:
		return fmt.Sprint(t), nil
	}
}

// applyTemplate substitutes the fetched value into the block's little text
// template. Not html/template: the output is pixels, not markup.
func applyTemplate(tpl, v string) string {
	if tpl == "" {
		return v
	}
	out := strings.ReplaceAll(tpl, "{{v}}", v)
	return strings.ReplaceAll(out, "{{value}}", v)
}

// runRefresher drives the background fetches until the context is cancelled.
func runRefresher(ctx context.Context, store *Store, data *dataStore) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		var blocks []Block
		for _, sc := range store.Screens() {
			blocks = append(blocks, sc.DataBlocks()...)
		}
		data.Refresh(ctx, blocks, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

var _ = log.Printf
