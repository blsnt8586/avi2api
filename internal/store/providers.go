package store

import (
	"context"
	"encoding/json"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func (s *Store) ListProviders(ctx context.Context) ([]domain.Provider, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,display_name,enabled,auth_type,credit_unit,priority,capabilities,settings,created_at,updated_at FROM providers ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Provider
	for rows.Next() {
		var provider domain.Provider
		var capabilities []byte
		if err := rows.Scan(&provider.ID, &provider.DisplayName, &provider.Enabled, &provider.AuthType, &provider.CreditUnit, &provider.Priority, &capabilities, &provider.Settings, &provider.CreatedAt, &provider.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(capabilities, &provider.Capabilities); err != nil {
			return nil, err
		}
		out = append(out, provider)
	}
	return out, rows.Err()
}
