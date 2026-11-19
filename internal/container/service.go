package container

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/audit"
	"dmanager/internal/config"
	"dmanager/internal/db"
	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
	"dmanager/internal/managed"
	"dmanager/internal/notification"
)

const (
	actionSave   = "save"
	actionDelete = "delete"
)

// Service implements the dmanagerv1connect.ContainerServiceHandler interface.
type Service struct {
	dmanagerv1connect.UnimplementedContainerServiceHandler
	db           db.DBTX
	broker       *Broker
	dockerClient *client.Client
	logger       *slog.Logger
	registries   []config.Registry
	notifier     *notification.Dispatcher
	auditor      audit.Auditor // nil disables audit recording

	// external decides which containers are owned by another tool; they are
	// never upgraded. notifyExternal re-enables update-available
	// notifications for them.
	external       managed.Policy
	notifyExternal bool
}

// SetExternalManagement configures the externally-managed policy. It must be
// called before the service starts handling requests.
func (s *Service) SetExternalManagement(policy managed.Policy, notifyUpdates bool) {
	s.external = policy
	s.notifyExternal = notifyUpdates
}

// externalOwner reports whether the record is externally managed, decided from
// its current labels and the configured selectors.
func (s *Service) externalOwner(r db.Container) (string, bool) {
	return s.external.Check(managed.DecodeLabels(r.Labels))
}

func (s *Service) mapRecord(r db.Container) *dmanagerv1.Container {
	return MapContainerRecord(r, s.external)
}

func externallyManagedError(name, by string) error {
	return connect.NewError(connect.CodeFailedPrecondition,
		fmt.Errorf("container %s is managed externally (%s) and cannot be upgraded or auto-updated by dmanager", name, by))
}

// NewService creates a new Container service.
func NewService(dbConn db.DBTX, broker *Broker, dockerClient *client.Client, logger *slog.Logger, registries []config.Registry, auditor audit.Auditor) *Service {
	return &Service{
		db:           dbConn,
		broker:       broker,
		dockerClient: dockerClient,
		logger:       logger,
		registries:   registries,
		notifier:     notification.NewDispatcher(dbConn, logger.With("module", "notification")),
		auditor:      auditor,
	}
}

// MapContainerRecord maps a database container record to a Protobuf Container structure.
func MapContainerRecord(r db.Container, policy managed.Policy) *dmanagerv1.Container {
	managedBy, external := policy.Check(managed.DecodeLabels(r.Labels))
	latestImageDigest := ""
	if digest, ok := r.LatestImageDigest.(string); ok {
		latestImageDigest = digest
	}

	lastCheckedAtStr := ""
	if t, ok := r.LastCheckedAt.(time.Time); ok {
		lastCheckedAtStr = t.Format(time.RFC3339)
	} else if sVal, ok := r.LastCheckedAt.(string); ok {
		lastCheckedAtStr = sVal
	}

	lastUpdatedAtStr := ""
	if t, ok := r.LastUpdatedAt.(time.Time); ok {
		lastUpdatedAtStr = t.Format(time.RFC3339)
	} else if sVal, ok := r.LastUpdatedAt.(string); ok {
		lastUpdatedAtStr = sVal
	}

	return &dmanagerv1.Container{
		Id:                r.ID,
		Name:              r.Name,
		Image:             r.Image,
		ImageId:           r.ImageID,
		State:             r.State,
		AutoUpdate:        r.AutoUpdate != 0,
		UpdateAvailable:   r.UpdateAvailable != 0,
		LatestImageDigest: latestImageDigest,
		LastCheckedAt:     lastCheckedAtStr,
		LastUpdatedAt:     lastUpdatedAtStr,
		ExternallyManaged: external,
		ManagedBy:         managedBy,
	}
}

// ListContainers queries the database for all containers and returns them mapped to Protobuf.
func (s *Service) ListContainers(ctx context.Context, req *connect.Request[dmanagerv1.ListContainersRequest]) (*connect.Response[dmanagerv1.ListContainersResponse], error) {
	queries := db.New(s.db)
	records, err := queries.ListContainers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoContainers := make([]*dmanagerv1.Container, len(records))
	for i, r := range records {
		protoContainers[i] = s.mapRecord(r)
	}

	res := connect.NewResponse(&dmanagerv1.ListContainersResponse{
		Containers: protoContainers,
	})
	return res, nil
}
