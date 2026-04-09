package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/baely/staticer/internal/models"
)

// database handles SQLite operations
type database struct {
	db     *sql.DB
	logger *slog.Logger
}

// newDatabase creates a new database connection and initializes schema
func newDatabase(dbPath string, logger *slog.Logger) (*database, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(2)

	d := &database{
		db:     db,
		logger: logger,
	}

	if err := d.initSchema(); err != nil {
		return nil, err
	}

	return d, nil
}

// initSchema creates the database schema
func (d *database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sites (
		subdomain TEXT PRIMARY KEY,
		api_key TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		custom_domain TEXT,
		file_count INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		listed INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_created_at ON sites(created_at);
	CREATE INDEX IF NOT EXISTS idx_custom_domain ON sites(custom_domain);
	CREATE INDEX IF NOT EXISTS idx_listed ON sites(listed);
	`

	// Migrations: add columns if they don't exist
	_, _ = d.db.Exec(`ALTER TABLE sites ADD COLUMN expires_at TIMESTAMP`)
	_, _ = d.db.Exec(`ALTER TABLE sites ADD COLUMN custom_domain TEXT`)
	_, _ = d.db.Exec(`ALTER TABLE sites ADD COLUMN listed INTEGER NOT NULL DEFAULT 0`)
	_, _ = d.db.Exec(`ALTER TABLE sites ADD COLUMN title TEXT NOT NULL DEFAULT ''`)
	_, _ = d.db.Exec(`ALTER TABLE sites ADD COLUMN description TEXT NOT NULL DEFAULT ''`)
	_, _ = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_custom_domain ON sites(custom_domain)`)
	_, _ = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_listed ON sites(listed)`)

	_, err := d.db.Exec(schema)
	return err
}

// CreateSite inserts a new site into the database
func (d *database) CreateSite(site *models.Site, apiKeyHash string) error {
	query := `
		INSERT INTO sites (subdomain, api_key, file_count, size_bytes, expires_at, custom_domain, listed, title, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var customDomain *string
	if site.CustomDomain != "" {
		customDomain = &site.CustomDomain
	}
	_, err := d.db.Exec(query, site.Subdomain, apiKeyHash, site.FileCount, site.SizeBytes, site.ExpiresAt, customDomain, site.Listed, site.Title, site.Description)
	if err != nil {
		return fmt.Errorf("failed to create site: %w", err)
	}

	d.logger.Info("Site created in database", "subdomain", site.Subdomain)
	return nil
}

// GetSite retrieves a site by subdomain
func (d *database) GetSite(subdomain string) (*models.Site, error) {
	query := `
		SELECT subdomain, created_at, expires_at, custom_domain, file_count, size_bytes, listed, title, description
		FROM sites
		WHERE subdomain = ?
	`

	site := &models.Site{}
	var customDomain *string
	err := d.db.QueryRow(query, subdomain).Scan(
		&site.Subdomain,
		&site.CreatedAt,
		&site.ExpiresAt,
		&customDomain,
		&site.FileCount,
		&site.SizeBytes,
		&site.Listed,
		&site.Title,
		&site.Description,
	)
	if customDomain != nil {
		site.CustomDomain = *customDomain
	}

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found: %s", subdomain)
		}
		return nil, fmt.Errorf("failed to get site: %w", err)
	}

	return site, nil
}

// ListSites retrieves all sites
func (d *database) ListSites() ([]*models.Site, error) {
	query := `
		SELECT subdomain, created_at, expires_at, custom_domain, file_count, size_bytes, listed, title, description
		FROM sites
		ORDER BY created_at DESC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list sites: %w", err)
	}
	defer rows.Close()

	var sites []*models.Site
	for rows.Next() {
		site := &models.Site{}
		var customDomain *string
		err := rows.Scan(
			&site.Subdomain,
			&site.CreatedAt,
			&site.ExpiresAt,
			&customDomain,
			&site.FileCount,
			&site.SizeBytes,
			&site.Listed,
			&site.Title,
			&site.Description,
		)
		if customDomain != nil {
			site.CustomDomain = *customDomain
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan site: %w", err)
		}
		sites = append(sites, site)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sites: %w", err)
	}

	return sites, nil
}

// ListPublicSites retrieves all sites where listed = true
func (d *database) ListPublicSites() ([]*models.Site, error) {
	query := `
		SELECT subdomain, created_at, expires_at, custom_domain, file_count, size_bytes, listed, title, description
		FROM sites
		WHERE listed = 1
		ORDER BY created_at DESC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list public sites: %w", err)
	}
	defer rows.Close()

	var sites []*models.Site
	for rows.Next() {
		site := &models.Site{}
		var customDomain *string
		err := rows.Scan(
			&site.Subdomain,
			&site.CreatedAt,
			&site.ExpiresAt,
			&customDomain,
			&site.FileCount,
			&site.SizeBytes,
			&site.Listed,
			&site.Title,
			&site.Description,
		)
		if customDomain != nil {
			site.CustomDomain = *customDomain
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan site: %w", err)
		}
		sites = append(sites, site)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sites: %w", err)
	}

	return sites, nil
}

// UpdateSiteMetadata updates mutable metadata fields for a site
func (d *database) UpdateSiteMetadata(subdomain string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	setClauses := make([]string, 0, len(updates))
	args := make([]interface{}, 0, len(updates)+1)
	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	args = append(args, subdomain)

	query := fmt.Sprintf("UPDATE sites SET %s WHERE subdomain = ?", strings.Join(setClauses, ", "))

	result, err := d.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update site: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("site not found: %s", subdomain)
	}

	d.logger.Info("Site metadata updated", "subdomain", subdomain)
	return nil
}

// DeleteSite removes a site from the database
func (d *database) DeleteSite(subdomain string) error {
	query := `DELETE FROM sites WHERE subdomain = ?`

	result, err := d.db.Exec(query, subdomain)
	if err != nil {
		return fmt.Errorf("failed to delete site: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("site not found: %s", subdomain)
	}

	d.logger.Info("Site deleted from database", "subdomain", subdomain)
	return nil
}

// GetSiteByCustomDomain retrieves a site by its custom domain
func (d *database) GetSiteByCustomDomain(domain string) (*models.Site, error) {
	query := `
		SELECT subdomain, created_at, expires_at, custom_domain, file_count, size_bytes, listed, title, description
		FROM sites
		WHERE custom_domain = ?
	`

	site := &models.Site{}
	var customDomain *string
	err := d.db.QueryRow(query, domain).Scan(
		&site.Subdomain,
		&site.CreatedAt,
		&site.ExpiresAt,
		&customDomain,
		&site.FileCount,
		&site.SizeBytes,
		&site.Listed,
		&site.Title,
		&site.Description,
	)
	if customDomain != nil {
		site.CustomDomain = *customDomain
	}

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found for domain: %s", domain)
		}
		return nil, fmt.Errorf("failed to get site by domain: %w", err)
	}

	return site, nil
}

// GetStorageStats returns storage usage statistics
func (d *database) GetStorageStats() (*models.StorageStats, error) {
	// Get total count and size
	query := `
		SELECT COUNT(*), COALESCE(SUM(size_bytes), 0)
		FROM sites
	`

	stats := &models.StorageStats{}
	err := d.db.QueryRow(query).Scan(&stats.TotalSites, &stats.TotalBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage stats: %w", err)
	}

	// Get largest sites
	largestQuery := `
		SELECT subdomain, size_bytes
		FROM sites
		ORDER BY size_bytes DESC
		LIMIT 10
	`

	rows, err := d.db.Query(largestQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get largest sites: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var siteSize models.SiteSize
		err := rows.Scan(&siteSize.Subdomain, &siteSize.SizeBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to scan site size: %w", err)
		}
		stats.LargestSites = append(stats.LargestSites, siteSize)
	}

	return stats, nil
}

// VerifyAPIKey checks if the API key matches the site
func (d *database) VerifyAPIKey(subdomain, apiKey string) (bool, error) {
	query := `SELECT api_key FROM sites WHERE subdomain = ?`

	var storedHash string
	err := d.db.QueryRow(query, subdomain).Scan(&storedHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to verify API key: %w", err)
	}

	// Hash the provided API key and compare
	providedHash := hashAPIKey(apiKey)
	return storedHash == providedHash, nil
}

// hashAPIKey creates a SHA-256 hash of the API key
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}
