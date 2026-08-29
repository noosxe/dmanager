// Package admin implements the AdminService, a view over Docker host
// resources (images, volumes, networks) for the Administration page,
// plus admin-gated image deletion.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

// PruneImages deletes unused images from the Docker host in one daemon call
// (design.md §9.8, #196). Unlike DeleteImage, the response carries the
// daemon's per-image report and the bytes actually reclaimed; the client
// still re-fetches ListImages for the authoritative inventory. With
// dangling_only the daemon restricts to untagged (dangling) images; the
// default prunes every image no container references — in-use protection is
// enforced server-side regardless.
func (s *Service) PruneImages(ctx context.Context, req *connect.Request[dmanagerv1.PruneImagesRequest]) (*connect.Response[dmanagerv1.PruneImagesResponse], error) {
	// The daemon's default when the dangling filter is absent is dangling-only
	// (GetBoolOrDefault("dangling", true)), so the filter is always sent
	// explicitly: false (the default scope) prunes every unused image, true
	// prunes untagged (dangling) images only — #196 follow-up.
	filters := client.Filters{
		"dangling": map[string]bool{strconv.FormatBool(req.Msg.DanglingOnly): true},
	}

	result, err := s.dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filters})
	if err != nil {
		s.logger.Error("Failed to prune images", "dangling_only", req.Msg.DanglingOnly, "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to prune images: %w", err))
	}

	resp := &dmanagerv1.PruneImagesResponse{
		SpaceReclaimed: result.Report.SpaceReclaimed,
	}
	for _, item := range result.Report.ImagesDeleted {
		resp.ImagesDeleted = append(resp.ImagesDeleted, &dmanagerv1.PrunedImage{
			Deleted:  item.Deleted,
			Untagged: item.Untagged,
		})
	}

	return connect.NewResponse(resp), nil
}

// CheckEngine reports whether the Docker Engine is reachable (design.md
// §10.2). Unlike every other procedure, daemon unreachability is NOT a
// Connect error here: the outage is the answer, so the RPC succeeds with
// connected=false and a short reason. Only request/auth/transport problems
// fail the call — that distinction is what the sidebar pill relies on.
func (s *Service) CheckEngine(ctx context.Context, req *connect.Request[dmanagerv1.CheckEngineRequest]) (*connect.Response[dmanagerv1.CheckEngineResponse], error) {
	// A hung socket must not pile up goroutines under 30s client polling.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ping, err := s.dockerClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		s.logger.Warn("Docker Engine unreachable", "error", err)
		return connect.NewResponse(&dmanagerv1.CheckEngineResponse{
			Connected: false,
			Error:     err.Error(),
		}), nil
	}

	return connect.NewResponse(&dmanagerv1.CheckEngineResponse{
		Connected:  true,
		ApiVersion: ping.APIVersion,
	}), nil
}
