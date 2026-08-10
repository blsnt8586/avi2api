package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func decodeJSON(r *http.Request, v any) error {
	return decodeJSONLimit(r, v, 2<<20)
}

func decodeJSONLimit(r *http.Request, v any, maxBytes int64) error {
	defer r.Body.Close()
	if maxBytes < 1 {
		maxBytes = 2 << 20
	}
	d := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	if code == "" {
		code = "error"
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": code, "code": code}})
}

func writeErrorDetails(w http.ResponseWriter, status int, code, msg string, details any) {
	if code == "" {
		code = "error"
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": code, "code": code, "details": details}})
}

func writeCreateTaskError(w http.ResponseWriter, err error) {
	code := "invalid_request"
	var re *requestError
	if errors.As(err, &re) {
		code = re.Code
		if re.RetryAfter > 0 {
			seconds := int(re.RetryAfter.Round(time.Second) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
	} else if strings.Contains(err.Error(), "idempotency") {
		code = "idempotency_conflict"
	}
	writeError(w, statusFor(err), code, err.Error())
}

func routingRequestError(err error) error {
	switch {
	case errors.Is(err, store.ErrInsufficientPoolBalance):
		return &requestError{Status: http.StatusPaymentRequired, Code: "insufficient_pool_balance", Message: "the account pool does not have enough available credits"}
	case errors.Is(err, store.ErrAccountQueueCapacity):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "account_queue_full", Message: "all eligible account queues are full", RetryAfter: 8 * time.Second}
	case errors.Is(err, store.ErrAPIKeyCapacity):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "api_key_capacity_exhausted", Message: "this API key has reached its execution and waiting capacity", RetryAfter: 8 * time.Second}
	case errors.Is(err, store.ErrSystemMaintenance):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "system_maintenance", Message: "the service is draining for maintenance and is not accepting new tasks", RetryAfter: 60 * time.Second}
	case errors.Is(err, store.ErrSystemQueueCapacity):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "system_queue_full", Message: "the system waiting queue has reached its hard limit", RetryAfter: 15 * time.Second}
	case errors.Is(err, store.ErrSystemOverloaded):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "system_overloaded", Message: "the system queue is above its overload watermark", RetryAfter: 15 * time.Second}
	case errors.Is(err, store.ErrSubmissionCircuitOpen):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "provider_circuit_open", Message: "provider submission uncertainty budget is exhausted", RetryAfter: 60 * time.Second}
	case errors.Is(err, store.ErrNoHealthyAccount):
		return &requestError{Status: http.StatusServiceUnavailable, Code: "no_account", Message: "no healthy account with a fresh balance is available", RetryAfter: 60 * time.Second}
	case errors.Is(err, store.ErrCostRuleUnavailable):
		return &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "the selected price rule is no longer active"}
	default:
		return err
	}
}

func bearer(v string) string {
	p := strings.SplitN(v, " ", 2)
	if len(p) == 2 && strings.EqualFold(p[0], "Bearer") {
		return strings.TrimSpace(p[1])
	}
	return ""
}

func allowed(list []string, m string) bool {
	for _, v := range list {
		if v == "*" || v == m {
			return true
		}
	}
	return false
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func constantEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func statusFor(err error) int {
	var re *requestError
	if errors.As(err, &re) {
		return re.Status
	}
	if strings.Contains(err.Error(), "idempotency") {
		return 409
	}
	return 400
}

func promptTooLong(prompt string, limit int) bool {
	return utf8.RuneCountInString(prompt) > limit
}

func sendSSE(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}
