package container

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

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

		// Check if container already exists to preserve auto_update and update_available
		var autoUpdate int64 = 0
		var updateAvailable int64 = 0
		var latestImageDigest interface{} = nil
		var lastCheckedAt interface{} = nil
		var lastUpdatedAt interface{} = nil

		existing, err := queries.GetContainer(ctx, c.ID)
		if err != nil && errors.Is(err, sql.ErrNoRows) {
			existing, err = queries.GetContainerByName(ctx, name)
		}
		if err == nil {
			autoUpdate = existing.AutoUpdate
			updateAvailable = existing.UpdateAvailable
			latestImageDigest = existing.LatestImageDigest
			lastCheckedAt = existing.LastCheckedAt
			lastUpdatedAt = existing.LastUpdatedAt
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		params := db.SaveContainerParams{
			ID:                c.ID,
			Name:              name,
			Image:             c.Image,
			ImageID:           c.ImageID,
			State:             string(c.State),
			AutoUpdate:        autoUpdate,
			UpdateAvailable:   updateAvailable,
			LatestImageDigest: latestImageDigest,
			LastCheckedAt:     lastCheckedAt,
			LastUpdatedAt:     lastUpdatedAt,
			UpdatedAt:         time.Now(),
		}

		if err := queries.SaveContainer(ctx, params); err != nil {
			return err
		}
	}

	// Delete orphan containers that no longer exist on the host
	if err := queries.DeleteOrphanContainers(ctx, activeIDs); err != nil {
		return err
	}

	return nil
}
