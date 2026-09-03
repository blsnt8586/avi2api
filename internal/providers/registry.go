package providers

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/leonardo2api/leonardo2api/internal/adobe"
)

const (
	Leonardo        = "leonardo"
	Adobe           = "adobe"
	CreativeFabrica = "creativefabrica"
)

var ErrUnsupported = errors.New("provider adapter is not registered")

type Descriptor struct {
	ID           string                                   `json:"id"`
	DisplayName  string                                   `json:"display_name"`
	AuthType     string                                   `json:"auth_type"`
	Capabilities []string                                 `json:"capabilities"`
	ResolveModel func(kind, model string) (string, error) `json:"-"`
}

// Registry is the composition root for provider-specific authentication and
// generation adapters. A task is pinned to one descriptor before reservation.
type Registry struct {
	mu          sync.RWMutex
	descriptors map[string]Descriptor
}

func NewRegistry() *Registry {
	r := &Registry{descriptors: make(map[string]Descriptor)}
	r.Register(Descriptor{
		ID: Leonardo, DisplayName: "Leonardo AI", AuthType: "browser_session", Capabilities: []string{"image", "video", "audio"},
		ResolveModel: func(_ string, model string) (string, error) { return model, nil },
	})
	r.Register(Descriptor{
		ID: Adobe, DisplayName: "Adobe Firefly", AuthType: "oauth", Capabilities: []string{"image", "video"},
		ResolveModel: func(kind, model string) (string, error) {
			spec, ok := adobe.Model(model)
			if !ok || spec.Kind != kind {
				return "", fmt.Errorf("selected model is not available from provider %s", Adobe)
			}
			return spec.PublicID, nil
		},
	})
	r.Register(Descriptor{
		ID: CreativeFabrica, DisplayName: "Creative Fabrica Studio", AuthType: "cookie", Capabilities: []string{"image", "video"},
		// Creative Fabrica publishes account-scoped model IDs. The database
		// catalog is authoritative, so the registry only normalizes the value.
		ResolveModel: func(kind, model string) (string, error) {
			if kind != "image" && kind != "video" {
				return "", fmt.Errorf("provider %s does not support %s generation", CreativeFabrica, kind)
			}
			if strings.TrimSpace(model) == "" {
				return "", errors.New("model is required")
			}
			return strings.TrimSpace(model), nil
		},
	})
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

func (r *Registry) ResolveModel(kind, providerID, model string) (string, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	model = strings.TrimSpace(model)
	if providerID == "" {
		providerID = Leonardo
	}
	if model == "" {
		return "", errors.New("model is required")
	}
	descriptor, err := r.Get(providerID)
	if err != nil {
		return "", err
	}
	if !descriptorSupports(descriptor, kind) {
		return "", fmt.Errorf("provider %s does not support %s generation", providerID, kind)
	}
	if descriptor.ResolveModel == nil {
		return "", ErrUnsupported
	}
	return descriptor.ResolveModel(kind, model)
}

func descriptorSupports(descriptor Descriptor, capability string) bool {
	for _, current := range descriptor.Capabilities {
		if current == capability {
			return true
		}
	}
	return false
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
