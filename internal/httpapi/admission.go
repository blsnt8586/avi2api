package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
)

func paidCreationRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/v1/images/generations", "/v1/images/edits", "/v1/tasks/images",
		"/v1/videos/generations", "/v1/audio/generations", "/v1/chat/completions":
		return true
	default:
		return false
	}
}

func synchronousCreationRequest(r *http.Request) bool {
	switch r.URL.Path {
	case "/v1/images/generations", "/v1/images/edits", "/v1/chat/completions":
		return true
	default:
		return false
	}
}

func (s *Server) preAuthIPAdmission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if paidCreationRequest(r) && s.Redis != nil {
			if err := s.admitIP(r.Context(), clientIPFromRequest(r)); err != nil {
				metrics.AdmissionRejected.WithLabelValues("ip_rate_limit").Inc()
				writeCreateTaskError(w, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) apiKeyAdmission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !paidCreationRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Context().Value(apiKeyContext).(domain.APIKey)
		if err := s.admitAPIKeyRequest(r.Context(), key.ID); err != nil {
			metrics.AdmissionRejected.WithLabelValues("api_key_rate_limit").Inc()
			writeCreateTaskError(w, err)
			return
		}
		idempotencyKey, idempotencyErr := normalizeIdempotencyKey(r.Header.Get("Idempotency-Key"))
		if idempotencyErr != nil {
			metrics.AdmissionRejected.WithLabelValues(idempotencyErr.Code).Inc()
			writeError(w, idempotencyErr.Status, idempotencyErr.Code, idempotencyErr.Message)
			return
		}
		r.Header.Set("Idempotency-Key", idempotencyKey)
		if s.Circuit.Redis != nil {
			if until, open, err := s.Circuit.CheckProvider(r.Context(), "leonardo"); err != nil {
				writeError(w, http.StatusServiceUnavailable, "admission_control_unavailable", err.Error())
				return
			} else if open {
				metrics.AdmissionRejected.WithLabelValues("provider_circuit_open").Inc()
				writeCreateTaskError(w, &requestError{Status: http.StatusServiceUnavailable, Code: "provider_circuit_open", Message: "provider submission circuit is open", RetryAfter: time.Until(until)})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func normalizeIdempotencyKey(raw string) (string, *requestError) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "idempotency_key_required",
			Message: "Idempotency-Key header is required",
		}
	}
	if utf8.RuneCountInString(key) > 255 {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "idempotency_key_too_long",
			Message: "Idempotency-Key must not exceed 255 characters",
		}
	}
	return key, nil
}

func (s *Server) generationBulkhead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !paidCreationRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !tryAcquire(s.generationSlots) {
			metrics.AdmissionRejected.WithLabelValues("gateway_overloaded").Inc()
			writeCreateTaskError(w, &requestError{Status: http.StatusServiceUnavailable, Code: "gateway_overloaded", Message: "generation admission is temporarily full", RetryAfter: time.Second})
			return
		}
		defer releaseSlot(s.generationSlots)
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			if !tryAcquire(s.multipartSlots) {
				metrics.AdmissionRejected.WithLabelValues("upload_capacity_exhausted").Inc()
				writeCreateTaskError(w, &requestError{Status: http.StatusServiceUnavailable, Code: "upload_capacity_exhausted", Message: "multipart upload capacity is temporarily full", RetryAfter: time.Second})
				return
			}
			defer releaseSlot(s.multipartSlots)
		}
		if synchronousCreationRequest(r) {
			if !tryAcquire(s.syncSlots) {
				metrics.AdmissionRejected.WithLabelValues("sync_capacity_exhausted").Inc()
				writeCreateTaskError(w, &requestError{Status: http.StatusServiceUnavailable, Code: "sync_capacity_exhausted", Message: "synchronous generation capacity is temporarily full", RetryAfter: time.Second})
				return
			}
			defer releaseSlot(s.syncSlots)
		}
		next.ServeHTTP(w, r)
	})
}

func tryAcquire(slots chan struct{}) bool {
	if slots == nil {
		return true
	}
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseSlot(slots chan struct{}) {
	if slots != nil {
		<-slots
	}
}
