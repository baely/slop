package store

import (
	"sync"

	"github.com/baely/listing/internal/model"
)

// ContainerStore provides thread-safe storage for containers
type ContainerStore struct {
	mu         sync.RWMutex
	containers map[string]model.Container
}

// New creates a new ContainerStore
func New() *ContainerStore {
	return &ContainerStore{
		containers: make(map[string]model.Container),
	}
}

// Add adds or updates a container in the store
func (s *ContainerStore) Add(c model.Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[c.ID] = c
}

// Remove removes a container from the store by ID
func (s *ContainerStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.containers, id)
}

// List returns a copy of all containers in the store
func (s *ContainerStore) List() []model.Container {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]model.Container, 0, len(s.containers))
	for _, c := range s.containers {
		result = append(result, c)
	}
	return result
}
