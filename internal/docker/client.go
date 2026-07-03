package docker

import (
	"github.com/moby/moby/client"
)

// NewClient initializes the standard Docker client using client.NewClientWithOpts.
func NewClient(host string) (*client.Client, error) {
	var opts []client.Opt
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}

	return client.New(opts...)
}
