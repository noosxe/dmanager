package docker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"dmanager/internal/db"
)

// StartEventMonitor starts a background goroutine to monitor Docker events.
func StartEventMonitor(ctx context.Context, logger *slog.Logger, queries *db.Queries, dockerClient *client.Client, onEvent func(action string, containerID string)) {
	go func() {
		filter := make(client.Filters).Add("type", "container")

		res := dockerClient.Events(ctx, client.EventsListOptions{
			Filters: filter,
		})
		eventChan, errChan := res.Messages, res.Err

		for {
			select {
			case <-ctx.Done():
				logger.Info("Docker event monitor shutting down", "reason", "context cancelled")
				return
			case err := <-errChan:
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					logger.Error("Docker event monitor error", "error", err)
					select {
					case <-ctx.Done():
						return
					case <-time.After(1 * time.Second):
						res = dockerClient.Events(ctx, client.EventsListOptions{
							Filters: filter,
						})
						eventChan, errChan = res.Messages, res.Err
					}
				}
			case event := <-eventChan:
				handleEvent(ctx, logger, queries, dockerClient, event, onEvent)
			}
		}
	}()
}

func handleEvent(ctx context.Context, logger *slog.Logger, queries *db.Queries, dockerClient *client.Client, event events.Message, onEvent func(action string, containerID string)) {
	action := event.Action
	containerID := event.Actor.ID
	if containerID == "" {
		return
	}

	if action == "destroy" || action == "delete" {
		logger.Info("Docker event: container deleted. Removing from DB", "container_id", containerID)
		if err := queries.DeleteContainer(ctx, containerID); err != nil {
			logger.Error("Failed to delete container from DB", "container_id", containerID, "error", err)
		} else if onEvent != nil {
			onEvent("delete", containerID)
		}
		return
	}

	if action == "create" || action == "start" || action == "stop" || action == "die" || action == "update" {
		logger.Info("Docker event: container state changed. Inspecting...", "container_id", containerID, "action", action)
		inspect, err := dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			logger.Error("Failed to inspect container", "container_id", containerID, "error", err)
			return
		}

		name := strings.TrimPrefix(inspect.Container.Name, "/")
		image := ""
		if inspect.Container.Config != nil {
			image = inspect.Container.Config.Image
		}
		imageID := inspect.Container.Image
		state := ""
		if inspect.Container.State != nil {
			state = string(inspect.Container.State.Status)
		}

		var autoUpdate int64 = 0
		var updateAvailable int64 = 0
		var latestImageDigest interface{} = nil
		var lastCheckedAt interface{} = nil
		var lastUpdatedAt interface{} = nil

		existing, err := queries.GetContainer(ctx, containerID)
		if err == nil {
			autoUpdate = existing.AutoUpdate
			updateAvailable = existing.UpdateAvailable
			latestImageDigest = existing.LatestImageDigest
			lastCheckedAt = existing.LastCheckedAt
			lastUpdatedAt = existing.LastUpdatedAt
		} else if !errors.Is(err, sql.ErrNoRows) {
			logger.Error("Failed to check existing container in DB", "container_id", containerID, "error", err)
			return
		}

		params := db.SaveContainerParams{
			ID:                containerID,
			Name:              name,
			Image:             image,
			ImageID:           imageID,
			State:             state,
			AutoUpdate:        autoUpdate,
			UpdateAvailable:   updateAvailable,
			LatestImageDigest: latestImageDigest,
			LastCheckedAt:     lastCheckedAt,
			LastUpdatedAt:     lastUpdatedAt,
			UpdatedAt:         time.Now(),
		}

		if err := queries.SaveContainer(ctx, params); err != nil {
			logger.Error("Failed to save container to DB", "container_id", containerID, "error", err)
		} else if onEvent != nil {
			onEvent("save", containerID)
		}
	}
}
