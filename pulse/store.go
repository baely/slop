package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Retention caps. These are stated in the UI; keep the two in sync.
const (
	MaxChecksPerTarget    = 500 // rolling window of individual checks
	MaxDaySummaries       = 90  // rolled-up days kept after checks are evicted
	MaxIncidentsPerTarget = 100
	MaxTargets            = 200
	MaxNameLen            = 64
	MaxURLLen             = 400
	MaxKeywordLen         = 120
)

// idAlphabet has no ambiguous characters (no l, o, 0, 1).
const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

var idEncoding = base32.NewEncoding(idAlphabet).WithPadding(base32.NoPadding)

// newID returns a 96-bit random identifier, 20 characters long.
func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("pulse: crypto/rand unavailable: " + err.Error())
	}
	return idEncoding.EncodeToString(b)
}

// Target is a monitored endpoint.
type Target struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Method    string    `json:"method"`        // GET or HEAD
	Interval  int       `json:"interval_sec"`  // seconds between checks
	Timeout   int       `json:"timeout_sec"`   // per-request bound
	Expect    string    `json:"expect_status"` // "2xx", "200", "200,204", "3xx"…
	Keyword   string    `json:"keyword"`       // must appear in body (GET only)
	CreatedAt time.Time `json:"created_at"`
}

// Check is one probe result.
type Check struct {
	At        time.Time `json:"at"`
	Status    int       `json:"status"` // 0 when the request never completed
	LatencyMs int       `json:"latency_ms"`
	OK        bool      `json:"ok"`
	Err       string    `json:"err,omitempty"`
}

// DaySummary is the rollup of checks that have aged out of the rolling window.
// First/Last bound the samples it represents so a window query can prorate it.
type DaySummary struct {
	Day        string    `json:"day"` // YYYY-MM-DD, local
	Total      int       `json:"total"`
	OK         int       `json:"ok"`
	LatencySum int64     `json:"latency_sum_ms"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
}

// Incident is a derived down period: opened on a transition into not-ok,
// closed on the transition back to ok.
type Incident struct {
	ID       string     `json:"id"`
	TargetID string     `json:"target_id"`
	Start    time.Time  `json:"start"`
	End      *time.Time `json:"end,omitempty"`
	Err      string     `json:"err"`
	Status   int        `json:"status"`
}

// Open reports whether the incident is still running.
func (i Incident) Open() bool { return i.End == nil }

// Duration is the incident length; for an open incident it is measured to now.
func (i Incident) Duration(now time.Time) time.Duration {
	if i.End != nil {
		return i.End.Sub(i.Start)
	}
	return now.Sub(i.Start)
}

type targetState struct {
	Target    Target       `json:"target"`
	Checks    []Check      `json:"checks"`
	Days      []DaySummary `json:"days"`
	Incidents []Incident   `json:"incidents"`
	NextDue   time.Time    `json:"next_due"`
}

type fileData struct {
	Version int            `json:"version"`
	Targets []*targetState `json:"targets"`
}

// Store holds every target and its history in memory and persists the whole
// thing as one JSON file. The data is tiny (200 targets x 500 checks) so a
// mutex plus atomic rename beats a database dependency.
type Store struct {
	mu     sync.RWMutex
	path   string
	states map[string]*targetState
	order  []string
	dirty  bool
}

// NewStore loads existing state from dir, or starts empty.
func NewStore(dir string) (*Store, error) {
	s := &Store{
		path:   filepath.Join(dir, "pulse.json"),
		states: map[string]*targetState{},
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var fd fileData
	if err := json.Unmarshal(b, &fd); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	for _, st := range fd.Targets {
		if st == nil || st.Target.ID == "" {
			continue
		}
		if _, dup := s.states[st.Target.ID]; dup {
			continue
		}
		s.states[st.Target.ID] = st
		s.order = append(s.order, st.Target.ID)
	}
	return s, nil
}

// Save writes the whole store atomically: temp file in the same directory,
// fsync, rename, then fsync the directory.
func (s *Store) Save() error {
	s.mu.Lock()
	fd := fileData{Version: 1}
	for _, id := range s.order {
		fd.Targets = append(fd.Targets, s.states[id])
	}
	b, err := json.Marshal(fd)
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicWrite(s.path, b)
}

func atomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// SaveIfDirty flushes only when something changed since the last write.
func (s *Store) SaveIfDirty() error {
	s.mu.RLock()
	dirty := s.dirty
	s.mu.RUnlock()
	if !dirty {
		return nil
	}
	return s.Save()
}

func (s *Store) markDirty() { s.dirty = true }

// ---------- targets ----------

var errNotFound = errors.New("target not found")

// TargetInput is the unvalidated form payload.
type TargetInput struct {
	Name     string
	URL      string
	Method   string
	Interval string
	Timeout  string
	Expect   string
	Keyword  string
}

// Validate normalizes and checks a target definition.
func (in TargetInput) Validate() (Target, error) {
	t := Target{}
	t.Name = strings.TrimSpace(in.Name)
	if t.Name == "" {
		return t, errors.New("name is required")
	}
	if len(t.Name) > MaxNameLen {
		return t, fmt.Errorf("name must be %d characters or fewer", MaxNameLen)
	}

	raw := strings.TrimSpace(in.URL)
	if raw == "" {
		return t, errors.New("url is required")
	}
	if len(raw) > MaxURLLen {
		return t, fmt.Errorf("url must be %d characters or fewer", MaxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return t, errors.New("url could not be parsed")
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return t, errors.New("url must be http or https")
	}
	if u.Host == "" {
		return t, errors.New("url must include a host")
	}
	// Private and link-local addresses are allowed on purpose: the intended
	// targets are bailey's own internal services. The bound is the timeout.
	t.URL = u.String()

	t.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	if t.Method == "" {
		t.Method = "GET"
	}
	if t.Method != "GET" && t.Method != "HEAD" {
		return t, errors.New("method must be GET or HEAD")
	}

	t.Interval, err = parseBounded(in.Interval, 60, 10, 86400)
	if err != nil {
		return t, fmt.Errorf("interval: %w", err)
	}
	t.Timeout, err = parseBounded(in.Timeout, 10, 1, 60)
	if err != nil {
		return t, fmt.Errorf("timeout: %w", err)
	}
	if t.Timeout > t.Interval {
		return t, errors.New("timeout must not exceed the interval")
	}

	t.Expect = strings.TrimSpace(in.Expect)
	if t.Expect == "" {
		t.Expect = "2xx"
	}
	if _, err := ParseExpect(t.Expect); err != nil {
		return t, err
	}

	t.Keyword = strings.TrimSpace(in.Keyword)
	if len(t.Keyword) > MaxKeywordLen {
		return t, fmt.Errorf("keyword must be %d characters or fewer", MaxKeywordLen)
	}
	if t.Keyword != "" && t.Method == "HEAD" {
		return t, errors.New("keyword requires method GET")
	}
	return t, nil
}

func parseBounded(raw string, def, lo, hi int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("must be a whole number of seconds")
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("must be between %d and %d seconds", lo, hi)
	}
	return n, nil
}

// AddTarget stores a validated target.
func (s *Store) AddTarget(t Target, now time.Time) (Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.order) >= MaxTargets {
		return Target{}, fmt.Errorf("target limit reached (%d)", MaxTargets)
	}
	t.ID = newID()
	t.CreatedAt = now
	s.states[t.ID] = &targetState{Target: t, NextDue: now}
	s.order = append(s.order, t.ID)
	s.markDirty()
	return t, nil
}

// UpdateTarget replaces the editable fields of an existing target.
func (s *Store) UpdateTarget(id string, t Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[id]
	if !ok {
		return errNotFound
	}
	t.ID = st.Target.ID
	t.CreatedAt = st.Target.CreatedAt
	st.Target = t
	st.NextDue = time.Now()
	s.markDirty()
	return nil
}

// DeleteTarget removes a target and all of its history.
func (s *Store) DeleteTarget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.states[id]; !ok {
		return errNotFound
	}
	delete(s.states, id)
	for i, v := range s.order {
		if v == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.markDirty()
	return nil
}

// Target returns a copy of one target.
func (s *Store) Target(id string) (Target, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	if !ok {
		return Target{}, false
	}
	return st.Target, true
}

// Targets returns every target in creation order.
func (s *Store) Targets() []Target {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Target, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.states[id].Target)
	}
	return out
}

// Count returns the number of targets.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

// Due returns targets whose next check is at or before now, and reserves them
// by pushing NextDue forward so a slow check is not dispatched twice.
func (s *Store) Due(now time.Time) []Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Target
	for _, id := range s.order {
		st := s.states[id]
		if !st.NextDue.After(now) {
			out = append(out, st.Target)
			st.NextDue = now.Add(time.Duration(st.Target.Interval) * time.Second)
		}
	}
	return out
}

// Reschedule sets the next due time for a target (used after a manual check).
func (s *Store) Reschedule(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[id]; ok {
		st.NextDue = at
	}
}

// ---------- recording ----------

// Record appends a check, applies retention, and derives incidents.
func (s *Store) Record(id string, c Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[id]
	if !ok {
		return
	}

	// Incident derivation happens against the state before this check.
	last := len(st.Incidents) - 1
	openIncident := last >= 0 && st.Incidents[last].Open()
	switch {
	case !c.OK && !openIncident:
		st.Incidents = append(st.Incidents, Incident{
			ID:       newID(),
			TargetID: id,
			Start:    c.At,
			Err:      c.Err,
			Status:   c.Status,
		})
	case c.OK && openIncident:
		end := c.At
		st.Incidents[last].End = &end
	}
	if n := len(st.Incidents); n > MaxIncidentsPerTarget {
		st.Incidents = append([]Incident(nil), st.Incidents[n-MaxIncidentsPerTarget:]...)
	}

	st.Checks = append(st.Checks, c)
	if over := len(st.Checks) - MaxChecksPerTarget; over > 0 {
		for _, old := range st.Checks[:over] {
			st.rollUp(old)
		}
		st.Checks = append([]Check(nil), st.Checks[over:]...)
	}
	if n := len(st.Days); n > MaxDaySummaries {
		st.Days = append([]DaySummary(nil), st.Days[n-MaxDaySummaries:]...)
	}
	s.markDirty()
}

// rollUp folds an evicted check into its day summary. Summaries and the
// rolling window are therefore disjoint — nothing is counted twice.
func (st *targetState) rollUp(c Check) {
	day := c.At.Format("2006-01-02")
	for i := range st.Days {
		if st.Days[i].Day == day {
			d := &st.Days[i]
			d.Total++
			if c.OK {
				d.OK++
			}
			d.LatencySum += int64(c.LatencyMs)
			if c.At.After(d.Last) {
				d.Last = c.At
			}
			if c.At.Before(d.First) {
				d.First = c.At
			}
			return
		}
	}
	d := DaySummary{Day: day, Total: 1, LatencySum: int64(c.LatencyMs), First: c.At, Last: c.At}
	if c.OK {
		d.OK = 1
	}
	st.Days = append(st.Days, d)
	sort.Slice(st.Days, func(i, j int) bool { return st.Days[i].Day < st.Days[j].Day })
}

// ---------- reads ----------

// Checks returns the rolling window for a target, oldest first.
func (s *Store) Checks(id string) []Check {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	if !ok {
		return nil
	}
	return append([]Check(nil), st.Checks...)
}

// Days returns the rolled-up day summaries for a target, oldest first.
func (s *Store) Days(id string) []DaySummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	if !ok {
		return nil
	}
	return append([]DaySummary(nil), st.Days...)
}

// Incidents returns a target's incidents, newest first.
func (s *Store) Incidents(id string) []Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	if !ok {
		return nil
	}
	out := append([]Incident(nil), st.Incidents...)
	reverseIncidents(out)
	return out
}

// AllIncidents returns incidents across every target, newest first.
func (s *Store) AllIncidents(limit int) []Incident {
	s.mu.RLock()
	var out []Incident
	for _, id := range s.order {
		out = append(out, s.states[id].Incidents...)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func reverseIncidents(in []Incident) {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
}

// Uptime is the result of a windowed availability query.
type Uptime struct {
	Total  int
	OK     int
	Known  bool // at least one sample in the window
	Rolled bool // includes prorated day-summary data
}

// Pct is availability as a percentage; only meaningful when Known.
func (u Uptime) Pct() float64 {
	if u.Total == 0 {
		return 0
	}
	return float64(u.OK) / float64(u.Total) * 100
}

// Uptime computes availability for a target over [now-window, now].
//
// Individual checks inside the window are counted exactly. If the rolling
// window of checks does not reach back far enough, the remaining span is
// filled from day summaries, prorated across the span each summary covers.
func (s *Store) Uptime(id string, window time.Duration, now time.Time) Uptime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	if !ok {
		return Uptime{}
	}
	return st.uptime(window, now)
}

func (st *targetState) uptime(window time.Duration, now time.Time) Uptime {
	cut := now.Add(-window)
	var u Uptime
	for _, c := range st.Checks {
		if c.At.Before(cut) || c.At.After(now) {
			continue
		}
		u.Total++
		if c.OK {
			u.OK++
		}
	}
	// The rolling window reaches back to its oldest sample; everything before
	// that lives in the day summaries.
	rollEnd := now
	if len(st.Checks) > 0 {
		rollEnd = st.Checks[0].At
	}
	if rollEnd.After(cut) {
		for _, d := range st.Days {
			frac := d.portionIn(cut, rollEnd)
			if frac <= 0 {
				continue
			}
			total := int(float64(d.Total)*frac + 0.5)
			if total == 0 {
				continue
			}
			okc := int(float64(d.OK)*frac + 0.5)
			if okc > total {
				okc = total
			}
			u.Total += total
			u.OK += okc
			u.Rolled = true
		}
	}
	u.Known = u.Total > 0
	return u
}

// portionIn is the fraction of this summary's samples falling in [from, to).
func (d DaySummary) portionIn(from, to time.Time) float64 {
	if d.Total == 0 || !from.Before(to) {
		return 0
	}
	if d.Last.Before(from) || !d.First.Before(to) {
		return 0
	}
	if !d.First.Before(from) && !d.Last.After(to) {
		return 1
	}
	span := d.Last.Sub(d.First)
	if span <= 0 {
		return 1
	}
	lo, hi := d.First, d.Last
	if from.After(lo) {
		lo = from
	}
	if to.Before(hi) {
		hi = to
	}
	if !hi.After(lo) {
		return 0
	}
	return float64(hi.Sub(lo)) / float64(span)
}
