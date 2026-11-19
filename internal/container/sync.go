package container

import (
	"context"
	"strings"

	"github.com/moby/moby/client"

	"dmanager/internal/db"
	"dmanager/internal/managed"
)

// SyncContainers fetches all containers on the host and caches them in SQLite.
func SyncContainers(ctx context.Context, dbConn db.DBTX, dockerClient *client.Client) error {
	res, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	containers := res.Items

	queries := db.New(dbConn)
	activeIDs := make([]string, 0, len(containers))

	for _, c := range containers {
		activeIDs = append(activeIDs, c.ID)

		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		if err := queries.UpsertContainerFromEvent(ctx, db.UpsertContainerFromEventParams{
			ID:      c.ID,
			Name:    name,
			Image:   c.Image,
			ImageID: c.ImageID,
			State:   string(c.State),
			Labels:  managed.EncodeLabels(c.Labels),
		}); err != nil {
			return err
		}
	}

	// Delete orphan containers that no longer exist on the host
	return pruneContainers(ctx, queries, activeIDs)
}

// pruneContainers deletes rows whose ID is not in activeIDs. An empty list
// means the host has no containers, so every row is stale; sqlc renders an
// empty slice as NOT IN (NULL), which matches nothing, so it is handled here.
func pruneContainers(ctx context.Context, queries *db.Queries, activeIDs []string) error {
	if len(activeIDs) == 0 {
		return queries.DeleteAllContainers(ctx)
	}
	return queries.DeleteOrphanContainers(ctx, activeIDs)
}
