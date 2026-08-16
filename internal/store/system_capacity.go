package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type SystemCapacityConfig struct {
	MaxExecuting         int       `json:"max_executing"`
	MaxQueued            int       `json:"max_queued"`
	QueueHighWatermark   int       `json:"queue_high_watermark"`
	QueueResumeWatermark int       `json:"queue_resume_watermark"`
	QueueTimeoutSeconds  int       `json:"queue_timeout_seconds"`
	MaintenanceMode      bool      `json:"maintenance_mode"`
	ExecutionPaused      bool      `json:"execution_paused"`
	OverloadActive       bool      `json:"overload_active"`
	Revision             int64     `json:"revision"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type SystemCapacitySnapshot struct {
	SystemCapacityConfig
	Queued                  int                        `json:"queued"`
	Executing               int                        `json:"executing"`
	ExecutingImages         int                        `json:"executing_images"`
	ExecutingVideos         int                        `json:"executing_videos"`
	ExecutingAudio          int                        `json:"executing_audio"`
	EligibleAccounts        int                        `json:"eligible_accounts"`
	EligibleExecutionSlots  int                        `json:"eligible_execution_slots"`
	EligibleQueueSlots      int                        `json:"eligible_queue_slots"`
	EffectiveExecutionLimit int                        `json:"effective_execution_limit"`
	ExecutionHeadroom       int                        `json:"execution_headroom"`
	QueueHeadroom           int                        `json:"queue_headroom"`
	OldestQueuedSeconds     float64                    `json:"oldest_queued_seconds"`
	Providers               []ProviderCapacitySnapshot `json:"providers"`
}

type ProviderCapacitySnapshot struct {
	ProviderID             string `json:"provider_id"`
	DisplayName            string `json:"display_name"`
	Enabled                bool   `json:"enabled"`
	Queued                 int    `json:"queued"`
	Executing              int    `json:"executing"`
	ExecutingImages        int    `json:"executing_images"`
	ExecutingVideos        int    `json:"executing_videos"`
	ExecutingAudio         int    `json:"executing_audio"`
	EligibleAccounts       int    `json:"eligible_accounts"`
	EligibleExecutionSlots int    `json:"eligible_execution_slots"`
	EligibleQueueSlots     int    `json:"eligible_queue_slots"`
}

func (config SystemCapacityConfig) Validate() error {
	if config.MaxExecuting < 1 || config.MaxExecuting > 10000 {
		return errors.New("max_executing must be between 1 and 10000")
	}
	if config.MaxQueued < 1 || config.MaxQueued > 100000 {
		return errors.New("max_queued must be between 1 and 100000")
	}
	if config.QueueHighWatermark < 1 || config.QueueHighWatermark > config.MaxQueued {
		return errors.New("queue_high_watermark must be between 1 and max_queued")
	}
	if config.QueueResumeWatermark < 0 || config.QueueResumeWatermark >= config.QueueHighWatermark {
		return errors.New("queue_resume_watermark must be lower than queue_high_watermark")
	}
	if config.QueueTimeoutSeconds < 60 || config.QueueTimeoutSeconds > 86400 {
		return errors.New("queue_timeout_seconds must be between 60 and 86400")
	}
	if config.ExecutionPaused && !config.MaintenanceMode {
		return errors.New("execution_paused requires maintenance_mode")
	}
	return nil
}

func scanSystemCapacityConfig(row pgx.Row) (SystemCapacityConfig, error) {
	var config SystemCapacityConfig
	err := row.Scan(
		&config.MaxExecuting,
		&config.MaxQueued,
		&config.QueueHighWatermark,
		&config.QueueResumeWatermark,
		&config.QueueTimeoutSeconds,
		&config.MaintenanceMode,
		&config.ExecutionPaused,
		&config.OverloadActive,
		&config.Revision,
		&config.UpdatedAt,
	)
	return config, err
}

const systemCapacityColumns = `max_executing,max_queued,queue_high_watermark,
	queue_resume_watermark,queue_timeout_seconds,maintenance_mode,execution_paused,
	overload_active,revision,updated_at`

func loadSystemCapacityConfigTx(ctx context.Context, tx pgx.Tx, lock bool) (SystemCapacityConfig, error) {
	query := `SELECT ` + systemCapacityColumns + ` FROM system_capacity_config WHERE id=1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanSystemCapacityConfig(tx.QueryRow(ctx, query))
}

func (s *Store) GetSystemCapacity(ctx context.Context) (SystemCapacitySnapshot, error) {
	config, err := scanSystemCapacityConfig(s.DB.QueryRow(ctx, `SELECT `+systemCapacityColumns+` FROM system_capacity_config WHERE id=1`))
	if err != nil {
		return SystemCapacitySnapshot{}, err
	}
	return s.loadSystemCapacitySnapshot(ctx, config)
}

func (s *Store) loadSystemCapacitySnapshot(ctx context.Context, config SystemCapacityConfig) (SystemCapacitySnapshot, error) {
	snapshot := SystemCapacitySnapshot{SystemCapacityConfig: config}
	if err := s.DB.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status='queued'),
		count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling')),
		count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='image'),
		count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='video'),
		count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='audio'),
		COALESCE(EXTRACT(epoch FROM now()-min(created_at) FILTER (WHERE status='queued')),0)::float8
		FROM tasks WHERE status IN ('queued','reserving','uploading','submitted','polling')`).Scan(
		&snapshot.Queued,
		&snapshot.Executing,
		&snapshot.ExecutingImages,
		&snapshot.ExecutingVideos,
		&snapshot.ExecutingAudio,
		&snapshot.OldestQueuedSeconds,
	); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*),COALESCE(sum(image_concurrency),0),COALESCE(sum(queue_capacity),0)
		FROM accounts WHERE archived_at IS NULL AND status='active' AND (cooldown_until IS NULL OR cooldown_until<=now())
		  AND access_token_expires_at IS NOT NULL AND access_token_expires_at>now()
		  AND last_checked_at IS NOT NULL`).Scan(
		&snapshot.EligibleAccounts,
		&snapshot.EligibleExecutionSlots,
		&snapshot.EligibleQueueSlots,
	); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	rows, err := s.DB.Query(ctx, `WITH task_stats AS (
		SELECT provider_id,
			count(*) FILTER (WHERE status='queued') AS queued,
			count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling')) AS executing,
			count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='image') AS executing_images,
			count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='video') AS executing_videos,
			count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling') AND kind='audio') AS executing_audio
		FROM tasks WHERE status IN ('queued','reserving','uploading','submitted','polling') GROUP BY provider_id
	), account_stats AS (
		SELECT provider_id,count(*) AS eligible_accounts,COALESCE(sum(image_concurrency),0) AS execution_slots,
			COALESCE(sum(queue_capacity),0) AS queue_slots
		FROM accounts WHERE archived_at IS NULL AND status='active'
			AND (cooldown_until IS NULL OR cooldown_until<=now())
			AND access_token_expires_at IS NOT NULL AND access_token_expires_at>now()
			AND last_checked_at IS NOT NULL GROUP BY provider_id
	)
	SELECT p.id,p.display_name,p.enabled,COALESCE(t.queued,0),COALESCE(t.executing,0),
		COALESCE(t.executing_images,0),COALESCE(t.executing_videos,0),COALESCE(t.executing_audio,0),
		COALESCE(a.eligible_accounts,0),COALESCE(a.execution_slots,0),COALESCE(a.queue_slots,0)
	FROM providers p LEFT JOIN task_stats t ON t.provider_id=p.id
	LEFT JOIN account_stats a ON a.provider_id=p.id ORDER BY p.priority,p.id`)
	if err != nil {
		return SystemCapacitySnapshot{}, err
	}
	defer rows.Close()
	snapshot.Providers = make([]ProviderCapacitySnapshot, 0)
	for rows.Next() {
		var provider ProviderCapacitySnapshot
		if err := rows.Scan(&provider.ProviderID, &provider.DisplayName, &provider.Enabled,
			&provider.Queued, &provider.Executing, &provider.ExecutingImages, &provider.ExecutingVideos,
			&provider.ExecutingAudio, &provider.EligibleAccounts, &provider.EligibleExecutionSlots,
			&provider.EligibleQueueSlots); err != nil {
			return SystemCapacitySnapshot{}, err
		}
		snapshot.Providers = append(snapshot.Providers, provider)
	}
	if err := rows.Err(); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	snapshot.EffectiveExecutionLimit = min(config.MaxExecuting, snapshot.EligibleExecutionSlots)
	snapshot.ExecutionHeadroom = max(0, snapshot.EffectiveExecutionLimit-snapshot.Executing)
	snapshot.QueueHeadroom = max(0, config.MaxQueued-snapshot.Queued)
	return snapshot, nil
}

func (s *Store) UpdateSystemCapacity(ctx context.Context, next SystemCapacityConfig) (SystemCapacitySnapshot, error) {
	if err := next.Validate(); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return SystemCapacitySnapshot{}, err
	}
	defer tx.Rollback(ctx)
	current, err := loadSystemCapacityConfigTx(ctx, tx, true)
	if err != nil {
		return SystemCapacitySnapshot{}, err
	}
	var queued int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE status='queued'`).Scan(&queued); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	overloaded := current.OverloadActive
	if queued <= next.QueueResumeWatermark {
		overloaded = false
	} else if queued >= next.QueueHighWatermark {
		overloaded = true
	}
	updated, err := scanSystemCapacityConfig(tx.QueryRow(ctx, `UPDATE system_capacity_config SET
		max_executing=$1,max_queued=$2,queue_high_watermark=$3,queue_resume_watermark=$4,
		queue_timeout_seconds=$5,maintenance_mode=$6,execution_paused=$7,overload_active=$8,
		revision=revision+1,updated_at=now() WHERE id=1 RETURNING `+systemCapacityColumns,
		next.MaxExecuting,
		next.MaxQueued,
		next.QueueHighWatermark,
		next.QueueResumeWatermark,
		next.QueueTimeoutSeconds,
		next.MaintenanceMode,
		next.ExecutionPaused,
		overloaded,
	))
	if err != nil {
		return SystemCapacitySnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SystemCapacitySnapshot{}, err
	}
	return s.loadSystemCapacitySnapshot(ctx, updated)
}

func lockSystemAdmission(ctx context.Context, tx pgx.Tx) (SystemCapacityConfig, error) {
	config, err := loadSystemCapacityConfigTx(ctx, tx, true)
	if err != nil {
		return SystemCapacityConfig{}, err
	}
	if config.MaintenanceMode {
		return SystemCapacityConfig{}, ErrSystemMaintenance
	}
	var queued int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE status='queued'`).Scan(&queued); err != nil {
		return SystemCapacityConfig{}, err
	}
	if config.OverloadActive {
		if queued > config.QueueResumeWatermark {
			return SystemCapacityConfig{}, ErrSystemOverloaded
		}
		if _, err := tx.Exec(ctx, `UPDATE system_capacity_config SET overload_active=false,updated_at=now() WHERE id=1`); err != nil {
			return SystemCapacityConfig{}, err
		}
		config.OverloadActive = false
	}
	if queued >= config.MaxQueued {
		_, _ = tx.Exec(ctx, `UPDATE system_capacity_config SET overload_active=true,updated_at=now() WHERE id=1`)
		return SystemCapacityConfig{}, ErrSystemQueueCapacity
	}
	if queued >= config.QueueHighWatermark {
		if _, err := tx.Exec(ctx, `UPDATE system_capacity_config SET overload_active=true,updated_at=now() WHERE id=1`); err != nil {
			return SystemCapacityConfig{}, err
		}
		return SystemCapacityConfig{}, ErrSystemOverloaded
	}
	return config, nil
}

func lockSystemExecution(ctx context.Context, tx pgx.Tx) (bool, string, error) {
	config, err := loadSystemCapacityConfigTx(ctx, tx, true)
	if err != nil {
		return false, "", err
	}
	if config.ExecutionPaused {
		return false, "system execution is paused", nil
	}
	var executing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks
		WHERE status IN ('reserving','uploading','submitted','polling')`).Scan(&executing); err != nil {
		return false, "", err
	}
	if executing >= config.MaxExecuting {
		return false, fmt.Sprintf("system execution capacity is full (%d/%d)", executing, config.MaxExecuting), nil
	}
	return true, "", nil
}
