package container

import (
	"context"
	"time"

	"dmanager/internal/db"
)

// StartScheduler starts the background registry update checker scheduler.
func StartScheduler(ctx context.Context, s *Service, intervalMinutes int) {
	if intervalMinutes <= 0 {
		intervalMinutes = 60
	}
	s.logger.Info("Starting periodic registry update checker scheduler", "interval_minutes", intervalMinutes)

	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logger.Info("Periodic registry update checker scheduler shutting down", "reason", "context cancelled")
				return
			case <-ticker.C:
				s.logger.Info("Running periodic registry update check")
				s.checkAllContainers(ctx)
			}
		}
	}()
}

func (s *Service) checkAllContainers(ctx context.Context) {
	queries := db.New(s.db)
	records, err := queries.ListContainers(ctx)
	if err != nil {
		s.logger.Error("Scheduler: failed to list containers from database", "error", err)
		return
	}

	for _, record := range records {
		s.logger.Debug("Scheduler: checking container updates", "container_name", record.Name, "container_id", record.ID)
		updateAvailable, _, err := s.checkContainerUpdatesInternal(ctx, record.ID)
		if err != nil {
			s.logger.Error("Scheduler: failed to check update for container", "container_name", record.Name, "container_id", record.ID, "error", err)
			continue
		}

		if updateAvailable {
			s.logger.Info("Scheduler: update is available for container", "container_name", record.Name, "container_id", record.ID)
			// Trigger automated re-deployment workflow if auto-update is enabled
			if record.AutoUpdate != 0 {
				s.logger.Info("Scheduler: auto-update is enabled. Triggering automated container re-deployment", "container_name", record.Name, "container_id", record.ID)
				_, upgradeErr := s.upgradeContainerInternal(ctx, record.ID)
				if upgradeErr != nil {
					s.logger.Error("Scheduler: automated re-deployment failed", "container_name", record.Name, "container_id", record.ID, "error", upgradeErr)
				} else {
					s.logger.Info("Scheduler: automated re-deployment succeeded", "container_name", record.Name, "container_id", record.ID)
				}
			}
		} else {
			s.logger.Debug("Scheduler: container is up to date", "container_name", record.Name, "container_id", record.ID)
		}
	}
}
