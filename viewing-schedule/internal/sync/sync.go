// Package sync periodically scrapes a Letterboxd user's diary and persists
// each viewing to the local store, enriching new films with TMDB metadata.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/baileybutler/viewing-schedule/internal/letterboxd"
	"github.com/baileybutler/viewing-schedule/internal/store"
	"github.com/baileybutler/viewing-schedule/internal/tmdb"
)

// stopAfterKnown controls early-exit during incremental syncs: once we see
// this many already-known viewings in a row we assume the rest are unchanged.
const stopAfterKnown = 30

// Service orchestrates Letterboxd diary syncs.
type Service struct {
	store      *store.Store
	letterboxd *letterboxd.Client
	tmdb       *tmdb.Client
	user       string
	interval   time.Duration

	mu      sync.Mutex
	running bool
	last    Result
}

// Result summarises a sync run.
type Result struct {
	Time      time.Time
	Mode      string // "incremental" | "full"
	Scanned   int
	Inserted  int
	Updated   int
	Enriched  int
	Failed    int
	Duration  time.Duration
	Err       string
}

// Options configures a Service.
type Options struct {
	Store      *store.Store
	Letterboxd *letterboxd.Client
	TMDB       *tmdb.Client
	User       string
	Interval   time.Duration // 0 disables periodic sync
}

// New constructs a sync Service.
func New(o Options) (*Service, error) {
	if o.Store == nil {
		return nil, errors.New("sync: store required")
	}
	if o.Letterboxd == nil {
		return nil, errors.New("sync: letterboxd client required")
	}
	if o.User == "" {
		return nil, errors.New("sync: user required")
	}
	return &Service{
		store:      o.Store,
		letterboxd: o.Letterboxd,
		tmdb:       o.TMDB,
		user:       o.User,
		interval:   o.Interval,
	}, nil
}

// User returns the configured Letterboxd username.
func (s *Service) User() string { return s.user }

// Interval returns the configured periodic interval (0 means disabled).
func (s *Service) Interval() time.Duration { return s.interval }

// Last returns the last sync result.
func (s *Service) Last() Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// LastSyncTime reads the persisted last-sync timestamp from settings.
func (s *Service) LastSyncTime() time.Time {
	if s.store == nil {
		return time.Time{}
	}
	v := s.store.GetSetting(s.lastSyncKey())
	if v == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

func (s *Service) lastSyncKey() string { return "viewings_last_sync_" + s.user }

// Start runs the periodic sync loop until ctx is cancelled. Does nothing if
// interval <= 0. The first sync runs immediately (incremental).
func (s *Service) Start(ctx context.Context) {
	if s.interval <= 0 {
		log.Printf("sync: periodic sync disabled (SYNC_INTERVAL=0)")
		return
	}
	go func() {
		// Initial sync on startup.
		s.runOnce(ctx, false)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runOnce(ctx, false)
			}
		}
	}()
}

// SyncNow runs a single sync. When force is true the entire diary is
// re-scraped; otherwise we early-exit once stopAfterKnown known viewings have
// been seen in a row.
func (s *Service) SyncNow(ctx context.Context, force bool) Result {
	return s.runOnce(ctx, force)
}

func (s *Service) runOnce(ctx context.Context, force bool) Result {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return Result{Time: time.Now(), Err: "another sync is already running"}
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	start := time.Now()
	mode := "incremental"
	if force {
		mode = "full"
	}
	r := Result{Time: start, Mode: mode}

	consecutiveKnown := 0
	stop := func(e letterboxd.DiaryEntry) bool {
		if force {
			return false
		}
		if s.store.HasViewing(e.ViewingID) {
			consecutiveKnown++
			if consecutiveKnown >= stopAfterKnown {
				return true
			}
		} else {
			consecutiveKnown = 0
		}
		return false
	}

	entries, err := s.letterboxd.FetchDiary(ctx, s.user, stop)
	r.Scanned = len(entries)
	if err != nil {
		r.Err = err.Error()
		r.Duration = time.Since(start)
		s.recordResult(r)
		return r
	}

	for _, e := range entries {
		alreadyExisted := s.store.HasViewing(e.ViewingID)
		v := store.Viewing{
			ViewingID:   e.ViewingID,
			User:        s.user,
			WatchedDate: e.WatchedDate,
			Slug:        e.Slug,
			Title:       e.Title,
			Year:        e.Year,
			Rating:      e.Rating,
			Liked:       e.Liked,
			Rewatch:     e.Rewatch,
			HasReview:   e.HasReview,
		}
		if !alreadyExisted {
			s.enrich(ctx, &v, &r)
		}
		if err := s.store.UpsertViewing(v); err != nil {
			r.Failed++
			log.Printf("sync: upsert viewing %d failed: %v", e.ViewingID, err)
			continue
		}
		if alreadyExisted {
			r.Updated++
		} else {
			r.Inserted++
		}
	}

	if err := s.store.SetSetting(s.lastSyncKey(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("sync: persist last-sync time failed: %v", err)
	}
	r.Duration = time.Since(start)
	s.recordResult(r)
	log.Printf("sync: user=%s mode=%s scanned=%d inserted=%d updated=%d enriched=%d failed=%d in %s",
		s.user, mode, r.Scanned, r.Inserted, r.Updated, r.Enriched, r.Failed, r.Duration.Truncate(time.Millisecond))
	return r
}

// enrich attaches TMDB metadata to a viewing. Cached metadata for the same
// film slug is reused before falling back to a live TMDB call.
func (s *Service) enrich(ctx context.Context, v *store.Viewing, r *Result) {
	if cached, ok := s.store.FindCachedTMDBBySlug(v.Slug); ok {
		v.TMDBID = cached.TMDBID
		v.TMDBTitle = cached.TMDBTitle
		v.Director = cached.Director
		v.Runtime = cached.Runtime
		v.Genres = cached.Genres
		v.TMDBRating = cached.TMDBRating
		v.Poster = cached.Poster
		v.PosterLg = cached.PosterLg
		v.Backdrop = cached.Backdrop
		v.Overview = cached.Overview
		v.Tagline = cached.Tagline
		v.ReleaseDate = cached.ReleaseDate
		r.Enriched++
		return
	}
	if s.tmdb == nil || !s.tmdb.HasToken() {
		return
	}
	q := v.Title
	if v.Year != "" {
		q = fmt.Sprintf("%s (%s)", v.Title, v.Year)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := s.tmdb.Lookup(lookupCtx, q)
	if err != nil || info == nil {
		return
	}
	v.TMDBID = info.ID
	v.TMDBTitle = info.Title
	v.Director = info.Director
	v.Runtime = info.Runtime
	v.Genres = strings.Join(info.Genres, ", ")
	v.TMDBRating = info.Rating
	v.Poster = info.Poster
	v.PosterLg = info.PosterLg
	v.Backdrop = info.Backdrop
	v.Overview = info.Overview
	v.Tagline = info.Tagline
	v.ReleaseDate = info.ReleaseDate
	if v.Year == "" {
		v.Year = info.Year
	}
	r.Enriched++
}

func (s *Service) recordResult(r Result) {
	s.mu.Lock()
	s.last = r
	s.mu.Unlock()
}
