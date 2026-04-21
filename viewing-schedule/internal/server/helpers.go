package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/baileybutler/viewing-schedule/internal/store"
	"github.com/baileybutler/viewing-schedule/internal/tmdb"
)

// viewerMovie is the JSON shape consumed by the embedded viewer page.
type viewerMovie struct {
	Title       string   `json:"title"`
	TMDBTitle   string   `json:"tmdb_title,omitempty"`
	Year        string   `json:"year,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	Director    string   `json:"director,omitempty"`
	Runtime     int      `json:"runtime,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Poster      string   `json:"poster,omitempty"`
	PosterLg    string   `json:"poster_lg,omitempty"`
	Backdrop    string   `json:"backdrop,omitempty"`
	ReleaseDate string   `json:"release_date,omitempty"`
	Tagline     string   `json:"tagline,omitempty"`
}

type viewerEntry struct {
	Date   string        `json:"date"`
	Day    string        `json:"day"`
	Reason string        `json:"reason,omitempty"`
	Movies []viewerMovie `json:"movies"`
}

func toViewerSchedule(entries []store.Entry) []viewerEntry {
	out := make([]viewerEntry, 0, len(entries))
	for _, e := range entries {
		ve := viewerEntry{Date: e.Date, Day: e.Day, Reason: e.Reason}
		for _, m := range e.Movies {
			vm := viewerMovie{
				Title:       m.Title,
				TMDBTitle:   m.TMDBTitle,
				Year:        m.Year,
				Overview:    m.Overview,
				Director:    m.Director,
				Runtime:     m.Runtime,
				Rating:      m.Rating,
				Poster:      m.Poster,
				PosterLg:    m.PosterLg,
				Backdrop:    m.Backdrop,
				ReleaseDate: m.ReleaseDate,
				Tagline:     m.Tagline,
			}
			if m.Genres != "" {
				for _, g := range strings.Split(m.Genres, ",") {
					g = strings.TrimSpace(g)
					if g != "" {
						vm.Genres = append(vm.Genres, g)
					}
				}
			}
			ve.Movies = append(ve.Movies, vm)
		}
		out = append(out, ve)
	}
	return out
}

func mergeTMDB(m store.Movie, info *tmdb.Movie) store.Movie {
	m.TMDBID = info.ID
	m.TMDBTitle = info.Title
	if info.Year != "" {
		m.Year = info.Year
	}
	m.Overview = info.Overview
	m.Director = info.Director
	m.Runtime = info.Runtime
	m.Genres = strings.Join(info.Genres, ", ")
	m.Rating = info.Rating
	m.Poster = info.Poster
	m.PosterLg = info.PosterLg
	m.Backdrop = info.Backdrop
	m.ReleaseDate = info.ReleaseDate
	m.Tagline = info.Tagline
	return m
}

func contextWithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

func weekdayFromDate(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

func fmtSscanID(s string, dst *int64) (int, error) {
	return fmt.Sscanf(strings.TrimSpace(s), "%d", dst)
}
