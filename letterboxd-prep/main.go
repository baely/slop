package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/* static/*
var assets embed.FS

const (
	maxUploadBytes = 50 << 20 // 50 MB
	tmdbConcurrency = 10
)

func main() {
	port := envOr("PORT", "8080")
	token := os.Getenv("TMDB_READ_TOKEN")
	if token == "" {
		log.Fatal("TMDB_READ_TOKEN env var is required")
	}

	store := NewJobStore()
	tmdb := NewTMDBClient(token)
	srv := &server{store: store, tmdb: tmdb, tmpl: mustParseTemplates()}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/upload", srv.handleUpload)
	mux.HandleFunc("/jobs/", srv.handleJob)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	staticSub, err := fs.Sub(assets, "static")
	if err != nil {
		log.Fatalf("static sub: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	addr := ":" + port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, logRequest(mux)); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	store *JobStore
	tmdb  *TMDBClient
	tmpl  *template.Template
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "index.html", nil)
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "upload too large or malformed: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("zip")
	if err != nil {
		http.Error(w, "missing zip", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read entire upload into memory (small files only).
	buf, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	data, err := ParseLetterboxdZip(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		http.Error(w, "parse: "+err.Error(), http.StatusBadRequest)
		return
	}

	job := s.store.New(header.Filename)
	job.Data = data

	go job.Run(context.Background(), s.tmdb, tmdbConcurrency)

	http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
}

func (s *server) handleJob(w http.ResponseWriter, r *http.Request) {
	// /jobs/{id}[/...]
	rest := strings.TrimPrefix(r.URL.Path, "/jobs/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	job := s.store.Get(id)
	if job == nil {
		http.NotFound(w, r)
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "":
		s.renderJobPage(w, r, job)
	case "status":
		s.handleStatus(w, r, job)
	case "resolve":
		s.handleResolve(w, r, job)
	case "search":
		s.handleSearchOverride(w, r, job)
	case "download":
		s.handleDownload(w, r, job)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) renderJobPage(w http.ResponseWriter, r *http.Request, j *Job) {
	state, errMsg, films, total, processed := j.Snapshot()
	pending := []FilmMatch{}
	for _, f := range films {
		if f.Status == StatusAwaiting || f.Status == StatusError {
			pending = append(pending, f)
		}
	}
	stats := computeStats(films)
	s.render(w, "job.html", map[string]any{
		"JobID":     j.ID,
		"Filename":  j.Filename,
		"State":     string(state),
		"Error":     errMsg,
		"Pending":   pending,
		"Total":     total,
		"Processed": processed,
		"Stats":     stats,
	})
}

type statusResp struct {
	State     string      `json:"state"`
	Error     string      `json:"error,omitempty"`
	Total     int         `json:"total"`
	Processed int         `json:"processed"`
	Stats     statSummary `json:"stats"`
	Pending   []filmJSON  `json:"pending"`
}

type statSummary struct {
	Auto, Manual, Skipped, Awaiting, Errored int
}

type filmJSON struct {
	Key        string         `json:"key"`
	URI        string         `json:"uri,omitempty"`
	Name       string         `json:"name"`
	Year       string         `json:"year"`
	Status     string         `json:"status"`
	Error      string         `json:"error,omitempty"`
	Candidates []candidateJSON `json:"candidates"`
}

type candidateJSON struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          string `json:"year"`
	Overview      string `json:"overview"`
	PosterURL     string `json:"poster_url"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request, j *Job) {
	state, errMsg, films, total, processed := j.Snapshot()
	resp := statusResp{
		State:     string(state),
		Error:     errMsg,
		Total:     total,
		Processed: processed,
		Stats:     computeStats(films),
	}
	for _, f := range films {
		if f.Status == StatusAwaiting || f.Status == StatusError {
			resp.Pending = append(resp.Pending, filmToJSON(f))
		}
	}
	writeJSON(w, resp)
}

func (s *server) handleResolve(w http.ResponseWriter, r *http.Request, j *Job) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Key    string `json:"key"`
		TMDBID int    `json:"tmdb_id"`
		Skip   bool   `json:"skip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := j.Resolve(ctx, s.tmdb, body.Key, body.TMDBID, body.Skip); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSearchOverride(w http.ResponseWriter, r *http.Request, j *Job) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Key   string `json:"key"`
		Query string `json:"query"`
		Year  string `json:"year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	results, err := j.SearchOverride(ctx, s.tmdb, body.Key, body.Query, body.Year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]candidateJSON, 0, len(results))
	for _, c := range results {
		out = append(out, candidateToJSON(c))
	}
	writeJSON(w, map[string]any{"candidates": out})
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request, j *Job) {
	state, _, _, _, _ := j.Snapshot()
	if state != JobReady {
		http.Error(w, "job not ready", http.StatusConflict)
		return
	}
	zipBytes, err := BuildOutput(j)
	if err != nil {
		http.Error(w, "build: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := outputFilename(j.Filename)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, out))
	w.Header().Set("Content-Length", strconv.Itoa(len(zipBytes)))
	w.Write(zipBytes)
}

func computeStats(films []FilmMatch) statSummary {
	var s statSummary
	for _, f := range films {
		switch f.Status {
		case StatusAuto:
			s.Auto++
		case StatusResolved:
			s.Manual++
		case StatusSkipped:
			s.Skipped++
		case StatusAwaiting, StatusPending:
			s.Awaiting++
		case StatusError:
			s.Errored++
		}
	}
	return s
}

func filmToJSON(f FilmMatch) filmJSON {
	out := filmJSON{
		Key:    f.Key,
		URI:    f.URI,
		Name:   f.Name,
		Year:   f.Year,
		Status: string(f.Status),
		Error:  f.Error,
	}
	for _, c := range f.Candidates {
		out.Candidates = append(out.Candidates, candidateToJSON(c))
	}
	return out
}

func candidateToJSON(c TMDBMovie) candidateJSON {
	return candidateJSON{
		ID:            c.ID,
		Title:         c.Title,
		OriginalTitle: c.OriginalTitle,
		Year:          c.Year(),
		Overview:      c.Overview,
		PosterURL:     c.PosterURL(),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func mustParseTemplates() *template.Template {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"pct": func(num, denom int) int {
			if denom == 0 {
				return 0
			}
			return num * 100 / denom
		},
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	return tmpl
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func outputFilename(in string) string {
	base := strings.TrimSuffix(in, ".zip")
	if base == "" {
		base = "letterboxd-export"
	}
	return base + "-prepped.zip"
}

func logRequest(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
