package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/store"
)

func TestRoutingRequestErrors(t *testing.T) {
	tests := []struct {
		input  error
		status int
		code   string
	}{
		{store.ErrInsufficientPoolBalance, http.StatusPaymentRequired, "insufficient_pool_balance"},
		{store.ErrAccountQueueCapacity, http.StatusServiceUnavailable, "account_queue_full"},
		{store.ErrAPIKeyCapacity, http.StatusServiceUnavailable, "api_key_capacity_exhausted"},
		{store.ErrSystemMaintenance, http.StatusServiceUnavailable, "system_maintenance"},
		{store.ErrSystemQueueCapacity, http.StatusServiceUnavailable, "system_queue_full"},
		{store.ErrSystemOverloaded, http.StatusServiceUnavailable, "system_overloaded"},
		{store.ErrNoHealthyAccount, http.StatusServiceUnavailable, "no_account"},
	}
	for _, test := range tests {
		var requestErr *requestError
		if !errors.As(routingRequestError(test.input), &requestErr) {
			t.Fatalf("expected requestError for %v", test.input)
		}
		if requestErr.Status != test.status || requestErr.Code != test.code {
			t.Fatalf("status=%d code=%q", requestErr.Status, requestErr.Code)
		}
	}
}
