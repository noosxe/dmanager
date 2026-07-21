package container

import (
	"context"
	"strings"

	"github.com/moby/moby/client"

	"dmanager/internal/db"
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
		}); err != nil {
			return err
		}
	}

	// Delete orphan containers that no longer exist on the host
	if err := queries.DeleteOrphanContainers(ctx, activeIDs); err != nil {
		return err
	}

	return nil
}
