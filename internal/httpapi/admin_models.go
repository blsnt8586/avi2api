package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) adminCostRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListModelCostRules(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, rules)
}

func (s *Server) publicCostRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListModelCostRules(r.Context())
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
	if rule.Kind == "video" && (rule.Size != "" || rule.Quality != "" || rule.Resolution == "" || rule.Duration == 0) {
		return errors.New("video rules require resolution and duration and cannot set size or quality")
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
	costs, err := s.Store.ListModelCosts(r.Context(), 500)
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
	rawModels, schemaVersion, syncedAt, err := s.Store.ListPlatformModels(r.Context(), mediaType)
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
	s.writePlatformModels(w, r, mediaType, schemaVersion, syncedAt, platform)
}

func (s *Server) adminSyncPlatformModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaType string `json:"media_type"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.MediaType != "image" && req.MediaType != "video" && req.MediaType != "audio" {
		writeError(w, 400, "invalid_media_type", "media_type must be image, video or audio")
		return
	}
	platform, schemaVersion, err := s.fetchPlatformModels(r.Context(), req.MediaType)
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
	syncedAt, err := s.Store.ReplacePlatformModels(r.Context(), req.MediaType, schemaVersion, rawModels)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "platform_models.sync", req.MediaType, map[string]any{"count": len(platform), "schema_version": schemaVersion})
	s.writePlatformModels(w, r, req.MediaType, schemaVersion, &syncedAt, platform)
}

func (s *Server) fetchPlatformModels(ctx context.Context, mediaType string) ([]leonardo.PlatformImageModel, string, error) {
	accountsList, err := s.Store.ListAccounts(ctx)
	if err != nil {
		return nil, "", err
	}
	var account *domain.Account
	for i := range accountsList {
		if accountsList[i].Status == "active" {
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

func (s *Server) writePlatformModels(w http.ResponseWriter, r *http.Request, mediaType, schemaVersion string, syncedAt *time.Time, platform []leonardo.PlatformImageModel) {
	curated, err := s.Store.ListModels(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	exposed := make(map[string]string, len(curated))
	for _, model := range curated {
		exposed[model.UpstreamModel] = model.ID
	}
	data := make([]map[string]any, 0, len(platform))
	for _, model := range platform {
		publicID, selected := exposed[model.ID]
		data = append(data, map[string]any{"platform": model, "exposed": selected, "public_id": publicID})
	}
	writeJSON(w, 200, map[string]any{"schema_version": schemaVersion, "media_type": mediaType, "synced_at": syncedAt, "data": data})
}
