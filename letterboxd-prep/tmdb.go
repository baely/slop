package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const tmdbBase = "https://api.themoviedb.org/3"
const tmdbImageBase = "https://image.tmdb.org/t/p/w500"

type TMDBClient struct {
	token string
	hc    *http.Client

	mu          sync.Mutex
	detailCache map[int]*TMDBMovie
}

func NewTMDBClient(token string) *TMDBClient {
	return &TMDBClient{
		token:       token,
		hc:          &http.Client{Timeout: 20 * time.Second},
		detailCache: map[int]*TMDBMovie{},
	}
}

type TMDBMovie struct {
	ID               int      `json:"id"`
	Title            string   `json:"title"`
	OriginalTitle    string   `json:"original_title"`
	OriginalLanguage string   `json:"original_language,omitempty"`
	ReleaseDate      string   `json:"release_date"`
	Overview         string   `json:"overview"`
	PosterPath       string   `json:"poster_path"`
	IMDBID           string   `json:"imdb_id,omitempty"`
	Runtime          int      `json:"runtime,omitempty"`
	Genres           []string `json:"-"`
	Director         string   `json:"-"`
	DOP              string   `json:"-"`
	Writers          string   `json:"-"`
	Producers        string   `json:"-"`
	Cast             string   `json:"-"`
	Studios          string   `json:"-"`

	// Raw genre objects from search results
	GenreIDs []int `json:"genre_ids,omitempty"`
}

func (m *TMDBMovie) Year() string {
	if len(m.ReleaseDate) >= 4 {
		return m.ReleaseDate[:4]
	}
	return ""
}

func (m *TMDBMovie) PosterURL() string {
	if m.PosterPath == "" {
		return ""
	}
	return tmdbImageBase + m.PosterPath
}

type searchResp struct {
	Results []TMDBMovie `json:"results"`
}

type detailResp struct {
	TMDBMovie
	Genres              []struct{ Name string } `json:"genres"`
	ProductionCompanies []struct{ Name string } `json:"production_companies"`
	Credits             struct {
		Cast []struct {
			Name  string `json:"name"`
			Order int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			Job        string `json:"job"`
			Department string `json:"department"`
			Name       string `json:"name"`
		} `json:"crew"`
	} `json:"credits"`
}

func (c *TMDBClient) do(ctx context.Context, method, path string, query url.Values, out any) error {
	u := tmdbBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	// retry on 429 with backoff
	var resp *http.Response
	for attempt := 0; attempt < 4; attempt++ {
		resp, err = c.hc.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != 429 {
			break
		}
		ra := resp.Header.Get("Retry-After")
		resp.Body.Close()
		wait := time.Duration(1+attempt) * time.Second
		if n, err := strconv.Atoi(ra); err == nil && n > 0 && n < 10 {
			wait = time.Duration(n) * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tmdb %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *TMDBClient) Search(ctx context.Context, name, year string) ([]TMDBMovie, error) {
	q := url.Values{}
	q.Set("query", name)
	q.Set("include_adult", "false")
	q.Set("language", "en-US")
	if year != "" {
		q.Set("year", year)
	}
	var out searchResp
	if err := c.do(ctx, "GET", "/search/movie", q, &out); err != nil {
		return nil, err
	}
	// fallback: search again without year if no results
	if len(out.Results) == 0 && year != "" {
		q2 := url.Values{}
		q2.Set("query", name)
		q2.Set("include_adult", "false")
		q2.Set("language", "en-US")
		if err := c.do(ctx, "GET", "/search/movie", q2, &out); err != nil {
			return nil, err
		}
	}
	return out.Results, nil
}

// Details fetches full movie info including credits + genres + director.
func (c *TMDBClient) Details(ctx context.Context, id int) (*TMDBMovie, error) {
	c.mu.Lock()
	if cached, ok := c.detailCache[id]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	q := url.Values{}
	q.Set("language", "en-US")
	q.Set("append_to_response", "credits")
	var d detailResp
	if err := c.do(ctx, "GET", "/movie/"+strconv.Itoa(id), q, &d); err != nil {
		return nil, err
	}
	m := &d.TMDBMovie
	m.ID = id
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}

	// Crew: collect by role, dedup names per-role
	directors := newDedup()
	dop := newDedup()
	writers := newDedup()
	producers := newDedup()
	for _, cr := range d.Credits.Crew {
		switch cr.Job {
		case "Director":
			directors.add(cr.Name)
		case "Director of Photography", "Cinematography":
			dop.add(cr.Name)
		case "Writer", "Screenplay", "Story":
			writers.add(cr.Name)
		case "Producer":
			producers.add(cr.Name)
		}
	}
	m.Director = directors.joinAll()
	m.DOP = dop.joinAll()
	m.Writers = writers.joinAll()
	m.Producers = producers.joinAll()

	// Cast: top 5 by order
	cast := d.Credits.Cast
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })
	if len(cast) > 5 {
		cast = cast[:5]
	}
	castNames := make([]string, 0, len(cast))
	for _, a := range cast {
		castNames = append(castNames, a.Name)
	}
	m.Cast = strings.Join(castNames, ", ")

	// Studios: production companies, capped at 3
	pc := d.ProductionCompanies
	if len(pc) > 3 {
		pc = pc[:3]
	}
	studioNames := make([]string, 0, len(pc))
	for _, s := range pc {
		studioNames = append(studioNames, s.Name)
	}
	m.Studios = strings.Join(studioNames, "; ")

	c.mu.Lock()
	c.detailCache[id] = m
	c.mu.Unlock()
	return m, nil
}

// dedup collects unique strings preserving insertion order.
type dedup struct {
	seen  map[string]bool
	items []string
}

func newDedup() *dedup { return &dedup{seen: map[string]bool{}} }
func (d *dedup) add(s string) {
	if s == "" || d.seen[s] {
		return
	}
	d.seen[s] = true
	d.items = append(d.items, s)
}
func (d *dedup) joinAll() string { return strings.Join(d.items, ", ") }

// IsExactMatch returns true if the search result matches the LB title+year exactly enough to auto-resolve.
func IsExactMatch(lbName, lbYear string, candidate TMDBMovie) bool {
	if normTitle(lbName) != normTitle(candidate.Title) && normTitle(lbName) != normTitle(candidate.OriginalTitle) {
		return false
	}
	if lbYear == "" {
		return true
	}
	cy := candidate.Year()
	if cy == "" {
		return false
	}
	return cy == lbYear
}

func normTitle(s string) string {
	s = strings.ToLower(s)
	// keep alphanumerics and spaces only
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
