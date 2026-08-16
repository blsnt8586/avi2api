package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) adminCostRules(w http.ResponseWriter, r *http.Request) {
	providerID, kind, ok := s.adminPricingFilter(w, r)
	if !ok {
		return
	}
	rules, err := s.Store.ListModelCostRulesFiltered(r.Context(), providerID, kind)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, rules)
}

func (s *Server) publicCostRules(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" && kind != "image" && kind != "video" && kind != "audio" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be image, video or audio")
		return
	}
	if providerID != "" {
		if _, err := s.businessProvider(r.Context(), providerID, "", kind); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured for the selected media type")
			return
		}
	}
	rules, err := s.Store.ListModelCostRulesFiltered(r.Context(), providerID, kind)
	if err != nil {
		writeError(w, 500, "database_error", "pricing data is temporarily unavailable")
		return
	}
	enabled := make([]domain.ModelCostRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, 200, enabled)
}

func validateCostRule(rule *domain.ModelCostRule) error {
	rule.ProviderID = strings.ToLower(strings.TrimSpace(rule.ProviderID))
	if rule.ProviderID == "" {
		rule.ProviderID = providers.Leonardo
	}
	rule.Kind = strings.ToLower(strings.TrimSpace(rule.Kind))
	rule.Model = strings.TrimSpace(rule.Model)
	rule.Size = strings.ToLower(strings.TrimSpace(rule.Size))
	rule.Quality = strings.ToLower(strings.TrimSpace(rule.Quality))
	rule.Resolution = strings.ToLower(strings.TrimSpace(rule.Resolution))
	rule.PriceVersion = strings.TrimSpace(rule.PriceVersion)
	if rule.Source == "" {
		rule.Source = "manual"
	}
	if (rule.Kind != "image" && rule.Kind != "video" && rule.Kind != "audio") || rule.Model == "" || rule.PriceVersion == "" || rule.UnitTokens < 0 || rule.Duration < 0 {
		return errors.New("kind, model, non-negative unit_tokens and price_version are required")
	}
	if rule.Kind == "image" && (rule.Resolution != "" || rule.Duration != 0) {
		return errors.New("image rules cannot set resolution or duration")
	}
	if rule.Kind == "video" && (rule.Size != "" || rule.Resolution == "" || rule.Duration == 0) {
		return errors.New("video rules require resolution and duration and cannot set size")
	}
	if rule.Kind == "video" && rule.Quality != "" {
		if rule.ProviderID != providers.Adobe || rule.Model != adobe.AdobeKling30Omni ||
			(rule.Quality != adobe.AdobeKlingWorkflowT2V && rule.Quality != adobe.AdobeKlingWorkflowI2V && rule.Quality != adobe.AdobeKlingWorkflowRTV) {
			return errors.New("video quality is reserved for Adobe Kling workflow rules: t2v, i2v or rtv")
		}
	}
	if rule.Kind == "audio" && (rule.Size != "" || rule.Quality != "" || rule.Resolution != "") {
		return errors.New("audio rules cannot set size, quality or resolution")
	}
	return nil
}

func (s *Server) adminCreateCostRule(w http.ResponseWriter, r *http.Request) {
	var rule domain.ModelCostRule
	if err := decodeJSON(r, &rule); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if err := validateCostRule(&rule); err != nil {
		writeError(w, 400, "invalid_cost_rule", err.Error())
		return
	}
	if _, err := s.businessProvider(r.Context(), rule.ProviderID, providers.Leonardo, rule.Kind); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured for this media type")
		return
	}
	created, err := s.Store.CreateModelCostRule(r.Context(), rule)
	if err != nil {
		writeError(w, 409, "cost_rule_conflict", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "cost_rule.create", strconv.FormatInt(created.ID, 10), created)
	writeJSON(w, 201, created)
}

func (s *Server) adminUpdateCostRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid cost rule id")
		return
	}
	var rule domain.ModelCostRule
	if err := decodeJSON(r, &rule); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	rule.ID = id
	if err := validateCostRule(&rule); err != nil {
		writeError(w, 400, "invalid_cost_rule", err.Error())
		return
	}
	if _, err := s.businessProvider(r.Context(), rule.ProviderID, providers.Leonardo, rule.Kind); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured for this media type")
		return
	}
	updated, err := s.Store.UpdateModelCostRule(r.Context(), rule)
	if err != nil {
		writeError(w, 409, "cost_rule_update_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "cost_rule.update", strconv.FormatInt(updated.ID, 10), map[string]any{"replaces_rule_id": id, "rule": updated})
	writeJSON(w, 200, updated)
}

func (s *Server) adminDeleteCostRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid cost rule id")
		return
	}
	if err := s.Store.DeleteModelCostRule(r.Context(), id); err != nil {
		writeError(w, 409, "cost_rule_in_use", "cost rule does not exist or is referenced by task history")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "cost_rule.delete", strconv.FormatInt(id, 10), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminModelCosts(w http.ResponseWriter, r *http.Request) {
	providerID, kind, ok := s.adminPricingFilter(w, r)
	if !ok {
		return
	}
	costs, err := s.Store.ListModelCostsFiltered(r.Context(), providerID, kind, 500)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, costs)
}

func (s *Server) adminModels(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.ListModels(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) adminPlatformModels(w http.ResponseWriter, r *http.Request) {
	s.adminPlatformModelsByType(w, r, "image")
}

func (s *Server) adminPlatformVideoModels(w http.ResponseWriter, r *http.Request) {
	s.adminPlatformModelsByType(w, r, "video")
}

func (s *Server) adminPlatformAudioModels(w http.ResponseWriter, r *http.Request) {
	s.adminPlatformModelsByType(w, r, "audio")
}

func (s *Server) adminPlatformModelsByType(w http.ResponseWriter, r *http.Request, mediaType string) {
	provider, err := s.businessProvider(r.Context(), r.URL.Query().Get("provider"), providers.Leonardo, mediaType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured for this media type")
		return
	}
	if !provider.CatalogSync {
		platform, configuredErr := s.configuredProviderPlatformModels(r.Context(), provider, mediaType)
		if configuredErr != nil {
			writeError(w, 500, "database_error", configuredErr.Error())
			return
		}
		s.writePlatformModels(w, r, provider, mediaType, "adapter-config", nil, "configured_models", platform)
		return
	}
	rawModels, schemaVersion, syncedAt, err := s.Store.ListProviderPlatformModels(r.Context(), provider.ID, mediaType)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	platform := make([]leonardo.PlatformImageModel, 0, len(rawModels))
	for _, raw := range rawModels {
		var model leonardo.PlatformImageModel
		if err := json.Unmarshal(raw, &model); err != nil {
			writeError(w, 500, "catalog_error", err.Error())
			return
		}
		platform = append(platform, model)
	}
	s.writePlatformModels(w, r, provider, mediaType, schemaVersion, syncedAt, "upstream_schema", platform)
}

func (s *Server) adminSyncPlatformModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderID string `json:"provider_id"`
		MediaType  string `json:"media_type"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.MediaType != "image" && req.MediaType != "video" && req.MediaType != "audio" {
		writeError(w, 400, "invalid_media_type", "media_type must be image, video or audio")
		return
	}
	provider, err := s.businessProvider(r.Context(), req.ProviderID, providers.Leonardo, req.MediaType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured for this media type")
		return
	}
	if !provider.CatalogSync {
		writeError(w, http.StatusUnprocessableEntity, "catalog_sync_unsupported", "selected provider uses its configured adapter catalog")
		return
	}
	platform, schemaVersion, err := s.fetchPlatformModels(r.Context(), provider.ID, req.MediaType)
	if err != nil {
		writeError(w, 502, "platform_models_error", err.Error())
		return
	}
	rawModels := make([]json.RawMessage, 0, len(platform))
	for _, model := range platform {
		raw, err := json.Marshal(model)
		if err != nil {
			writeError(w, 500, "catalog_error", err.Error())
			return
		}
		rawModels = append(rawModels, raw)
	}
	syncedAt, err := s.Store.ReplaceProviderPlatformModels(r.Context(), provider.ID, req.MediaType, schemaVersion, rawModels)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "platform_models.sync", provider.ID+":"+req.MediaType, map[string]any{"provider_id": provider.ID, "count": len(platform), "schema_version": schemaVersion})
	s.writePlatformModels(w, r, provider, req.MediaType, schemaVersion, &syncedAt, "upstream_schema", platform)
}

func (s *Server) fetchPlatformModels(ctx context.Context, providerID, mediaType string) ([]leonardo.PlatformImageModel, string, error) {
	if providerID != providers.Leonardo {
		return nil, "", providers.ErrUnsupported
	}
	accountsList, err := s.Store.ListAccounts(ctx)
	if err != nil {
		return nil, "", err
	}
	var account *domain.Account
	for i := range accountsList {
		if accountsList[i].ProviderID == providerID && accountsList[i].Status == "active" {
			account = &accountsList[i]
			break
		}
	}
	if account == nil {
		return nil, "", errors.New("no active Leonardo account is available")
	}
	active, token, err := s.Accounts.Token(ctx, *account)
	if err != nil {
		return nil, "", err
	}
	schemaVersion := s.Store.GetSettingString(ctx, "schema_version", s.Config.SchemaVersion)
	client, err := leonardo.New(active.ProxyURL, active.UserAgent, schemaVersion)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "video" {
		models, err := client.ListPlatformVideoModels(ctx, token, active.TeamID)
		return models, schemaVersion, err
	}
	if mediaType == "audio" {
		models, err := client.ListPlatformAudioModels(ctx, token, active.TeamID)
		return models, schemaVersion, err
	}
	models, err := client.ListPlatformImageModels(ctx, token, active.TeamID)
	return models, schemaVersion, err
}

func (s *Server) writePlatformModels(w http.ResponseWriter, r *http.Request, provider domain.Provider, mediaType, schemaVersion string, syncedAt *time.Time, source string, platform []leonardo.PlatformImageModel) {
	curated, err := s.Store.ListProviderModelConfigs(r.Context(), provider.ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	exposed := make(map[string]string, len(curated))
	for _, model := range curated {
		exposed[model.UpstreamModel] = model.Model.ID
	}
	data := make([]map[string]any, 0, len(platform))
	for _, model := range platform {
		publicID, selected := exposed[model.ID]
		data = append(data, map[string]any{"platform": model, "exposed": selected, "public_id": publicID})
	}
	writeJSON(w, 200, map[string]any{
		"provider_id": provider.ID, "provider": newProviderBusinessView(provider, true),
		"schema_version": schemaVersion, "media_type": mediaType, "synced_at": syncedAt,
		"catalog_source": source, "sync_supported": provider.CatalogSync, "data": data,
	})
}

func (s *Server) adminPricingFilter(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	providerID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" && kind != "image" && kind != "video" && kind != "audio" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be image, video or audio")
		return "", "", false
	}
	if providerID != "" {
		if _, err := s.configuredProvider(r.Context(), providerID, ""); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured")
			return "", "", false
		}
	}
	return providerID, kind, true
}

func (s *Server) configuredProviderPlatformModels(ctx context.Context, provider domain.Provider, mediaType string) ([]leonardo.PlatformImageModel, error) {
	configured, err := s.Store.ListProviderModelConfigs(ctx, provider.ID)
	if err != nil {
		return nil, err
	}
	result := make([]leonardo.PlatformImageModel, 0, len(configured))
	for order, item := range configured {
		if !modelSupportsMedia(item.Model.Capabilities, mediaType) {
			continue
		}
		capabilities := make(map[string]bool, len(item.Model.Capabilities))
		for _, capability := range item.Model.Capabilities {
			capabilities[capability] = true
		}
		displayName := item.Model.DisplayName
		if spec, found := adobe.Model(item.Model.ID); provider.ID == providers.Adobe && found && spec.DisplayName != "" {
			displayName = spec.DisplayName
		}
		model := leonardo.PlatformImageModel{
			ID: item.UpstreamModel, Type: mediaType, ModelID: item.Model.ID,
			Name: displayName, Provider: provider.DisplayName,
			Description: "通过 " + provider.DisplayName + " 适配器配置的对外模型。",
			Order:       order, ProductionAPI: true, Capabilities: capabilities, CostType: "provider_pricing",
		}
		var defaults struct {
			Quality    string `json:"quality"`
			Quantity   int    `json:"quantity"`
			Duration   int    `json:"duration"`
			Resolution string `json:"resolution"`
		}
		_ = json.Unmarshal(item.Model.Defaults, &defaults)
		model.DefaultQuality = defaults.Quality
		model.DefaultQuantity = defaults.Quantity
		model.DefaultDuration = defaults.Duration
		if defaults.Resolution != "" {
			model.ResolutionModes = []string{defaults.Resolution}
		}
		if spec, found := adobe.Model(item.Model.ID); found {
			model.QualityOptions = append([]string(nil), spec.Qualities...)
			model.DurationOptions = append([]int(nil), spec.Durations...)
			model.ResolutionModes = append([]string(nil), spec.Resolutions...)
			if mediaType == "image" {
				model.MaximumQuantity = 1
			}
		}
		result = append(result, model)
	}
	return result, nil
}

func modelSupportsMedia(capabilities []string, mediaType string) bool {
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		switch mediaType {
		case "image":
			if capability == "image" || capability == "image-generation" || strings.HasSuffix(capability, "-to-image") {
				return true
			}
		case "video":
			if capability == "video" || capability == "video-generation" || strings.HasSuffix(capability, "-to-video") {
				return true
			}
		case "audio":
			if capability == "audio" || capability == "audio-generation" || strings.HasSuffix(capability, "-to-speech") || strings.HasSuffix(capability, "-to-music") || strings.HasSuffix(capability, "-to-sound") || strings.HasSuffix(capability, "-to-sound-effect") {
				return true
			}
		}
	}
	return false
}
