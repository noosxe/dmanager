package container

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	connect "connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const roleAdmin = "admin"

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

	// 3. Inspect container state on Docker host before starting
	if s.dockerClient == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("docker client not initialized"))
	}

	inspectBefore, err := s.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}
	previousState := inspectBefore.State.Status

	// 4. Start the container
	if startErr := s.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{}); startErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start container: %w", startErr))
	}

	// 5. Inspect container state after starting
	inspectAfter, err := s.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container after start: %w", err))
	}
	currentState := inspectAfter.State.Status

	// 6. Update database record with the new state
	params := db.SaveContainerParams{
		ID:                containerID,
		Name:              existing.Name,
		Image:             existing.Image,
		ImageID:           existing.ImageID,
		State:             currentState,
		AutoUpdate:        existing.AutoUpdate,
		UpdateAvailable:   existing.UpdateAvailable,
		LatestImageDigest: existing.LatestImageDigest,
		LastCheckedAt:     existing.LastCheckedAt,
		LastUpdatedAt:     existing.LastUpdatedAt,
		UpdatedAt:         time.Now(),
	}
	if saveErr := queries.SaveContainer(ctx, params); saveErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update container state in DB: %w", saveErr))
	}

	// 7. Publish updated state to active streaming clients
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      "save",
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	return connect.NewResponse(&v1.StartContainerResponse{
		Id:            containerID,
		PreviousState: previousState,
		CurrentState:  currentState,
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

	// 3. Inspect container state on Docker host before stopping
	if s.dockerClient == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("docker client not initialized"))
	}

	inspectBefore, err := s.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}
	previousState := inspectBefore.State.Status

	// 4. Stop the container with a graceful timeout of 15 seconds
	timeoutSeconds := 15
	stopOpts := container.StopOptions{
		Timeout: &timeoutSeconds,
	}
	if stopErr := s.dockerClient.ContainerStop(ctx, containerID, stopOpts); stopErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop container: %w", stopErr))
	}

	// 5. Inspect container state after stopping
	inspectAfter, err := s.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container after stop: %w", err))
	}
	currentState := inspectAfter.State.Status

	// 6. Update database record with the new state
	params := db.SaveContainerParams{
		ID:                containerID,
		Name:              existing.Name,
		Image:             existing.Image,
		ImageID:           existing.ImageID,
		State:             currentState,
		AutoUpdate:        existing.AutoUpdate,
		UpdateAvailable:   existing.UpdateAvailable,
		LatestImageDigest: existing.LatestImageDigest,
		LastCheckedAt:     existing.LastCheckedAt,
		LastUpdatedAt:     existing.LastUpdatedAt,
		UpdatedAt:         time.Now(),
	}
	if saveErr := queries.SaveContainer(ctx, params); saveErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update container state in DB: %w", saveErr))
	}

	// 7. Publish updated state to active streaming clients
	updatedRecord, err := queries.GetContainer(ctx, containerID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      "save",
			ContainerId: containerID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	return connect.NewResponse(&v1.StopContainerResponse{
		Id:            containerID,
		PreviousState: previousState,
		CurrentState:  currentState,
	}), nil
}
