package temporal

import (
	"log/slog"

	"go.temporal.io/sdk/log"
)

// SlogAdapter adapts slog.Logger to Temporal's log.Logger interface
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a new SlogAdapter
func NewSlogAdapter(logger *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: logger.With("component", "temporal")}
}

func (a *SlogAdapter) Debug(msg string, keyvals ...interface{}) {
	a.logger.Debug(msg, keyvals...)
}

func (a *SlogAdapter) Info(msg string, keyvals ...interface{}) {
	a.logger.Info(msg, keyvals...)
}

func (a *SlogAdapter) Warn(msg string, keyvals ...interface{}) {
	a.logger.Warn(msg, keyvals...)
}

func (a *SlogAdapter) Error(msg string, keyvals ...interface{}) {
	a.logger.Error(msg, keyvals...)
}

// Ensure SlogAdapter implements log.Logger
var _ log.Logger = (*SlogAdapter)(nil)
