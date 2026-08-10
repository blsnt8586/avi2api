package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"strings"
	"time"
)

func (s *Store) VerifyAPIKey(ctx context.Context, raw string) (domain.APIKey, error) {
	h := sha256.Sum256([]byte(raw))
	var k domain.APIKey
	var models []byte
	err := s.DB.QueryRow(ctx, `SELECT id,name,key_prefix,enabled,concurrency_limit,allowed_models FROM api_keys
		WHERE key_hash=$1 AND enabled=true AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, h[:]).Scan(&k.ID, &k.Name, &k.Prefix, &k.Enabled, &k.ConcurrencyLimit, &models)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	_ = json.Unmarshal(models, &k.AllowedModels)
	return k, err
}

func (s *Store) GetAPIKey(ctx context.Context, id uuid.UUID) (domain.APIKey, error) {
	var k domain.APIKey
	var models []byte
	err := s.DB.QueryRow(ctx, `SELECT id,name,key_prefix,enabled,concurrency_limit,allowed_models FROM api_keys WHERE id=$1 AND enabled=true AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, id).Scan(&k.ID, &k.Name, &k.Prefix, &k.Enabled, &k.ConcurrencyLimit, &models)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	_ = json.Unmarshal(models, &k.AllowedModels)
	return k, err
}

func (s *Store) CreateAPIKey(ctx context.Context, name, description, raw, prefix string, limit int, allowedModels []string, expiresAt *time.Time) (uuid.UUID, error) {
	h := sha256.Sum256([]byte(raw))
	models, err := json.Marshal(allowedModels)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.DB.QueryRow(ctx, `INSERT INTO api_keys(name,description,key_prefix,key_hash,concurrency_limit,allowed_models,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, name, description, prefix, h[:], limit, models, expiresAt).Scan(&id)
	return id, err
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]domain.APIKeyRecord, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,name,description,key_prefix,enabled,concurrency_limit,allowed_models,request_count,created_at,last_used_at,expires_at FROM api_keys WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.APIKeyRecord
	for rows.Next() {
		var k domain.APIKeyRecord
		var models []byte
		if err := rows.Scan(&k.ID, &k.Name, &k.Description, &k.Prefix, &k.Enabled, &k.ConcurrencyLimit, &models, &k.RequestCount, &k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(models, &k.AllowedModels)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) ListAPIKeysPage(ctx context.Context, page, pageSize int, search string) ([]domain.APIKeyRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	search = strings.TrimSpace(search)
	pattern := "%" + search + "%"
	var total int64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE deleted_at IS NULL AND ($1='' OR name ILIKE $2 OR description ILIKE $2 OR key_prefix ILIKE $2)`, search, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id,name,description,key_prefix,enabled,concurrency_limit,allowed_models,request_count,created_at,last_used_at,expires_at FROM api_keys WHERE deleted_at IS NULL AND ($1='' OR name ILIKE $2 OR description ILIKE $2 OR key_prefix ILIKE $2) ORDER BY created_at DESC LIMIT $3 OFFSET $4`, search, pattern, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.APIKeyRecord, 0, pageSize)
	for rows.Next() {
		var key domain.APIKeyRecord
		var models []byte
		if err := rows.Scan(&key.ID, &key.Name, &key.Description, &key.Prefix, &key.Enabled, &key.ConcurrencyLimit, &models, &key.RequestCount, &key.CreatedAt, &key.LastUsedAt, &key.ExpiresAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(models, &key.AllowedModels)
		out = append(out, key)
	}
	return out, total, rows.Err()
}

func (s *Store) SetAPIKeyEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	ct, err := s.DB.Exec(ctx, `UPDATE api_keys SET enabled=$2 WHERE id=$1 AND deleted_at IS NULL`, id, enabled)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	ct, err := s.DB.Exec(ctx, `UPDATE api_keys SET enabled=false,deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
