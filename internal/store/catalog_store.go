package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"time"
)

func (s *Store) ListModels(ctx context.Context) ([]domain.ModelConfig, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,upstream_model,display_name,capabilities,defaults FROM model_configs WHERE enabled=true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelConfig
	for rows.Next() {
		var m domain.ModelConfig
		var caps []byte
		if err := rows.Scan(&m.ID, &m.UpstreamModel, &m.DisplayName, &caps, &m.Defaults); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(caps, &m.Capabilities)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetModel(ctx context.Context, id string) (domain.ModelConfig, error) {
	var m domain.ModelConfig
	var caps []byte
	err := s.DB.QueryRow(ctx, `SELECT id,upstream_model,display_name,capabilities,defaults FROM model_configs WHERE id=$1 AND enabled=true`, id).Scan(&m.ID, &m.UpstreamModel, &m.DisplayName, &caps, &m.Defaults)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	_ = json.Unmarshal(caps, &m.Capabilities)
	return m, err
}

func (s *Store) ListModelCosts(ctx context.Context, limit int) ([]domain.ModelCostRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `SELECT t.model,t.kind,COALESCE(t.request->>'size',''),
		COALESCE(t.request->>'quality',''),COALESCE(t.request->>'resolution',''),
		COALESCE(t.request->>'duration',''),count(*),
		round(avg(COALESCE(r.settled_tokens,t.estimated_tokens)::numeric),2),
		min(COALESCE(r.settled_tokens,t.estimated_tokens)),
		max(COALESCE(r.settled_tokens,t.estimated_tokens)),
		count(t.upstream_reported_cost),
		round(avg(t.upstream_reported_cost)::numeric,2)::double precision,
		min(t.upstream_reported_cost),max(t.upstream_reported_cost),max(t.updated_at)
		FROM tasks t JOIN account_reservations r ON r.task_id=t.id
		WHERE t.status='succeeded' AND r.state='consumed'
		GROUP BY t.model,t.kind,t.request->>'size',t.request->>'quality',
			t.request->>'resolution',t.request->>'duration'
		ORDER BY max(t.updated_at) DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelCostRecord
	for rows.Next() {
		var c domain.ModelCostRecord
		if err := rows.Scan(&c.Model, &c.Kind, &c.Size, &c.Quality, &c.Resolution, &c.Duration,
			&c.Samples, &c.Average, &c.Minimum, &c.Maximum, &c.UpstreamReportedSamples,
			&c.UpstreamReportedAverage, &c.UpstreamReportedMinimum, &c.UpstreamReportedMaximum,
			&c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ReplacePlatformModels(ctx context.Context, mediaType, schemaVersion string, models []json.RawMessage) (time.Time, error) {
	if mediaType != "image" && mediaType != "video" && mediaType != "audio" {
		return time.Time{}, fmt.Errorf("invalid platform model media type %q", mediaType)
	}
	if len(models) == 0 {
		return time.Time{}, errors.New("platform model catalog is empty")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM platform_model_catalog WHERE media_type=$1`, mediaType); err != nil {
		return time.Time{}, err
	}
	syncedAt := time.Now().UTC()
	for order, raw := range models {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			return time.Time{}, fmt.Errorf("decode platform model %d: %w", order, err)
		}
		if identity.ID == "" {
			return time.Time{}, fmt.Errorf("platform model %d has no id", order)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO platform_model_catalog(media_type,upstream_id,schema_version,sort_order,model_data,synced_at) VALUES($1,$2,$3,$4,$5,$6)`, mediaType, identity.ID, schemaVersion, order, raw, syncedAt); err != nil {
			return time.Time{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return syncedAt, nil
}

func (s *Store) ListPlatformModels(ctx context.Context, mediaType string) ([]json.RawMessage, string, *time.Time, error) {
	if mediaType != "image" && mediaType != "video" && mediaType != "audio" {
		return nil, "", nil, fmt.Errorf("invalid platform model media type %q", mediaType)
	}
	rows, err := s.DB.Query(ctx, `SELECT model_data,schema_version,synced_at FROM platform_model_catalog WHERE media_type=$1 ORDER BY sort_order,upstream_id`, mediaType)
	if err != nil {
		return nil, "", nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	var schemaVersion string
	var syncedAt *time.Time
	for rows.Next() {
		var raw json.RawMessage
		var rowSyncedAt time.Time
		if err := rows.Scan(&raw, &schemaVersion, &rowSyncedAt); err != nil {
			return nil, "", nil, err
		}
		if syncedAt == nil {
			t := rowSyncedAt
			syncedAt = &t
		}
		out = append(out, raw)
	}
	return out, schemaVersion, syncedAt, rows.Err()
}
