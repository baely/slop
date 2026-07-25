package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Caps. The data volume here is tiny by design; these exist so a bot loop
// cannot turn a 40 KB JSON file into a 4 GB one.
const (
	maxLinks           = 2000
	maxUniquesPerLink  = 5000
	maxReferrerBuckets = 100
	dayRetention       = 90 // days of per-day counters kept per link
	chartDays          = 30
)

// UA buckets. Recorded rather than guessed at display time so the honest
// number is always available.
const (
	bucketBrowser = "browser"
	bucketBot     = "bot"
	bucketUnknown = "unknown"
)

// Link is one shortened URL plus its click ledger.
//
// Note what is NOT here: no raw IP addresses, no raw User-Agent strings, no
// full referrer URLs. Uniqueness is a truncated HMAC keyed by a per-install
// salt, which cannot be walked back to an address.
type Link struct {
	Slug      string     `json:"slug"`
	Target    string     `json:"target"`
	Created   time.Time  `json:"created"`
	Disabled  bool       `json:"disabled"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	MaxClicks int        `json:"max_clicks,omitempty"`

	Clicks        int        `json:"clicks"`
	BrowserClicks int        `json:"browser_clicks"`
	BotClicks     int        `json:"bot_clicks"`
	UnknownClicks int        `json:"unknown_clicks"`
	FirstClick    *time.Time `json:"first_click,omitempty"`
	LastClick     *time.Time `json:"last_click,omitempty"`

	Days      map[string]int `json:"days,omitempty"`      // non-bot clicks per local date
	BotDays   map[string]int `json:"bot_days,omitempty"`  // bot/prefetch clicks per local date
	Referrers map[string]int `json:"referrers,omitempty"` // origin -> non-bot clicks

	Uniques       []string `json:"uniques,omitempty"`
	UniquesCapped bool     `json:"uniques_capped,omitempty"`

	uniqueSet map[string]struct{}
}

// HumanClicks counts everything we did not classify as a bot or prefetch.
func (l *Link) HumanClicks() int { return l.BrowserClicks + l.UnknownClicks }

// UniqueCount is deliberately named "-ish" in the UI: it is distinct
// salted IP+UA hashes, which under-counts shared NAT and over-counts people
// who switch networks or browsers.
func (l *Link) UniqueCount() int { return len(l.Uniques) }

type linkState int

const (
	stateActive linkState = iota
	stateDisabled
	stateExpired
	stateExhausted
)

func (s linkState) String() string {
	switch s {
	case stateDisabled:
		return "Disabled"
	case stateExpired:
		return "Expired"
	case stateExhausted:
		return "Exhausted"
	}
	return "Active"
}

// State reports whether the link still resolves at time now.
func (l *Link) State(now time.Time) linkState {
	if l.Disabled {
		return stateDisabled
	}
	if l.ExpiresAt != nil && !now.Before(*l.ExpiresAt) {
		return stateExpired
	}
	if l.MaxClicks > 0 && l.Clicks >= l.MaxClicks {
		return stateExhausted
	}
	return stateActive
}

func (l *Link) clone() *Link {
	c := *l
	c.Days = copyCounts(l.Days)
	c.BotDays = copyCounts(l.BotDays)
	c.Referrers = copyCounts(l.Referrers)
	c.Uniques = append([]string(nil), l.Uniques...)
	c.uniqueSet = nil
	if l.ExpiresAt != nil {
		t := *l.ExpiresAt
		c.ExpiresAt = &t
	}
	if l.FirstClick != nil {
		t := *l.FirstClick
		c.FirstClick = &t
	}
	if l.LastClick != nil {
		t := *l.LastClick
		c.LastClick = &t
	}
	return &c
}

func copyCounts(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

type persisted struct {
	Version int     `json:"version"`
	Salt    string  `json:"salt"`
	Links   []*Link `json:"links"`
}

// Store owns every link. All state lives in memory behind the mutex; the JSON
// file on disk is a faithful snapshot rewritten atomically.
type Store struct {
	mu    sync.RWMutex
	dir   string
	path  string
	salt  []byte
	links map[string]*Link // key: slugKey(slug)
	dirty bool
}

var errNotFound = errors.New("link not found")

// conflictError marks a slug collision so callers can answer 409 rather than
// lumping it in with every other bad request.
type conflictError struct{ msg string }

func (e conflictError) Error() string { return e.msg }

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:   dir,
		path:  filepath.Join(dir, "stub.json"),
		links: map[string]*Link{},
	}
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		var p persisted
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
		salt, err := hex.DecodeString(p.Salt)
		if err != nil || len(salt) < 16 {
			return nil, fmt.Errorf("%s: stored salt is missing or malformed", s.path)
		}
		s.salt = salt
		for _, l := range p.Links {
			l.uniqueSet = make(map[string]struct{}, len(l.Uniques))
			for _, u := range l.Uniques {
				l.uniqueSet[u] = struct{}{}
			}
			s.links[slugKey(l.Slug)] = l
		}
	case errors.Is(err, os.ErrNotExist):
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		s.salt = salt
		if err := s.save(); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return s, nil
}

// save writes the whole state to a temp file in the same directory, fsyncs it,
// renames it over the real file, then fsyncs the directory. A crash at any
// point leaves either the old file or the new one — never half of either.
// Callers must hold the write lock.
func (s *Store) save() error {
	links := make([]*Link, 0, len(s.links))
	for _, l := range s.links {
		links = append(links, l)
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Created.Before(links[j].Created) })

	data, err := json.Marshal(persisted{Version: 1, Salt: hex.EncodeToString(s.salt), Links: links})
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(s.dir, "stub-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	if d, err := os.Open(s.dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	s.dirty = false
	return nil
}

// Flush persists if a click has been recorded since the last write.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	return s.save()
}

// visitorHash is the whole privacy story: HMAC-SHA256 over IP + UA keyed by a
// 32-byte per-install salt that never leaves the data directory, truncated to
// 12 hex characters. Reversing it would require the salt and a dictionary of
// every IP/UA pair; we keep neither.
func (s *Store) visitorHash(ip, ua string) string {
	m := hmac.New(sha256.New, s.salt)
	m.Write([]byte(ip))
	m.Write([]byte{0})
	m.Write([]byte(ua))
	return hex.EncodeToString(m.Sum(nil))[:12]
}

type CreateParams struct {
	Slug      string // optional; generated when empty
	Target    string
	ExpiresAt *time.Time
	MaxClicks int
}

// Create validates and stores a new link.
func (s *Store) Create(p CreateParams, now time.Time) (*Link, error) {
	target, err := validateTarget(p.Target)
	if err != nil {
		return nil, err
	}
	if p.MaxClicks < 0 {
		return nil, errors.New("Max clicks cannot be negative.")
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
		return nil, errors.New("Expiry is in the past. Pick a time in the future or leave it blank.")
	}

	slug := ""
	if p.Slug != "" {
		slug, err = normalizeSlug(p.Slug)
		if err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.links) >= maxLinks {
		return nil, fmt.Errorf("Link limit reached — %d of %d stored. Delete something first.", len(s.links), maxLinks)
	}

	if slug != "" {
		if _, exists := s.links[slugKey(slug)]; exists {
			return nil, conflictError{fmt.Sprintf("Slug %q is already taken. Pick another, or leave the field blank for a generated one.", slug)}
		}
	} else {
		for attempt := 0; ; attempt++ {
			n := defaultSlugLen + attempt/8 // widen if we somehow keep colliding
			cand, err := randomID(n)
			if err != nil {
				return nil, errors.New("Could not generate a slug.")
			}
			if reservedSlugs[cand] {
				continue
			}
			if _, exists := s.links[slugKey(cand)]; !exists {
				slug = cand
				break
			}
			if attempt > 64 {
				return nil, errors.New("Could not generate a free slug.")
			}
		}
	}

	l := &Link{
		Slug:      slug,
		Target:    target,
		Created:   now,
		ExpiresAt: p.ExpiresAt,
		MaxClicks: p.MaxClicks,
		uniqueSet: map[string]struct{}{},
	}
	s.links[slugKey(slug)] = l
	if err := s.save(); err != nil {
		delete(s.links, slugKey(slug))
		return nil, fmt.Errorf("Could not persist the link: %w", err)
	}
	return l.clone(), nil
}

// Get returns a copy of one link.
func (s *Store) Get(slug string) (*Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[slugKey(slug)]
	if !ok {
		return nil, false
	}
	return l.clone(), true
}

// List returns copies of every link, newest first.
func (s *Store) List() []*Link {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Link, 0, len(s.links))
	for _, l := range s.links {
		out = append(out, l.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.links)
}

func (s *Store) Delete(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := slugKey(slug)
	l, ok := s.links[k]
	if !ok {
		return errNotFound
	}
	delete(s.links, k)
	if err := s.save(); err != nil {
		s.links[k] = l
		return err
	}
	return nil
}

func (s *Store) SetDisabled(slug string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[slugKey(slug)]
	if !ok {
		return errNotFound
	}
	prev := l.Disabled
	l.Disabled = disabled
	if err := s.save(); err != nil {
		l.Disabled = prev
		return err
	}
	return nil
}

// Visit describes one inbound request to a short link.
type Visit struct {
	IP       string
	UA       string
	Referer  string
	Prefetch bool
	Now      time.Time
}

// Resolve looks a slug up, records the click if it is live, and returns the
// target. The state tells the caller whether to redirect (stateActive) or
// serve 410 Gone.
func (s *Store) Resolve(slug string, v Visit) (target string, st linkState, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.links[slugKey(slug)]
	if !ok {
		return "", stateActive, false
	}
	st = l.State(v.Now)
	if st != stateActive {
		return "", st, true
	}
	s.record(l, v)
	s.dirty = true
	return l.Target, stateActive, true
}

// record updates the ledger for one live click. Caller holds the write lock.
func (s *Store) record(l *Link, v Visit) {
	bucket := classifyUA(v.UA, v.Prefetch)

	l.Clicks++
	switch bucket {
	case bucketBot:
		l.BotClicks++
	case bucketBrowser:
		l.BrowserClicks++
	default:
		l.UnknownClicks++
	}

	t := v.Now
	if l.FirstClick == nil {
		f := t
		l.FirstClick = &f
	}
	last := t
	l.LastClick = &last

	day := t.Format("2006-01-02")
	if bucket == bucketBot {
		if l.BotDays == nil {
			l.BotDays = map[string]int{}
		}
		l.BotDays[day]++
	} else {
		if l.Days == nil {
			l.Days = map[string]int{}
		}
		l.Days[day]++

		// Uniques and referrers only track non-bot traffic; mixing crawlers in
		// would make both numbers lies.
		h := s.visitorHash(v.IP, v.UA)
		if l.uniqueSet == nil {
			l.uniqueSet = make(map[string]struct{}, len(l.Uniques))
			for _, u := range l.Uniques {
				l.uniqueSet[u] = struct{}{}
			}
		}
		if _, seen := l.uniqueSet[h]; !seen {
			if len(l.Uniques) < maxUniquesPerLink {
				l.uniqueSet[h] = struct{}{}
				l.Uniques = append(l.Uniques, h)
			} else {
				l.UniquesCapped = true
			}
		}

		origin := referrerOrigin(v.Referer)
		if l.Referrers == nil {
			l.Referrers = map[string]int{}
		}
		if _, known := l.Referrers[origin]; !known && len(l.Referrers) >= maxReferrerBuckets {
			origin = otherReferrer
		}
		l.Referrers[origin]++
	}

	pruneDays(l.Days, t)
	pruneDays(l.BotDays, t)
}

func pruneDays(m map[string]int, now time.Time) {
	if len(m) <= dayRetention {
		return
	}
	cutoff := now.AddDate(0, 0, -dayRetention).Format("2006-01-02")
	for k := range m {
		if k < cutoff {
			delete(m, k)
		}
	}
}
