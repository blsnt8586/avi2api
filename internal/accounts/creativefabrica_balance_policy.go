package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrCreativeFabricaBalanceUnverified = errors.New("Creative Fabrica balance could not be verified; account invalid")

// A missing balance is not zero and must never leave an account routable using
// a historical balance. Keep the historical amount for accounting only.
func (s *Service) invalidateCreativeFabricaBalance(ctx context.Context, id uuid.UUID) error {
	if err := s.Store.SetAccountError(ctx, id, "invalid", ErrCreativeFabricaBalanceUnverified.Error(), nil); err != nil {
		return fmt.Errorf("%w: persist account status: %v", ErrCreativeFabricaBalanceUnverified, err)
	}
	return ErrCreativeFabricaBalanceUnverified
}
