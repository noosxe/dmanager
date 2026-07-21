package container

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// UpgradeContainer pulls the latest image tag digest and recreates the container preserving its parameters.
func (s *Service) UpgradeContainer(ctx context.Context, req *connect.Request[v1.UpgradeContainerRequest]) (*connect.Response[v1.UpgradeContainerResponse], error) {
	// 1. Verify authorization rules (Authenticated, Admin-only)
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	if user.Role != roleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin privilege required"))
	}

	containerID := req.Msg.Id
	if containerID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("container ID is required"))
	}

	resp, err := s.upgradeContainerInternal(ctx, containerID)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (s *Service) upgradeContainerInternal(ctx context.Context, containerID string) (*v1.UpgradeContainerResponse, error) {
	// 2. Retrieve existing container details from database
	queries := db.New(s.db)
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	s.logger.Info("Upgrading container", "container_id", containerID, "container_name", existing.Name, "image", existing.Image)

	// 3. Inspect container settings on Docker host
	if s.dockerClient == nil {
		err = errors.New("docker client not initialized")
		s.logger.Error("Container upgrade failed", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	inspect, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			s.logger.Error("Container upgrade failed: container not found on Docker host", "container_id", containerID, "container_name", existing.Name)
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		s.logger.Error("Container upgrade failed: inspect container failed", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}

	// 4. Pull the latest image tag digest
	imageRef := inspect.Container.Config.Image
	if imageRef == "" {
		err = errors.New("container does not have an associated image")
		s.logger.Error("Container upgrade failed", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	authHeader, err := s.getRegistryAuth(imageRef)
	if err != nil {
		s.logger.Warn("Failed to resolve registry auth for pull, attempting unauthenticated pull", "image", imageRef, "error", err)
	}

	s.logger.Info("Pulling latest image for container upgrade", "container_name", existing.Name, "image", imageRef)
	reader, err := s.dockerClient.ImagePull(ctx, imageRef, client.ImagePullOptions{
		RegistryAuth: authHeader,
	})
	if err != nil {
		s.logger.Error("Container upgrade failed: pull image failed", "container_id", containerID, "container_name", existing.Name, "image", imageRef, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to pull image %s: %w", imageRef, err))
	}
	defer func() { _ = reader.Close() }()
	// Consume pull progress stream to block until pull is fully complete
	_, _ = io.Copy(io.Discard, reader)

	// Fetch the new image ID/digest after pull
	inspectNewImage, err := s.dockerClient.ImageInspect(ctx, imageRef)
	if err != nil {
		s.logger.Error("Container upgrade failed: inspect pulled image failed", "container_id", containerID, "container_name", existing.Name, "image", imageRef, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect pulled image: %w", err))
	}
	newImageID := inspectNewImage.ID
	s.logger.Info("Latest image pulled successfully", "container_name", existing.Name, "image", imageRef, "new_image_id", newImageID)

	// 5. Stop the container if it's currently running
	if inspect.Container.State != nil && inspect.Container.State.Running {
		s.logger.Info("Stopping running container for upgrade", "container_id", containerID, "container_name", existing.Name)
		timeoutSeconds := 15
		stopOpts := client.ContainerStopOptions{
			Timeout: &timeoutSeconds,
		}
		if _, stopErr := s.dockerClient.ContainerStop(ctx, containerID, stopOpts); stopErr != nil {
			s.logger.Error("Container upgrade failed: stop running container failed", "container_id", containerID, "container_name", existing.Name, "error", stopErr)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop container before upgrade: %w", stopErr))
		}
	}

	// 6. Delete the old container
	s.logger.Info("Removing old container for upgrade", "container_id", containerID, "container_name", existing.Name)
	removeOpts := client.ContainerRemoveOptions{
		Force: true,
	}
	if _, removeErr := s.dockerClient.ContainerRemove(ctx, containerID, removeOpts); removeErr != nil {
		s.logger.Error("Container upgrade failed: remove container failed", "container_id", containerID, "container_name", existing.Name, "error", removeErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove old container: %w", removeErr))
	}

	// 7. Prepare parameters for recreation
	// Trim leading slash from name if present
	containerName := strings.TrimPrefix(inspect.Container.Name, "/")

	// Map networks (handle multiple networks by connecting additional networks after creation)
	var firstNetworkName string
	var firstNetworkConfig *network.EndpointSettings
	otherNetworks := make(map[string]*network.EndpointSettings)

	if inspect.Container.NetworkSettings != nil {
		for netName, netConfig := range inspect.Container.NetworkSettings.Networks {
			if firstNetworkName == "" {
				firstNetworkName = netName
				firstNetworkConfig = netConfig
			} else {
				otherNetworks[netName] = netConfig
			}
		}
	}

	var netConfig *network.NetworkingConfig
	if firstNetworkName != "" {
		netConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				firstNetworkName: firstNetworkConfig,
			},
		}
	}

	createOpts := client.ContainerCreateOptions{
		Config:           inspect.Container.Config,
		HostConfig:       inspect.Container.HostConfig,
		NetworkingConfig: netConfig,
		Name:             containerName,
	}

	// 8. Re-create the container
	s.logger.Info("Re-creating upgraded container", "container_name", containerName, "image", imageRef)
	created, createErr := s.dockerClient.ContainerCreate(ctx, createOpts)
	if createErr != nil {
		s.logger.Error("Container upgrade failed: re-create container failed", "container_name", containerName, "error", createErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to re-create container: %w", createErr))
	}

	// Connect any additional networks
	for netName, netOpts := range otherNetworks {
		connectOpts := client.NetworkConnectOptions{
			Container:      created.ID,
			EndpointConfig: netOpts,
		}
		if _, netConnectErr := s.dockerClient.NetworkConnect(ctx, netName, connectOpts); netConnectErr != nil {
			// Clean up the created container if networking setup fails to prevent zombie containers
			s.logger.Error("Container upgrade failed: connect additional network failed, rolling back container creation", "container_name", containerName, "network", netName, "error", netConnectErr)
			_, _ = s.dockerClient.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to connect container to network %s: %w", netName, netConnectErr))
		}
	}

	// 9. Start the new instance
	if _, startErr := s.dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
		s.logger.Error("Container upgrade failed: start upgraded container failed", "container_name", containerName, "error", startErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start upgraded container: %w", startErr))
	}
	s.logger.Info("Upgraded container started successfully", "container_id", created.ID, "container_name", containerName, "image", imageRef)

	// 10. Inspect new container to get its actual state
	newInspect, err := s.dockerClient.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	newState := "running"
	if err == nil && newInspect.Container.State != nil {
		newState = string(newInspect.Container.State.Status)
	}

	// Update the database record in place (atomically swaps container ID and
	// preserves auto_update, avoiding the race condition of delete-then-insert)
	result, err := queries.UpdateContainerForUpgrade(ctx, db.UpdateContainerForUpgradeParams{
		ID:            created.ID,
		Name:          containerName,
		Image:         imageRef,
		ImageID:       newImageID,
		State:         newState,
		LastUpdatedAt: time.Now(),
		ID_2:          containerID,
	})
	if err != nil {
		s.logger.Error("Container upgrade failed: update container record in DB failed", "container_id", containerID, "new_container_id", created.ID, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update container record in DB: %w", err))
	}

	// If the in-place update found no rows (e.g., the event monitor processed a
	// destroy event and already deleted the old record), fall back to an upsert
	// that still writes the preserved auto_update value.
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		s.logger.Warn("Container upgrade: old record not found for in-place update, falling back to upsert", "old_container_id", containerID, "new_container_id", created.ID)
		saveParams := db.SaveContainerParams{
			ID:                created.ID,
			Name:              containerName,
			Image:             imageRef,
			ImageID:           newImageID,
			State:             newState,
			AutoUpdate:        existing.AutoUpdate,
			UpdateAvailable:   0,
			LatestImageDigest: existing.LatestImageDigest,
			LastCheckedAt:     existing.LastCheckedAt,
			LastUpdatedAt:     time.Now(),
			UpdatedAt:         time.Now(),
		}
		if saveErr := queries.SaveContainer(ctx, saveParams); saveErr != nil {
			s.logger.Error("Container upgrade failed: fallback save to DB failed", "container_id", created.ID, "container_name", containerName, "error", saveErr)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save upgraded container to DB: %w", saveErr))
		}
	}

	// 11. Broadcast database synchronization events to streaming clients
	// If the container ID changed, notify clients to remove the old entry
	if created.ID != containerID {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionDelete,
			ContainerId: containerID,
		})
	}

	// Stream save of new/updated container
	updatedRecord, err := queries.GetContainer(ctx, created.ID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: created.ID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	s.logger.Info("Container upgrade completed successfully", "old_container_id", containerID, "new_container_id", created.ID, "container_name", containerName)

	return &v1.UpgradeContainerResponse{
		Id:              created.ID,
		PreviousImageId: inspect.Container.Image,
		CurrentImageId:  newImageID,
	}, nil
}
