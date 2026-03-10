package temporal

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ScheduledDeletionInput is the input for the ScheduledDeletionWorkflow
type ScheduledDeletionInput struct {
	Subdomain string
	CreatedAt time.Time
	Delay     time.Duration
}

// ScheduledDeletionWorkflow waits for the configured delay then deletes the site
func ScheduledDeletionWorkflow(ctx workflow.Context, input ScheduledDeletionInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting scheduled deletion workflow", "subdomain", input.Subdomain, "delay", input.Delay)

	// Wait for the configured delay (durable timer)
	if err := workflow.Sleep(ctx, input.Delay); err != nil {
		logger.Error("Sleep interrupted", "error", err)
		return err
	}

	logger.Info("Delay elapsed, executing deletion", "subdomain", input.Subdomain)

	// Configure activity options with retry policy
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Execute the delete activity
	var activities *Activities
	err := workflow.ExecuteActivity(ctx, activities.DeleteSite, input.Subdomain).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to delete site", "subdomain", input.Subdomain, "error", err)
		return err
	}

	logger.Info("Site deletion completed", "subdomain", input.Subdomain)
	return nil
}
