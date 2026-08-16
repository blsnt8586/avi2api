package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) adminTasks(w http.ResponseWriter, r *http.Request) {
	pageText := r.URL.Query().Get("page")
	pageSizeText := r.URL.Query().Get("page_size")
	filterRequested := r.URL.Query().Get("search") != "" || r.URL.Query().Get("status") != "" ||
		r.URL.Query().Get("kind") != "" || r.URL.Query().Get("model") != "" ||
		r.URL.Query().Get("provider") != "" ||
		r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != ""
	if pageText == "" && pageSizeText == "" && !filterRequested {
		tasks, err := s.Store.ListAdminTasks(r.Context(), 200)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, tasks)
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
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if !validAdminTaskStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid_status", "status is not a supported task status")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && kind != "image" && kind != "video" && kind != "audio" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be image, video or audio")
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
	t, total, err := s.Store.ListAdminTasksPageFiltered(r.Context(), page, pageSize, store.TaskPageFilter{
		Search: r.URL.Query().Get("search"), Status: status, Kind: kind, Model: r.URL.Query().Get("model"),
		ProviderID: providerID, CreatedFrom: createdFrom, CreatedTo: createdTo,
	})
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"data": t, "total": total, "page": page, "page_size": pageSize})
}

func validAdminTaskStatus(status string) bool {
	switch status {
	case "", "active", "queued", "reserving", "uploading", "submitted", "polling", "succeeded", "failed", "cancelled", "submission_uncertain":
		return true
	default:
		return false
	}
}

func (s *Server) adminTask(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid task id")
		return
	}
	task, err := s.Store.GetAdminTask(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found", "task not found")
		return
	}
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, task)
}

func (s *Server) adminReleaseTaskReservation(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid task id")
		return
	}
	task, err := s.Store.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, 404, "not_found", "task not found")
		return
	}
	if task.Status != domain.TaskSubmissionUncertain {
		writeError(w, 409, "reservation_not_releasable", "only submission_uncertain reservations require manual release")
		return
	}
	if err := s.Store.ReleaseReservation(r.Context(), id, "manual_reconciliation"); err != nil {
		writeError(w, 500, "reservation_release_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "task.reservation_release", id.String(), map[string]any{"reason": "manual_reconciliation"})
	writeJSON(w, 200, map[string]any{"id": id, "reservation_state": "released"})
}

func (s *Server) adminConfirmTaskNotSubmitted(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_id", "invalid task id")
		return
	}
	confirmed, err := s.Store.ConfirmSubmissionNotCreated(r.Context(), id)
	if err != nil {
		writeError(w, 500, "task_reconciliation_failed", err.Error())
		return
	}
	if !confirmed {
		writeError(w, 409, "task_not_reconcilable", "only submission_uncertain tasks can be marked as not created")
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "task.submission_not_created", id.String(), map[string]any{
		"result": "failed",
		"reason": "administrator_confirmed_no_upstream_generation",
	})
	writeJSON(w, 200, map[string]any{"id": id, "status": domain.TaskFailed, "reservation_state": "released"})
}

type uncertainTaskCleanupResult struct {
	ID           uuid.UUID `json:"id"`
	Resolved     bool      `json:"resolved"`
	ErrorCode    string    `json:"error_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

func (s *Server) adminFailSubmissionUncertainTasks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.IDs) < 1 || len(req.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "ids must contain between 1 and 100 task ids")
		return
	}

	seen := make(map[uuid.UUID]struct{}, len(req.IDs))
	results := make([]uncertainTaskCleanupResult, 0, len(req.IDs))
	resolved := 0
	for _, id := range req.IDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result := uncertainTaskCleanupResult{ID: id}
		confirmed, err := s.Store.ConfirmSubmissionNotCreated(r.Context(), id)
		if err != nil {
			result.ErrorCode = "task_reconciliation_failed"
			result.ErrorMessage = err.Error()
		} else if !confirmed {
			result.ErrorCode = "task_not_reconcilable"
			result.ErrorMessage = "task is no longer submission_uncertain"
		} else {
			result.Resolved = true
			resolved++
			s.writeAudit(r.Context(), s.Config.AdminUsername, "task.submission_not_created", id.String(), map[string]any{
				"batch":  true,
				"result": "failed",
				"reason": "administrator_confirmed_no_upstream_generation",
			})
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resolved": resolved,
		"skipped":  len(results) - resolved,
		"results":  results,
	})
}
