package store

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryableClaimTransactionError(t *testing.T) {
	for _, code := range []string{"40P01", "40001"} {
		if !retryableClaimTransactionError(&pgconn.PgError{Code: code}) {
			t.Fatalf("expected PostgreSQL %s to be retryable", code)
		}
	}
	if retryableClaimTransactionError(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violations must not be retried by ClaimTask")
	}
	if retryableClaimTransactionError(errors.New("fixture")) {
		t.Fatal("generic errors must not be retried by ClaimTask")
	}
}
