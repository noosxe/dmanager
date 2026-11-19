package docker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"dmanager/internal/db"
	"dmanager/internal/managed"
)

// StartEventMonitor starts a background goroutine to monitor Docker events.
// After every reconnect of the event stream, resync (if non-nil) is called to
// pick up changes made while the stream was down. A resync failure is logged
// and does not stop the monitor.
func StartEventMonitor(ctx context.Context, logger *slog.Logger, queries *db.Queries, dockerClient *client.Client, resync func(ctx context.Context) error, onEvent func(action string, containerID string)) {
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
						if resync != nil {
							// The stream is subscribed first so nothing is missed between
							// the sync and the first event.
							if err := resync(ctx); err != nil {
								logger.Error("Container resync after event stream reconnect failed", "error", err)
							}
						}
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

	if action == "create" || action == "start" || action == "stop" || action == "die" || action == "update" ||
		action == "pause" || action == "unpause" || action == "rename" || action == "kill" {
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
		var labels map[string]string
		if inspect.Container.Config != nil {
			labels = inspect.Container.Config.Labels
		}
		state := ""
		if inspect.Container.State != nil {
			state = string(inspect.Container.State.Status)
		}

		if err := queries.UpsertContainerFromEvent(ctx, db.UpsertContainerFromEventParams{
			ID:      containerID,
			Name:    name,
			Image:   image,
			ImageID: imageID,
			State:   state,
			Labels:  managed.EncodeLabels(labels),
		}); err != nil {
			logger.Error("Failed to save container to DB", "container_id", containerID, "error", err)
		} else if onEvent != nil {
			onEvent("save", containerID)
		}
	}
}
