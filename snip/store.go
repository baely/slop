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

// MaxPasteBytes is the hard cap on a single paste body.
const MaxPasteBytes = 1_000_000

// MaxPastes caps how many live pastes the store will hold.
const MaxPastes = 5000

// slugAlphabet is Crockford base32, lowercased: no i, l, o or u, so a slug
// cannot be misread or turn into a word. 32 symbols means a random byte can be
// masked to 5 bits with no modulo bias.
const slugAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// slugLen symbols of 5 bits each = 100 bits of entropy.
const slugLen = 20

var (
	// ErrNotFound covers "never existed", "expired" and "already burned". The
	// caller must not be able to tell them apart.
	ErrNotFound = errors.New("not found")
	ErrFull     = errors.New("store full")
	ErrTooLarge = errors.New("paste too large")
)

// Paste is one stored paste. Expires is the zero time when the paste never
// expires. Body is only populated when the record is read from disk.
type Paste struct {
	Slug    string    `json:"slug"`
	Title   string    `json:"title,omitempty"`
	Body    string    `json:"body"`
	Size    int       `json:"size"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires,omitempty"`
	Burn    bool      `json:"burn,omitempty"`
}

// Expired reports whether the paste is past its expiry at t.
func (p Paste) Expired(t time.Time) bool {
	return !p.Expires.IsZero() && !t.Before(p.Expires)
}

// Store keeps paste metadata in memory and one JSON file per paste on disk.
// Bodies are not held in memory: a paste can be 1 MB and there can be 5000 of
// them, so the index stays small and the body is read on demand.
type Store struct {
	mu   sync.Mutex
	dir  string
	meta map[string]Paste // Body is always empty here
	now  func() time.Time
}

// NewStore opens (creating if needed) the paste directory and loads the index.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, meta: map[string]Paste{}, now: time.Now}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".json")
		if !validSlug(slug) {
			continue
		}
		p, err := s.readFile(slug)
		if err != nil {
			// A truncated file is not worth killing the process over; skip it.
			continue
		}
		p.Body = ""
		s.meta[slug] = p
	}
	return nil
}

func (s *Store) path(slug string) string {
	return filepath.Join(s.dir, slug+".json")
}

func (s *Store) readFile(slug string) (Paste, error) {
	b, err := os.ReadFile(s.path(slug))
	if err != nil {
		return Paste{}, err
	}
	var p Paste
	if err := json.Unmarshal(b, &p); err != nil {
		return Paste{}, err
	}
	if p.Slug != slug {
		return Paste{}, fmt.Errorf("slug mismatch in %s", slug)
	}
	return p, nil
}

// Create stores a new paste and returns it (with its generated slug).
func (s *Store) Create(title, body string, ttl time.Duration, burn bool) (Paste, error) {
	if len(body) > MaxPasteBytes {
		return Paste{}, ErrTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepLocked()
	if len(s.meta) >= MaxPastes {
		return Paste{}, ErrFull
	}

	now := s.now()
	p := Paste{
		Title:   title,
		Body:    body,
		Size:    len(body),
		Created: now,
		Burn:    burn,
	}
	if ttl > 0 {
		p.Expires = now.Add(ttl)
	}

	for attempt := 0; ; attempt++ {
		slug, err := newSlug()
		if err != nil {
			return Paste{}, err
		}
		if _, clash := s.meta[slug]; clash {
			if attempt > 8 {
				return Paste{}, errors.New("could not allocate a slug")
			}
			continue
		}
		p.Slug = slug
		break
	}

	blob, err := json.Marshal(p)
	if err != nil {
		return Paste{}, err
	}
	if err := writeFileAtomic(s.path(p.Slug), blob); err != nil {
		return Paste{}, err
	}
	meta := p
	meta.Body = ""
	s.meta[p.Slug] = meta
	return p, nil
}

// Fetch returns the paste with its body. An expired paste is treated (and
// collected) as missing. A burn-after-reading paste is destroyed durably
// before this returns, and only one concurrent caller can win: the whole
// operation runs under the store's exclusive lock, and if the destroy fails
// the body is not returned at all.
func (s *Store) Fetch(slug string) (Paste, error) {
	if !validSlug(slug) {
		return Paste{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, ok := s.meta[slug]
	if !ok {
		return Paste{}, ErrNotFound
	}
	if meta.Expired(s.now()) {
		s.destroyLocked(slug)
		return Paste{}, ErrNotFound
	}
	p, err := s.readFile(slug)
	if err != nil {
		// Index and disk disagree; drop the index entry and 404.
		delete(s.meta, slug)
		return Paste{}, ErrNotFound
	}
	if meta.Burn {
		if err := s.destroyLocked(slug); err != nil {
			// Could not guarantee destruction, so do not serve the body.
			return Paste{}, err
		}
	}
	return p, nil
}

// Peek returns paste metadata without the body and without burning it. It is
// used for HEAD requests: a HEAD is not a read, so it must not consume a
// burn-after-reading paste.
func (s *Store) Peek(slug string) (Paste, error) {
	if !validSlug(slug) {
		return Paste{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.meta[slug]
	if !ok {
		return Paste{}, ErrNotFound
	}
	if meta.Expired(s.now()) {
		s.destroyLocked(slug)
		return Paste{}, ErrNotFound
	}
	return meta, nil
}

// Delete removes a paste. Missing pastes are reported as ErrNotFound.
func (s *Store) Delete(slug string) error {
	if !validSlug(slug) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.meta[slug]; !ok {
		return ErrNotFound
	}
	return s.destroyLocked(slug)
}

// destroyLocked unlinks the paste file, fsyncs the directory so the unlink is
// durable, and drops the index entry. The index entry is only dropped once the
// file is actually gone.
func (s *Store) destroyLocked(slug string) error {
	if err := os.Remove(s.path(slug)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDir(s.dir); err != nil {
		return err
	}
	delete(s.meta, slug)
	return nil
}

// List returns metadata for every live paste, newest first.
func (s *Store) List() []Paste {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	out := make([]Paste, 0, len(s.meta))
	for _, p := range s.meta {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created.Equal(out[j].Created) {
			return out[i].Slug < out[j].Slug
		}
		return out[i].Created.After(out[j].Created)
	})
	return out
}

// Sweep collects expired pastes and returns how many it removed.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweepLocked()
}

func (s *Store) sweepLocked() int {
	now := s.now()
	var dead []string
	for slug, p := range s.meta {
		if p.Expired(now) {
			dead = append(dead, slug)
		}
	}
	n := 0
	for _, slug := range dead {
		if err := s.destroyLocked(slug); err == nil {
			n++
		}
	}
	return n
}

// Len is the number of live pastes currently indexed.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.meta)
}

// newSlug returns an unguessable capability id from crypto/rand.
func newSlug() (string, error) {
	buf := make([]byte, slugLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, slugLen)
	for i, b := range buf {
		out[i] = slugAlphabet[b&31]
	}
	return string(out), nil
}

// validSlug guards both lookups and filenames: nothing outside the alphabet
// can reach the filesystem.
func validSlug(s string) bool {
	if len(s) != slugLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(slugAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}

// writeFileAtomic writes via a temp file in the same directory, fsyncs it,
// renames it into place and fsyncs the directory.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
