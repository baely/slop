package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxRequestsPerBin is the retention cap: only the newest N are kept.
	maxRequestsPerBin = 100
	// binTTL is how long a bin survives without being written to or read.
	binTTL = 30 * 24 * time.Hour
	// bodyStoreCap is the largest body we keep on disk, per request.
	bodyStoreCap = 64 << 10
	// idAlphabet is Crockford-ish base32: no i, l, o, u.
	idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	// binIDLen * 5 bits = 100 bits of entropy.
	binIDLen = 20
	reqIDLen = 12
)

var errNotFound = errors.New("bin not found")

// Header is one header name and every value sent for it.
type Header struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Request is one captured HTTP request. Immutable once stored.
type Request struct {
	ID          string    `json:"id"`
	At          time.Time `json:"at"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`     // full path as received
	SubPath     string    `json:"sub_path"` // path below /b/<id>
	Query       string    `json:"query"`    // raw query string
	Headers     []Header  `json:"headers"`
	Body        []byte    `json:"body"` // capped at bodyStoreCap
	BodySize    int64     `json:"body_size"`
	Truncated   bool      `json:"truncated"`
	RemoteIP    string    `json:"remote_ip"`
	ContentType string    `json:"content_type"`
}

// Bin is a capture endpoint plus its recent requests, newest first.
type Bin struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Created  time.Time  `json:"created"`
	LastSeen time.Time  `json:"last_seen"` // last captured request, zero if none
	Touched  time.Time  `json:"touched"`   // last capture or view; drives expiry
	Status   int        `json:"status"`    // response status for captures
	Requests []*Request `json:"requests"`
}

// Expires reports when the bin is swept if nothing else touches it.
func (b *Bin) Expires() time.Time { return b.Touched.Add(binTTL) }

// Store keeps every bin in memory and mirrors each one to its own JSON file.
type Store struct {
	dir  string
	mu   sync.RWMutex
	bins map[string]*Bin
}

// OpenStore loads every bin file in dir, dropping any that already expired.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, bins: map[string]*Bin{}}
	matches, err := filepath.Glob(filepath.Join(dir, "bin-*.json"))
	if err != nil {
		return nil, err
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		var b Bin
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		if b.ID == "" {
			continue
		}
		if b.Status == 0 {
			b.Status = 200
		}
		if b.Touched.IsZero() {
			b.Touched = b.Created
		}
		s.bins[b.ID] = &b
	}
	return s, nil
}

// NewID returns an unguessable id from crypto/rand over idAlphabet.
func NewID(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, c := range buf {
		sb.WriteByte(idAlphabet[int(c)%len(idAlphabet)])
	}
	return sb.String(), nil
}

// Create makes a new bin. status must be a plausible HTTP status.
func (s *Store) Create(label string, status int, now time.Time) (*Bin, error) {
	if status < 100 || status > 599 {
		status = 200
	}
	if len(label) > 80 {
		label = label[:80]
	}
	id, err := NewID(binIDLen)
	if err != nil {
		return nil, err
	}
	b := &Bin{
		ID:       id,
		Label:    strings.TrimSpace(label),
		Created:  now,
		Touched:  now,
		Status:   status,
		Requests: []*Request{},
	}
	s.mu.Lock()
	s.bins[id] = b
	err = s.persist(b)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return b.clone(), nil
}

// Get returns a snapshot of the bin. Requests are immutable so the slice is
// copied but the elements are shared.
func (s *Store) Get(id string) (*Bin, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bins[id]
	if !ok {
		return nil, false
	}
	return b.clone(), true
}

// List returns every bin, newest activity first.
func (s *Store) List() []*Bin {
	s.mu.RLock()
	out := make([]*Bin, 0, len(s.bins))
	for _, b := range s.bins {
		out = append(out, b.clone())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Touched.Equal(out[j].Touched) {
			return out[i].ID < out[j].ID
		}
		return out[i].Touched.After(out[j].Touched)
	})
	return out
}

// Capture stores a request against a bin and returns the bin's reply status.
func (s *Store) Capture(id string, req *Request) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bins[id]
	if !ok {
		return 0, errNotFound
	}
	b.Requests = append([]*Request{req}, b.Requests...)
	if len(b.Requests) > maxRequestsPerBin {
		b.Requests = b.Requests[:maxRequestsPerBin]
	}
	b.LastSeen = req.At
	b.Touched = req.At
	return b.Status, s.persist(b)
}

// Touch marks a bin as still in use (a view counts, not just a capture).
func (s *Store) Touch(id string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bins[id]
	if !ok {
		return
	}
	// Persist at most hourly, so polling a busy bin does not rewrite a
	// multi-megabyte file every few seconds. In-memory is enough to keep the
	// sweeper away in the meantime.
	stale := now.Sub(b.Touched) >= time.Hour
	b.Touched = now
	if stale {
		_ = s.persist(b)
	}
}

// Update changes a bin's label and reply status.
func (s *Store) Update(id, label string, status int, now time.Time) error {
	if status < 100 || status > 599 {
		return errors.New("status out of range")
	}
	if len(label) > 80 {
		label = label[:80]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bins[id]
	if !ok {
		return errNotFound
	}
	b.Label = strings.TrimSpace(label)
	b.Status = status
	b.Touched = now
	return s.persist(b)
}

// Clear drops every captured request but keeps the bin.
func (s *Store) Clear(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bins[id]
	if !ok {
		return errNotFound
	}
	b.Requests = []*Request{}
	b.LastSeen = time.Time{}
	b.Touched = now
	return s.persist(b)
}

// Delete removes the bin and its file.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bins[id]; !ok {
		return errNotFound
	}
	delete(s.bins, id)
	err := os.Remove(s.path(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Sweep removes bins untouched for longer than binTTL.
func (s *Store) Sweep(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, b := range s.bins {
		if now.Sub(b.Touched) > binTTL {
			delete(s.bins, id)
			if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
				continue
			}
			n++
		}
	}
	return n
}

// Stats returns the number of bins and the total requests held.
func (s *Store) Stats() (bins, requests int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, b := range s.bins {
		bins++
		requests += len(b.Requests)
	}
	return bins, requests
}

func (b *Bin) clone() *Bin {
	c := *b
	c.Requests = append([]*Request(nil), b.Requests...)
	return &c
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, "bin-"+id+".json")
}

// persist writes the bin atomically: temp file in the same dir, fsync, rename.
// Callers hold s.mu.
func (s *Store) persist(b *Bin) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(b.ID), raw)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(name, 0o600); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
