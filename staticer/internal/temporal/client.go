package temporal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
)

const (
	// TaskQueue is the name of the task queue for site deletion workflows
	TaskQueue = "site-deletion"

	// DeletionDelay is how long to wait before deleting a site
	DeletionDelay = 24 * time.Hour
)

// Config holds Temporal client configuration
type Config struct {
	Host      string
	Namespace string
}

// Client wraps the Temporal client for site deletion operations
type Client struct {
	client client.Client
	logger *slog.Logger
}

// NewClient creates a new Temporal client
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	c, err := client.Dial(client.Options{
		HostPort:  cfg.Host,
		Namespace: cfg.Namespace,
		Logger:    NewSlogAdapter(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}

	logger.Info("Connected to Temporal", "host", cfg.Host, "namespace", cfg.Namespace)

	return &Client{
		client: c,
		logger: logger,
	}, nil
}

// ScheduleSiteDeletion starts a workflow to delete the site after the specified delay.
// If delay is 0, the default DeletionDelay is used.
func (c *Client) ScheduleSiteDeletion(ctx context.Context, subdomain string, createdAt time.Time, delay time.Duration) error {
	if delay <= 0 {
		delay = DeletionDelay
	}

	workflowID := fmt.Sprintf("delete-site-%s", subdomain)

	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}

	input := ScheduledDeletionInput{
		Subdomain: subdomain,
		CreatedAt: createdAt,
		Delay:     delay,
	}

	_, err := c.client.ExecuteWorkflow(ctx, options, ScheduledDeletionWorkflow, input)
	if err != nil {
		c.logger.Error("Failed to schedule site deletion", "subdomain", subdomain, "error", err)
		return fmt.Errorf("failed to schedule deletion: %w", err)
	}

	c.logger.Info("Scheduled site deletion", "subdomain", subdomain, "workflowID", workflowID, "delay", delay)
	return nil
}

// CancelSiteDeletion cancels a pending deletion workflow
func (c *Client) CancelSiteDeletion(ctx context.Context, subdomain string) error {
	workflowID := fmt.Sprintf("delete-site-%s", subdomain)

	err := c.client.CancelWorkflow(ctx, workflowID, "")
	if err != nil {
		// Workflow might not exist if site was created before Temporal was enabled
		c.logger.Warn("Failed to cancel site deletion workflow", "subdomain", subdomain, "error", err)
		return nil // Don't return error - site deletion should still proceed
	}

	c.logger.Info("Cancelled site deletion workflow", "subdomain", subdomain, "workflowID", workflowID)
	return nil
}

// GetClient returns the underlying Temporal client
func (c *Client) GetClient() client.Client {
	return c.client
}

// Close closes the Temporal client connection
func (c *Client) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
