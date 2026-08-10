package providers

import (
	"errors"
	"sort"
	"sync"
)

const Leonardo = "leonardo"

var ErrUnsupported = errors.New("provider adapter is not registered")

type Descriptor struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	AuthType     string   `json:"auth_type"`
	Capabilities []string `json:"capabilities"`
}

// Registry is the composition root for provider-specific authentication and
// generation adapters. A task is pinned to one descriptor before reservation.
type Registry struct {
	mu          sync.RWMutex
	descriptors map[string]Descriptor
}

func NewRegistry() *Registry {
	r := &Registry{descriptors: make(map[string]Descriptor)}
	r.Register(Descriptor{ID: Leonardo, DisplayName: "Leonardo AI", AuthType: "browser_session", Capabilities: []string{"image", "video", "audio"}})
	return r
}

func (r *Registry) Register(descriptor Descriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.descriptors[descriptor.ID] = descriptor
}

func (r *Registry) Get(id string) (Descriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.descriptors[id]
	if !ok {
		return Descriptor{}, ErrUnsupported
	}
	return descriptor, nil
}

func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		out = append(out, descriptor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
