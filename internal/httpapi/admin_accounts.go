package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.Store.GetAccountOverview(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	providerOverviews, err := s.Store.GetProviderOverviews(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	taskOverview, err := s.Store.GetTaskOverview(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"accounts": overview.TotalAccounts, "active_accounts": overview.ActiveAccounts,
		"task_counts": taskOverview.Counts, "task_total": taskOverview.Total,
		"failed_last_hour":   taskOverview.FailedLastHour,
		"provider_summaries": providerOverviews,
	})
}

func (s *Server) adminAccounts(w http.ResponseWriter, r *http.Request) {
	pageText := r.URL.Query().Get("page")
	pageSizeText := r.URL.Query().Get("page_size")
	search := r.URL.Query().Get("search")
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	providerID := strings.TrimSpace(r.URL.Query().Get("provider"))
	if status != "" && status != "attention" && status != "active" && status != "rate_limited" && status != "cooldown" && status != "invalid" && status != "disabled" {
		writeError(w, 400, "invalid_filter", "status filter is invalid")
		return
	}
	if role != "" && role != "general" && role != "video_reserved" {
		writeError(w, 400, "invalid_filter", "role filter is invalid")
		return
	}
	if pageText != "" || pageSizeText != "" || search != "" || status != "" || role != "" || providerID != "" {
		page, pageSize, err := parsePage(pageText, pageSizeText, 20, 100)
		if err != nil {
			writeError(w, 400, "invalid_pagination", err.Error())
			return
		}
		result, err := s.Store.ListAccountsPageFiltered(r.Context(), page, pageSize, store.AccountPageFilter{
			Search: search, Status: status, Role: role, ProviderID: providerID,
		})
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, result)
		return
	}
	a, err := s.Store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, a)
}

type accountArchiveResult struct {
	ID               uuid.UUID `json:"id"`
	Archived         bool      `json:"archived"`
	ErrorCode        string    `json:"error_code,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	ActiveTasks      int       `json:"active_tasks,omitempty"`
	HeldReservations int       `json:"held_reservations,omitempty"`
}

func (s *Server) archiveAccount(ctx context.Context, id uuid.UUID) accountArchiveResult {
	result := accountArchiveResult{ID: id}
	err := s.Store.ArchiveAccount(ctx, id)
	if err == nil {
		result.Archived = true
		return result
	}
	if errors.Is(err, store.ErrNotFound) {
		result.ErrorCode = "not_found"
		result.ErrorMessage = "account not found"
		return result
	}
	var inUse *store.AccountInUseError
	if errors.As(err, &inUse) {
		result.ErrorCode = "account_in_use"
		result.ErrorMessage = "账号仍有未完成任务或积分预留，请等待任务结算后再归档"
		result.ActiveTasks = inUse.ActiveTasks
		result.HeldReservations = inUse.HeldReservations
		return result
	}
	result.ErrorCode = "database_error"
	result.ErrorMessage = err.Error()
	return result
}

func (s *Server) adminArchiveAccount(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	result := s.archiveAccount(r.Context(), id)
	if result.Archived {
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.archive", id.String(), nil)
		writeJSON(w, 200, result)
		return
	}
	if result.ErrorCode == "not_found" {
		writeError(w, 404, result.ErrorCode, result.ErrorMessage)
		return
	}
	if result.ErrorCode == "account_in_use" {
		writeErrorDetails(w, http.StatusConflict, result.ErrorCode, result.ErrorMessage, map[string]any{
			"active_tasks": result.ActiveTasks, "held_reservations": result.HeldReservations,
		})
		return
	}
	writeError(w, 500, result.ErrorCode, result.ErrorMessage)
}

func (s *Server) adminArchiveAccounts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if len(req.IDs) < 1 || len(req.IDs) > 100 {
		writeError(w, 400, "invalid_request", "ids must contain between 1 and 100 account ids")
		return
	}
	seen := make(map[uuid.UUID]struct{}, len(req.IDs))
	results := make([]accountArchiveResult, 0, len(req.IDs))
	archived := 0
	for _, id := range req.IDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result := s.archiveAccount(r.Context(), id)
		if result.Archived {
			archived++
			s.writeAudit(r.Context(), s.Config.AdminUsername, "account.archive", id.String(), map[string]any{"batch": true})
		}
		results = append(results, result)
	}
	writeJSON(w, 200, map[string]any{"archived": archived, "failed": len(results) - archived, "results": results})
}

func parsePage(pageText, pageSizeText string, defaultPageSize, maximumPageSize int) (int, int, error) {
	page := 1
	if pageText != "" {
		value, err := strconv.Atoi(pageText)
		if err != nil || value < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
		page = value
	}
	pageSize := defaultPageSize
	if pageSizeText != "" {
		value, err := strconv.Atoi(pageSizeText)
		if err != nil || value < 1 || value > maximumPageSize {
			return 0, 0, fmt.Errorf("page_size must be between 1 and %d", maximumPageSize)
		}
		pageSize = value
	}
	return page, pageSize, nil
}

func normalizeAccountConcurrency(providerID string, value int) (int, error) {
	maximum := 5
	if providerID == providers.Adobe {
		maximum = 100
	} else if providerID == providers.CreativeFabrica {
		maximum = 10
	}
	if value == 0 {
		return 5, nil
	}
	if value < 1 || value > maximum {
		return 0, fmt.Errorf("image_concurrency must be between 1 and %d for provider %s", maximum, providerID)
	}
	return value, nil
}

func normalizeAccountQueueCapacity(value int) (int, error) {
	if value == 0 {
		value = 40
	}
	if value < 1 || value > 1000 {
		return 0, errors.New("queue_capacity must be between 1 and 1000")
	}
	return value, nil
}

func normalizeAccountRouting(role string, protectedTokens int64, videoReservedSlots, concurrency int) (string, int64, int, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "general"
	}
	if role != "general" && role != "video_reserved" {
		return "", 0, 0, errors.New("routing_role must be general or video_reserved")
	}
	if protectedTokens < 0 {
		return "", 0, 0, errors.New("protected_tokens must be non-negative")
	}
	if videoReservedSlots < 0 || videoReservedSlots > concurrency {
		return "", 0, 0, errors.New("video_reserved_slots must be between 0 and image_concurrency")
	}
	return role, protectedTokens, videoReservedSlots, nil
}

func (s *Server) adminCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderID         string                   `json:"provider_id"`
		Name               string                   `json:"name"`
		Email              string                   `json:"email"`
		Password           string                   `json:"password"`
		OTP                string                   `json:"otp"`
		AccessToken        string                   `json:"access_token"`
		CookieJSON         json.RawMessage          `json:"cookie_json"`
		ProxyURL           string                   `json:"proxy_url"`
		ImageConcurrency   int                      `json:"image_concurrency"`
		QueueCapacity      int                      `json:"queue_capacity"`
		RoutingRole        string                   `json:"routing_role"`
		ProtectedTokens    int64                    `json:"protected_tokens"`
		VideoReservedSlots int                      `json:"video_reserved_slots"`
		BrowserWorkerGroup string                   `json:"browser_worker_group"`
		Session            *accounts.BrowserSession `json:"session"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.ProxyURL = strings.TrimSpace(req.ProxyURL)
	req.ProviderID = strings.ToLower(strings.TrimSpace(req.ProviderID))
	if req.ProviderID == "" {
		req.ProviderID = providers.Leonardo
	}
	registry := s.Providers
	if registry == nil {
		registry = providers.NewRegistry()
	}
	if _, registryErr := registry.Get(req.ProviderID); registryErr != nil {
		writeError(w, 422, "provider_unavailable", "provider authentication adapter is not registered")
		return
	}
	req.BrowserWorkerGroup = strings.TrimSpace(req.BrowserWorkerGroup)
	if req.BrowserWorkerGroup == "" {
		req.BrowserWorkerGroup = "default"
	}
	if !validBrowserWorkerGroup(req.BrowserWorkerGroup) {
		writeError(w, 400, "invalid_request", "browser_worker_group must use 1 to 100 letters, digits, dots, underscores or hyphens")
		return
	}
	if req.Name == "" {
		writeError(w, 400, "invalid_request", "name is required")
		return
	}
	if req.Password != "" && req.Email == "" {
		writeError(w, 400, "invalid_request", "email is required when password is configured")
		return
	}
	concurrency, err := normalizeAccountConcurrency(req.ProviderID, req.ImageConcurrency)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.ImageConcurrency = concurrency
	queueCapacity, err := normalizeAccountQueueCapacity(req.QueueCapacity)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.QueueCapacity = queueCapacity
	req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots, err = normalizeAccountRouting(req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots, req.ImageConcurrency)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.Session != nil {
		writeError(w, 400, "invalid_request", "new accounts must use cookie_json; browser session import is only for internal refresh")
		return
	}
	if req.ProviderID == providers.Leonardo && len(req.CookieJSON) == 0 {
		writeError(w, 400, "invalid_request", "cookie_json is required for Leonardo accounts")
		return
	}
	if req.ProviderID == providers.Adobe && strings.TrimSpace(req.AccessToken) == "" && len(req.CookieJSON) == 0 {
		writeError(w, 400, "invalid_request", "access_token or cookie_json is required for Adobe accounts")
		return
	}
	if req.ProviderID == providers.CreativeFabrica && len(req.CookieJSON) == 0 && (req.Email == "" || req.Password == "") {
		writeError(w, 400, "invalid_request", "Creative Fabrica accounts require cookie_json or email and password")
		return
	}
	var a domain.Account
	switch req.ProviderID {
	case providers.Adobe:
		if len(req.CookieJSON) > 0 {
			a, err = s.Accounts.CreateAdobeWithCookieJSON(r.Context(), req.Name, req.Email, req.CookieJSON, req.ProxyURL, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots)
		} else {
			a, err = s.Accounts.CreateAdobeWithAccessToken(r.Context(), req.Name, req.Email, req.AccessToken, req.ProxyURL, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots)
		}
	case providers.Leonardo:
		a, err = s.Accounts.CreateWithCompleteCookieJSON(r.Context(), req.Name, req.Email, req.Password, req.CookieJSON, req.ProxyURL, req.BrowserWorkerGroup, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots)
	case providers.CreativeFabrica:
		if len(req.CookieJSON) > 0 {
			a, err = s.Accounts.CreateCreativeFabricaWithCookieJSON(r.Context(), req.Name, req.Email, req.Password, req.CookieJSON, req.ProxyURL, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots)
		} else {
			a, err = s.Accounts.CreateCreativeFabricaWithCredentials(r.Context(), req.Name, req.Email, req.Password, req.OTP, req.ProxyURL, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots)
		}
	default:
		writeError(w, 422, "provider_unavailable", "provider authentication adapter is not registered")
		return
	}
	if err != nil {
		var otpRequired *accounts.CreativeFabricaOTPRequiredError
		if errors.As(err, &otpRequired) {
			writeErrorDetails(w, http.StatusPreconditionRequired, "otp_required", "Creative Fabrica sent a verification code to the account email", map[string]any{
				"provider_id":    providers.CreativeFabrica,
				"challenge_type": otpRequired.Challenge.Type,
				"message":        otpRequired.Challenge.Message,
			})
			return
		}
		writeError(w, 400, "account_invalid", err.Error())
		return
	}
	if req.ProviderID == providers.CreativeFabrica {
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.create", a.ID.String(), map[string]any{
			"name": a.Name, "provider_id": a.ProviderID, "image_concurrency": a.ImageConcurrency,
			"queue_capacity": a.QueueCapacity, "cookie_json_saved": a.HasCompleteCookieJSON,
			"automatic_login_configured": a.HasLoginCredentials,
		})
		writeJSON(w, http.StatusCreated, map[string]any{
			"account": a, "status": "active", "pricing_status": "requires_provider_sync",
		})
		return
	}
	if req.ProviderID == providers.Adobe {
		account, token, tokenErr := s.Accounts.AdobeToken(r.Context(), a)
		pricingRules := 0
		pricingError := ""
		if tokenErr == nil {
			pricingRules, err = s.Accounts.SyncAdobePricing(r.Context(), account, token)
		}
		if tokenErr != nil {
			pricingError = tokenErr.Error()
		} else if err != nil {
			pricingError = err.Error()
		}
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.create", a.ID.String(), map[string]any{"name": a.Name, "provider_id": a.ProviderID, "image_concurrency": a.ImageConcurrency, "queue_capacity": a.QueueCapacity, "pricing_rules_synced": pricingRules})
		writeJSON(w, http.StatusCreated, map[string]any{"account": a, "status": "active", "pricing_rules_synced": pricingRules, "pricing_sync_error": pricingError})
		return
	}
	job, err := s.Store.EnqueueBrowserSessionRefreshJob(r.Context(), a.ID, 1000)
	if err != nil {
		_ = s.Store.DeleteAccount(r.Context(), a.ID)
		writeError(w, 500, "session_refresh_enqueue_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.create", a.ID.String(), map[string]any{"name": a.Name, "provider_id": a.ProviderID, "image_concurrency": a.ImageConcurrency, "queue_capacity": a.QueueCapacity, "routing_role": a.RoutingRole, "protected_tokens": a.ProtectedTokens, "video_reserved_slots": a.VideoReservedSlots, "browser_worker_group": a.BrowserWorkerGroup, "automatic_login_configured": a.HasLoginCredentials})
	writeJSON(w, http.StatusAccepted, map[string]any{"account": a, "job": job, "status": "pending_browser_validation"})
}

func (s *Server) adminUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var req struct {
		Name               *string `json:"name"`
		Email              *string `json:"email"`
		Password           *string `json:"password"`
		ProxyURL           *string `json:"proxy_url"`
		ImageConcurrency   *int    `json:"image_concurrency"`
		QueueCapacity      *int    `json:"queue_capacity"`
		RoutingRole        *string `json:"routing_role"`
		ProtectedTokens    *int64  `json:"protected_tokens"`
		VideoReservedSlots *int    `json:"video_reserved_slots"`
		BrowserWorkerGroup *string `json:"browser_worker_group"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.Name == nil && req.Email == nil && req.Password == nil && req.ProxyURL == nil && req.ImageConcurrency == nil && req.QueueCapacity == nil && req.RoutingRole == nil && req.ProtectedTokens == nil && req.VideoReservedSlots == nil && req.BrowserWorkerGroup == nil {
		writeError(w, 400, "invalid_request", "at least one account field is required")
		return
	}
	if req.Name != nil {
		value := strings.TrimSpace(*req.Name)
		if value == "" || utf8.RuneCountInString(value) > 200 {
			writeError(w, 400, "invalid_request", "name must contain 1 to 200 characters")
			return
		}
		req.Name = &value
	}
	if req.Email != nil {
		value := strings.TrimSpace(*req.Email)
		req.Email = &value
	}
	if req.Password != nil && *req.Password != "" {
		credentialEmail := ""
		if req.Email != nil {
			credentialEmail = *req.Email
		} else if current, getErr := s.Store.GetAccount(r.Context(), id); getErr == nil {
			credentialEmail = current.Email
		}
		if strings.TrimSpace(credentialEmail) == "" {
			writeError(w, 400, "invalid_request", "email is required when password is configured")
			return
		}
	}
	if req.ProxyURL != nil {
		value := strings.TrimSpace(*req.ProxyURL)
		req.ProxyURL = &value
	}
	if req.QueueCapacity != nil && (*req.QueueCapacity < 1 || *req.QueueCapacity > 1000) {
		writeError(w, 400, "invalid_request", "queue_capacity must be between 1 and 1000")
		return
	}
	current, currentErr := s.Store.GetAccount(r.Context(), id)
	if currentErr != nil {
		if errors.Is(currentErr, store.ErrNotFound) {
			writeError(w, 404, "not_found", "account not found")
			return
		}
		writeError(w, 500, "database_error", currentErr.Error())
		return
	}
	if req.ImageConcurrency != nil {
		if _, concurrencyErr := normalizeAccountConcurrency(current.ProviderID, *req.ImageConcurrency); concurrencyErr != nil {
			writeError(w, 400, "invalid_request", concurrencyErr.Error())
			return
		}
	}
	role := current.RoutingRole
	protectedTokens := current.ProtectedTokens
	videoReservedSlots := current.VideoReservedSlots
	concurrencyForRouting := current.ImageConcurrency
	if req.RoutingRole != nil {
		role = strings.TrimSpace(*req.RoutingRole)
		req.RoutingRole = &role
	}
	if req.ProtectedTokens != nil {
		protectedTokens = *req.ProtectedTokens
	}
	if req.VideoReservedSlots != nil {
		videoReservedSlots = *req.VideoReservedSlots
	}
	if req.ImageConcurrency != nil {
		concurrencyForRouting = *req.ImageConcurrency
	}
	if _, _, _, routingErr := normalizeAccountRouting(role, protectedTokens, videoReservedSlots, concurrencyForRouting); routingErr != nil {
		writeError(w, 400, "invalid_request", routingErr.Error())
		return
	}
	if req.BrowserWorkerGroup != nil {
		value := strings.TrimSpace(*req.BrowserWorkerGroup)
		if !validBrowserWorkerGroup(value) {
			writeError(w, 400, "invalid_request", "browser_worker_group must use 1 to 100 letters, digits, dots, underscores or hyphens")
			return
		}
		req.BrowserWorkerGroup = &value
	}

	updated, err := s.Store.UpdateAccountConfig(r.Context(), id, store.AccountConfigPatch{
		Name: req.Name, Email: req.Email, ProxyURL: req.ProxyURL,
		ImageConcurrency: req.ImageConcurrency, QueueCapacity: req.QueueCapacity,
		RoutingRole: req.RoutingRole, ProtectedTokens: req.ProtectedTokens, VideoReservedSlots: req.VideoReservedSlots,
		BrowserWorkerGroup: req.BrowserWorkerGroup,
	})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	var capacityErr *store.AccountCapacityInUseError
	if errors.As(err, &capacityErr) {
		message := fmt.Sprintf("当前有 %d 个执行任务、%d 个排队任务；新并发不能低于执行数，新队列容量不能低于排队数", capacityErr.ExecutingTasks, capacityErr.QueuedTasks)
		writeErrorDetails(w, http.StatusConflict, "account_capacity_in_use", message, map[string]any{
			"executing_tasks":          capacityErr.ExecutingTasks,
			"queued_tasks":             capacityErr.QueuedTasks,
			"requested_concurrency":    capacityErr.RequestedConcurrency,
			"requested_queue_capacity": capacityErr.RequestedQueue,
		})
		return
	}
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if req.Password != nil {
		credentialEmail := updated.Email
		if current.ProviderID == providers.CreativeFabrica {
			if existing, credentialErr := s.Accounts.CreativeFabricaCredentials(r.Context(), id); credentialErr == nil && strings.TrimSpace(credentialEmail) == "" {
				credentialEmail = existing.Email
			}
			if err := s.Accounts.SetCreativeFabricaLoginCredential(r.Context(), id, credentialEmail, *req.Password); err != nil {
				writeError(w, 500, "credential_update_failed", err.Error())
				return
			}
		} else if err := s.Accounts.SetLoginCredential(r.Context(), id, credentialEmail, *req.Password); err != nil {
			writeError(w, 500, "credential_update_failed", err.Error())
			return
		}
	} else if req.Email != nil && updated.HasLoginCredentials {
		if current.ProviderID == providers.CreativeFabrica {
			credential, credentialErr := s.Accounts.CreativeFabricaCredentials(r.Context(), id)
			if credentialErr != nil {
				writeError(w, 500, "credential_update_failed", credentialErr.Error())
				return
			}
			if err := s.Accounts.SetCreativeFabricaLoginCredential(r.Context(), id, updated.Email, credential.Password); err != nil {
				writeError(w, 500, "credential_update_failed", err.Error())
				return
			}
		} else {
			credential, credentialErr := s.Accounts.LoginCredential(r.Context(), id)
			if credentialErr != nil {
				writeError(w, 500, "credential_update_failed", credentialErr.Error())
				return
			}
			if err := s.Accounts.SetLoginCredential(r.Context(), id, updated.Email, credential.Password); err != nil {
				writeError(w, 500, "credential_update_failed", err.Error())
				return
			}
		}
	}
	if req.Password != nil || (req.Email != nil && updated.HasLoginCredentials) {
		updated, err = s.Store.GetAccount(r.Context(), id)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.update", id.String(), map[string]any{
		"name":                       updated.Name,
		"email":                      updated.Email,
		"proxy_configured":           updated.ProxyURL != "",
		"image_concurrency":          updated.ImageConcurrency,
		"queue_capacity":             updated.QueueCapacity,
		"routing_role":               updated.RoutingRole,
		"protected_tokens":           updated.ProtectedTokens,
		"video_reserved_slots":       updated.VideoReservedSlots,
		"browser_worker_group":       updated.BrowserWorkerGroup,
		"automatic_login_configured": updated.HasLoginCredentials,
	})
	writeJSON(w, 200, updated)
}

func (s *Server) adminRefreshAccount(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	a, err := s.Store.GetAccount(r.Context(), id)
	if err == nil && a.CooldownUntil != nil && a.CooldownUntil.After(time.Now()) {
		seconds := int(time.Until(*a.CooldownUntil).Round(time.Second) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests, "account_cooldown", "account is cooling down after an upstream rate limit; retry after the indicated interval")
		return
	}
	if err == nil {
		a, err = s.Accounts.RefreshAccount(r.Context(), id, true)
	}
	if err != nil {
		writeError(w, 400, "refresh_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.refresh", id.String(), nil)
	writeJSON(w, 200, a)
}

func (s *Server) adminCheckGenerationPermission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid account id")
		return
	}
	account, err := s.Accounts.CheckGenerationPermission(r.Context(), id, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	if err != nil && accounts.IsGenerationPermissionBlocked(err) {
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.generation_permission_blocked", id.String(), nil)
		writeJSON(w, http.StatusOK, account)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "generation_permission_check_failed", accounts.SanitizedUpstreamError(err))
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.generation_permission_verified", id.String(), map[string]any{"model": account.GenerationPermissionModel})
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) adminSyncAccountPricing(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid account id")
		return
	}
	account, err := s.Store.GetAccount(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	// Creative Fabrica pricing only needs a valid RPC token. Its current
	// balance endpoint may return an empty 200 envelope, so avoid coupling a
	// pricing sync to a balance refresh; CreativeFabricaToken refreshes only
	// when the stored token is actually expired.
	if account.ProviderID != providers.CreativeFabrica {
		account, err = s.Accounts.RefreshAccount(r.Context(), id, false)
		if err != nil {
			writeError(w, http.StatusBadRequest, "refresh_failed", err.Error())
			return
		}
	}
	var rules int
	var source string
	switch account.ProviderID {
	case providers.Adobe:
		account, token, tokenErr := s.Accounts.AdobeToken(r.Context(), account)
		if tokenErr != nil {
			writeError(w, http.StatusBadRequest, "token_unavailable", tokenErr.Error())
			return
		}
		rules, err = s.Accounts.SyncAdobePricing(r.Context(), account, token)
		source = "adobe-bks"
	case providers.CreativeFabrica:
		account, token, tokenErr := s.Accounts.CreativeFabricaToken(r.Context(), account)
		if tokenErr != nil {
			writeError(w, http.StatusBadRequest, "token_unavailable", tokenErr.Error())
			return
		}
		rules, err = s.Accounts.SyncCreativeFabricaPricing(r.Context(), account, token)
		source = "creativefabrica-preset"
	default:
		writeError(w, http.StatusUnprocessableEntity, "provider_mismatch", "该平台暂未提供可同步的动态价格适配器")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "pricing_sync_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.pricing_sync", id.String(), map[string]any{
		"provider_id":          account.ProviderID,
		"pricing_source":       source,
		"pricing_rules_synced": rules,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"account":              account,
		"provider_id":          account.ProviderID,
		"pricing_source":       source,
		"pricing_rules_synced": rules,
	})
}

// Keep the old method name for callers compiled against the pre-provider
// handler; the route now dispatches through the provider-aware implementation.
func (s *Server) adminSyncAdobeAccountPricing(w http.ResponseWriter, r *http.Request) {
	s.adminSyncAccountPricing(w, r)
}

func (s *Server) adminImportAccountSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var session accounts.BrowserSession
	if err := decodeJSON(r, &session); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	a, err := s.Accounts.ImportBrowserSession(r.Context(), id, session)
	if err != nil {
		writeError(w, 400, "session_import_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.session_import", id.String(), nil)
	writeJSON(w, 200, a)
}

func (s *Server) adminImportAccountCookieJSON(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var req struct {
		CookieJSON json.RawMessage `json:"cookie_json"`
	}
	if err := decodeJSONLimit(r, &req, 768<<10); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	current, err := s.Store.GetAccount(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if current.ProviderID == providers.Adobe {
		account, importErr := s.Accounts.ImportAdobeCookieJSON(r.Context(), id, req.CookieJSON)
		if importErr != nil {
			writeError(w, 400, "cookie_import_failed", importErr.Error())
			return
		}
		_, token, tokenErr := s.Accounts.AdobeToken(r.Context(), account)
		pricingRules := 0
		pricingError := ""
		if tokenErr == nil {
			pricingRules, importErr = s.Accounts.SyncAdobePricing(r.Context(), account, token)
		}
		if tokenErr != nil {
			pricingError = tokenErr.Error()
		} else if importErr != nil {
			pricingError = importErr.Error()
		}
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.cookie_json_import", id.String(), map[string]any{"provider_id": providers.Adobe, "pricing_rules_synced": pricingRules})
		writeJSON(w, http.StatusOK, map[string]any{"account": account, "status": "active", "pricing_rules_synced": pricingRules, "pricing_sync_error": pricingError})
		return
	}
	if current.ProviderID == providers.CreativeFabrica {
		account, importErr := s.Accounts.ImportCreativeFabricaCookieJSON(r.Context(), id, req.CookieJSON)
		if importErr != nil {
			writeError(w, 400, "cookie_import_failed", importErr.Error())
			return
		}
		s.writeAudit(r.Context(), s.Config.AdminUsername, "account.cookie_json_import", id.String(), map[string]any{"provider_id": providers.CreativeFabrica})
		writeJSON(w, http.StatusOK, map[string]any{"account": account, "status": "active", "pricing_status": "requires_provider_sync"})
		return
	}
	account, err := s.Accounts.ImportCompleteCookieJSON(r.Context(), id, req.CookieJSON)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	if err != nil {
		writeError(w, 400, "cookie_import_failed", err.Error())
		return
	}
	job, err := s.Store.EnqueueBrowserSessionRefreshJob(r.Context(), id, 1000)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, 500, "session_refresh_enqueue_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.cookie_json_import", id.String(), map[string]any{"cookie_count_validated": true})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"account": account,
		"job":     job,
		"status":  "pending_browser_validation",
	})
}

func (s *Server) internalImportAccountSession(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSessionWorker(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var session accounts.BrowserSession
	if err := decodeJSON(r, &session); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	a, err := s.Accounts.ImportBrowserSession(r.Context(), id, session)
	if err != nil {
		writeError(w, 400, "session_import_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), "session-sync", "account.session_import", id.String(), nil)
	writeJSON(w, 200, a)
}

func (s *Server) authorizeSessionWorker(w http.ResponseWriter, r *http.Request) bool {
	if !s.Config.SessionWorkerAllowRemote {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			writeError(w, 403, "forbidden", "session worker is restricted to loopback")
			return false
		}
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	expected := s.Config.SessionSyncToken
	providedHash := sha256.Sum256([]byte(token))
	expectedHash := sha256.Sum256([]byte(expected))
	if expected == "" || token == "" || !constantEqual(providedHash[:], expectedHash[:]) {
		writeError(w, 401, "unauthorized", "invalid session worker token")
		return false
	}
	return true
}

func (s *Server) internalClaimSessionRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSessionWorker(w, r) {
		return
	}
	var req struct {
		WorkerID        string `json:"worker_id"`
		WorkerGroup     string `json:"worker_group"`
		HeadlessRefresh bool   `json:"headless_refresh"`
		HeadedLogin     bool   `json:"headed_login"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.WorkerID = strings.TrimSpace(req.WorkerID)
	req.WorkerGroup = strings.TrimSpace(req.WorkerGroup)
	if req.WorkerGroup == "" {
		req.WorkerGroup = "default"
	}
	if req.WorkerID == "" || len(req.WorkerID) > 200 {
		writeError(w, 400, "invalid_request", "worker_id must contain 1 to 200 characters")
		return
	}
	if !validBrowserWorkerGroup(req.WorkerGroup) {
		writeError(w, 400, "invalid_request", "worker_group is invalid")
		return
	}
	job, ok, err := s.Store.ClaimSessionRefreshJob(r.Context(), "browser", req.WorkerID, req.WorkerGroup, s.Config.SessionRefreshBrowserLease)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	account, err := s.Store.GetAccount(r.Context(), job.AccountID)
	if err != nil {
		_ = s.Store.FailSessionRefreshJob(r.Context(), job.ID, *job.LeaseToken, 5*time.Minute, "account disappeared after refresh claim")
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if retryAfter := s.Accounts.ProxyControlRemaining(r.Context(), account.ProxyURL); retryAfter > 0 {
		_ = s.Store.DeferSessionRefreshJob(r.Context(), job.ID, *job.LeaseToken, retryAfter, "shared upstream proxy control window is unavailable")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	profileKey := account.BrowserProfileKey
	if profileKey == "" {
		profileKey = account.ID.String()
	}
	response := map[string]any{
		"job":                 job,
		"browser_profile_key": profileKey,
		"proxy_url":           account.ProxyURL,
	}
	if req.HeadlessRefresh {
		cookieJSON, source, fingerprint, err := s.Accounts.SessionBrowserCookieJSON(r.Context(), account.ID)
		if err == nil && source != "" {
			response["cookie_json"] = cookieJSON
			response["cookie_json_source"] = source
			response["cookie_json_fingerprint"] = fingerprint
		} else if err == nil {
			cookieHeader, headerErr := s.Accounts.SessionCookie(r.Context(), account.ID)
			if headerErr == nil && strings.TrimSpace(cookieHeader) != "" {
				response["cookie_header"] = cookieHeader
			}
		}
	}
	if req.HeadedLogin && account.HasLoginCredentials {
		credential, err := s.Accounts.LoginCredential(r.Context(), account.ID)
		if err == nil {
			response["login_email"] = credential.Email
			response["login_password"] = credential.Password
		}
	}
	writeJSON(w, 200, response)
}

func (s *Server) internalHeartbeatSessionRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSessionWorker(w, r) {
		return
	}
	id, leaseToken, ok := decodeSessionRefreshLease(w, r)
	if !ok {
		return
	}
	extended, err := s.Store.ExtendSessionRefreshLease(r.Context(), id, leaseToken, s.Config.SessionRefreshBrowserLease)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !extended {
		writeError(w, 409, "lease_lost", "session refresh lease is no longer owned by this worker")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) internalCompleteSessionRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSessionWorker(w, r) {
		return
	}
	var req struct {
		LeaseToken string `json:"lease_token"`
		DurationMS int64  `json:"duration_ms"`
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || decodeJSON(r, &req) != nil {
		writeError(w, 400, "invalid_request", "invalid job id or completion payload")
		return
	}
	leaseToken, err := uuid.Parse(req.LeaseToken)
	if err != nil || req.DurationMS < 0 {
		writeError(w, 400, "invalid_request", "invalid lease_token or duration_ms")
		return
	}
	if err := s.Store.CompleteSessionRefreshJob(r.Context(), id, leaseToken, "browser", time.Duration(req.DurationMS)*time.Millisecond, s.Config.SessionRefreshMinFresh); err != nil {
		writeError(w, 409, "session_not_fresh", "browser session was not imported with sufficient remaining JWT lifetime during this lease")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) internalFailSessionRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSessionWorker(w, r) {
		return
	}
	var req struct {
		LeaseToken            string `json:"lease_token"`
		Error                 string `json:"error"`
		Terminal              bool   `json:"terminal"`
		CookieJSONFingerprint string `json:"cookie_json_fingerprint"`
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || decodeJSON(r, &req) != nil {
		writeError(w, 400, "invalid_request", "invalid job id or failure payload")
		return
	}
	leaseToken, err := uuid.Parse(req.LeaseToken)
	if err != nil {
		writeError(w, 400, "invalid_request", "invalid lease_token")
		return
	}
	job, err := s.Store.GetSessionRefreshJob(r.Context(), id)
	if err != nil {
		writeError(w, 404, "not_found", "session refresh job not found")
		return
	}
	if req.Terminal {
		if err := s.Store.TerminalFailSessionRefreshJob(r.Context(), id, leaseToken, req.Error, req.CookieJSONFingerprint); err != nil {
			writeError(w, 409, "lease_lost", "session refresh lease is no longer owned by this worker")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "terminal": true})
		return
	}
	retrySteps := []time.Duration{2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute}
	retryIndex := min(max(job.AttemptCount-1, 0), len(retrySteps)-1)
	if err := s.Store.FailSessionRefreshJob(r.Context(), id, leaseToken, retrySteps[retryIndex], req.Error); err != nil {
		writeError(w, 409, "lease_lost", "session refresh lease is no longer owned by this worker")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "retry_after_seconds": int(retrySteps[retryIndex].Seconds())})
}

func decodeSessionRefreshLease(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	var req struct {
		LeaseToken string `json:"lease_token"`
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || decodeJSON(r, &req) != nil {
		writeError(w, 400, "invalid_request", "invalid job id or lease payload")
		return uuid.Nil, uuid.Nil, false
	}
	leaseToken, err := uuid.Parse(req.LeaseToken)
	if err != nil {
		writeError(w, 400, "invalid_request", "invalid lease_token")
		return uuid.Nil, uuid.Nil, false
	}
	return id, leaseToken, true
}

func (s *Server) adminEnqueueSessionRefresh(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	job, err := s.Store.EnqueueSessionRefreshJob(r.Context(), id, 100)
	if err != nil {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.session_refresh_enqueue", id.String(), map[string]any{"job_id": job.ID})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) adminSessionRefreshJobs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := s.Store.ListSessionRefreshJobs(r.Context(), limit)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, jobs)
}

func (s *Server) adminSetAccountStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeError(w, 400, "invalid_status", "status must be active or disabled")
		return
	}
	if err := s.Store.SetAccountStatus(r.Context(), id, req.Status); err != nil {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.status", id.String(), map[string]any{"status": req.Status})
	writeJSON(w, 200, map[string]any{"id": id, "status": req.Status})
}

func (s *Server) adminSetAccountSessionWorkerGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid account id")
		return
	}
	var req struct {
		WorkerGroup string `json:"browser_worker_group"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req.WorkerGroup = strings.TrimSpace(req.WorkerGroup)
	if !validBrowserWorkerGroup(req.WorkerGroup) {
		writeError(w, 400, "invalid_request", "browser_worker_group must use 1 to 100 letters, digits, dots, underscores or hyphens")
		return
	}
	if err := s.Store.SetAccountBrowserWorkerGroup(r.Context(), id, req.WorkerGroup); err != nil {
		writeError(w, 404, "not_found", "account not found")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "account.session_worker_group", id.String(), map[string]any{"browser_worker_group": req.WorkerGroup})
	writeJSON(w, 200, map[string]any{"id": id, "browser_worker_group": req.WorkerGroup})
}

func validBrowserWorkerGroup(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
