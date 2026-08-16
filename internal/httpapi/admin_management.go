package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) adminAPIKeys(w http.ResponseWriter, r *http.Request) {
	pageText := r.URL.Query().Get("page")
	if pageText != "" {
		page, err := strconv.Atoi(pageText)
		if err != nil || page < 1 {
			writeError(w, 400, "invalid_page", "page must be a positive integer")
			return
		}
		pageSize := 10
		if value := r.URL.Query().Get("page_size"); value != "" {
			pageSize, err = strconv.Atoi(value)
			if err != nil || pageSize < 1 || pageSize > 100 {
				writeError(w, 400, "invalid_page_size", "page_size must be between 1 and 100")
				return
			}
		}
		keys, total, err := s.Store.ListAPIKeysPage(r.Context(), page, pageSize, r.URL.Query().Get("search"))
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"data": keys, "total": total, "page": page, "page_size": pageSize})
		return
	}
	keys, err := s.Store.ListAPIKeys(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, keys)
}

func (s *Server) adminSetAPIKeyEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid API key id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if err := s.Store.SetAPIKeyEnabled(r.Context(), id, req.Enabled); err != nil {
		writeError(w, 404, "not_found", "API key not found")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "api_key.enabled", id.String(), map[string]any{"enabled": req.Enabled})
	writeJSON(w, 200, map[string]any{"id": id, "enabled": req.Enabled})
}

func (s *Server) adminDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid API key id")
		return
	}
	if err := s.Store.DeleteAPIKey(r.Context(), id); err != nil {
		writeError(w, 404, "not_found", "API key not found")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "api_key.delete", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	pageText := r.URL.Query().Get("page")
	pageSizeText := r.URL.Query().Get("page_size")
	filterRequested := r.URL.Query().Get("search") != "" || r.URL.Query().Get("action") != "" ||
		r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != ""
	if pageText == "" && pageSizeText == "" && !filterRequested {
		logs, err := s.Store.ListAuditLogs(r.Context(), 200)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, logs)
		return
	}
	page := 1
	if pageText != "" {
		parsed, err := strconv.Atoi(pageText)
		if err != nil || parsed < 1 {
			writeError(w, 400, "invalid_page", "page must be a positive integer")
			return
		}
		page = parsed
	}
	pageSize := 20
	if pageSizeText != "" {
		parsed, err := strconv.Atoi(pageSizeText)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, 400, "invalid_page_size", "page_size must be between 1 and 100")
			return
		}
		pageSize = parsed
	}
	createdFrom, createdTo, err := parseAdminTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range", err.Error())
		return
	}
	logs, total, err := s.Store.ListAuditLogsPageFiltered(r.Context(), page, pageSize, store.AuditLogPageFilter{
		Search: r.URL.Query().Get("search"), Action: r.URL.Query().Get("action"),
		CreatedFrom: createdFrom, CreatedTo: createdTo,
	})
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	actions, err := s.Store.ListAuditActions(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize, "actions": actions})
}

func (s *Server) adminRequestLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := parsePage(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"), 20, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	status := 0
	if value := r.URL.Query().Get("status"); value != "" {
		status, err = strconv.Atoi(value)
		if err != nil || status < 100 || status > 599 {
			writeError(w, http.StatusBadRequest, "invalid_status", "status must be an HTTP status between 100 and 599")
			return
		}
	}
	method := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("method")))
	if method != "" && method != http.MethodGet && method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete && method != http.MethodHead && method != http.MethodOptions {
		writeError(w, http.StatusBadRequest, "invalid_method", "method is not a supported HTTP method")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && kind != "image" && kind != "video" && kind != "audio" && kind != "chat" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be image, video, audio or chat")
		return
	}
	providerID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if providerID != "" {
		if _, err := s.configuredProvider(r.Context(), providerID, ""); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_provider", "provider is not configured")
			return
		}
	}
	createdFrom, createdTo, err := parseAdminTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range", err.Error())
		return
	}
	logs, total, err := s.Store.ListAPIRequestLogsPageFiltered(r.Context(), page, pageSize, store.APIRequestLogPageFilter{
		Search: r.URL.Query().Get("search"), ProviderID: providerID, Status: status, Method: method, Kind: kind,
		Path: r.URL.Query().Get("path"), Model: r.URL.Query().Get("model"), APIKey: r.URL.Query().Get("api_key"),
		ClientIP: r.URL.Query().Get("ip"), CreatedFrom: createdFrom, CreatedTo: createdTo,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

func parseAdminTimeRange(fromText, toText string) (*time.Time, *time.Time, error) {
	parse := func(name, value string) (*time.Time, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("%s must be an RFC3339 timestamp", name)
		}
		return &parsed, nil
	}
	from, err := parse("from", fromText)
	if err != nil {
		return nil, nil, err
	}
	to, err := parse("to", toText)
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, errors.New("from must be earlier than to")
	}
	return from, to, nil
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"schema_version": s.Store.GetSettingString(r.Context(), "schema_version", s.Config.SchemaVersion)})
}

func (s *Server) adminSystemCapacity(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.Store.GetSystemCapacity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"capacity": snapshot,
		"runtime": map[string]any{
			"task_dispatcher_concurrency": s.Config.TaskDispatcherConcurrency,
			"database_max_connections":    s.Config.DatabaseMaxConns,
			"api_key_queue_multiplier":    s.Config.APIKeyQueueMultiplier,
			"gateway_inflight":            len(s.generationSlots),
			"gateway_inflight_limit":      cap(s.generationSlots),
			"multipart_inflight":          len(s.multipartSlots),
			"multipart_inflight_limit":    cap(s.multipartSlots),
			"sync_inflight":               len(s.syncSlots),
			"sync_inflight_limit":         cap(s.syncSlots),
		},
	})
}

func (s *Server) adminUpdateSystemCapacity(w http.ResponseWriter, r *http.Request) {
	var req store.SystemCapacityConfig
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	before, err := s.Store.GetSystemCapacity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	after, err := s.Store.UpdateSystemCapacity(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_system_capacity", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "settings.system_capacity", "system_capacity", map[string]any{
		"before": before.SystemCapacityConfig,
		"after":  after.SystemCapacityConfig,
	})
	writeJSON(w, http.StatusOK, map[string]any{"capacity": after})
}

func (s *Server) adminSetSchemaVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.SchemaVersion = strings.TrimSpace(req.SchemaVersion)
	if req.SchemaVersion == "" || len(req.SchemaVersion) > 32 {
		writeError(w, 400, "invalid_schema_version", "schema_version is required")
		return
	}
	if err := s.Store.SetSetting(r.Context(), "schema_version", req.SchemaVersion); err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "settings.schema_version", "schema_version", map[string]any{"value": req.SchemaVersion})
	writeJSON(w, 200, map[string]any{"schema_version": req.SchemaVersion})
}

func (s *Server) adminCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		ConcurrencyLimit int      `json:"concurrency_limit"`
		ExpiresInDays    int      `json:"expires_in_days"`
		AllowedModels    []string `json:"allowed_models"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		req.Name = "default"
	}
	if len(req.Name) > 80 || len(req.Description) > 240 {
		writeError(w, 400, "invalid_request", "name must not exceed 80 characters and description must not exceed 240 characters")
		return
	}
	if req.ConcurrencyLimit < 1 {
		req.ConcurrencyLimit = 20
	}
	if req.ConcurrencyLimit > 1000 || req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		writeError(w, 400, "invalid_request", "concurrency_limit must be 1..1000 and expires_in_days must be 0..3650")
		return
	}
	allowed, err := s.supportedAPIKeyModels(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", "provider model permissions are temporarily unavailable")
		return
	}
	if len(req.AllowedModels) == 0 {
		for model := range allowed {
			req.AllowedModels = append(req.AllowedModels, model)
		}
	}
	seen := make(map[string]bool, len(req.AllowedModels))
	models := make([]string, 0, len(req.AllowedModels))
	for _, model := range req.AllowedModels {
		if !allowed[model] {
			writeError(w, 400, "invalid_request", "allowed_models contains an unknown model")
			return
		}
		if !seen[model] {
			seen[model] = true
			models = append(models, model)
		}
	}
	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		value := time.Now().UTC().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &value
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "key_generation_failed", "failed to generate API key")
		return
	}
	raw := "leo_" + token
	id, err := s.Store.CreateAPIKey(r.Context(), req.Name, req.Description, raw, raw[:12], req.ConcurrencyLimit, models, expiresAt)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "api_key.create", id.String(), map[string]any{"name": req.Name, "concurrency_limit": req.ConcurrencyLimit, "expires_in_days": req.ExpiresInDays, "allowed_models": models})
	writeJSON(w, 201, map[string]any{"id": id, "key": raw, "warning": "This key is shown once."})
}

func (s *Server) supportedAPIKeyModels(ctx context.Context) (map[string]bool, error) {
	configured, err := s.Store.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, provider := range configured {
		if !provider.Enabled {
			continue
		}
		if _, adapterErr := s.providerRegistry().Get(provider.ID); adapterErr != nil {
			continue
		}
		models, listErr := s.Store.ListProviderModelConfigs(ctx, provider.ID)
		if listErr != nil {
			return nil, listErr
		}
		for _, model := range models {
			allowed[mediaModelPermission(provider.ID, model.Model.ID)] = true
		}
	}
	return allowed, nil
}
