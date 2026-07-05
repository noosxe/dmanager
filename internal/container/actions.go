package container

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	"dmanager/internal/auth"
	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	roleAdmin = "admin"
	dockerIO  = "docker.io"
)

// StartContainer transitions a container to a started execution state.
func (s *Service) StartContainer(ctx context.Context, req *connect.Request[v1.StartContainerRequest]) (*connect.Response[v1.StartContainerResponse], error) {
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

	// 2. Check if the container exists in database
	queries := db.New(s.db)
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	s.logger.Info("Starting container", "container_id", containerID, "container_name", existing.Name)

	// 3. Inspect container state on Docker host before starting
	if s.dockerClient == nil {
		err = errors.New("docker client not initialized")
		s.logger.Error("Failed to start container", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	inspectBefore, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			s.logger.Error("Failed to start container: not found on Docker host", "container_id", containerID, "container_name", existing.Name)
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		s.logger.Error("Failed to inspect container before starting", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}
	previousState := inspectBefore.Container.State.Status

	// 4. Start the container
	if _, startErr := s.dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); startErr != nil {
		s.logger.Error("Failed to start container on Docker host", "container_id", containerID, "container_name", existing.Name, "error", startErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start container: %w", startErr))
	}

	// 5. Inspect container state after starting
	inspectAfter, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.logger.Error("Failed to inspect container after start", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container after start: %w", err))
	}
	currentState := inspectAfter.Container.State.Status

	// 6. Update database record with the new state
	params := db.SaveContainerParams{
		ID:                containerID,
		Name:              existing.Name,
		Image:             existing.Image,
		ImageID:           existing.ImageID,
		State:             string(currentState),
		AutoUpdate:        existing.AutoUpdate,
		UpdateAvailable:   existing.UpdateAvailable,
		LatestImageDigest: existing.LatestImageDigest,
		LastCheckedAt:     existing.LastCheckedAt,
		LastUpdatedAt:     existing.LastUpdatedAt,
		UpdatedAt:         time.Now(),
	}
	if saveErr := queries.SaveContainer(ctx, params); saveErr != nil {
		s.logger.Error("Failed to update container state in DB", "container_id", containerID, "container_name", existing.Name, "error", saveErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update container state in DB: %w", saveErr))
	}

	// 7. Publish updated state to active streaming clients
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	s.logger.Info("Container started successfully", "container_id", containerID, "container_name", existing.Name, "previous_state", string(previousState), "current_state", string(currentState))

	return connect.NewResponse(&v1.StartContainerResponse{
		Id:            containerID,
		PreviousState: string(previousState),
		CurrentState:  string(currentState),
	}), nil
}

// StopContainer gracefully stops a running container.
func (s *Service) StopContainer(ctx context.Context, req *connect.Request[v1.StopContainerRequest]) (*connect.Response[v1.StopContainerResponse], error) {
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

	// 2. Check if the container exists in database
	queries := db.New(s.db)
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	s.logger.Info("Stopping container", "container_id", containerID, "container_name", existing.Name)

	// 3. Inspect container state on Docker host before stopping
	if s.dockerClient == nil {
		err = errors.New("docker client not initialized")
		s.logger.Error("Failed to stop container", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	inspectBefore, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			s.logger.Error("Failed to stop container: not found on Docker host", "container_id", containerID, "container_name", existing.Name)
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		s.logger.Error("Failed to inspect container before stopping", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}
	previousState := inspectBefore.Container.State.Status

	// 4. Stop the container with a graceful timeout of 15 seconds
	timeoutSeconds := 15
	stopOpts := client.ContainerStopOptions{
		Timeout: &timeoutSeconds,
	}
	if _, stopErr := s.dockerClient.ContainerStop(ctx, containerID, stopOpts); stopErr != nil {
		s.logger.Error("Failed to stop container on Docker host", "container_id", containerID, "container_name", existing.Name, "error", stopErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop container: %w", stopErr))
	}

	// 5. Inspect container state after stopping
	inspectAfter, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.logger.Error("Failed to inspect container after stop", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container after stop: %w", err))
	}
	currentState := inspectAfter.Container.State.Status

	// 6. Update database record with the new state
	params := db.SaveContainerParams{
		ID:                containerID,
		Name:              existing.Name,
		Image:             existing.Image,
		ImageID:           existing.ImageID,
		State:             string(currentState),
		AutoUpdate:        existing.AutoUpdate,
		UpdateAvailable:   existing.UpdateAvailable,
		LatestImageDigest: existing.LatestImageDigest,
		LastCheckedAt:     existing.LastCheckedAt,
		LastUpdatedAt:     existing.LastUpdatedAt,
		UpdatedAt:         time.Now(),
	}
	if saveErr := queries.SaveContainer(ctx, params); saveErr != nil {
		s.logger.Error("Failed to update container state in DB after stop", "container_id", containerID, "container_name", existing.Name, "error", saveErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update container state in DB: %w", saveErr))
	}

	// 7. Publish updated state to active streaming clients
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	s.logger.Info("Container stopped successfully", "container_id", containerID, "container_name", existing.Name, "previous_state", string(previousState), "current_state", string(currentState))

	return connect.NewResponse(&v1.StopContainerResponse{
		Id:            containerID,
		PreviousState: string(previousState),
		CurrentState:  string(currentState),
	}), nil
}

// SetContainerAutoUpdate persists a container's auto-update settings in the SQLite database and publishes the sync event.
func (s *Service) SetContainerAutoUpdate(ctx context.Context, req *connect.Request[v1.SetContainerAutoUpdateRequest]) (*connect.Response[v1.SetContainerAutoUpdateResponse], error) {
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

	queries := db.New(s.db)
	// Check if container exists in database
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	s.logger.Info("Setting container auto-update", "container_id", containerID, "container_name", existing.Name, "auto_update", req.Msg.AutoUpdate)

	var autoUpdateVal int64
	if req.Msg.AutoUpdate {
		autoUpdateVal = 1
	}

	err = queries.SetContainerAutoUpdate(ctx, db.SetContainerAutoUpdateParams{
		ID:         containerID,
		AutoUpdate: autoUpdateVal,
	})
	if err != nil {
		s.logger.Error("Failed to update container auto-update setting in DB", "container_id", containerID, "container_name", existing.Name, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update auto-update setting: %w", err))
	}

	// Fetch updated record and publish synchronization event
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	s.logger.Info("Container auto-update setting updated successfully", "container_id", containerID, "container_name", existing.Name, "auto_update", req.Msg.AutoUpdate)

	return connect.NewResponse(&v1.SetContainerAutoUpdateResponse{
		Id:         containerID,
		AutoUpdate: req.Msg.AutoUpdate,
	}), nil
}

// CheckContainerUpdates triggers an immediate, out-of-band registry check for a specific container image and updates its database state.
func (s *Service) CheckContainerUpdates(ctx context.Context, req *connect.Request[v1.CheckContainerUpdatesRequest]) (*connect.Response[v1.CheckContainerUpdatesResponse], error) {
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

	updateAvailable, remoteDigest, err := s.checkContainerUpdatesInternal(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&v1.CheckContainerUpdatesResponse{
		Id:                containerID,
		UpdateAvailable:   updateAvailable,
		LatestImageDigest: remoteDigest,
	}), nil
}

func (s *Service) checkContainerUpdatesInternal(ctx context.Context, containerID string) (bool, string, error) {
	queries := db.New(s.db)
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		return false, "", err
	}

	// Get image reference. Try to inspect container on host first.
	imageRef := existing.Image
	localImageID := existing.ImageID
	if s.dockerClient != nil {
		inspect, inspectErr := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if inspectErr == nil {
			if inspect.Container.Config != nil && inspect.Container.Config.Image != "" {
				imageRef = inspect.Container.Config.Image
			}
			if inspect.Container.Image != "" {
				localImageID = inspect.Container.Image
			}
		}
	}

	s.logger.Info("Checking container updates", "container_id", containerID, "container_name", existing.Name, "image", imageRef)

	// Resolve registry authentication
	authHeader, err := s.getRegistryAuth(imageRef)
	if err != nil {
		s.logger.Warn("Failed to resolve registry auth, attempting unauthenticated check", "image", imageRef, "error", err)
	}

	if s.dockerClient == nil {
		err = errors.New("docker client not initialized")
		s.logger.Error("Failed to check container updates", "container_id", containerID, "container_name", existing.Name, "error", err)
		return false, "", err
	}

	// Contact registry for digest
	distInspect, err := s.dockerClient.DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{
		EncodedRegistryAuth: authHeader,
	})
	if err != nil {
		s.logger.Error("Failed to check container updates: registry check failed", "container_id", containerID, "container_name", existing.Name, "error", err)
		return false, "", fmt.Errorf("failed to query registry for image %s: %w", imageRef, err)
	}

	remoteDigest := string(distInspect.Descriptor.Digest)
	if remoteDigest == "" {
		err = fmt.Errorf("registry returned empty digest for image %s", imageRef)
		s.logger.Error("Failed to check container updates: empty remote digest", "container_id", containerID, "container_name", existing.Name, "error", err)
		return false, "", err
	}

	// Inspect local image to get its RepoDigests for comparison
	var repoDigests []string
	if imgInspect, imgInspectErr := s.dockerClient.ImageInspect(ctx, localImageID); imgInspectErr == nil {
		repoDigests = imgInspect.RepoDigests
	}

	// Check if local image digest matches the remote digest
	updateAvailable := isUpdateAvailable(repoDigests, remoteDigest)

	var updateAvailableVal int64
	if updateAvailable {
		updateAvailableVal = 1
	}

	// Update container state in SQLite DB
	now := time.Now()
	err = queries.UpdateContainerUpdateState(ctx, db.UpdateContainerUpdateStateParams{
		ID:                containerID,
		UpdateAvailable:   updateAvailableVal,
		LatestImageDigest: remoteDigest,
		LastCheckedAt:     now,
	})
	if err != nil {
		s.logger.Error("Failed to update container state in DB after update check", "container_id", containerID, "container_name", existing.Name, "error", err)
		return false, "", fmt.Errorf("failed to update container state in DB: %w", err)
	}

	// Publish sync event to streams
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	s.logger.Info("Container update check finished", "container_id", containerID, "container_name", existing.Name, "update_available", updateAvailable, "remote_digest", remoteDigest)

	return updateAvailable, remoteDigest, nil
}

// getRegistryAuth resolves base64-encoded registry credentials based on the image reference.
func (s *Service) getRegistryAuth(imageRef string) (string, error) {
	if len(s.registries) == 0 {
		return "", nil
	}

	host := getRegistryHost(imageRef)

	var matchedReg *config.Registry
	for _, reg := range s.registries {
		if reg.Host == "" {
			continue
		}
		if hostMatches(host, reg.Host) {
			r := reg // Pin loop variable
			matchedReg = &r
			break
		}
	}

	if matchedReg == nil {
		return "", nil
	}

	authConfig := registry.AuthConfig{
		Username:      matchedReg.Username,
		Password:      matchedReg.Password,
		ServerAddress: matchedReg.Host,
	}

	// #nosec G101 G117
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(encodedJSON), nil
}

func getRegistryHost(imageRef string) string {
	parts := strings.Split(imageRef, "/")
	if len(parts) > 1 {
		firstPart := parts[0]
		if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") || firstPart == "localhost" {
			return firstPart
		}
	}
	return dockerIO
}

func hostMatches(imageHost, regHost string) bool {
	return normalizeHost(imageHost) == normalizeHost(regHost)
}

func normalizeHost(h string) string {
	h = strings.ToLower(h)
	if h == "index.docker.io" || h == "registry-1.docker.io" || h == dockerIO {
		return dockerIO
	}
	return h
}

func isUpdateAvailable(localRepoDigests []string, remoteDigest string) bool {
	if remoteDigest == "" {
		return false
	}
	for _, rd := range localRepoDigests {
		parts := strings.Split(rd, "@")
		if len(parts) == 2 && parts[1] == remoteDigest {
			return false // Local matches remote digest, no update available
		}
	}
	return true // No matching digest found locally, update available
}
