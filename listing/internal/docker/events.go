package docker

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/baely/listing/internal/model"
	"github.com/baely/listing/internal/store"
)

// ListenEvents listens for Docker container events and updates the store accordingly
func ListenEvents(ctx context.Context, cli *client.Client, s *store.ContainerStore) {
	for {
		if err := listenEventsOnce(ctx, cli, s); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("Event listener disconnected: %v, reconnecting...", err)
			time.Sleep(time.Second)
		}
	}
}

func listenEventsOnce(ctx context.Context, cli *client.Client, s *store.ContainerStore) error {
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("event", "start")
	f.Add("event", "stop")
	f.Add("event", "die")

	eventsCh, errCh := cli.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case event := <-eventsCh:
			handleEvent(ctx, cli, s, event)
		}
	}
}

func handleEvent(ctx context.Context, cli *client.Client, s *store.ContainerStore, event events.Message) {
	switch event.Action {
	case "start":
		// Inspect container to get full details including labels
		info, err := cli.ContainerInspect(ctx, event.Actor.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v", event.Actor.ID, err)
			return
		}

		// Check if container has the required URL label
		url, ok := info.Config.Labels[model.LabelURL]
		if !ok || url == "" {
			return
		}

		name := strings.TrimPrefix(info.Name, "/")

		s.Add(model.Container{
			ID:          info.ID,
			Name:        name,
			URL:         url,
			Title:       info.Config.Labels[model.LabelTitle],
			Description: info.Config.Labels[model.LabelDescription],
		})
		log.Printf("Added container: %s (%s)", name, info.ID[:12])

	case "stop", "die":
		s.Remove(event.Actor.ID)
		log.Printf("Removed container: %s", event.Actor.ID[:12])
	}
}

