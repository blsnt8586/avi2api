package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type providerBusinessView struct {
	ID                string              `json:"id"`
	DisplayName       string              `json:"display_name"`
	Enabled           bool                `json:"enabled"`
	AdapterRegistered bool                `json:"adapter_registered"`
	AuthType          string              `json:"auth_type"`
	CreditUnit        string              `json:"credit_unit"`
	Capabilities      []string            `json:"capabilities"`
	CatalogSync       bool                `json:"catalog_sync"`
	Models            map[string][]string `json:"models"`
}

func newProviderBusinessView(provider domain.Provider, adapterRegistered bool) providerBusinessView {
	return providerBusinessView{
		ID: provider.ID, DisplayName: provider.DisplayName, Enabled: provider.Enabled, AdapterRegistered: adapterRegistered,
		AuthType: provider.AuthType, CreditUnit: provider.CreditUnit,
		Capabilities: provider.Capabilities, CatalogSync: provider.CatalogSync,
		Models: map[string][]string{"image": {}, "video": {}, "audio": {}},
	}
}

func (s *Server) providerRegistry() *providers.Registry {
	if s.Providers != nil {
		return s.Providers
	}
	return providers.NewRegistry()
}

func (s *Server) providerView(ctx context.Context, provider domain.Provider, adapterRegistered bool) (providerBusinessView, error) {
	view := newProviderBusinessView(provider, adapterRegistered)
	models, err := s.Store.ListProviderModelConfigs(ctx, provider.ID)
	if err != nil {
		return view, err
	}
	for _, model := range models {
		for _, kind := range []string{"image", "video", "audio"} {
			if modelSupportsMedia(model.Model.Capabilities, kind) {
				view.Models[kind] = append(view.Models[kind], model.Model.ID)
			}
		}
	}
	return view, nil
}

func (s *Server) providerViews(ctx context.Context, includeUnavailable bool) ([]providerBusinessView, error) {
	configured, err := s.Store.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	registry := s.providerRegistry()
	result := make([]providerBusinessView, 0, len(configured))
	for _, provider := range configured {
		_, adapterErr := registry.Get(provider.ID)
		adapterRegistered := adapterErr == nil
		if !includeUnavailable && (!provider.Enabled || !adapterRegistered) {
			continue
		}
		view, viewErr := s.providerView(ctx, provider, adapterRegistered)
		if viewErr != nil {
			return nil, viewErr
		}
		result = append(result, view)
	}
	return result, nil
}

func providerHasCapability(provider domain.Provider, capability string) bool {
	if capability == "" {
		return true
	}
	for _, current := range provider.Capabilities {
		if current == capability {
			return true
		}
	}
	return false
}

func (s *Server) configuredProvider(ctx context.Context, rawID, defaultID string) (domain.Provider, error) {
	id := strings.ToLower(strings.TrimSpace(rawID))
	if id == "" {
		id = defaultID
	}
	provider, err := s.Store.GetProvider(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return provider, providers.ErrUnsupported
	}
	if err != nil {
		return provider, err
	}
	return provider, nil
}

func (s *Server) businessProvider(ctx context.Context, rawID, defaultID, capability string) (domain.Provider, error) {
	provider, err := s.configuredProvider(ctx, rawID, defaultID)
	if err != nil {
		return provider, err
	}
	if !provider.Enabled || !providerHasCapability(provider, capability) {
		return provider, providers.ErrUnsupported
	}
	if _, err := s.providerRegistry().Get(provider.ID); err != nil {
		return provider, providers.ErrUnsupported
	}
	return provider, nil
}

func (s *Server) publicProviders(w http.ResponseWriter, r *http.Request) {
	result, err := s.providerViews(r.Context(), false)
	if err != nil {
		writeError(w, 500, "database_error", "provider catalog is temporarily unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, 200, result)
}

func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
	result, err := s.providerViews(r.Context(), true)
	if err != nil {
		writeError(w, 500, "database_error", "provider catalog is temporarily unavailable")
		return
	}
	writeJSON(w, 200, result)
}
