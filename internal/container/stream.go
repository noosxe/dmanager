package container

import (
	"context"

	connect "connectrpc.com/connect"

	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
)

// StreamContainers streams real-time container states/events to connected clients.
func (s *Service) StreamContainers(
	ctx context.Context,
	req *connect.Request[dmanagerv1.StreamContainersRequest],
	stream *connect.ServerStream[dmanagerv1.StreamContainersResponse],
) error {
	ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}
