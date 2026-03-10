package temporal

import (
	"context"
	"log/slog"

	"github.com/baely/staticer/internal/storage"
)

// Activities holds the dependencies for Temporal activities
type Activities struct {
	storage storage.Storage
	logger  *slog.Logger
}

// DeleteSite deletes a site by subdomain
// This activity is idempotent - it returns success if the site is already deleted
func (a *Activities) DeleteSite(ctx context.Context, subdomain string) error {
	a.logger.Info("Executing DeleteSite activity", "subdomain", subdomain)

	// Check if site still exists (handles manual deletion gracefully)
	if !a.storage.SubdomainExists(subdomain) {
		a.logger.Info("Site already deleted, skipping", "subdomain", subdomain)
		return nil // Idempotent - success if already gone
	}

	// Delete the site
	if err := a.storage.DeleteSite(subdomain); err != nil {
		a.logger.Error("Failed to delete site", "subdomain", subdomain, "error", err)
		return err
	}

	a.logger.Info("Site deleted successfully by scheduled workflow", "subdomain", subdomain)
	return nil
}
