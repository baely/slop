package staticer

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// Site represents a publicly listed static site from staticer
type Site struct {
	Subdomain    string  `json:"subdomain"`
	URL          string  `json:"url"`
	CustomDomain string  `json:"custom_domain,omitempty"`
	CreatedAt    string  `json:"created_at"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
	FileCount    int     `json:"file_count"`
	SizeBytes    int64   `json:"size_bytes"`
	Title        string  `json:"title,omitempty"`
	Description  string  `json:"description,omitempty"`
}

// Client polls staticer's public API for listed sites
type Client struct {
	baseURL    string
	httpClient *http.Client

	mu    sync.RWMutex
	sites []Site
}

// NewClient creates a new staticer client that polls for public sites
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start begins polling for public sites at the given interval
func (c *Client) Start(interval time.Duration) {
	// Fetch immediately
	c.fetch()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			c.fetch()
		}
	}()
}

// List returns the current list of public sites
func (c *Client) List() []Site {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]Site, len(c.sites))
	copy(result, c.sites)
	return result
}

func (c *Client) fetch() {
	url := c.baseURL + "/api/public/sites"
	resp, err := c.httpClient.Get(url)
	if err != nil {
		log.Printf("Failed to fetch public sites from staticer: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Staticer returned status %d", resp.StatusCode)
		return
	}

	var result struct {
		Sites []Site `json:"sites"`
		Total int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to decode staticer response: %v", err)
		return
	}

	c.mu.Lock()
	c.sites = result.Sites
	c.mu.Unlock()

	log.Printf("Fetched %d public sites from staticer", len(result.Sites))
}
