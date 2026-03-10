package storage

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/baely/staticer/internal/models"
)

// DeployOptions holds optional parameters for site deployment
type DeployOptions struct {
	ExpiresAt    *time.Time
	CustomDomain string
}

// Storage defines the interface for site storage operations
type Storage interface {
	// CreateSite creates a new site from a ZIP file
	CreateSite(subdomain string, zipData io.Reader, maxFiles int, maxSize int64, host string, opts *DeployOptions) (*models.Site, error)

	// CreateSingleFileSite creates a new site from a single HTML file
	CreateSingleFileSite(subdomain string, fileData io.Reader, filename string, size int64, host string, opts *DeployOptions) (*models.Site, error)

	// GetSite retrieves site information by subdomain
	GetSite(subdomain string) (*models.Site, error)

	// GetSiteByCustomDomain retrieves a site by its custom domain
	GetSiteByCustomDomain(domain string) (*models.Site, error)

	// GetSitePath returns the filesystem path for a site
	GetSitePath(subdomain string) string

	// ListSites retrieves all sites
	ListSites() ([]*models.Site, error)

	// DeleteSite removes a site and its files
	DeleteSite(subdomain string) error

	// GetStorageStats returns storage usage statistics
	GetStorageStats() (*models.StorageStats, error)

	// VerifyAPIKey checks if the API key matches the site
	VerifyAPIKey(subdomain, apiKey string) (bool, error)

	// SubdomainExists checks if a subdomain is already in use
	SubdomainExists(subdomain string) bool
}

// storage implements the Storage interface
type storage struct {
	db       *database
	fs       *filesystem
	logger   *slog.Logger
}

// NewStorage creates a new storage instance
func NewStorage(dbPath, sitesDir string, logger *slog.Logger) (Storage, error) {
	// Initialize database
	db, err := newDatabase(dbPath, logger)
	if err != nil {
		return nil, err
	}

	// Initialize filesystem
	fs := newFilesystem(sitesDir, logger)

	return &storage{
		db:     db,
		fs:     fs,
		logger: logger,
	}, nil
}

// CreateSite creates a new site from a ZIP file
func (s *storage) CreateSite(subdomain string, zipData io.Reader, maxFiles int, maxSize int64, host string, opts *DeployOptions) (*models.Site, error) {
	// Read ZIP data into memory
	zipBytes, err := io.ReadAll(zipData)
	if err != nil {
		return nil, fmt.Errorf("failed to read ZIP data: %w", err)
	}

	// Extract ZIP to filesystem
	result, err := s.fs.ExtractZIP(subdomain, zipBytes, maxFiles, maxSize)
	if err != nil {
		return nil, err
	}

	// Generate API key
	apiKey, err := generateAPIKey()
	if err != nil {
		// Clean up extracted files
		s.fs.DeleteSite(subdomain)
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Create site model
	site := &models.Site{
		Subdomain: subdomain,
		URL:       fmt.Sprintf("https://%s.%s", subdomain, host),
		APIKey:    apiKey,
		CreatedAt: time.Now(),
		FileCount: result.FileCount,
		SizeBytes: result.TotalSize,
	}
	if opts != nil {
		site.ExpiresAt = opts.ExpiresAt
		site.CustomDomain = opts.CustomDomain
	}

	// Save to database (with hashed API key)
	apiKeyHash := hashAPIKey(apiKey)
	if err := s.db.CreateSite(site, apiKeyHash); err != nil {
		// Clean up extracted files
		s.fs.DeleteSite(subdomain)
		return nil, fmt.Errorf("failed to save site to database: %w", err)
	}

	s.logger.Info("Site created successfully", "subdomain", subdomain)
	return site, nil
}

// CreateSingleFileSite creates a new site from a single HTML file
func (s *storage) CreateSingleFileSite(subdomain string, fileData io.Reader, filename string, size int64, host string, opts *DeployOptions) (*models.Site, error) {
	// Read file data into memory
	fileBytes, err := io.ReadAll(fileData)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	// Save file to filesystem
	if err := s.fs.SaveSingleFile(subdomain, filename, fileBytes); err != nil {
		return nil, err
	}

	// Generate API key
	apiKey, err := generateAPIKey()
	if err != nil {
		// Clean up saved file
		s.fs.DeleteSite(subdomain)
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Create site model
	site := &models.Site{
		Subdomain: subdomain,
		URL:       fmt.Sprintf("https://%s.%s", subdomain, host),
		APIKey:    apiKey,
		CreatedAt: time.Now(),
		FileCount: 1,
		SizeBytes: size,
	}
	if opts != nil {
		site.ExpiresAt = opts.ExpiresAt
		site.CustomDomain = opts.CustomDomain
	}

	// Save to database (with hashed API key)
	apiKeyHash := hashAPIKey(apiKey)
	if err := s.db.CreateSite(site, apiKeyHash); err != nil {
		// Clean up saved file
		s.fs.DeleteSite(subdomain)
		return nil, fmt.Errorf("failed to save site to database: %w", err)
	}

	s.logger.Info("Single-file site created successfully", "subdomain", subdomain, "filename", filename)
	return site, nil
}

// GetSite retrieves site information by subdomain
func (s *storage) GetSite(subdomain string) (*models.Site, error) {
	site, err := s.db.GetSite(subdomain)
	if err != nil {
		return nil, err
	}

	// Note: API key is not returned from database
	return site, nil
}

// GetSiteByCustomDomain retrieves a site by its custom domain
func (s *storage) GetSiteByCustomDomain(domain string) (*models.Site, error) {
	return s.db.GetSiteByCustomDomain(domain)
}

// GetSitePath returns the filesystem path for a site
func (s *storage) GetSitePath(subdomain string) string {
	return s.fs.GetSitePath(subdomain)
}

// ListSites retrieves all sites
func (s *storage) ListSites() ([]*models.Site, error) {
	return s.db.ListSites()
}

// DeleteSite removes a site and its files
func (s *storage) DeleteSite(subdomain string) error {
	// Delete from database first
	if err := s.db.DeleteSite(subdomain); err != nil {
		return err
	}

	// Delete files from filesystem
	if err := s.fs.DeleteSite(subdomain); err != nil {
		s.logger.Error("Failed to delete site files (database already deleted)", "subdomain", subdomain, "error", err)
		return fmt.Errorf("site removed from database but files remain: %w", err)
	}

	s.logger.Info("Site deleted successfully", "subdomain", subdomain)
	return nil
}

// GetStorageStats returns storage usage statistics
func (s *storage) GetStorageStats() (*models.StorageStats, error) {
	return s.db.GetStorageStats()
}

// VerifyAPIKey checks if the API key matches the site
func (s *storage) VerifyAPIKey(subdomain, apiKey string) (bool, error) {
	return s.db.VerifyAPIKey(subdomain, apiKey)
}

// SubdomainExists checks if a subdomain is already in use
func (s *storage) SubdomainExists(subdomain string) bool {
	_, err := s.db.GetSite(subdomain)
	return err == nil
}

// generateAPIKey generates a cryptographically secure API key
func generateAPIKey() (string, error) {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Encode as base64 and add prefix
	encoded := base64.URLEncoding.EncodeToString(bytes)
	return "sk_" + encoded, nil
}
