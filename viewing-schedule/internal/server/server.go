package server

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/baileybutler/viewing-schedule/internal/letterboxd"
	"github.com/baileybutler/viewing-schedule/internal/store"
	syncpkg "github.com/baileybutler/viewing-schedule/internal/sync"
	"github.com/baileybutler/viewing-schedule/internal/tmdb"
)

//go:embed templates/*.html
var templateFS embed.FS

// Options configures a Server.
type Options struct {
	Store      *store.Store
	TMDB       *tmdb.Client
	Letterboxd *letterboxd.Client
	Sync       *syncpkg.Service
	Title      string
	DateRange  string
	AdminToken string // optional, layered on top of IP allow-list
	TrustProxy bool   // honour X-Forwarded-For when computing client IP
}

// Server is the HTTP handler for the viewing-schedule app.
type Server struct {
	opts      Options
	mux       *http.ServeMux
	templates *template.Template
}

// New constructs a Server with the given options.
func New(opts Options) *Server {
	tpl := template.Must(template.New("").Funcs(funcMap()).ParseFS(templateFS, "templates/*.html"))
	s := &Server{opts: opts, mux: http.NewServeMux(), templates: tpl}
	s.routes()
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleViewer)
	s.mux.HandleFunc("GET /history", s.handleHistory)
	s.mux.HandleFunc("GET /api/schedule", s.handleSchedule)
	s.mux.HandleFunc("GET /api/history", s.handleAPIHistory)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// Admin routes are gated by the private-IP middleware.
	guard := func(h http.HandlerFunc) http.Handler { return s.requirePrivateIP(h) }
	s.mux.Handle("GET /admin", guard(s.handleAdmin))
	s.mux.Handle("GET /admin/", guard(s.handleAdmin))
	s.mux.Handle("POST /admin/entries", guard(s.handleAdminUpsertEntry))
	s.mux.Handle("POST /admin/entries/delete", guard(s.handleAdminDeleteEntry))
	s.mux.Handle("GET /admin/lookup", guard(s.handleAdminLookup))
	s.mux.Handle("POST /admin/lookup", guard(s.handleAdminLookup))
	s.mux.Handle("POST /admin/letterboxd/preview", guard(s.handleAdminLetterboxdPreview))
	s.mux.Handle("POST /admin/letterboxd/import", guard(s.handleAdminLetterboxdImport))
	s.mux.Handle("POST /admin/refresh-tmdb", guard(s.handleAdminRefreshTMDB))
	s.mux.Handle("POST /admin/sync", guard(s.handleAdminSync))
	s.mux.Handle("GET /admin/viewings.csv", guard(s.handleAdminViewingsCSV))
}

// handleAdminRefreshTMDB re-runs TMDB enrichment for every existing entry. By
// default it only enriches movies missing TMDB metadata; pass `force=1` to
// re-enrich all of them.
func (s *Server) handleAdminRefreshTMDB(w http.ResponseWriter, r *http.Request) {
	if s.opts.TMDB == nil || !s.opts.TMDB.HasToken() {
		http.Error(w, "TMDB token not configured", http.StatusServiceUnavailable)
		return
	}
	_ = r.ParseForm()
	force := r.FormValue("force") == "1"

	entries, err := s.opts.Store.ListEntries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enriched, skipped, failed := 0, 0, 0
	for _, e := range entries {
		updated := make([]store.Movie, 0, len(e.Movies))
		dirty := false
		for _, m := range e.Movies {
			if m.TMDBID != 0 && !force {
				updated = append(updated, m)
				skipped++
				continue
			}
			ctx, cancel := contextWithTimeout(r.Context(), 20*time.Second)
			info, err := s.opts.TMDB.Lookup(ctx, m.Title)
			cancel()
			if err != nil || info == nil {
				updated = append(updated, m)
				failed++
				continue
			}
			updated = append(updated, mergeTMDB(m, info))
			enriched++
			dirty = true
		}
		if dirty {
			if err := s.opts.Store.ReplaceMovies(e.ID, updated); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	http.Redirect(w, r,
		"/admin?refreshed="+strconv.Itoa(enriched)+"&skipped="+strconv.Itoa(skipped)+"&failed="+strconv.Itoa(failed),
		http.StatusSeeOther)
}

// --- public handlers ---

func (s *Server) handleViewer(w http.ResponseWriter, r *http.Request) {
	entries, err := s.opts.Store.ListEntries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonBytes, _ := json.Marshal(toViewerSchedule(entries))
	data := map[string]any{
		"Title":     s.opts.Title,
		"DateRange": s.opts.DateRange,
		"ScheduleJSON": template.JS(jsonBytes),
	}
	s.render(w, "viewer.html", data)
}

func (s *Server) handleSchedule(w http.ResponseWriter, _ *http.Request) {
	entries, err := s.opts.Store.ListEntries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toViewerSchedule(entries))
}

// --- admin handlers ---

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	entries, err := s.opts.Store.ListEntries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	flash := ""
	switch {
	case q.Get("imported") != "" || q.Get("skipped") != "":
		flash = "Imported " + q.Get("imported") + " entries (" + q.Get("skipped") + " skipped)."
	case q.Get("refreshed") != "":
		flash = "TMDB refresh: " + q.Get("refreshed") + " enriched, " +
			q.Get("skipped") + " already had metadata, " + q.Get("failed") + " failed."
	case q.Get("synced") != "" || q.Get("updated") != "":
		flash = "Diary sync: " + q.Get("synced") + " new, " + q.Get("updated") + " updated, " +
			q.Get("enriched") + " enriched, " + q.Get("failed") + " failed."
		if e := q.Get("err"); e != "" {
			flash += " Error: " + e
		}
	}

	hasSync := s.opts.Sync != nil
	syncUser := ""
	syncInterval := ""
	syncCount := 0
	lastSync := ""
	if hasSync {
		syncUser = s.opts.Sync.User()
		if d := s.opts.Sync.Interval(); d > 0 {
			syncInterval = d.String()
		}
		syncCount, _ = s.opts.Store.CountViewings(syncUser)
		if t := s.opts.Sync.LastSyncTime(); !t.IsZero() {
			lastSync = t.Local().Format("2006-01-02 15:04 MST")
		}
	}

	data := map[string]any{
		"Title":         s.opts.Title,
		"DateRange":     s.opts.DateRange,
		"Entries":       entries,
		"HasTMDB":       s.opts.TMDB != nil && s.opts.TMDB.HasToken(),
		"HasLetterboxd": s.opts.Letterboxd != nil,
		"Flash":         flash,
		"HasSync":       hasSync,
		"SyncUser":      syncUser,
		"SyncInterval":  syncInterval,
		"SyncCount":     syncCount,
		"LastSync":      lastSync,
	}
	s.render(w, "admin.html", data)
}

// handleAdminLetterboxdPreview scrapes a Letterboxd URL and renders the films
// it contains so the user can pick what to schedule.
func (s *Server) handleAdminLetterboxdPreview(w http.ResponseWriter, r *http.Request) {
	if s.opts.Letterboxd == nil {
		http.Error(w, "letterboxd client not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw := strings.TrimSpace(r.FormValue("url"))
	if raw == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	startDate := r.FormValue("start_date")
	if startDate == "" {
		startDate = time.Now().Format("2006-01-02")
	}
	cadence := r.FormValue("cadence") // daily | weekly | weekdays
	if cadence == "" {
		cadence = "weekly"
	}

	ctx, cancel := contextWithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	films, err := s.opts.Letterboxd.FetchURL(ctx, raw)
	if err != nil {
		http.Error(w, "letterboxd: "+err.Error(), http.StatusBadGateway)
		return
	}

	dates := scheduleDates(startDate, cadence, len(films))
	rows := make([]map[string]any, len(films))
	for i, f := range films {
		date := ""
		if i < len(dates) {
			date = dates[i]
		}
		rows[i] = map[string]any{
			"Index":     i,
			"Date":      date,
			"FullTitle": f.FullTitle(),
			"Title":     f.Title,
			"Year":      f.Year,
			"Slug":      f.Slug,
			"Link":      "https://letterboxd.com" + f.Link,
		}
	}

	data := map[string]any{
		"Title":     s.opts.Title,
		"SourceURL": raw,
		"StartDate": startDate,
		"Cadence":   cadence,
		"Films":     rows,
		"HasTMDB":   s.opts.TMDB != nil && s.opts.TMDB.HasToken(),
	}
	s.render(w, "letterboxd_preview.html", data)
}

// handleAdminLetterboxdImport persists the films selected in the preview form.
func (s *Server) handleAdminLetterboxdImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	count, err := strconv.Atoi(r.FormValue("count"))
	if err != nil || count <= 0 {
		http.Error(w, "invalid count", http.StatusBadRequest)
		return
	}
	source := r.FormValue("source")
	saved, skipped := 0, 0
	for i := 0; i < count; i++ {
		idx := strconv.Itoa(i)
		if r.FormValue("include_"+idx) == "" {
			skipped++
			continue
		}
		date := strings.TrimSpace(r.FormValue("date_" + idx))
		title := strings.TrimSpace(r.FormValue("title_" + idx))
		if date == "" || title == "" {
			skipped++
			continue
		}
		reason := ""
		if source != "" {
			reason = "Imported from " + source
		}
		entryID, err := s.opts.Store.UpsertEntry(date, weekdayFromDate(date), reason)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		m := store.Movie{Title: title}
		if s.opts.TMDB != nil && s.opts.TMDB.HasToken() {
			ctx, cancel := contextWithTimeout(r.Context(), 20*time.Second)
			info, err := s.opts.TMDB.Lookup(ctx, title)
			cancel()
			if err == nil && info != nil {
				m = mergeTMDB(m, info)
			} else {
				_, m.Year = tmdb.ParseTitle(title)
			}
		} else {
			_, m.Year = tmdb.ParseTitle(title)
		}
		if err := s.opts.Store.ReplaceMovies(entryID, []store.Movie{m}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		saved++
	}
	http.Redirect(w, r, "/admin?imported="+strconv.Itoa(saved)+"&skipped="+strconv.Itoa(skipped), http.StatusSeeOther)
}

// scheduleDates produces a sequence of ISO dates starting at startDate using
// the given cadence (daily, weekly, weekdays).
func scheduleDates(startDate, cadence string, n int) []string {
	t, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		t = time.Now()
	}
	out := make([]string, 0, n)
	for len(out) < n {
		switch cadence {
		case "weekdays":
			if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
				out = append(out, t.Format("2006-01-02"))
			}
			t = t.AddDate(0, 0, 1)
		case "daily":
			out = append(out, t.Format("2006-01-02"))
			t = t.AddDate(0, 0, 1)
		default: // weekly
			out = append(out, t.Format("2006-01-02"))
			t = t.AddDate(0, 0, 7)
		}
	}
	return out
}

func (s *Server) handleAdminUpsertEntry(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	date := r.FormValue("date")
	if date == "" {
		http.Error(w, "date required", http.StatusBadRequest)
		return
	}
	day := weekdayFromDate(date)
	reason := r.FormValue("reason")

	id, err := s.opts.Store.UpsertEntry(date, day, reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	titles := []string{r.FormValue("movie1"), r.FormValue("movie2")}
	movies := make([]store.Movie, 0, 2)
	for _, t := range titles {
		if t == "" {
			continue
		}
		m := store.Movie{Title: t}
		if s.opts.TMDB != nil && s.opts.TMDB.HasToken() {
			ctx, cancel := contextWithTimeout(r.Context(), 20*time.Second)
			info, err := s.opts.TMDB.Lookup(ctx, t)
			cancel()
			if err == nil && info != nil {
				m = mergeTMDB(m, info)
			} else {
				_, m.Year = tmdb.ParseTitle(t)
			}
		} else {
			_, m.Year = tmdb.ParseTitle(t)
		}
		movies = append(movies, m)
	}
	if err := s.opts.Store.ReplaceMovies(id, movies); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleAdminDeleteEntry(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int64
	if _, err := fmtSscanID(r.FormValue("id"), &id); err != nil {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := s.opts.Store.DeleteEntry(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// handleAdminLookup performs a TMDB lookup and returns JSON for ad-hoc enrichment.
func (s *Server) handleAdminLookup(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		_ = r.ParseForm()
		title = r.FormValue("title")
	}
	if title == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	if s.opts.TMDB == nil || !s.opts.TMDB.HasToken() {
		http.Error(w, "TMDB token not configured", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := contextWithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info, err := s.opts.TMDB.Lookup(ctx, title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

// --- helpers ---

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"join": func(parts []string, sep string) string {
			out := ""
			for i, p := range parts {
				if i > 0 {
					out += sep
				}
				out += p
			}
			return out
		},
	}
}

// Verify embed.FS isn't empty at startup (helps catch missing template dir).
var _ fs.FS = templateFS

// --- history (calendar) ---

// handleHistory renders the year-at-a-glance calendar of past viewings.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	year := time.Now().Year()
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y >= 1900 && y <= 2100 {
		year = y
	}
	years := s.viewingYears()
	if len(years) == 0 {
		// fall back to current year
		years = []int{year}
	}
	user := s.syncUser()
	data := map[string]any{
		"Title":  s.opts.Title,
		"Year":   year,
		"Years":  years,
		"User":   user,
		"HasAny": s.viewingsExist(),
	}
	s.render(w, "history.html", data)
}

type apiViewing struct {
	ViewingID   int64    `json:"viewing_id"`
	Date        string   `json:"date"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Year        string   `json:"year,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Liked       bool     `json:"liked,omitempty"`
	Rewatch     bool     `json:"rewatch,omitempty"`
	HasReview   bool     `json:"has_review,omitempty"`
	TMDBTitle   string   `json:"tmdb_title,omitempty"`
	Director    string   `json:"director,omitempty"`
	Runtime     int      `json:"runtime,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Poster      string   `json:"poster,omitempty"`
	PosterLg    string   `json:"poster_lg,omitempty"`
	Backdrop    string   `json:"backdrop,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	Tagline     string   `json:"tagline,omitempty"`
	ReleaseDate string   `json:"release_date,omitempty"`
	Letterboxd  string   `json:"letterboxd_url,omitempty"`
}

// handleAPIHistory returns viewings for a given year keyed by date.
func (s *Server) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	year := time.Now().Year()
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y >= 1900 && y <= 2100 {
		year = y
	}
	user := s.syncUser()
	from := fmt.Sprintf("%04d-01-01", year)
	to := fmt.Sprintf("%04d-12-31", year)
	vs, err := s.opts.Store.ListViewings(user, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string][]apiViewing{}
	for _, v := range vs {
		av := apiViewing{
			ViewingID:   v.ViewingID,
			Date:        v.WatchedDate,
			Slug:        v.Slug,
			Title:       v.Title,
			Year:        v.Year,
			Rating:      v.Rating,
			Liked:       v.Liked,
			Rewatch:     v.Rewatch,
			HasReview:   v.HasReview,
			TMDBTitle:   v.TMDBTitle,
			Director:    v.Director,
			Runtime:     v.Runtime,
			Poster:      v.Poster,
			PosterLg:    v.PosterLg,
			Backdrop:    v.Backdrop,
			Overview:    v.Overview,
			Tagline:     v.Tagline,
			ReleaseDate: v.ReleaseDate,
		}
		if v.Genres != "" {
			for _, g := range strings.Split(v.Genres, ",") {
				if g = strings.TrimSpace(g); g != "" {
					av.Genres = append(av.Genres, g)
				}
			}
		}
		if user != "" && v.Slug != "" {
			av.Letterboxd = "https://letterboxd.com/" + user + "/film/" + v.Slug + "/"
		}
		out[v.WatchedDate] = append(out[v.WatchedDate], av)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"year":     year,
		"user":     user,
		"viewings": out,
	})
}

// --- sync ---

// handleAdminSync runs a Letterboxd diary sync (incremental by default,
// `force=1` for a full re-scrape).
func (s *Server) handleAdminSync(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sync == nil {
		http.Error(w, "Sync not configured (set LETTERBOXD_USER)", http.StatusServiceUnavailable)
		return
	}
	_ = r.ParseForm()
	force := r.FormValue("force") == "1"
	res := s.opts.Sync.SyncNow(r.Context(), force)
	q := url(map[string]string{
		"synced":   strconv.Itoa(res.Inserted),
		"updated":  strconv.Itoa(res.Updated),
		"enriched": strconv.Itoa(res.Enriched),
		"failed":   strconv.Itoa(res.Failed),
		"err":      res.Err,
	})
	http.Redirect(w, r, "/admin?"+q, http.StatusSeeOther)
}

// --- CSV export ---

// handleAdminViewingsCSV streams a CSV of every stored viewing for the
// configured user (or all users when LETTERBOXD_USER is unset).
func (s *Server) handleAdminViewingsCSV(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		user = s.syncUser()
	}
	vs, err := s.opts.Store.ListViewings(user, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filename := "viewings.csv"
	if user != "" {
		filename = "viewings-" + user + ".csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"viewing_id", "user", "watched_date", "film_slug", "film_title", "film_year",
		"rating", "liked", "rewatch", "has_review",
		"tmdb_id", "tmdb_title", "director", "runtime", "genres", "tmdb_rating",
		"release_date", "tagline", "overview",
		"poster_url", "poster_url_large", "backdrop_url",
		"letterboxd_url", "synced_at",
	})
	for _, v := range vs {
		rating := ""
		if v.Rating > 0 {
			rating = strconv.FormatFloat(v.Rating, 'f', -1, 64)
		}
		tmdbRating := ""
		if v.TMDBRating > 0 {
			tmdbRating = strconv.FormatFloat(v.TMDBRating, 'f', 2, 64)
		}
		runtime := ""
		if v.Runtime > 0 {
			runtime = strconv.Itoa(v.Runtime)
		}
		tmdbID := ""
		if v.TMDBID > 0 {
			tmdbID = strconv.FormatInt(v.TMDBID, 10)
		}
		lbURL := ""
		if v.User != "" && v.Slug != "" {
			lbURL = "https://letterboxd.com/" + v.User + "/film/" + v.Slug + "/"
		}
		_ = cw.Write([]string{
			strconv.FormatInt(v.ViewingID, 10), v.User, v.WatchedDate, v.Slug, v.Title, v.Year,
			rating, boolStr(v.Liked), boolStr(v.Rewatch), boolStr(v.HasReview),
			tmdbID, v.TMDBTitle, v.Director, runtime, v.Genres, tmdbRating,
			v.ReleaseDate, v.Tagline, v.Overview,
			v.Poster, v.PosterLg, v.Backdrop,
			lbURL, v.SyncedAt.Format(time.RFC3339),
		})
	}
}

// --- helpers ---

func (s *Server) syncUser() string {
	if s.opts.Sync != nil {
		return s.opts.Sync.User()
	}
	return ""
}

func (s *Server) viewingYears() []int {
	user := s.syncUser()
	vs, err := s.opts.Store.ListViewings(user, "", "")
	if err != nil || len(vs) == 0 {
		return nil
	}
	ySet := map[int]struct{}{}
	for _, v := range vs {
		if t, err := time.Parse("2006-01-02", v.WatchedDate); err == nil {
			ySet[t.Year()] = struct{}{}
		}
	}
	years := make([]int, 0, len(ySet))
	for y := range ySet {
		years = append(years, y)
	}
	// Sort descending.
	for i := 1; i < len(years); i++ {
		for j := i; j > 0 && years[j-1] < years[j]; j-- {
			years[j-1], years[j] = years[j], years[j-1]
		}
	}
	return years
}

func (s *Server) viewingsExist() bool {
	n, _ := s.opts.Store.CountViewings(s.syncUser())
	return n > 0
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// url builds a query string from a map, omitting empty values.
func url(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}
