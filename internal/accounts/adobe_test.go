package accounts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/adobe"
)

func TestEstimateAdobePricingCostRetriesRateLimit(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"features":{"feature":{"cost":{"credits":7}}}}`)
	}))
	defer server.Close()

	client := adobe.NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	nextRequestAt := time.Time{}
	result, err := estimateAdobePricingCost(context.Background(), client, "TOKEN", adobe.CostRequest{Features: map[string]int{"feature": 1}}, &nextRequestAt, 0, time.Millisecond)
	if err != nil || result.Credits != 7 || attempts != 2 {
		t.Fatalf("cost=%+v attempts=%d err=%v", result, attempts, err)
	}
}

func TestEstimateAdobePricingCostPreservesNonRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := adobe.NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	nextRequestAt := time.Time{}
	_, err := estimateAdobePricingCost(context.Background(), client, "TOKEN", adobe.CostRequest{Features: map[string]int{"feature": 1}}, &nextRequestAt, 0, time.Millisecond)
	if err == nil {
		t.Fatal("expected non-rate-limit error")
	}
}
