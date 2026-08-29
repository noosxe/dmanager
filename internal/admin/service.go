// Package admin implements the AdminService, a view over Docker host
// resources (images, volumes, networks) for the Administration page,
// plus admin-gated image deletion.
package admin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	build "github.com/moby/moby/api/types/build"
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

// GetVolumeUsage measures local volume disk usage on demand (design.md §9.11,
// #212). One DiskUsage{Volumes: true} daemon call — expensive: the daemon
// recursively walks every local volume's directory tree, serially and
// uncached, so the frontend triggers this only via explicit user action.
// Aggregates are computed server-side so clients never re-derive them; a
// size of -1 (daemon walk failure) passes through verbatim and is excluded
// from the sums, matching the daemon's own accounting.
func (s *Service) GetVolumeUsage(ctx context.Context, req *connect.Request[dmanagerv1.GetVolumeUsageRequest]) (*connect.Response[dmanagerv1.GetVolumeUsageResponse], error) {
	// Verbose=true is required on modern daemons (API >= 1.52): the
	// non-verbose decode populates the aggregates but drops Items, leaving
	// the per-volume sizes empty. Same trap as the build-cache records call.
	usage, err := s.dockerClient.DiskUsage(ctx, client.DiskUsageOptions{Volumes: true, Verbose: true})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to measure volume usage: %w", err))
	}

	volumes := make([]*dmanagerv1.VolumeUsage, len(usage.Volumes.Items))
	var totalSize, reclaimable int64
	unusedCount := uint32(0)
	for i, v := range usage.Volumes.Items {
		var size, refCount int64
		if v.UsageData != nil {
			size = v.UsageData.Size
			refCount = v.UsageData.RefCount
		}
		volumes[i] = &dmanagerv1.VolumeUsage{
			Name:      v.Name,
			SizeBytes: size,
			RefCount:  refCount,
		}
		if size >= 0 {
			totalSize += size
			if refCount == 0 {
				reclaimable += size
			}
		}
		// Unused means unreferenced, regardless of whether the size is known:
		// a walk-failed volume is still reclaimable, its bytes are just unknown.
		if refCount == 0 {
			unusedCount++ //nolint:gosec // non-negative count
		}
	}

	return connect.NewResponse(&dmanagerv1.GetVolumeUsageResponse{
		Volumes:          volumes,
		TotalSizeBytes:   totalSize,
		ReclaimableBytes: reclaimable,
		UnusedCount:      unusedCount,
	}), nil
}

// PruneVolumes removes all unused volumes in one daemon call (design.md
// §9.11, #212). All=true includes named volumes (the daemon's default prunes
// anonymous only); the daemon re-evaluates container references at prune
// time, so volumes referenced by any container — running or stopped — are
// protected regardless of any stale client preview.
func (s *Service) PruneVolumes(ctx context.Context, req *connect.Request[dmanagerv1.PruneVolumesRequest]) (*connect.Response[dmanagerv1.PruneVolumesResponse], error) {
	result, err := s.dockerClient.VolumePrune(ctx, client.VolumePruneOptions{All: true})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to prune volumes: %w", err))
	}

	return connect.NewResponse(&dmanagerv1.PruneVolumesResponse{
		VolumesDeleted: uint32(len(result.Report.VolumesDeleted)), //nolint:gosec // non-negative daemon count
		Names:          result.Report.VolumesDeleted,
		SpaceReclaimed: result.Report.SpaceReclaimed,
	}), nil
}

// ListNetworks returns all networks present on the Docker host.
func (s *Service) ListNetworks(ctx context.Context, req *connect.Request[dmanagerv1.ListNetworksRequest]) (*connect.Response[dmanagerv1.ListNetworksResponse], error) {
	result, err := s.dockerClient.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to list networks: %w", err))
	}

	networks := make([]*dmanagerv1.Network, len(result.Items))
	for i, summary := range result.Items {
		// Enrichment (design.md §9.12, #215): the list endpoint carries no
		// attachment data on API >= 1.28, so usage comes from one inspect per
		// network — an in-memory libnetwork read, no filesystem walk. A failing
		// inspect degrades that row to -1 (unknown) instead of failing the list.
		containersCount := int64(-1)
		inspect, err := s.dockerClient.NetworkInspect(ctx, summary.ID, client.NetworkInspectOptions{})
		if err != nil {
			s.logger.Warn("Failed to inspect network; usage unknown", "network_id", summary.ID, "error", err)
		} else {
			containersCount = int64(len(inspect.Network.Containers))
		}

		networks[i] = &dmanagerv1.Network{
			Id:              summary.ID,
			Name:            summary.Name,
			Driver:          summary.Driver,
			Scope:           summary.Scope,
			Internal:        summary.Internal,
			ContainersCount: containersCount,
			Predefined:      isPredefinedNetwork(summary.Name),
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

// isPredefinedNetwork mirrors the daemon's isPreDefined rule for Linux hosts
// (daemon/network/network_mode_unix.go): these networks are daemon-owned and
// NetworkRemove is always refused with "is a pre-defined network". Windows
// hosts use a broader rule (everything not user-defined); dmanager targets
// Linux daemons and the daemon remains the gatekeeper regardless.
func isPredefinedNetwork(name string) bool {
	switch name {
	case "bridge", "host", "none":
		return true
	default:
		return false
	}
}

// DeleteNetwork removes a network from the Docker host. The empty response
// is deliberate: clients re-fetch ListNetworks for the authoritative
// inventory. The daemon refuses in-use networks ("has active endpoints",
// 403) and pre-defined ones regardless of the request; the service maps
// both to CodeFailedPrecondition with the daemon's message surfaced.
func (s *Service) DeleteNetwork(ctx context.Context, req *connect.Request[dmanagerv1.DeleteNetworkRequest]) (*connect.Response[dmanagerv1.DeleteNetworkResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("network ID is required"))
	}

	if _, err := s.dockerClient.NetworkRemove(ctx, req.Msg.Id, client.NetworkRemoveOptions{}); err != nil {
		switch {
		case cerrdefs.IsNotFound(err):
			s.logger.Error("Failed to delete network: not found on Docker host", "network_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeNotFound, errors.New("network not found on Docker host"))
		case cerrdefs.IsPermissionDenied(err):
			s.logger.Error("Failed to delete network: in use or pre-defined", "network_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("network is in use or pre-defined: %w", err))
		default:
			s.logger.Error("Failed to delete network", "network_id", req.Msg.Id, "error", err)
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to delete network: %w", err))
		}
	}

	return connect.NewResponse(&dmanagerv1.DeleteNetworkResponse{}), nil
}

// PruneNetworks removes unused networks from the Docker host in one daemon
// call (design.md §9.12, #215). No filters: the daemon's scope is fixed —
// config-only, non-pruneable (pre-defined) and endpoint-carrying networks
// are skipped locally, swarm-ingress additionally on the cluster path, all
// re-evaluated at prune time. The report carries names only — the daemon
// reports no byte figures for network prunes — so none are mapped; the
// client re-fetches ListNetworks for the authoritative inventory.
func (s *Service) PruneNetworks(ctx context.Context, req *connect.Request[dmanagerv1.PruneNetworksRequest]) (*connect.Response[dmanagerv1.PruneNetworksResponse], error) {
	result, err := s.dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to prune networks: %w", err))
	}

	return connect.NewResponse(&dmanagerv1.PruneNetworksResponse{
		NetworksDeleted: uint64(len(result.Report.NetworksDeleted)),
		Names:           result.Report.NetworksDeleted,
	}), nil
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

// GetBuildCacheStats reports builder-owned disk space (design.md §9.9, #206):
// the BuildKit build cache aggregates as supplied by the daemon —
// GET /system/df?type=build-cache (the hyphen matters; type=buildcache is
// rejected). No client-side summation: TotalSize, Reclaimable, TotalCount
// and ActiveCount arrive pre-aggregated.
func (s *Service) GetBuildCacheStats(ctx context.Context, req *connect.Request[dmanagerv1.GetBuildCacheStatsRequest]) (*connect.Response[dmanagerv1.GetBuildCacheStatsResponse], error) {
	usage, err := s.dockerClient.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true})
	if err != nil {
		s.logger.Error("Failed to read build cache stats", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to read build cache stats: %w", err))
	}
	bc := usage.BuildCache
	return connect.NewResponse(&dmanagerv1.GetBuildCacheStatsResponse{
		TotalBytes:       uint64(bc.TotalSize),   //nolint:gosec // non-negative daemon value
		ReclaimableBytes: uint64(bc.Reclaimable), //nolint:gosec // non-negative daemon value
		RecordCount:      uint32(bc.TotalCount),  //nolint:gosec // non-negative daemon value
		ActiveCount:      uint32(bc.ActiveCount), //nolint:gosec // non-negative daemon value
	}), nil
}

// PruneBuildCache deletes build cache records in one daemon call (design.md
// §9.9, #206) — POST /build/prune via BuildCachePrune. With all=false
// buildkit-internal cache types are preserved; records in active use are
// never removed, enforced daemon-side. The response carries the deleted-
// record count and the bytes the daemon actually freed — per-record IDs
// are opaque hashes and are not shipped.
func (s *Service) PruneBuildCache(ctx context.Context, req *connect.Request[dmanagerv1.PruneBuildCacheRequest]) (*connect.Response[dmanagerv1.PruneBuildCacheResponse], error) {
	result, err := s.dockerClient.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: req.Msg.All})
	if err != nil {
		s.logger.Error("Failed to prune build cache", "all", req.Msg.All, "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to prune build cache: %w", err))
	}

	count := len(result.Report.CachesDeleted)
	return connect.NewResponse(&dmanagerv1.PruneBuildCacheResponse{
		CachesDeleted:  uint32(count), //nolint:gosec // non-negative daemon value
		SpaceReclaimed: result.Report.SpaceReclaimed,
	}), nil
}

// ListBuildCacheRecords returns the daemon's build cache records sorted by
// size descending (design.md §9.10, #209) — the top-offenders view. It
// reuses the stats daemon call (GET /system/df?type=build-cache): the
// per-record Items already ship with the aggregates. Sorting is the
// server's contract so clients render largest-first without sort state.
func (s *Service) ListBuildCacheRecords(ctx context.Context, req *connect.Request[dmanagerv1.ListBuildCacheRecordsRequest]) (*connect.Response[dmanagerv1.ListBuildCacheRecordsResponse], error) {
	// Verbose is required on modern daemons (API >= 1.52): the non-verbose
	// decode drops Items, so the records view would silently render empty.
	usage, err := s.dockerClient.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
	if err != nil {
		s.logger.Error("Failed to list build cache records", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to list build cache records: %w", err))
	}

	items := usage.BuildCache.Items
	slices.SortFunc(items, func(a, b build.CacheRecord) int {
		if a.Size != b.Size {
			return cmp.Compare(b.Size, a.Size)
		}
		return strings.Compare(a.ID, b.ID)
	})

	records := make([]*dmanagerv1.BuildCacheRecord, 0, len(items))
	for _, item := range items {
		record := &dmanagerv1.BuildCacheRecord{
			Id:          item.ID,
			Type:        item.Type,
			Description: item.Description,
			SizeBytes:   uint64(item.Size), //nolint:gosec // non-negative daemon value
			InUse:       item.InUse,
			Shared:      item.Shared,
			UsageCount:  uint64(item.UsageCount), //nolint:gosec // non-negative daemon value
			CreatedAt:   timestamppb.New(item.CreatedAt),
		}
		if item.LastUsedAt != nil {
			record.LastUsedAt = timestamppb.New(*item.LastUsedAt)
		}
		records = append(records, record)
	}
	return connect.NewResponse(&dmanagerv1.ListBuildCacheRecordsResponse{Records: records}), nil
}

// PruneBuildCacheRecord deletes exactly one build cache record by its full
// ID (design.md §9.10, #209) — POST /build/prune with the id filter. The
// filter restricts candidates to that one record, so all=true only lifts
// the internal-type restriction for the explicitly targeted record
// (buildkit-internal types like exec.cachemount are otherwise undeletable
// under all=false); blast radius stays 1. Records in active use are never
// removed, enforced daemon-side. The response carries the daemon's actual
// deleted count and freed bytes — 0/0 when the daemon protected the record.
func (s *Service) PruneBuildCacheRecord(ctx context.Context, req *connect.Request[dmanagerv1.PruneBuildCacheRecordRequest]) (*connect.Response[dmanagerv1.PruneBuildCacheRecordResponse], error) {
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("record id is required"))
	}

	result, err := s.dockerClient.BuildCachePrune(ctx, client.BuildCachePruneOptions{
		All:     true,
		Filters: client.Filters{"id": {req.Msg.Id: true}},
	})
	if err != nil {
		s.logger.Error("Failed to prune build cache record", "id", req.Msg.Id, "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to prune build cache record: %w", err))
	}

	count := len(result.Report.CachesDeleted)
	return connect.NewResponse(&dmanagerv1.PruneBuildCacheRecordResponse{
		CachesDeleted:  uint32(count), //nolint:gosec // non-negative daemon value
		SpaceReclaimed: result.Report.SpaceReclaimed,
	}), nil
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
