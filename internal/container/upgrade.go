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

	// 2. Retrieve existing container details from database
	queries := db.New(s.db)
	existing, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	// 3. Inspect container settings on Docker host
	if s.dockerClient == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("docker client not initialized"))
	}

	inspect, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}

	// 4. Pull the latest image tag digest
	imageRef := inspect.Container.Config.Image
	if imageRef == "" {
		return nil, connect.NewError(connect.CodeInternal, errors.New("container does not have an associated image"))
	}

	reader, err := s.dockerClient.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to pull image %s: %w", imageRef, err))
	}
	defer func() { _ = reader.Close() }()
	// Consume pull progress stream to block until pull is fully complete
	_, _ = io.Copy(io.Discard, reader)

	// Fetch the new image ID/digest after pull
	inspectNewImage, err := s.dockerClient.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect pulled image: %w", err))
	}
	newImageID := inspectNewImage.ID

	// 5. Stop the container if it's currently running
	if inspect.Container.State != nil && inspect.Container.State.Running {
		timeoutSeconds := 15
		stopOpts := client.ContainerStopOptions{
			Timeout: &timeoutSeconds,
		}
		if _, stopErr := s.dockerClient.ContainerStop(ctx, containerID, stopOpts); stopErr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop container before upgrade: %w", stopErr))
		}
	}

	// 6. Delete the old container
	removeOpts := client.ContainerRemoveOptions{
		Force: true,
	}
	if _, removeErr := s.dockerClient.ContainerRemove(ctx, containerID, removeOpts); removeErr != nil {
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
	created, createErr := s.dockerClient.ContainerCreate(ctx, createOpts)
	if createErr != nil {
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
			_, _ = s.dockerClient.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to connect container to network %s: %w", netName, netConnectErr))
		}
	}

	// 9. Start the new instance
	if _, startErr := s.dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start upgraded container: %w", startErr))
	}

	// 10. Update the database: delete old container record and insert new container record
	if deleteErr := queries.DeleteContainer(ctx, containerID); deleteErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete old container record from DB: %w", deleteErr))
	}

	// Inspect the new container state to store accurate DB representation
	newInspect, err := s.dockerClient.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	newState := "running"
	if err == nil && newInspect.Container.State != nil {
		newState = string(newInspect.Container.State.Status)
	}

	// Save new container record
	saveParams := db.SaveContainerParams{
		ID:                created.ID,
		Name:              containerName,
		Image:             imageRef,
		ImageID:           newImageID,
		State:             newState,
		AutoUpdate:        existing.AutoUpdate,
		UpdateAvailable:   0, // Reset update flags
		LatestImageDigest: existing.LatestImageDigest,
		LastCheckedAt:     existing.LastCheckedAt,
		LastUpdatedAt:     time.Now(),
		UpdatedAt:         time.Now(),
	}

	if saveErr := queries.SaveContainer(ctx, saveParams); saveErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save upgraded container to DB: %w", saveErr))
	}

	// 11. Broadcast database synchronization events to streaming clients
	// Stream deletion of old container ID
	s.broker.Publish(&v1.StreamContainersResponse{
		Action:      actionDelete,
		ContainerId: containerID,
	})

	// Stream save of new container
	updatedRecord, err := queries.GetContainer(ctx, created.ID)
	if err == nil {
		s.broker.Publish(&v1.StreamContainersResponse{
			Action:      actionSave,
			ContainerId: created.ID,
			Container:   MapContainerRecord(updatedRecord),
		})
	}

	return connect.NewResponse(&v1.UpgradeContainerResponse{
		Id:              created.ID,
		PreviousImageId: inspect.Container.Image,
		CurrentImageId:  newImageID,
	}), nil
}
