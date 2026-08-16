package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
)

func scanProvider(row pgx.Row, provider *domain.Provider) error {
	var capabilities []byte
	if err := row.Scan(&provider.ID, &provider.DisplayName, &provider.Enabled, &provider.AuthType, &provider.CreditUnit, &provider.Priority, &capabilities, &provider.Settings, &provider.CreatedAt, &provider.UpdatedAt); err != nil {
		return err
	}
	if err := json.Unmarshal(capabilities, &provider.Capabilities); err != nil {
		return err
	}
	var settings struct {
		CatalogSync *bool `json:"catalog_sync"`
	}
	_ = json.Unmarshal(provider.Settings, &settings)
	provider.CatalogSync = provider.ID == providers.Leonardo
	if settings.CatalogSync != nil {
		provider.CatalogSync = *settings.CatalogSync
	}
	return nil
}

func (s *Store) GetProvider(ctx context.Context, id string) (domain.Provider, error) {
	var provider domain.Provider
	err := scanProvider(s.DB.QueryRow(ctx, `SELECT id,display_name,enabled,auth_type,credit_unit,priority,capabilities,settings,created_at,updated_at FROM providers WHERE id=$1`, id), &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider, ErrNotFound
	}
	return provider, err
}

func (s *Store) ListProviders(ctx context.Context) ([]domain.Provider, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,display_name,enabled,auth_type,credit_unit,priority,capabilities,settings,created_at,updated_at FROM providers ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Provider
	for rows.Next() {
		var provider domain.Provider
		if err := scanProvider(rows, &provider); err != nil {
			return nil, err
		}
		out = append(out, provider)
	}
	return out, rows.Err()
}
