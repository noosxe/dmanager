package docker

import (
	"github.com/docker/docker/client"
)

// NewClient initializes the standard Docker client using client.NewClientWithOpts.
func NewClient(host string) (*client.Client, error) {
	var opts []client.Opt
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	opts = append(opts, client.WithAPIVersionNegotiation())

	return client.NewClientWithOpts(opts...)
}
