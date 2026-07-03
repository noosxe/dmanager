package container

import (
	"context"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/db"
	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

// Service implements the dmanagerv1connect.ContainerServiceHandler interface.
type Service struct {
	dmanagerv1connect.UnimplementedContainerServiceHandler
	db           db.DBTX
	broker       *Broker
	dockerClient *client.Client
}

// NewService creates a new Container service.
func NewService(dbConn db.DBTX, broker *Broker, dockerClient *client.Client) *Service {
	return &Service{
		db:           dbConn,
		broker:       broker,
		dockerClient: dockerClient,
	}
}

// MapContainerRecord maps a database container record to a Protobuf Container structure.
func MapContainerRecord(r db.Container) *dmanagerv1.Container {
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
		protoContainers[i] = MapContainerRecord(r)
	}

	res := connect.NewResponse(&dmanagerv1.ListContainersResponse{
		Containers: protoContainers,
	})
	return res, nil
}
