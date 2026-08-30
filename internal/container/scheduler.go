package container

import (
	"context"
	"fmt"
	"time"

	"dmanager/internal/audit"
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
			s.notifier.SendGotify(ctx, "Registry Check Error", fmt.Sprintf("Failed to check for image updates for container %s: %v", record.Name, err), 3)
			continue
		}

		if updateAvailable {
			s.logger.Info("Scheduler: update is available for container", "container_name", record.Name, "container_id", record.ID)
			s.notifier.SendGotify(ctx, "Image Update Available", fmt.Sprintf("A new image update has been detected in the registry for container %s.", record.Name), 5)

			// Trigger automated re-deployment workflow if auto-update is enabled
			if record.AutoUpdate != 0 {
				s.logger.Info("Scheduler: auto-update is enabled. Triggering automated container re-deployment", "container_name", record.Name, "container_id", record.ID)
				resp, upgradeErr := s.upgradeContainerInternal(ctx, record.ID)
				s.auditUpgrade(ctx, audit.SourceSystem, audit.SystemActor, "", record.ID, resp, upgradeErr)
				if upgradeErr != nil {
					s.logger.Error("Scheduler: automated re-deployment failed", "container_name", record.Name, "container_id", record.ID, "error", upgradeErr)
					s.notifier.SendGotify(ctx, "Auto-Update Failed", fmt.Sprintf("Automated container re-deployment for %s failed: %v", record.Name, upgradeErr), 8)
				} else {
					s.logger.Info("Scheduler: automated re-deployment succeeded", "container_name", record.Name, "container_id", record.ID)
					s.notifier.SendGotify(ctx, "Auto-Update Succeeded", fmt.Sprintf("Automated container %s re-deployment succeeded. The container was successfully updated and restarted.", record.Name), 5)
				}
			}
		} else {
			s.logger.Debug("Scheduler: container is up to date", "container_name", record.Name, "container_id", record.ID)
		}
	}
}
