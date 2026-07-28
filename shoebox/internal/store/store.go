// Package store persists receipts on disk: one directory per receipt holding
// meta.json, the original raw.eml, extracted body.html/body.txt, and an att/
// directory of attachment payloads. Metadata is kept in memory for listing.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("receipt not found")

type Attachment struct {
	File        string `json:"file"` // filename under att/
	Name        string `json:"name"` // display name
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	ContentID   string `json:"content_id,omitempty"`
	Inline      bool   `json:"inline,omitempty"`
	Generated   bool   `json:"generated,omitempty"` // produced by us (email.pdf), not from the sender
}

type Receipt struct {
	ID          string       `json:"id"`
	From        string       `json:"from"`
	FromName    string       `json:"from_name,omitempty"`
	ForwardedBy string       `json:"forwarded_by,omitempty"` // owner address the forward came through
	To          string       `json:"to,omitempty"`
	Subject     string       `json:"subject"`
	Date        time.Time    `json:"date"`
	ReceivedAt  time.Time    `json:"received_at"`
	Source      string       `json:"source"`       // smtp | upload
	AmountCents int64        `json:"amount_cents"` // -1 = unknown
	AmountAuto  bool         `json:"amount_auto,omitempty"`
	Notes       string       `json:"notes,omitempty"`
	HasHTML     bool         `json:"has_html,omitempty"`
	HasText     bool         `json:"has_text,omitempty"`
	HasRaw      bool         `json:"has_raw,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// FileCount is the number of proper attachments the sender included —
// inline images and our generated email.pdf don't count.
func (r Receipt) FileCount() int {
	n := 0
	for _, a := range r.Attachments {
		if !a.Inline && !a.Generated {
			n++
		}
	}
	return n
}

type File struct {
	Name string
	Data []byte
}

// Bundle is everything needed to persist one receipt.
type Bundle struct {
	Meta     Receipt
	Raw      []byte // original .eml, may be nil for direct uploads
	BodyHTML []byte
	BodyText []byte
	Files    []File // payloads; names match Meta.Attachments[].File
}

type Store struct {
	root string
	mu   sync.RWMutex
	byID map[string]*Receipt
}

func Open(root string) (*Store, error) {
	dir := filepath.Join(root, "receipts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root, byID: map[string]*Receipt{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var r Receipt
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			continue
		}
		s.byID[r.ID] = &r
	}
	return s, nil
}

func (s *Store) dir(id string) string { return filepath.Join(s.root, "receipts", id) }

func newID(t time.Time) string {
	b := make([]byte, 3)
	rand.Read(b)
	return t.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}

// Save writes the bundle to disk atomically (staging dir + rename), assigns
// the receipt its ID, and registers it in memory.
func (s *Store) Save(b *Bundle) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := newID(b.Meta.ReceivedAt)
	for s.byID[id] != nil {
		id = newID(b.Meta.ReceivedAt)
	}
	b.Meta.ID = id

	tmp := filepath.Join(s.root, "receipts", ".tmp-"+id)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return Receipt{}, err
	}
	fail := func(err error) (Receipt, error) {
		os.RemoveAll(tmp)
		return Receipt{}, err
	}
	write := func(name string, data []byte) error {
		if len(data) == 0 {
			return nil
		}
		return os.WriteFile(filepath.Join(tmp, name), data, 0o644)
	}

	meta, err := json.MarshalIndent(b.Meta, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := write("meta.json", meta); err != nil {
		return fail(err)
	}
	if err := write("raw.eml", b.Raw); err != nil {
		return fail(err)
	}
	if err := write("body.html", b.BodyHTML); err != nil {
		return fail(err)
	}
	if err := write("body.txt", b.BodyText); err != nil {
		return fail(err)
	}
	if len(b.Files) > 0 {
		if err := os.MkdirAll(filepath.Join(tmp, "att"), 0o755); err != nil {
			return fail(err)
		}
		for _, f := range b.Files {
			if err := os.WriteFile(filepath.Join(tmp, "att", f.Name), f.Data, 0o644); err != nil {
				return fail(err)
			}
		}
	}
	if err := os.Rename(tmp, s.dir(id)); err != nil {
		return fail(err)
	}

	r := b.Meta
	s.byID[id] = &r
	return r, nil
}

// List returns all receipts, newest first.
func (s *Store) List() []Receipt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Receipt, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date.Equal(out[j].Date) {
			return out[i].ID > out[j].ID
		}
		return out[i].Date.After(out[j].Date)
	})
	return out
}

func (s *Store) Get(id string) (Receipt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.byID[id]
	if r == nil {
		return Receipt{}, false
	}
	return *r, true
}

// Update sets the amount (in cents, -1 for unknown) and notes.
func (s *Store) Update(id string, cents int64, notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.byID[id]
	if r == nil {
		return ErrNotFound
	}
	r.AmountCents = cents
	r.AmountAuto = false
	r.Notes = notes
	meta, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir(id), "meta.json"), meta, 0o644)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID[id] == nil {
		return ErrNotFound
	}
	if err := os.RemoveAll(s.dir(id)); err != nil {
		return err
	}
	delete(s.byID, id)
	return nil
}

// Headers are the walked-back identity fields of a receipt — everything
// ReparseHeaders may rewrite; amounts and notes are deliberately excluded.
type Headers struct {
	From, FromName, ForwardedBy, To, Subject string
	Date                                     time.Time
}

func (s *Store) UpdateHeaders(id string, h Headers) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.byID[id]
	if r == nil {
		return ErrNotFound
	}
	r.From, r.FromName, r.ForwardedBy = h.From, h.FromName, h.ForwardedBy
	r.To, r.Subject, r.Date = h.To, h.Subject, h.Date
	meta, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir(id), "meta.json"), meta, 0o644)
}

// RemoveGenerated drops our generated attachments (email.pdf) so they can be
// rebuilt after a header change.
func (s *Store) RemoveGenerated(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.byID[id]
	if r == nil {
		return ErrNotFound
	}
	kept := make([]Attachment, 0, len(r.Attachments))
	for _, a := range r.Attachments {
		if a.Generated {
			os.Remove(filepath.Join(s.dir(id), "att", a.File))
			continue
		}
		kept = append(kept, a)
	}
	if len(kept) == len(r.Attachments) {
		return nil
	}
	r.Attachments = kept
	meta, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir(id), "meta.json"), meta, 0o644)
}

// AddAttachment writes one more attachment payload and updates the metadata.
func (s *Store) AddAttachment(id string, att Attachment, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.byID[id]
	if r == nil {
		return ErrNotFound
	}
	if err := os.MkdirAll(filepath.Join(s.dir(id), "att"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir(id), "att", att.File), data, 0o644); err != nil {
		return err
	}
	r.Attachments = append(r.Attachments, att)
	meta, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir(id), "meta.json"), meta, 0o644)
}

func (s *Store) RawPath(id string) string { return filepath.Join(s.dir(id), "raw.eml") }

func (s *Store) BodyPath(id string, html bool) string {
	if html {
		return filepath.Join(s.dir(id), "body.html")
	}
	return filepath.Join(s.dir(id), "body.txt")
}

func (s *Store) AttPath(id, file string) string {
	return filepath.Join(s.dir(id), "att", filepath.Base(file))
}
