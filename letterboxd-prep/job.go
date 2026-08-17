package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

type FilmStatus string

const (
	StatusPending  FilmStatus = "pending"
	StatusAuto     FilmStatus = "auto"     // automatically matched by exact title+year
	StatusAwaiting FilmStatus = "awaiting" // candidates fetched, waiting for user
	StatusResolved FilmStatus = "resolved" // user picked a candidate
	StatusSkipped  FilmStatus = "skipped"  // user chose no match
	StatusError    FilmStatus = "error"
)

type FilmMatch struct {
	Key         string // synthetic dedup key — "name|year"
	Name        string
	Year        string
	URI         string // canonical Letterboxd film URI (from watched/watchlist), if known
	InWatched   bool
	InWatchlist bool

	Status     FilmStatus
	Candidates []TMDBMovie
	Selected   *TMDBMovie
	Error      string
}

// FilmKey is the dedup key used across the job. Diary entries carry per-watch
// "entry" URIs that don't match the film URI from watched.csv, so we key on
// (name, year) instead.
func FilmKey(name, year string) string {
	return name + "|" + year
}

type JobState string

const (
	JobProcessing JobState = "processing"
	JobReady      JobState = "ready" // all done, no pending
	JobError      JobState = "error"
)

type Job struct {
	ID         string
	Filename   string
	CreatedAt  time.Time
	mu         sync.Mutex
	State      JobState
	ErrorMsg   string
	Data       *LBData
	Films      map[string]*FilmMatch // keyed by FilmKey(name, year)
	order      []string              // stable order of keys
	totalFilms int
	processed  int
}

type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewJobStore() *JobStore {
	return &JobStore{jobs: map[string]*Job{}}
}

func (s *JobStore) New(filename string) *Job {
	id := newID()
	j := &Job{
		ID:        id,
		Filename:  filename,
		CreatedAt: time.Now(),
		State:     JobProcessing,
		Films:     map[string]*FilmMatch{},
	}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
	return j
}

func (s *JobStore) Get(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

// Snapshot returns a read-only copy of the films in stable order.
func (j *Job) Snapshot() (state JobState, errMsg string, films []FilmMatch, total, processed int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state = j.State
	errMsg = j.ErrorMsg
	for _, k := range j.order {
		if f, ok := j.Films[k]; ok {
			films = append(films, *f)
		}
	}
	total = j.totalFilms
	processed = j.processed
	return
}

// Pending returns only films awaiting user input or errored.
func (j *Job) Pending() []FilmMatch {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []FilmMatch
	for _, k := range j.order {
		f := j.Films[k]
		if f == nil {
			continue
		}
		if f.Status == StatusAwaiting || f.Status == StatusError {
			out = append(out, *f)
		}
	}
	return out
}

// initFilms populates the unique films from watched + watchlist + diary.
func (j *Job) initFilms() {
	j.mu.Lock()
	defer j.mu.Unlock()
	add := func(uri, name, year string, watched, watchlist bool) {
		if name == "" {
			return
		}
		key := FilmKey(name, year)
		f, ok := j.Films[key]
		if !ok {
			f = &FilmMatch{Key: key, Name: name, Year: year, URI: uri, Status: StatusPending}
			j.Films[key] = f
			j.order = append(j.order, key)
		} else if f.URI == "" && uri != "" {
			f.URI = uri
		}
		if watched {
			f.InWatched = true
		}
		if watchlist {
			f.InWatchlist = true
		}
	}
	for _, w := range j.Data.Watched {
		add(w.URI, w.Name, w.Year, true, false)
	}
	for _, d := range j.Data.Diary {
		add("", d.Name, d.Year, true, false) // diary URIs are entry-scoped, ignore
	}
	for _, w := range j.Data.Watchlist {
		add(w.URI, w.Name, w.Year, false, true)
	}
	// stable order by name then year
	sort.SliceStable(j.order, func(a, b int) bool {
		fa := j.Films[j.order[a]]
		fb := j.Films[j.order[b]]
		if fa.Name != fb.Name {
			return fa.Name < fb.Name
		}
		return fa.Year < fb.Year
	})
	j.totalFilms = len(j.order)
}

// Run executes the matching pipeline against TMDB.
func (j *Job) Run(ctx context.Context, tmdb *TMDBClient, concurrency int) {
	j.initFilms()

	tasks := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range tasks {
				j.processOne(ctx, tmdb, k)
				j.mu.Lock()
				j.processed++
				j.mu.Unlock()
			}
		}()
	}

	j.mu.Lock()
	keys := append([]string{}, j.order...)
	j.mu.Unlock()
	for _, k := range keys {
		select {
		case <-ctx.Done():
			close(tasks)
			wg.Wait()
			j.mu.Lock()
			j.State = JobError
			j.ErrorMsg = ctx.Err().Error()
			j.mu.Unlock()
			return
		case tasks <- k:
		}
	}
	close(tasks)
	wg.Wait()
	j.refreshState()
}

func (j *Job) processOne(ctx context.Context, tmdb *TMDBClient, key string) {
	j.mu.Lock()
	f := j.Films[key]
	j.mu.Unlock()
	if f == nil {
		return
	}
	results, err := tmdb.Search(ctx, f.Name, f.Year)
	if err != nil {
		j.mu.Lock()
		f.Status = StatusError
		f.Error = err.Error()
		j.mu.Unlock()
		return
	}
	if len(results) == 0 {
		j.mu.Lock()
		f.Status = StatusAwaiting
		f.Candidates = nil
		j.mu.Unlock()
		return
	}
	if IsExactMatch(f.Name, f.Year, results[0]) {
		// fetch details
		d, err := tmdb.Details(ctx, results[0].ID)
		if err != nil {
			j.mu.Lock()
			f.Status = StatusError
			f.Error = err.Error()
			j.mu.Unlock()
			return
		}
		j.mu.Lock()
		f.Status = StatusAuto
		f.Selected = d
		j.mu.Unlock()
		return
	}
	// keep top 5 candidates
	if len(results) > 5 {
		results = results[:5]
	}
	j.mu.Lock()
	f.Status = StatusAwaiting
	f.Candidates = results
	j.mu.Unlock()
}

// Resolve sets the user's pick (or skip) for a film.
func (j *Job) Resolve(ctx context.Context, tmdb *TMDBClient, key string, tmdbID int, skip bool) error {
	j.mu.Lock()
	f := j.Films[key]
	j.mu.Unlock()
	if f == nil {
		return nil
	}
	if skip {
		j.mu.Lock()
		f.Status = StatusSkipped
		f.Selected = nil
		j.mu.Unlock()
		j.refreshState()
		return nil
	}
	d, err := tmdb.Details(ctx, tmdbID)
	if err != nil {
		return err
	}
	j.mu.Lock()
	f.Status = StatusResolved
	f.Selected = d
	j.mu.Unlock()
	j.refreshState()
	return nil
}

// SearchOverride lets the user search TMDB freely for one film.
func (j *Job) SearchOverride(ctx context.Context, tmdb *TMDBClient, key, query, year string) ([]TMDBMovie, error) {
	results, err := tmdb.Search(ctx, query, year)
	if err != nil {
		return nil, err
	}
	if len(results) > 10 {
		results = results[:10]
	}
	// also stash on the film so the UI can re-render with new candidates
	j.mu.Lock()
	if f := j.Films[key]; f != nil {
		f.Candidates = results
	}
	j.mu.Unlock()
	return results, nil
}

func (j *Job) refreshState() {
	j.mu.Lock()
	defer j.mu.Unlock()
	pending := 0
	for _, f := range j.Films {
		if f.Status == StatusPending || f.Status == StatusAwaiting || f.Status == StatusError {
			pending++
		}
	}
	if pending == 0 {
		j.State = JobReady
	} else {
		j.State = JobProcessing
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
