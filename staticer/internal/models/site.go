package models

import "time"

// Site represents a deployed static site
type Site struct {
	Subdomain    string     `json:"subdomain"`
	URL          string     `json:"url"`
	CustomDomain string     `json:"custom_domain,omitempty"`
	APIKey       string     `json:"api_key,omitempty"` // Omitted in list responses
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	FileCount    int        `json:"file_count"`
	SizeBytes    int64      `json:"size_bytes"`
	Listed       bool       `json:"listed"`
	Title        string     `json:"title,omitempty"`
	Description  string     `json:"description,omitempty"`
}

// StorageStats represents storage usage statistics
type StorageStats struct {
	TotalSites    int            `json:"total_sites"`
	TotalBytes    int64          `json:"total_size_bytes"`
	LargestSites  []SiteSize     `json:"largest_sites"`
}

// SiteSize represents a site's storage usage
type SiteSize struct {
	Subdomain string `json:"subdomain"`
	SizeBytes int64  `json:"size_bytes"`
}
