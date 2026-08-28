// Package admin implements the AdminService, a view over Docker host
// resources (images, volumes, networks) for the Administration page,
// plus admin-gated image deletion.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"google.golang.org/protobuf/types/known/timestamppb"

	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

// Service implements the dmanagerv1connect.AdminServiceHandler interface.
type Service struct {
	dmanagerv1connect.UnimplementedAdminServiceHandler
	dockerClient *client.Client
	logger       *slog.Logger
}

// NewService creates a new Admin service.
func NewService(dockerClient *client.Client, logger *slog.Logger) *Service {
	return &Service{
		dockerClient: dockerClient,
		logger:       logger,
	}
}

// ListImages returns all images present on the Docker host.
func (s *Service) ListImages(ctx context.Context, req *connect.Request[dmanagerv1.ListImagesRequest]) (*connect.Response[dmanagerv1.ListImagesResponse], error) {
	result, err := s.dockerClient.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to list images: %w", err))
	}

	images := make([]*dmanagerv1.Image, len(result.Items))
	for i, summary := range result.Items {
		images[i] = &dmanagerv1.Image{
			Id:              summary.ID,
			RepoTags:        summary.RepoTags,
			CreatedUnix:     summary.Created,
			SizeBytes:       summary.Size,
			ContainersCount: summary.Containers,
		}
	}

	return connect.NewResponse(&dmanagerv1.ListImagesResponse{Images: images}), nil
}

// ListVolumes returns all volumes present on the Docker host.
func (s *Service) ListVolumes(ctx context.Context, req *connect.Request[dmanagerv1.ListVolumesRequest]) (*connect.Response[dmanagerv1.ListVolumesResponse], error) {
	result, err := s.dockerClient.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to list volumes: %w", err))
	}

	volumes := make([]*dmanagerv1.Volume, len(result.Items))
	for i, v := range result.Items {
		var createdAt *timestamppb.Timestamp
		if t, parseErr := time.Parse(time.RFC3339Nano, v.CreatedAt); parseErr == nil {
			createdAt = timestamppb.New(t)
		}
		volumes[i] = &dmanagerv1.Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  createdAt,
			Labels:     v.Labels,
		}
	}

	return connect.NewResponse(&dmanagerv1.ListVolumesResponse{Volumes: volumes}), nil
}

// ListNetworks returns all networks present on the Docker host.
func (s *Service) ListNetworks(ctx context.Context, req *connect.Request[dmanagerv1.ListNetworksRequest]) (*connect.Response[dmanagerv1.ListNetworksResponse], error) {
	result, err := s.dockerClient.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to list networks: %w", err))
	}

	networks := make([]*dmanagerv1.Network, len(result.Items))
	for i, summary := range result.Items {
		networks[i] = &dmanagerv1.Network{
			Id:       summary.ID,
			Name:     summary.Name,
			Driver:   summary.Driver,
			Scope:    summary.Scope,
			Internal: summary.Internal,
			CreatedAt: func() *timestamppb.Timestamp {
				if summary.Created.IsZero() {
					return nil
				}
				return timestamppb.New(summary.Created)
			}(),
		}
	}

	return connect.NewResponse(&dmanagerv1.ListNetworksResponse{Networks: networks}), nil
}

// DeleteImage removes an image from the Docker host. The empty response is
// deliberate: clients re-fetch ListImages for the authoritative inventory.
func (s *Service) DeleteImage(ctx context.Context, req *connect.Request[dmanagerv1.DeleteImageRequest]) (*connect.Response[dmanagerv1.DeleteImageResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("image ID is required"))
	}

	if _, err := s.dockerClient.ImageRemove(ctx, req.Msg.Id, client.ImageRemoveOptions{Force: req.Msg.Force}); err != nil {
		switch {
		case cerrdefs.IsNotFound(err):
			s.logger.Error("Failed to delete image: not found on Docker host", "image_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeNotFound, errors.New("image not found on Docker host"))
		case cerrdefs.IsConflict(err):
			s.logger.Error("Failed to delete image: in use or tag conflict", "image_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("image is in use or has a tag conflict: %w", err))
		default:
			s.logger.Error("Failed to delete image", "image_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to delete image: %w", err))
		}
	}

	return connect.NewResponse(&dmanagerv1.DeleteImageResponse{}), nil
}
