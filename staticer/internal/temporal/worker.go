package temporal

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/baely/staticer/internal/storage"
)

// Worker processes site deletion workflows
type Worker struct {
	worker worker.Worker
	logger *slog.Logger
	cancel context.CancelFunc
}

// NewWorker creates a new Temporal worker
func NewWorker(c client.Client, store storage.Storage, logger *slog.Logger) *Worker {
	w := worker.New(c, TaskQueue, worker.Options{})

	// Register workflows
	w.RegisterWorkflow(ScheduledDeletionWorkflow)

	// Register activities with storage dependency
	activities := &Activities{
		storage: store,
		logger:  logger,
	}
	w.RegisterActivity(activities)

	return &Worker{
		worker: w,
		logger: logger,
	}
}

// Start starts the worker in a background goroutine
func (w *Worker) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	go func() {
		err := w.worker.Run(worker.InterruptCh())
		if err != nil && ctx.Err() == nil {
			w.logger.Error("Temporal worker stopped with error", "error", err)
		}
	}()

	w.logger.Info("Temporal worker started", "taskQueue", TaskQueue)
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.worker != nil {
		w.worker.Stop()
	}
	w.logger.Info("Temporal worker stopped")
}
