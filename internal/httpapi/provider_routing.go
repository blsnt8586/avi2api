package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type mediaModelRoute struct {
	Provider      string
	PublicModel   string
	InternalModel string
}

func mediaModelPermission(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.TrimSpace(model)
}

func allowedMediaModel(models []string, route mediaModelRoute) bool {
	return allowed(models, mediaModelPermission(route.Provider, route.PublicModel))
}

func (s *Server) resolveMediaModel(kind, provider, model string) (mediaModelRoute, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	if provider == "" {
		provider = providers.Leonardo
	}
	if strings.HasPrefix(model, "adobe-") || strings.HasPrefix(model, "adobe:") {
		return mediaModelRoute{}, errors.New("model must use its canonical ID; select Adobe with provider=adobe")
	}
	registry := s.Providers
	if registry == nil {
		registry = providers.NewRegistry()
	}
	canonical, err := registry.ResolveModel(kind, provider, model)
	if err != nil {
		if errors.Is(err, providers.ErrUnsupported) {
			return mediaModelRoute{}, errors.New("provider adapter is not registered")
		}
		return mediaModelRoute{}, err
	}
	return mediaModelRoute{Provider: provider, PublicModel: canonical, InternalModel: canonical}, nil
}

type providerModelStore struct {
	store      *store.Store
	providerID string
}

func (rules providerModelStore) GetModel(ctx context.Context, model string) (domain.ModelConfig, error) {
	return rules.store.GetModel(ctx, model)
}

func (rules providerModelStore) FindModelCostRule(ctx context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	return rules.store.FindProviderModelCostRule(ctx, rules.providerID, kind, model, size, quality, resolution, duration)
}

func (s *Server) providerModelContext(ctx context.Context, providerID, model string) (domain.ModelProviderConfig, providerModelStore, error) {
	config, err := s.Store.GetModelProviderConfig(ctx, providerID, model)
	return config, providerModelStore{store: s.Store, providerID: config.ProviderID}, err
}
