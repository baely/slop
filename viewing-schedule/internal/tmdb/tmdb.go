package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	baseURL = "https://api.themoviedb.org/3"
	imgURL  = "https://image.tmdb.org/t/p"
)

// Client is a small TMDB API client.
type Client struct {
	token string
	http  *http.Client
}

// New creates a new TMDB client. token may be empty (lookups will fail gracefully).
func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Movie is the result of a TMDB lookup.
type Movie struct {
	ID          int64
	Title       string
	Year        string
	Overview    string
	Director    string
	Runtime     int
	Genres      []string
	Rating      float64
	Poster      string
	PosterLg    string
	Backdrop    string
	ReleaseDate string
	Tagline     string
}

// HasToken reports whether the client is configured with a token.
func (c *Client) HasToken() bool { return c.token != "" }

// ParseTitle extracts "name" and "year" from a string like "The Apartment (1960)".
func ParseTitle(s string) (name, year string) {
	re := regexp.MustCompile(`^(.+?)\s*\((\d{4})\)\s*$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) != 3 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(m[1]), m[2]
}

// Lookup searches TMDB for a movie and returns enriched details.
func (c *Client) Lookup(ctx context.Context, rawTitle string) (*Movie, error) {
	if !c.HasToken() {
		return nil, errors.New("tmdb: no token configured")
	}
	name, year := ParseTitle(rawTitle)
	if name == "" {
		return nil, errors.New("tmdb: empty title")
	}

	params := url.Values{}
	params.Set("query", name)
	if year != "" {
		params.Set("year", year)
	}
	params.Set("include_adult", "false")

	var search struct {
		Results []struct {
			ID           int64   `json:"id"`
			Title        string  `json:"title"`
			Overview     string  `json:"overview"`
			PosterPath   string  `json:"poster_path"`
			BackdropPath string  `json:"backdrop_path"`
			VoteAverage  float64 `json:"vote_average"`
			ReleaseDate  string  `json:"release_date"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/movie?"+params.Encode(), &search); err != nil {
		return nil, err
	}
	if len(search.Results) == 0 {
		return nil, fmt.Errorf("tmdb: no result for %q", rawTitle)
	}
	r := search.Results[0]

	var details struct {
		Runtime int `json:"runtime"`
		Genres  []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Tagline string `json:"tagline"`
		Credits struct {
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
	}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d?append_to_response=credits", r.ID), &details); err != nil {
		return nil, err
	}

	director := ""
	for _, c := range details.Credits.Crew {
		if c.Job == "Director" {
			director = c.Name
			break
		}
	}

	genres := make([]string, 0, len(details.Genres))
	for _, g := range details.Genres {
		genres = append(genres, g.Name)
	}

	m := &Movie{
		ID:          r.ID,
		Title:       r.Title,
		Year:        year,
		Overview:    r.Overview,
		Director:    director,
		Runtime:     details.Runtime,
		Genres:      genres,
		Rating:      r.VoteAverage,
		ReleaseDate: r.ReleaseDate,
		Tagline:     details.Tagline,
	}
	if r.PosterPath != "" {
		m.Poster = imgURL + "/w342" + r.PosterPath
		m.PosterLg = imgURL + "/w500" + r.PosterPath
	}
	if r.BackdropPath != "" {
		m.Backdrop = imgURL + "/w780" + r.BackdropPath
	}
	if year == "" && len(r.ReleaseDate) >= 4 {
		m.Year = r.ReleaseDate[:4]
	}
	return m, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("tmdb: status %d for %s", res.StatusCode, path)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
