package accounts

import (
	"fmt"
	"testing"
)

func TestCreativeFabricaUnverifiedBalanceInvalidatesSession(t *testing.T) {
	for _, err := range []error{ErrCreativeFabricaBalanceUnverified, fmt.Errorf("refresh: %w", ErrCreativeFabricaBalanceUnverified)} {
		status, message, cooldown := classifySessionError(err)
		if status != "invalid" || message != ErrCreativeFabricaBalanceUnverified.Error() || cooldown != nil {
			t.Fatalf("unexpected classification: %s %s %v", status, message, cooldown)
		}
	}
}
