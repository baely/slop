package docker

import (
	"github.com/docker/docker/client"
)

// NewClient creates a new Docker client configured from environment
func NewClient() (*client.Client, error) {
	return client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
}
