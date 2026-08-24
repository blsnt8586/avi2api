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

const publicModelSeparator = "/"

func publicMediaModelID(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + publicModelSeparator + strings.TrimSpace(model)
}

func parsePublicMediaModelID(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", errors.New("model is required and must use platform/model")
	}
	parts := strings.Split(value, publicModelSeparator)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("model must use platform/model, for example leonardo/gpt-image-2")
	}
	return strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1]), nil
}

func mediaModelPermission(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.TrimSpace(model)
}

func allowedMediaModel(models []string, route mediaModelRoute) bool {
	return allowed(models, mediaModelPermission(route.Provider, route.InternalModel))
}

func (s *Server) resolveMediaModel(kind, model string) (mediaModelRoute, error) {
	provider, internalModel, err := parsePublicMediaModelID(model)
	if err != nil {
		return mediaModelRoute{}, err
	}
	registry := s.Providers
	if registry == nil {
		registry = providers.NewRegistry()
	}
	canonical, err := registry.ResolveModel(kind, provider, internalModel)
	if err != nil {
		if errors.Is(err, providers.ErrUnsupported) {
			return mediaModelRoute{}, errors.New("provider adapter is not registered")
		}
		return mediaModelRoute{}, err
	}
	return mediaModelRoute{Provider: provider, PublicModel: publicMediaModelID(provider, canonical), InternalModel: canonical}, nil
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
