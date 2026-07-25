package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Content caps. Stated in the UI, enforced here, and the renderer truncates
// whatever still does not fit the panel.
const (
	maxRows        = 24
	maxTitleLen    = 64
	maxLabelLen    = 48
	maxValueLen    = 48
	maxFooterLen   = 120
	maxRequestBody = 32 << 10
)

// Row is one label/value line on the panel.
type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Config is everything the panel shows, apart from the clock.
type Config struct {
	Title     string    `json:"title"`
	Rows      []Row     `json:"rows"`
	Footer    string    `json:"footer"`
	DeviceKey string    `json:"device_key"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Version identifies this exact content for cache keys and ETags.
func (c Config) Version() string {
	return fmt.Sprintf("%d", c.UpdatedAt.UnixNano())
}

func defaultConfig() Config {
	return Config{
		Title:  "panel",
		Rows:   []Row{},
		Footer: "",
	}
}

// Store holds the config in memory behind an RWMutex and persists it with an
// atomic write. Tiny data; a database would be a bigger liability than the file.
type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "panel.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		var cfg Config
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
		s.cfg = cfg
	case errors.Is(err, fs.ErrNotExist):
		s.cfg = defaultConfig()
	default:
		return nil, err
	}

	if s.cfg.Rows == nil {
		s.cfg.Rows = []Row{}
	}
	if s.cfg.DeviceKey == "" {
		key, err := newDeviceKey()
		if err != nil {
			return nil, err
		}
		s.cfg.DeviceKey = key
		s.cfg.UpdatedAt = time.Now().UTC()
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.Rows = append([]Row(nil), s.cfg.Rows...)
	return cfg
}

// Save replaces the content (not the device key) and persists.
func (s *Store) Save(title string, rows []Row, footer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Title = clean(title, maxTitleLen)
	s.cfg.Footer = clean(footer, maxFooterLen)
	out := make([]Row, 0, maxRows)
	for _, r := range rows {
		l := clean(r.Label, maxLabelLen)
		v := clean(r.Value, maxValueLen)
		if l == "" && v == "" {
			continue
		}
		out = append(out, Row{Label: l, Value: v})
		if len(out) == maxRows {
			break
		}
	}
	s.cfg.Rows = out
	s.cfg.UpdatedAt = time.Now().UTC()
	return s.persist()
}

// RotateKey issues a fresh device key, invalidating every device URL.
func (s *Store) RotateKey() (string, error) {
	key, err := newDeviceKey()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.DeviceKey = key
	s.cfg.UpdatedAt = time.Now().UTC()
	if err := s.persist(); err != nil {
		return "", err
	}
	return key, nil
}

// persist writes to a temp file in the same directory, fsyncs it, renames it
// over the target, then fsyncs the directory. A crash leaves either the old
// file or the new one, never half of either. Caller holds the write lock.
func (s *Store) persist() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".panel-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// deviceKeyAlphabet is Crockford-ish base32 without ambiguous characters, from
// the standard base32 alphabet with I, L, O and U removed by substitution.
var deviceKeyEnc = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// newDeviceKey returns a 128-bit capability id. It is not a password: it is an
// unguessable URL component so a battery panel can GET its image without
// carrying the app token.
func newDeviceKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(deviceKeyEnc.EncodeToString(b)), nil
}

// clean strips control characters, collapses whitespace and enforces a rune cap.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return strings.TrimSpace(string(r))
}
