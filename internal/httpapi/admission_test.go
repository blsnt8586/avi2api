package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/circuit"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/redis/go-redis/v9"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		want     string
		wantCode string
	}{
		{name: "missing", wantCode: "idempotency_key_required"},
		{name: "whitespace", value: " \t\r\n ", wantCode: "idempotency_key_required"},
		{name: "normalizes whitespace", value: "  operation-123  ", want: "operation-123"},
		{name: "maximum ASCII length", value: strings.Repeat("a", 255), want: strings.Repeat("a", 255)},
		{name: "maximum Unicode length", value: strings.Repeat("任", 255), want: strings.Repeat("任", 255)},
		{name: "too long", value: strings.Repeat("a", 256), wantCode: "idempotency_key_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeIdempotencyKey(test.value)
			if test.wantCode == "" {
				if err != nil || got != test.want {
					t.Fatalf("normalizeIdempotencyKey() = %q, %v; want %q, nil", got, err, test.want)
				}
				return
			}
			if err == nil || err.Code != test.wantCode {
				t.Fatalf("normalizeIdempotencyKey() error = %v; want code %q", err, test.wantCode)
			}
		})
	}
}

func TestAPIKeyAdmissionDefersProviderCircuitUntilRequestDecoded(t *testing.T) {
	circuitClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: time.Millisecond})
	t.Cleanup(func() { _ = circuitClient.Close() })
	server := &Server{Circuit: circuit.Breaker{Redis: circuitClient}}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	request.Header.Set("Idempotency-Key", "provider-aware-circuit")
	request = request.WithContext(context.WithValue(request.Context(), apiKeyContext, domain.APIKey{ID: uuid.New()}))
	response := httptest.NewRecorder()

	server.apiKeyAdmission(next).ServeHTTP(response, request)

	if !called {
		t.Fatal("expected provider circuit admission to run after the request body is decoded")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected handler status %d, got %d", http.StatusNoContent, response.Code)
	}
}
