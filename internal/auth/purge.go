package auth

import (
	"context"
	"log/slog"
	"time"

	"dmanager/internal/db"
)

// PurgeFunc represents a purge cleanup operation.
type PurgeFunc func(ctx context.Context) error

// SessionPurgeFunc returns a PurgeFunc that purges expired sessions based on both idle and absolute clocks.
func SessionPurgeFunc(queries *db.Queries) PurgeFunc {
	return func(ctx context.Context) error {
		now := time.Now()
		return queries.PurgeExpiredSessions(ctx, db.PurgeExpiredSessionsParams{
			ExpiresAt:         now,
			AbsoluteExpiresAt: now,
		})
	}
}

// StartPurgeJob starts a background goroutine that periodically executes the given purge functions.
// It stops when ctx is cancelled.
func StartPurgeJob(ctx context.Context, logger *slog.Logger, interval time.Duration, purgeFuncs ...PurgeFunc) {
	if interval <= 0 {
		interval = time.Hour
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runPurge(ctx, logger, purgeFuncs...)
			}
		}
	}()
}

// RunPurgeOnce runs all purge functions once sequentially, logging warnings for failures without halting execution.
func RunPurgeOnce(ctx context.Context, logger *slog.Logger, purgeFuncs ...PurgeFunc) {
	runPurge(ctx, logger, purgeFuncs...)
}

func runPurge(ctx context.Context, logger *slog.Logger, purgeFuncs ...PurgeFunc) {
	for _, fn := range purgeFuncs {
		if ctx.Err() != nil {
			return
		}
		if err := fn(ctx); err != nil {
			if logger != nil {
				logger.Warn("Purge job task failed", "error", err)
			}
		}
	}
}
