// movies is a thin, iPad-friendly frontend for Radarr: search, add with a
// quality profile, and manually pick releases to grab. It proxies the Radarr
// API so the key never reaches the browser.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var staticFS embed.FS

type server struct {
	radarrURL string
	apiKey    string
	client    *http.Client
}

func main() {
	radarrURL := strings.TrimRight(envOr("RADARR_URL", "http://radarr:7878"), "/")
	apiKey := os.Getenv("RADARR_API_KEY")
	if apiKey == "" {
		log.Fatal("RADARR_API_KEY is required")
	}
	addr := envOr("ADDR", ":8080")

	s := &server{
		radarrURL: radarrURL,
		apiKey:    apiKey,
		// Interactive release searches fan out to every indexer and can
		// take a minute on a cold cache.
		client: &http.Client{Timeout: 150 * time.Second},
	}

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/recent", s.handleRecent)
	mux.HandleFunc("GET /api/profiles", s.handleProfiles)
	mux.HandleFunc("GET /api/releases", s.handleReleases)
	mux.HandleFunc("GET /api/queue", s.handleQueue)
	mux.HandleFunc("GET /api/poster/", s.handlePoster)
	mux.HandleFunc("POST /api/add", s.handleAdd)
	mux.HandleFunc("POST /api/grab", s.handleGrab)
	mux.HandleFunc("POST /api/autosearch", s.handleAutoSearch)
	mux.Handle("GET /", http.FileServerFS(static))

	log.Printf("movies listening on %s, radarr at %s", addr, radarrURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ---------- Radarr client ----------

func (s *server) radarr(method, path string, query url.Values, body any) ([]byte, int, error) {
	u := s.radarrURL + "/api/v3" + path
	if query != nil {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Api-Key", s.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

func (s *server) radarrJSON(method, path string, query url.Values, body, out any) error {
	b, code, err := s.radarr(method, path, query, body)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return fmt.Errorf("radarr %s %s: %d: %s", method, path, code, radarrErrMsg(b))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// radarrErrMsg pulls a readable message out of a Radarr error body, which can
// be {"message": ...}, a validation array [{"errorMessage": ...}], or (behind
// a proxy) arbitrary HTML.
func radarrErrMsg(b []byte) string {
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	var arr []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(b, &arr) == nil && len(arr) > 0 && arr[0].ErrorMessage != "" {
		return arr[0].ErrorMessage
	}
	s := string(b)
	if strings.Contains(s, "<") {
		return "unexpected response"
	}
	return truncate(s, 300)
}

// ---------- Radarr shapes (partial) ----------

type radarrMovie struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	Year             int    `json:"year"`
	TmdbID           int    `json:"tmdbId"`
	Overview         string `json:"overview"`
	Runtime          int    `json:"runtime"`
	Status           string `json:"status"`
	IsAvailable      bool   `json:"isAvailable"`
	Certification    string `json:"certification"`
	HasFile          bool   `json:"hasFile"`
	Monitored        bool   `json:"monitored"`
	QualityProfileID int    `json:"qualityProfileId"`
	RemotePoster     string `json:"remotePoster"`
	Added            string `json:"added"`
	Images           []struct {
		CoverType string `json:"coverType"`
		RemoteURL string `json:"remoteUrl"`
	} `json:"images"`
	MovieFile struct {
		Quality struct {
			Quality struct {
				Name string `json:"name"`
			} `json:"quality"`
		} `json:"quality"`
	} `json:"movieFile"`
}

type radarrRelease struct {
	GUID             string  `json:"guid"`
	IndexerID        int     `json:"indexerId"`
	Indexer          string  `json:"indexer"`
	Title            string  `json:"title"`
	Size             int64   `json:"size"`
	Seeders          int     `json:"seeders"`
	Leechers         int     `json:"leechers"`
	Protocol         string  `json:"protocol"`
	AgeHours         float64 `json:"ageHours"`
	Rejected         bool    `json:"rejected"`
	Rejections       []string `json:"rejections"`
	CustomFormatScore int    `json:"customFormatScore"`
	Languages        []struct {
		Name string `json:"name"`
	} `json:"languages"`
	IndexerFlags []string `json:"indexerFlags"`
	Quality      struct {
		Quality struct {
			Name       string `json:"name"`
			Resolution int    `json:"resolution"`
		} `json:"quality"`
	} `json:"quality"`
}

// ---------- API shapes ----------

type movie struct {
	ID               int    `json:"id"`
	TmdbID           int    `json:"tmdbId"`
	Title            string `json:"title"`
	Year             int    `json:"year"`
	Overview         string `json:"overview"`
	Runtime          int    `json:"runtime"`
	Status           string `json:"status"`
	IsAvailable      bool   `json:"isAvailable"`
	Certification    string `json:"certification"`
	HasFile          bool   `json:"hasFile"`
	FileQuality      string `json:"fileQuality,omitempty"`
	Monitored        bool   `json:"monitored"`
	QualityProfileID int    `json:"qualityProfileId"`
	Poster           string `json:"poster"`
}

func mapMovie(m radarrMovie) movie {
	poster := m.RemotePoster
	if poster == "" {
		for _, img := range m.Images {
			if img.CoverType == "poster" {
				poster = img.RemoteURL
				break
			}
		}
	}
	if m.ID > 0 {
		// Library movies get a stable proxied poster (survives TMDB URL churn).
		poster = fmt.Sprintf("/api/poster/%d", m.ID)
	}
	return movie{
		ID:               m.ID,
		TmdbID:           m.TmdbID,
		Title:            m.Title,
		Year:             m.Year,
		Overview:         m.Overview,
		Runtime:          m.Runtime,
		Status:           m.Status,
		IsAvailable:      m.IsAvailable,
		Certification:    m.Certification,
		HasFile:          m.HasFile,
		FileQuality:      m.MovieFile.Quality.Quality.Name,
		Monitored:        m.Monitored,
		QualityProfileID: m.QualityProfileID,
		Poster:           poster,
	}
}

type release struct {
	GUID              string   `json:"guid"`
	IndexerID         int      `json:"indexerId"`
	Indexer           string   `json:"indexer"`
	Title             string   `json:"title"`
	Size              int64    `json:"size"`
	Seeders           int      `json:"seeders"`
	Leechers          int      `json:"leechers"`
	Protocol          string   `json:"protocol"`
	AgeDays           int      `json:"ageDays"`
	Quality           string   `json:"quality"`
	Resolution        int      `json:"resolution"`
	Rejected          bool     `json:"rejected"`
	Rejections        []string `json:"rejections"`
	CustomFormatScore int      `json:"customFormatScore"`
	Languages         []string `json:"languages"`
	Flags             []string `json:"flags"`
}

// ---------- handlers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	log.Printf("error: %v", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, []movie{})
		return
	}
	var results []radarrMovie
	if err := s.radarrJSON("GET", "/movie/lookup", url.Values{"term": {q}}, nil, &results); err != nil {
		writeErr(w, 502, err)
		return
	}
	out := make([]movie, 0, len(results))
	for _, m := range results {
		out = append(out, mapMovie(m))
	}
	writeJSON(w, out)
}

// handleRecent returns the most recently added library movies, for the
// landing screen.
func (s *server) handleRecent(w http.ResponseWriter, r *http.Request) {
	var all []radarrMovie
	if err := s.radarrJSON("GET", "/movie", nil, nil, &all); err != nil {
		writeErr(w, 502, err)
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Added > all[j].Added })
	if len(all) > 30 {
		all = all[:30]
	}
	out := make([]movie, 0, len(all))
	for _, m := range all {
		out = append(out, mapMovie(m))
	}
	writeJSON(w, out)
}

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	var profiles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := s.radarrJSON("GET", "/qualityprofile", nil, nil, &profiles); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, profiles)
}

func (s *server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TmdbID           int  `json:"tmdbId"`
		QualityProfileID int  `json:"qualityProfileId"`
		Search           bool `json:"search"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}

	// Fetch the full lookup object; Radarr wants the whole thing back when
	// adding, so keep it as a raw map and only set what we own.
	var body map[string]any
	if err := s.radarrJSON("GET", "/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(req.TmdbID)}}, nil, &body); err != nil {
		writeErr(w, 502, err)
		return
	}

	var roots []struct {
		Path string `json:"path"`
	}
	if err := s.radarrJSON("GET", "/rootfolder", nil, nil, &roots); err != nil {
		writeErr(w, 502, err)
		return
	}
	if len(roots) == 0 {
		writeErr(w, 502, fmt.Errorf("radarr has no root folders configured"))
		return
	}

	body["qualityProfileId"] = req.QualityProfileID
	body["rootFolderPath"] = roots[0].Path
	body["monitored"] = true
	body["minimumAvailability"] = "released"
	body["addOptions"] = map[string]any{"searchForMovie": req.Search}

	var added radarrMovie
	if err := s.radarrJSON("POST", "/movie", nil, body, &added); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, mapMovie(added))
}

func (s *server) handleReleases(w http.ResponseWriter, r *http.Request) {
	movieID := r.URL.Query().Get("movieId")
	if movieID == "" {
		writeErr(w, 400, fmt.Errorf("movieId is required"))
		return
	}
	var releases []radarrRelease
	if err := s.radarrJSON("GET", "/release", url.Values{"movieId": {movieID}}, nil, &releases); err != nil {
		writeErr(w, 502, err)
		return
	}
	out := make([]release, 0, len(releases))
	for _, rel := range releases {
		langs := make([]string, 0, len(rel.Languages))
		for _, l := range rel.Languages {
			langs = append(langs, l.Name)
		}
		out = append(out, release{
			GUID:              rel.GUID,
			IndexerID:         rel.IndexerID,
			Indexer:           rel.Indexer,
			Title:             rel.Title,
			Size:              rel.Size,
			Seeders:           rel.Seeders,
			Leechers:          rel.Leechers,
			Protocol:          rel.Protocol,
			AgeDays:           int(rel.AgeHours / 24),
			Quality:           rel.Quality.Quality.Name,
			Resolution:        rel.Quality.Quality.Resolution,
			Rejected:          rel.Rejected,
			Rejections:        rel.Rejections,
			CustomFormatScore: rel.CustomFormatScore,
			Languages:         langs,
			Flags:             rel.IndexerFlags,
		})
	}
	writeJSON(w, out)
}

func (s *server) handleGrab(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GUID      string `json:"guid"`
		IndexerID int    `json:"indexerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	body := map[string]any{"guid": req.GUID, "indexerId": req.IndexerID}
	if err := s.radarrJSON("POST", "/release", nil, body, nil); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) handleAutoSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MovieID int `json:"movieId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	body := map[string]any{"name": "MoviesSearch", "movieIds": []int{req.MovieID}}
	if err := s.radarrJSON("POST", "/command", nil, body, nil); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	var queue struct {
		Records []struct {
			ID                    int    `json:"id"`
			MovieID               int    `json:"movieId"`
			Title                 string `json:"title"`
			Status                string `json:"status"`
			TrackedDownloadState  string `json:"trackedDownloadState"`
			TrackedDownloadStatus string `json:"trackedDownloadStatus"`
			Size                  int64  `json:"size"`
			SizeLeft              int64  `json:"sizeleft"`
			TimeLeft              string `json:"timeleft"`
			Protocol              string `json:"protocol"`
			Movie                 struct {
				Title string `json:"title"`
				Year  int    `json:"year"`
			} `json:"movie"`
		} `json:"records"`
	}
	q := url.Values{"pageSize": {"200"}, "includeMovie": {"true"}}
	if err := s.radarrJSON("GET", "/queue", q, nil, &queue); err != nil {
		writeErr(w, 502, err)
		return
	}
	type item struct {
		ID           int    `json:"id"`
		MovieID      int    `json:"movieId"`
		MovieTitle   string `json:"movieTitle"`
		Year         int    `json:"year"`
		Poster       string `json:"poster"`
		ReleaseTitle string `json:"releaseTitle"`
		Status       string `json:"status"`
		State        string `json:"state"`
		Size         int64  `json:"size"`
		SizeLeft     int64  `json:"sizeleft"`
		TimeLeft     string `json:"timeleft"`
		Protocol     string `json:"protocol"`
	}
	out := make([]item, 0, len(queue.Records))
	for _, rec := range queue.Records {
		out = append(out, item{
			ID:           rec.ID,
			MovieID:      rec.MovieID,
			MovieTitle:   rec.Movie.Title,
			Year:         rec.Movie.Year,
			Poster:       fmt.Sprintf("/api/poster/%d", rec.MovieID),
			ReleaseTitle: rec.Title,
			Status:       rec.Status,
			State:        rec.TrackedDownloadState,
			Size:         rec.Size,
			SizeLeft:     rec.SizeLeft,
			TimeLeft:     rec.TimeLeft,
			Protocol:     rec.Protocol,
		})
	}
	// Actively moving downloads first, then the rest, stable by movie title.
	sort.SliceStable(out, func(i, j int) bool {
		ai := out[i].SizeLeft > 0 && out[i].Status == "downloading"
		aj := out[j].SizeLeft > 0 && out[j].Status == "downloading"
		if ai != aj {
			return ai
		}
		return out[i].MovieTitle < out[j].MovieTitle
	})
	writeJSON(w, out)
}

func (s *server) handlePoster(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/poster/")
	if _, err := strconv.Atoi(id); err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v3/mediacover/%s/poster-500.jpg", s.radarrURL, id), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	req.Header.Set("X-Api-Key", s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		http.Error(w, "no poster", resp.StatusCode)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	io.Copy(w, resp.Body)
}
