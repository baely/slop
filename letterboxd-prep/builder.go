package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type watchRow struct {
	uri, name, year string
	watchedDate     string
	logDate         string
	rating          string
	liked           bool
	rewatch         bool
	review          string
	tags            string
	match           *FilmMatch
}

// BuildOutput produces the cleaned + enriched zip bytes.
func BuildOutput(j *Job) ([]byte, error) {
	d := j.Data

	// Diary index: URI -> entries (sorted by Watched Date)
	diaryByURI := map[string][]DiaryEntry{}
	for _, e := range d.Diary {
		diaryByURI[e.URI] = append(diaryByURI[e.URI], e)
	}
	for uri := range diaryByURI {
		sort.SliceStable(diaryByURI[uri], func(a, b int) bool {
			return diaryByURI[uri][a].WatchedDate < diaryByURI[uri][b].WatchedDate
		})
	}

	// Reviews share diary entry URIs, so URI+WatchedDate is unique. Also build
	// a (FilmKey, WatchedDate) fallback in case diary URIs ever drift.
	type reviewKey struct{ k, date string }
	reviewByURI := map[string]ReviewEntry{}
	reviewByFilm := map[reviewKey]ReviewEntry{}
	for _, r := range d.Reviews {
		reviewByURI[r.URI] = r
		reviewByFilm[reviewKey{FilmKey(r.Name, r.Year), r.WatchedDate}] = r
	}

	// Liked status is keyed by film URI in the export. Translate to FilmKey
	// using watched.csv + watchlist.csv (which carry film URIs).
	likedKey := map[string]bool{}
	for _, w := range d.Watched {
		if d.LikedURIs[w.URI] {
			likedKey[FilmKey(w.Name, w.Year)] = true
		}
	}
	for _, w := range d.Watchlist {
		if d.LikedURIs[w.URI] {
			likedKey[FilmKey(w.Name, w.Year)] = true
		}
	}

	// Ratings (overall, not per-watch) are keyed by film URI too.
	ratingByKey := map[string]string{}
	for _, w := range d.Watched {
		if r, ok := d.Ratings[w.URI]; ok {
			ratingByKey[FilmKey(w.Name, w.Year)] = r
		}
	}

	var rows []watchRow

	// Track which films have at least one diary row, so films logged in the
	// diary aren't also emitted as a no-date row from watched.csv.
	hasDiary := map[string]bool{}

	for _, e := range d.Diary {
		key := FilmKey(e.Name, e.Year)
		hasDiary[key] = true

		rev := ""
		if r, ok := reviewByURI[e.URI]; ok {
			rev = r.Review
		} else if r, ok := reviewByFilm[reviewKey{key, e.WatchedDate}]; ok {
			rev = r.Review
		}

		fm := j.Films[key]
		// Prefer the canonical film URI in the output instead of the diary entry URI.
		uri := e.URI
		if fm != nil && fm.URI != "" {
			uri = fm.URI
		}

		rows = append(rows, watchRow{
			uri:         uri,
			name:        e.Name,
			year:        e.Year,
			watchedDate: e.WatchedDate,
			logDate:     e.Date,
			rating:      e.Rating,
			liked:       likedKey[key],
			rewatch:     strings.EqualFold(e.Rewatch, "yes"),
			review:      rev,
			tags:        e.Tags,
			match:       fm,
		})
	}

	// Watched-only (films marked watched but never logged in the diary)
	for _, w := range d.Watched {
		key := FilmKey(w.Name, w.Year)
		if hasDiary[key] {
			continue
		}
		rows = append(rows, watchRow{
			uri:    w.URI,
			name:   w.Name,
			year:   w.Year,
			rating: ratingByKey[key],
			liked:  likedKey[key],
			match:  j.Films[key],
		})
	}

	// Sort: by name, year, watched date
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].name != rows[b].name {
			return strings.ToLower(rows[a].name) < strings.ToLower(rows[b].name)
		}
		if rows[a].year != rows[b].year {
			return rows[a].year < rows[b].year
		}
		return rows[a].watchedDate < rows[b].watchedDate
	})

	// Build CSV bodies
	watchedCSV, err := writeWatchedCSV(rows)
	if err != nil {
		return nil, err
	}
	watchlistCSV, err := writeWatchlistCSV(d.Watchlist, j)
	if err != nil {
		return nil, err
	}

	// Pack the zip
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addFile := func(name string, content []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(content)
		return err
	}
	if len(d.Profile) > 0 {
		if err := addFile("profile.csv", d.Profile); err != nil {
			return nil, err
		}
	}
	if err := addFile("watched.csv", watchedCSV); err != nil {
		return nil, err
	}
	if err := addFile("watchlist.csv", watchlistCSV); err != nil {
		return nil, err
	}
	if err := addFile("README.txt", []byte(readmeText(j))); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var watchedHeader = []string{
	"name", "year", "letterboxd_uri",
	"watched_date", "log_date",
	"rating", "liked", "rewatch", "review", "tags",
	"tmdb_id", "tmdb_title", "imdb_id",
	"runtime_minutes", "original_language", "genres", "studios",
	"director", "writers", "dop", "producers", "cast",
	"overview", "poster_url",
}

func writeWatchedCSV(rows []watchRow) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(watchedHeader); err != nil {
		return nil, err
	}
	for _, r := range rows {
		tmdb := selectedMovie(r.match)
		rec := []string{
			r.name, r.year, r.uri,
			r.watchedDate, r.logDate,
			r.rating, boolStr(r.liked), boolStr(r.rewatch), r.review, r.tags,
			intOrEmpty(tmdb.id), tmdb.title, tmdb.imdb,
			intOrEmpty(tmdb.runtime), tmdb.language, tmdb.genres, tmdb.studios,
			tmdb.director, tmdb.writers, tmdb.dop, tmdb.producers, tmdb.cast,
			tmdb.overview, tmdb.poster,
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

var watchlistHeader = []string{
	"date_added", "name", "year", "letterboxd_uri",
	"tmdb_id", "tmdb_title", "imdb_id",
	"runtime_minutes", "original_language", "genres", "studios",
	"director", "writers", "dop", "producers", "cast",
	"overview", "poster_url",
}

func writeWatchlistCSV(items []WatchlistEntry, j *Job) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(watchlistHeader); err != nil {
		return nil, err
	}
	for _, it := range items {
		tmdb := selectedMovie(j.Films[FilmKey(it.Name, it.Year)])
		rec := []string{
			it.Date, it.Name, it.Year, it.URI,
			intOrEmpty(tmdb.id), tmdb.title, tmdb.imdb,
			intOrEmpty(tmdb.runtime), tmdb.language, tmdb.genres, tmdb.studios,
			tmdb.director, tmdb.writers, tmdb.dop, tmdb.producers, tmdb.cast,
			tmdb.overview, tmdb.poster,
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

type tmdbCols struct {
	id        int
	title     string
	imdb      string
	runtime   int
	language  string
	genres    string
	studios   string
	director  string
	writers   string
	dop       string
	producers string
	cast      string
	overview  string
	poster    string
}

func selectedMovie(m *FilmMatch) tmdbCols {
	if m == nil || m.Selected == nil {
		return tmdbCols{}
	}
	s := m.Selected
	return tmdbCols{
		id:        s.ID,
		title:     s.Title,
		imdb:      s.IMDBID,
		runtime:   s.Runtime,
		language:  s.OriginalLanguage,
		genres:    strings.Join(s.Genres, "; "),
		studios:   s.Studios,
		director:  s.Director,
		writers:   s.Writers,
		dop:       s.DOP,
		producers: s.Producers,
		cast:      s.Cast,
		overview:  s.Overview,
		poster:    s.PosterURL(),
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func readmeText(j *Job) string {
	auto, manual, skipped, errored := 0, 0, 0, 0
	for _, f := range j.Films {
		switch f.Status {
		case StatusAuto:
			auto++
		case StatusResolved:
			manual++
		case StatusSkipped:
			skipped++
		case StatusError, StatusAwaiting, StatusPending:
			errored++
		}
	}
	return fmt.Sprintf(`Letterboxd Prep export
Generated: %s

Source file: %s

Files:
  profile.csv     Passthrough of original profile
  watched.csv     One row per watch (diary entry). Films marked watched but
                  never logged with a date appear once with empty watched_date.
  watchlist.csv   Watchlist entries enriched with TMDB metadata.

Match summary (unique films):
  auto-matched:   %d
  manually set:   %d
  skipped:        %d
  unresolved:     %d

watched.csv columns:
  name, year, letterboxd_uri,
  watched_date  YYYY-MM-DD when the film was actually watched (from diary), empty if untracked
  log_date      YYYY-MM-DD when the diary entry was logged
  rating        0.5..5 (Letterboxd scale); from diary if present, else ratings.csv
  liked         true if the film appears in likes/films.csv
  rewatch       true if the diary entry was flagged as a rewatch
  review        matching review text, if any
  tags          diary tags
  tmdb_id, tmdb_title, imdb_id
  runtime_minutes, original_language (ISO 639-1 code), genres (semicolon-sep)
  studios (production companies, top 3, semicolon-sep)
  director, writers, dop, producers (comma-sep, deduped)
  cast (top 5 billed actors, comma-sep)
  overview, poster_url
`, time.Now().UTC().Format(time.RFC3339), j.Filename, auto, manual, skipped, errored)
}
