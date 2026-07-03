package docker

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"dmanager/internal/db"
)

// StartEventMonitor starts a background goroutine to monitor Docker events.
func StartEventMonitor(ctx context.Context, queries *db.Queries, dockerClient *client.Client, onEvent func(action string, containerID string)) {
	go func() {
		filter := filters.NewArgs()
		filter.Add("type", "container")

		eventChan, errChan := dockerClient.Events(ctx, types.EventsOptions{ //nolint:staticcheck
			Filters: filter,
		})

		for {
			select {
			case <-ctx.Done():
				log.Println("Docker event monitor shutting down: context cancelled")
				return
			case err := <-errChan:
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("Docker event monitor error: %v", err)
					select {
					case <-ctx.Done():
						return
					case <-time.After(1 * time.Second):
						eventChan, errChan = dockerClient.Events(ctx, types.EventsOptions{ //nolint:staticcheck
							Filters: filter,
						})
					}
				}
			case event := <-eventChan:
				handleEvent(ctx, queries, dockerClient, event, onEvent)
			}
		}
	}()
}

func handleEvent(ctx context.Context, queries *db.Queries, dockerClient *client.Client, event events.Message, onEvent func(action string, containerID string)) {
	action := event.Action
	containerID := event.Actor.ID
	if containerID == "" {
		return
	}

	if action == "destroy" || action == "delete" {
		log.Printf("Docker event: container %s deleted. Removing from DB", containerID)
		if err := queries.DeleteContainer(ctx, containerID); err != nil {
			log.Printf("Failed to delete container %s from DB: %v", containerID, err)
		} else if onEvent != nil {
			onEvent("delete", containerID)
		}
		return
	}

	if action == "create" || action == "start" || action == "stop" || action == "die" || action == "update" {
		log.Printf("Docker event: container %s state changed (%s). Inspecting...", containerID, action)
		inspect, err := dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v", containerID, err)
			return
		}

		name := strings.TrimPrefix(inspect.Name, "/")
		image := ""
		if inspect.Config != nil {
			image = inspect.Config.Image
		}
		imageID := inspect.Image
		state := ""
		if inspect.State != nil {
			state = inspect.State.Status
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
			log.Printf("Failed to check existing container %s in DB: %v", containerID, err)
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
			log.Printf("Failed to save container %s to DB: %v", containerID, err)
		} else if onEvent != nil {
			onEvent("save", containerID)
		}
	}
}
