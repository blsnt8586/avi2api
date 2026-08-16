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

func (s *Store) GetModelProviderConfig(ctx context.Context, providerID, modelID string) (domain.ModelProviderConfig, error) {
	var config domain.ModelProviderConfig
	err := s.DB.QueryRow(ctx, `SELECT c.provider_id,c.model_id,c.upstream_model,c.priority,c.settings
		FROM model_provider_configs c JOIN providers p ON p.id=c.provider_id
		WHERE c.provider_id=$1 AND c.model_id=$2 AND c.enabled=true AND p.enabled=true`, providerID, modelID).Scan(
		&config.ProviderID, &config.ModelID, &config.UpstreamModel, &config.Priority, &config.Settings)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, ErrNotFound
	}
	return config, err
}

func (s *Store) ListModelCosts(ctx context.Context, limit int) ([]domain.ModelCostRecord, error) {
	return s.ListModelCostsFiltered(ctx, "", "", limit)
}

func (s *Store) ListModelCostsFiltered(ctx context.Context, providerID, kind string, limit int) ([]domain.ModelCostRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `SELECT t.provider_id,t.model,t.kind,COALESCE(t.request->>'size',''),
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
		  AND ($1='' OR t.provider_id=$1) AND ($2='' OR t.kind=$2)
		GROUP BY t.provider_id,t.model,t.kind,t.request->>'size',t.request->>'quality',
			t.request->>'resolution',t.request->>'duration'
		ORDER BY max(t.updated_at) DESC LIMIT $3`, providerID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelCostRecord
	for rows.Next() {
		var c domain.ModelCostRecord
		if err := rows.Scan(&c.ProviderID, &c.Model, &c.Kind, &c.Size, &c.Quality, &c.Resolution, &c.Duration,
			&c.Samples, &c.Average, &c.Minimum, &c.Maximum, &c.UpstreamReportedSamples,
			&c.UpstreamReportedAverage, &c.UpstreamReportedMinimum, &c.UpstreamReportedMaximum,
			&c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type ProviderModelConfig struct {
	ProviderID    string
	Model         domain.ModelConfig
	UpstreamModel string
	Priority      int
	Settings      json.RawMessage
}

func (s *Store) ListProviderModelConfigs(ctx context.Context, providerID string) ([]ProviderModelConfig, error) {
	rows, err := s.DB.Query(ctx, `SELECT c.provider_id,m.id,m.display_name,m.capabilities,m.defaults,c.upstream_model,c.priority,c.settings
		FROM model_provider_configs c JOIN model_configs m ON m.id=c.model_id
		WHERE c.provider_id=$1 AND c.enabled=true AND m.enabled=true
		ORDER BY c.priority DESC,m.id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderModelConfig, 0)
	for rows.Next() {
		var item ProviderModelConfig
		var capabilities []byte
		if err := rows.Scan(&item.ProviderID, &item.Model.ID, &item.Model.DisplayName, &capabilities, &item.Model.Defaults, &item.UpstreamModel, &item.Priority, &item.Settings); err != nil {
			return nil, err
		}
		item.Model.UpstreamModel = item.UpstreamModel
		if err := json.Unmarshal(capabilities, &item.Model.Capabilities); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceProviderPlatformModels(ctx context.Context, providerID, mediaType, schemaVersion string, models []json.RawMessage) (time.Time, error) {
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
	if _, err := tx.Exec(ctx, `DELETE FROM platform_model_catalog WHERE provider_id=$1 AND media_type=$2`, providerID, mediaType); err != nil {
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
		if _, err := tx.Exec(ctx, `INSERT INTO platform_model_catalog(provider_id,media_type,upstream_id,schema_version,sort_order,model_data,synced_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, providerID, mediaType, identity.ID, schemaVersion, order, raw, syncedAt); err != nil {
			return time.Time{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return syncedAt, nil
}

func (s *Store) ListProviderPlatformModels(ctx context.Context, providerID, mediaType string) ([]json.RawMessage, string, *time.Time, error) {
	if mediaType != "image" && mediaType != "video" && mediaType != "audio" {
		return nil, "", nil, fmt.Errorf("invalid platform model media type %q", mediaType)
	}
	rows, err := s.DB.Query(ctx, `SELECT model_data,schema_version,synced_at FROM platform_model_catalog WHERE provider_id=$1 AND media_type=$2 ORDER BY sort_order,upstream_id`, providerID, mediaType)
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
