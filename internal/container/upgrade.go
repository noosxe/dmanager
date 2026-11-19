package container

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"dmanager/internal/audit"
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
	s.auditUpgrade(ctx, audit.SourceUser, user.Username, user.Role, containerID, resp, err)
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
	// Wait drains the progress stream until the pull is complete and returns
	// any error the registry reported inside the stream (denied, unauthorized...).
	if waitErr := reader.Wait(ctx); waitErr != nil {
		s.logger.Error("Container upgrade failed: image pull reported an error", "container_id", containerID, "container_name", existing.Name, "image", imageRef, "error", waitErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to pull image %s: %w", imageRef, waitErr))
	}

	// Fetch the new image ID/digest after pull
	inspectNewImage, err := s.dockerClient.ImageInspect(ctx, imageRef)
	if err != nil {
		s.logger.Error("Container upgrade failed: inspect pulled image failed", "container_id", containerID, "container_name", existing.Name, "image", imageRef, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect pulled image: %w", err))
	}
	newImageID := inspectNewImage.ID
	s.logger.Info("Latest image pulled successfully", "container_name", existing.Name, "image", imageRef, "new_image_id", newImageID)

	// 5. Stop the container if it's currently running
	wasRunning := inspect.Container.State != nil && inspect.Container.State.Running
	if wasRunning {
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

	// 6. Prepare parameters for recreation
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

	// 7. Rename the old container aside so the replacement can take its name.
	// The old container is only removed once the replacement is running, so a
	// failure below can restore it.
	asideName := upgradeAsideName(containerName, containerID)
	s.logger.Info("Renaming old container aside for upgrade", "container_id", containerID, "container_name", containerName, "aside_name", asideName)
	if _, renameErr := s.dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: asideName}); renameErr != nil {
		s.logger.Error("Container upgrade failed: rename old container failed", "container_id", containerID, "container_name", containerName, "error", renameErr)
		s.restartIfWasRunning(ctx, containerID, containerName, wasRunning)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to rename old container: %w", renameErr))
	}

	// rollback removes the half-made replacement (if any), restores the old
	// container's name and starts it again when it was running. It uses a
	// context that survives cancellation of the request.
	rollback := func(newID string) {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if newID != "" {
			if _, rmErr := s.dockerClient.ContainerRemove(rctx, newID, client.ContainerRemoveOptions{Force: true}); rmErr != nil {
				s.logger.Error("Upgrade rollback: failed to remove new container", "container_id", newID, "error", rmErr)
			}
		}
		if _, rnErr := s.dockerClient.ContainerRename(rctx, containerID, client.ContainerRenameOptions{NewName: containerName}); rnErr != nil {
			s.logger.Error("Upgrade rollback: failed to restore old container name", "container_id", containerID, "container_name", containerName, "error", rnErr)
		}
		s.restartIfWasRunning(rctx, containerID, containerName, wasRunning)
	}

	// 8. Re-create the container
	s.logger.Info("Re-creating upgraded container", "container_name", containerName, "image", imageRef)
	created, createErr := s.dockerClient.ContainerCreate(ctx, createOpts)
	if createErr != nil {
		s.logger.Error("Container upgrade failed: re-create container failed", "container_name", containerName, "error", createErr)
		rollback("")
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
			s.logger.Error("Container upgrade failed: connect additional network failed, rolling back upgrade", "container_name", containerName, "network", netName, "error", netConnectErr)
			rollback(created.ID)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to connect container to network %s: %w", netName, netConnectErr))
		}
	}

	// 9. Start the new instance, but only if the original was running
	newState := "created"
	if wasRunning {
		if _, startErr := s.dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
			s.logger.Error("Container upgrade failed: start upgraded container failed", "container_name", containerName, "error", startErr)
			rollback(created.ID)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start upgraded container: %w", startErr))
		}
		newState = "running"
		s.logger.Info("Upgraded container started successfully", "container_id", created.ID, "container_name", containerName, "image", imageRef)
	}

	// 10. Inspect new container to get its actual state
	newInspect, err := s.dockerClient.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
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
		rollback(created.ID)
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
			rollback(created.ID)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save upgraded container to DB: %w", saveErr))
		}
	}

	// The replacement is running and recorded: the old container can go now.
	s.logger.Info("Removing old container after successful upgrade", "container_id", containerID, "container_name", containerName)
	if _, removeErr := s.dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); removeErr != nil && !cerrdefs.IsNotFound(removeErr) {
		// The upgrade itself succeeded; the leftover is under the aside name.
		s.logger.Warn("Upgrade succeeded but old container could not be removed", "container_id", containerID, "aside_name", asideName, "error", removeErr)
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

// upgradeAsideName is the temporary name an old container gets while its
// replacement is being created.
func upgradeAsideName(name, id string) string {
	short := id
	if len(short) > 12 {
		short = short[:12]
	}
	return name + "-dmanager-old-" + short
}

// restartIfWasRunning starts the old container again after a failed upgrade
// step, if it was running before the upgrade stopped it.
func (s *Service) restartIfWasRunning(ctx context.Context, containerID, name string, wasRunning bool) {
	if !wasRunning {
		return
	}
	if _, err := s.dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		s.logger.Error("Upgrade rollback: failed to restart old container", "container_id", containerID, "container_name", name, "error", err)
	}
}

// auditUpgrade records one audit entry per container upgrade (design.md
// §12.3): user-originated via the RPC, system-originated via the scheduler.
// The interceptor deliberately skips this procedure — this is the single
// recording point, carrying the old → new image digest transition.
func (s *Service) auditUpgrade(ctx context.Context, source, actor, actorRole, containerID string, resp *v1.UpgradeContainerResponse, upgradeErr error) {
	if s.auditor == nil {
		return
	}
	e := audit.Entry{
		Actor:        actor,
		ActorRole:    actorRole,
		Source:       source,
		Action:       "container.upgrade",
		ResourceType: "container",
		ResourceID:   containerID,
	}
	if upgradeErr != nil {
		e.Outcome = audit.OutcomeFailure
		e.Detail = upgradeErr.Error()
		var cerr *connect.Error
		if errors.As(upgradeErr, &cerr) {
			e.Detail = cerr.Message()
		}
	} else {
		e.Outcome = audit.OutcomeSuccess
		e.Detail = fmt.Sprintf("upgraded %s → %s", shortDigest(resp.PreviousImageId), shortDigest(resp.CurrentImageId))
	}
	s.auditor.Record(ctx, e)
}

// shortDigest renders a full image ID as sha256:<12 hex> for audit detail.
func shortDigest(id string) string {
	const prefix = "sha256:"
	if len(id) > len(prefix)+12 && id[:len(prefix)] == prefix {
		return id[:len(prefix)+12] + "…"
	}
	return id
}
