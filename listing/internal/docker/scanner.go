package docker

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/baely/listing/internal/model"
	"github.com/baely/listing/internal/store"
)

// ScanContainers scans all running containers and adds those with baileys.public.url label to the store
func ScanContainers(ctx context.Context, cli *client.Client, s *store.ContainerStore) error {
	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return err
	}

	for _, c := range containers {
		// Check if container has the required URL label
		url, ok := c.Labels[model.LabelURL]
		if !ok || url == "" {
			continue
		}

		// Extract container name (remove leading /)
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		s.Add(model.Container{
			ID:          c.ID,
			Name:        name,
			URL:         url,
			Title:       c.Labels[model.LabelTitle],
			Description: c.Labels[model.LabelDescription],
		})
	}

	return nil
}
