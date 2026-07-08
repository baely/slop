// Package images resolves a place name ("Fiji", "Denarau Island") to a hero
// photo URL. Wikimedia is the default provider — keyless and location-accurate;
// set an Unsplash API access key to prefer Unsplash. Lookups are cached in the
// store so each query hits the network once (misses retry daily).
package images

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/baileybutler/voyage/internal/store"
)

const missRetryAfter = 24 * time.Hour

// Resolver looks up and caches hero images for place queries.
type Resolver struct {
	store       *store.Store
	client      *http.Client
	unsplashKey string

	// API bases, overridable in tests.
	wikiBase     string
	unsplashBase string
}

// New builds a Resolver. unsplashKey may be empty (Wikimedia only).
func New(st *store.Store, unsplashKey string) *Resolver {
	return &Resolver{
		store:        st,
		client:       &http.Client{Timeout: 3 * time.Second},
		unsplashKey:  strings.TrimSpace(unsplashKey),
		wikiBase:     "https://en.wikipedia.org",
		unsplashBase: "https://api.unsplash.com",
	}
}

// Resolve returns the first query that yields an image, trying each in order.
// Returns nil when nothing resolves — pages render fine without a hero.
func (r *Resolver) Resolve(queries ...string) *store.Image {
	if r == nil {
		return nil
	}
	for _, q := range queries {
		q = Normalize(q)
		if q == "" {
			continue
		}
		if img := r.lookup(q); img != nil && img.URL != "" {
			return img
		}
	}
	return nil
}

// lookup serves a single query from cache, fetching (and caching) on a miss.
func (r *Resolver) lookup(q string) *store.Image {
	key := r.provider() + ":" + strings.ToLower(q)
	if cached, err := r.store.GetImage(key); err == nil {
		if cached.URL != "" || time.Since(cached.FetchedTime()) < missRetryAfter {
			return cached
		}
	}

	img := store.Image{Query: key}
	var fetched *store.Image
	var err error
	if r.unsplashKey != "" {
		fetched, err = r.fetchUnsplash(q)
	} else {
		fetched, err = r.fetchWiki(q)
	}
	if err == nil && fetched != nil {
		img.URL, img.Credit, img.CreditURL = fetched.URL, fetched.Credit, fetched.CreditURL
	}
	_ = r.store.PutImage(img)
	return &img
}

func (r *Resolver) provider() string {
	if r.unsplashKey != "" {
		return "unsplash"
	}
	return "wiki"
}

// ---- Wikimedia ----

// fetchWiki finds the lead image of the best-matching Wikipedia article.
func (r *Resolver) fetchWiki(q string) (*store.Image, error) {
	u := r.wikiBase + "/w/api.php?action=query&generator=search&gsrnamespace=0&gsrlimit=3" +
		"&prop=pageimages%7Cinfo&piprop=thumbnail&pithumbsize=1200&inprop=url" +
		"&format=json&formatversion=2&gsrsearch=" + url.QueryEscape(q)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "voyage/1.0 (personal trip planner)")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wiki: status %d", resp.StatusCode)
	}

	var body struct {
		Query struct {
			Pages []struct {
				Index     int    `json:"index"`
				Title     string `json:"title"`
				FullURL   string `json:"fullurl"`
				Thumbnail *struct {
					Source string `json:"source"`
				} `json:"thumbnail"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	pages := body.Query.Pages
	sort.Slice(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	for _, p := range pages {
		if p.Thumbnail != nil && p.Thumbnail.Source != "" {
			return &store.Image{
				URL:       p.Thumbnail.Source,
				Credit:    p.Title + " · Wikipedia",
				CreditURL: p.FullURL,
			}, nil
		}
	}
	return nil, nil
}

// ---- Unsplash ----

// fetchUnsplash searches the official Unsplash API (requires an access key).
func (r *Resolver) fetchUnsplash(q string) (*store.Image, error) {
	u := r.unsplashBase + "/search/photos?per_page=1&orientation=landscape&query=" + url.QueryEscape(q)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Client-ID "+r.unsplashKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unsplash: status %d", resp.StatusCode)
	}

	var body struct {
		Results []struct {
			URLs struct {
				Regular string `json:"regular"`
			} `json:"urls"`
			Links struct {
				HTML string `json:"html"`
			} `json:"links"`
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Results) == 0 {
		return nil, nil
	}
	p := body.Results[0]
	if p.URLs.Regular == "" {
		return nil, nil
	}
	credit := "Unsplash"
	if p.User.Name != "" {
		credit = p.User.Name + " · Unsplash"
	}
	return &store.Image{
		URL:       p.URLs.Regular,
		Credit:    credit,
		CreditURL: p.Links.HTML + "?utm_source=voyage&utm_medium=referral",
	}, nil
}

// ---- query helpers ----

var yearRe = regexp.MustCompile(`\b(19|20)\d\d\b`)

// Normalize collapses whitespace in a query.
func Normalize(q string) string {
	return strings.Join(strings.Fields(q), " ")
}

// StripYears removes year tokens so trip titles like "Fiji 2026" query well.
func StripYears(q string) string {
	q = yearRe.ReplaceAllString(q, " ")
	return strings.Trim(Normalize(q), " -–·,")
}
