package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

type TaskOutboxMessage struct {
	TaskID       uuid.UUID
	Kind         string
	AttemptCount int
	Recovery     bool
}

type claimAccountState struct {
	ID                  uuid.UUID
	ProviderID          string
	Status              string
	CooldownUntil       *time.Time
	TokenExpiresAt      *time.Time
	LastCheckedAt       *time.Time
	Balance             int64
	Concurrency         int
	QueueCapacity       int
	RoutingRole         string
	ProtectedTokens     int64
	VideoReservedSlots  int
	HeldTokens          int64
	ActiveTasks         int
	ActiveNonVideoTasks int
	ReservationCount    int
}

func (state claimAccountState) routeable(providerID string, now time.Time) bool {
	return state.ProviderID == providerID && state.Status == "active" &&
		(state.CooldownUntil == nil || !state.CooldownUntil.After(now)) &&
		state.TokenExpiresAt != nil && state.TokenExpiresAt.After(now) &&
		state.LastCheckedAt != nil &&
		state.Balance >= state.HeldTokens
}

func loadClaimAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (claimAccountState, error) {
	query := `SELECT a.id,a.provider_id,a.status,a.cooldown_until,a.access_token_expires_at,a.last_checked_at,
		a.subscription_tokens+a.rollover_tokens+a.paid_tokens,a.image_concurrency,a.queue_capacity,
		a.routing_role,a.protected_tokens,a.video_reserved_slots,
		COALESCE(u.held_tokens,0),COALESCE(u.active_tasks,0),COALESCE(u.active_non_video_tasks,0),COALESCE(u.reservation_count,0)
		FROM accounts a LEFT JOIN LATERAL (
			SELECT COALESCE(sum(r.estimated_tokens),0) AS held_tokens,
				count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS active_tasks,
				count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling') AND t.kind<>'video') AS active_non_video_tasks,
				count(*) AS reservation_count
			FROM account_reservations r JOIN tasks t ON t.id=r.task_id
			WHERE r.account_id=a.id AND r.state='held'
		) u ON true WHERE a.id=$1`
	if lock {
		query += ` FOR UPDATE OF a`
	}
	var state claimAccountState
	err := tx.QueryRow(ctx, query, id).Scan(&state.ID, &state.ProviderID, &state.Status,
		&state.CooldownUntil, &state.TokenExpiresAt, &state.LastCheckedAt, &state.Balance, &state.Concurrency,
		&state.QueueCapacity, &state.RoutingRole, &state.ProtectedTokens, &state.VideoReservedSlots,
		&state.HeldTokens, &state.ActiveTasks, &state.ActiveNonVideoTasks, &state.ReservationCount)
	return state, err
}

func deferQueuedTaskTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string, delay time.Duration) error {
	if delay < time.Second {
		delay = time.Second
	}
	_, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=NULL,next_attempt_at=$2,
		last_error=$3,updated_at=now() WHERE task_id=$1`, id, time.Now().Add(delay), reason)
	return err
}

func wakeQueuedTasksTx(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, apiKeyID *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `WITH candidates AS MATERIALIZED (
			SELECT id FROM tasks WHERE account_id=$1 AND status='queued'
			ORDER BY created_at,id LIMIT 32 FOR UPDATE SKIP LOCKED
		)
		UPDATE task_outbox o SET delivered_at=NULL,next_attempt_at=now(),updated_at=now()
		FROM candidates c WHERE o.task_id=c.id`, accountID); err != nil {
		return err
	}
	if apiKeyID == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `WITH candidates AS MATERIALIZED (
			SELECT id FROM tasks WHERE api_key_id=$1 AND status='queued'
			ORDER BY created_at,id LIMIT 32 FOR UPDATE SKIP LOCKED
		)
		UPDATE task_outbox o SET delivered_at=NULL,next_attempt_at=now(),updated_at=now()
		FROM candidates c WHERE o.task_id=c.id`, *apiKeyID)
	return err
}

func rerouteQueuedTaskTx(ctx context.Context, tx pgx.Tx, task domain.Task, oldAccountID uuid.UUID, estimatedTokens int64, policy SchedulingPolicy) (uuid.UUID, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "aiv2api:provider-reroute:"+task.ProviderID); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, oldAccountID); err != nil {
		return uuid.Nil, err
	}
	query := `WITH usage AS (
		SELECT r.account_id,COALESCE(sum(r.estimated_tokens),0) AS reserved,
			count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS slots,
			count(*) AS total
		FROM account_reservations r JOIN tasks t ON t.id=r.task_id
		WHERE r.state='held' GROUP BY r.account_id
	)
	SELECT a.id FROM accounts a LEFT JOIN usage u ON u.account_id=a.id
	WHERE a.provider_id=$1 AND a.id<>$2 AND a.status='active'
	  AND (a.cooldown_until IS NULL OR a.cooldown_until<=now())
	  AND a.access_token_expires_at IS NOT NULL AND a.access_token_expires_at>now()
	  AND a.last_checked_at IS NOT NULL
	  AND (SELECT count(*) FROM tasks uncertain
	       JOIN account_reservations uncertain_reservation ON uncertain_reservation.task_id=uncertain.id AND uncertain_reservation.state='held'
	       WHERE uncertain.account_id=a.id AND uncertain.status='submission_uncertain')<$5
	  AND (SELECT count(*) FROM tasks uncertain
	       JOIN account_reservations uncertain_reservation ON uncertain_reservation.task_id=uncertain.id AND uncertain_reservation.state='held'
	       JOIN accounts uncertain_account ON uncertain_account.id=uncertain.account_id
	       WHERE uncertain.provider_id=$1 AND uncertain.status='submission_uncertain'
	         AND COALESCE(uncertain_account.proxy_url,'')=COALESCE(a.proxy_url,''))<$6
	  AND COALESCE(u.slots,0)<a.image_concurrency
	  AND COALESCE(u.total,0)<a.image_concurrency+a.queue_capacity-
	    CASE WHEN $4<>'video' AND a.routing_role='video_reserved' THEN a.video_reserved_slots ELSE 0 END
	  AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)-
	    CASE WHEN $4<>'video'
	      AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)>=a.protected_tokens
	      THEN a.protected_tokens ELSE 0 END >=$3
	ORDER BY CASE WHEN $4='video' THEN CASE WHEN a.routing_role='video_reserved' THEN 0 ELSE 1 END
	              ELSE CASE WHEN a.routing_role='general' THEN 0 ELSE 1 END END ASC,
	  COALESCE(u.slots,0)::numeric/a.image_concurrency ASC,
	  COALESCE(u.total,0)::numeric/(a.image_concurrency+a.queue_capacity) ASC,
	  COALESCE(a.last_submitted_at,'epoch'::timestamptz) ASC,
	  a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)-
	    CASE WHEN $4<>'video'
	      AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)>=a.protected_tokens
	      THEN a.protected_tokens ELSE 0 END-$3 ASC,
	  a.id
	FOR UPDATE OF a SKIP LOCKED LIMIT 1`
	var targetID uuid.UUID
	if err := tx.QueryRow(ctx, query, task.ProviderID, oldAccountID, estimatedTokens, task.Kind, policy.UncertainAccountLimit, policy.UncertainProxyLimit).Scan(&targetID); err != nil {
		return uuid.Nil, err
	}
	ct, err := tx.Exec(ctx, `WITH moved AS (
		UPDATE tasks SET account_id=$2,updated_at=now()
		WHERE id=$1 AND status='queued' AND generation_id='' RETURNING id
	)
	UPDATE account_reservations r SET account_id=$2,updated_at=now()
	FROM moved WHERE r.task_id=moved.id AND r.state='held'`, task.ID, targetID)
	if err != nil {
		return uuid.Nil, err
	}
	if ct.RowsAffected() != 1 {
		return uuid.Nil, errors.New("queued task reservation could not be rerouted")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message,payload)
		VALUES($1,'queued','reserved account rerouted',jsonb_build_object('from_account_id',$2::uuid,'to_account_id',$3::uuid))`,
		task.ID, oldAccountID, targetID); err != nil {
		return uuid.Nil, err
	}
	return targetID, nil
}

// ClaimTask gives one worker exclusive ownership before any upstream side effect.
func (s *Store) ClaimTask(ctx context.Context, id uuid.UUID, lease time.Duration) (domain.Task, uuid.UUID, bool, error) {
	const maxAttempts = 4
	for attempt := 0; attempt < maxAttempts; attempt++ {
		task, leaseID, claimed, err := s.claimTaskOnce(ctx, id, lease)
		if err == nil || !retryableClaimTransactionError(err) || attempt == maxAttempts-1 {
			return task, leaseID, claimed, err
		}
		delay := (20 * time.Millisecond) << attempt
		delay += time.Duration(id[0]%10) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return domain.Task{}, uuid.Nil, false, ctx.Err()
		case <-timer.C:
		}
	}
	return domain.Task{}, uuid.Nil, false, errors.New("claim task retry loop exhausted")
}

func retryableClaimTransactionError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40P01" || pgErr.Code == "40001")
}

func (s *Store) claimTaskOnce(ctx context.Context, id uuid.UUID, lease time.Duration) (domain.Task, uuid.UUID, bool, error) {
	if lease <= 0 {
		lease = 16 * time.Minute
	}
	leaseID := uuid.New()
	expiresAt := time.Now().Add(lease)
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	defer tx.Rollback(ctx)

	var task domain.Task
	var requestHash []byte
	err = scanTask(tx.QueryRow(ctx, `SELECT `+taskColumnsWithHash+` FROM tasks
		WHERE id=$1 AND cancel_requested=false
		  AND (status='queued' OR (status IN ('submitted','polling') AND generation_id<>''))
		  AND (status<>'queued' OR queue_deadline_at IS NULL OR queue_deadline_at>now())
		  AND (generation_id='' OR upstream_deadline_at IS NULL OR upstream_deadline_at>now())
		  AND (execution_lease_id IS NULL OR execution_lease_expires_at<=now())
		FOR UPDATE`, id), &task, &requestHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, uuid.Nil, false, nil
	}
	if err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}

	if task.Status == domain.TaskQueued {
		if task.AccountID == nil {
			return domain.Task{}, uuid.Nil, false, errors.New("queued task has no reserved account")
		}
		var estimatedTokens int64
		if err := tx.QueryRow(ctx, `SELECT estimated_tokens FROM tasks WHERE id=$1`, id).Scan(&estimatedTokens); err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
		var pricingActive bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM tasks t JOIN model_cost_rules r ON r.id=t.pricing_rule_id
			WHERE t.id=$1 AND r.provider_id=t.provider_id AND r.enabled=true AND r.drifted=false
		)`, id).Scan(&pricingActive); err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
		if !pricingActive {
			if _, err := tx.Exec(ctx, `UPDATE tasks SET status='failed',progress=100,
				error_code='cost_rule_unavailable',error_message='price rule was disabled before upstream submission',
				completed_at=now(),updated_at=now() WHERE id=$1 AND status='queued'`, id); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
				reconciled_snapshot_version=0,release_reason='cost_rule_unavailable',released_at=now(),updated_at=now()
				WHERE task_id=$1 AND state='held'`, id); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if err := wakeQueuedTasksTx(ctx, tx, *task.AccountID, task.APIKeyID); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message)
				VALUES($1,'failed','price rule disabled before upstream submission')`, id); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			return task, uuid.Nil, false, nil
		}
		accountID := *task.AccountID
		preview, err := loadClaimAccount(ctx, tx, accountID, false)
		if err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
		var account claimAccountState
		if preview.routeable(task.ProviderID, time.Now()) {
			account, err = loadClaimAccount(ctx, tx, accountID, true)
			if err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
		} else {
			targetID, rerouteErr := rerouteQueuedTaskTx(ctx, tx, task, accountID, estimatedTokens, s.SchedulingPolicy)
			if rerouteErr == nil {
				accountID = targetID
				task.AccountID = &accountID
				account, err = loadClaimAccount(ctx, tx, accountID, false)
				if err != nil {
					return domain.Task{}, uuid.Nil, false, err
				}
			} else if errors.Is(rerouteErr, pgx.ErrNoRows) {
				if err := deferQueuedTaskTx(ctx, tx, id, "waiting for a healthy account with balance and execution capacity", 30*time.Second); err != nil {
					return domain.Task{}, uuid.Nil, false, err
				}
				if err := tx.Commit(ctx); err != nil {
					return domain.Task{}, uuid.Nil, false, err
				}
				return task, uuid.Nil, false, nil
			} else {
				return domain.Task{}, uuid.Nil, false, rerouteErr
			}
		}
		if !account.routeable(task.ProviderID, time.Now()) {
			if err := deferQueuedTaskTx(ctx, tx, id, "reserved account balance or session snapshot is not routeable", 30*time.Second); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			return task, uuid.Nil, false, nil
		}
		var earlierQueued, videoQueued bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM tasks earlier WHERE earlier.account_id=$1 AND earlier.status='queued' AND earlier.id<>$3
			  AND (earlier.api_key_id IS NULL OR EXISTS(
			    SELECT 1 FROM api_keys earlier_key WHERE earlier_key.id=earlier.api_key_id
			      AND earlier_key.enabled=true AND earlier_key.deleted_at IS NULL
			      AND (earlier_key.expires_at IS NULL OR earlier_key.expires_at>now())
			      AND (earlier_key.concurrency_limit<=0 OR (
			        SELECT count(*) FROM tasks key_active
			        WHERE key_active.api_key_id=earlier.api_key_id
			          AND key_active.status IN ('reserving','uploading','submitted','polling')
			      )<earlier_key.concurrency_limit)
			  ))
			  AND (
			    ($4='video' AND (
			      (earlier.kind='video' AND (earlier.created_at,earlier.id)<($2,$3))
			      OR (earlier.kind<>'video' AND earlier.created_at<=now()-interval '10 minutes'
			          AND (earlier.created_at,earlier.id)<($2,$3))
			    ))
			    OR ($4<>'video' AND (
			      (earlier.kind='video' AND $2>now()-interval '10 minutes')
			      OR (earlier.kind<>'video' AND (earlier.created_at,earlier.id)<($2,$3))
			    ))
			  )), EXISTS(
			SELECT 1 FROM tasks WHERE account_id=$1 AND status='queued' AND kind='video'
			)`, accountID, task.CreatedAt, task.ID, task.Kind).Scan(&earlierQueued, &videoQueued); err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
		capacityAvailable := account.ActiveTasks < account.Concurrency && !earlierQueued
		capacityReason := "account execution capacity is full"
		if task.Kind != "video" && account.RoutingRole == "video_reserved" && videoQueued {
			nonVideoLimit := account.Concurrency - account.VideoReservedSlots
			capacityAvailable = capacityAvailable && account.ActiveNonVideoTasks < nonVideoLimit
			if account.ActiveNonVideoTasks >= nonVideoLimit {
				capacityReason = "video-reserved execution capacity is waiting for video work"
			}
		}
		if earlierQueued {
			capacityReason = "an earlier account task is still queued"
		}
		if capacityAvailable && task.APIKeyID != nil {
			var keyConcurrency int
			if err := tx.QueryRow(ctx, `SELECT concurrency_limit FROM api_keys WHERE id=$1 FOR UPDATE`, *task.APIKeyID).Scan(&keyConcurrency); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if keyConcurrency > 0 {
				var keyActive int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks
					WHERE api_key_id=$1 AND status IN ('reserving','uploading','submitted','polling')`, *task.APIKeyID).Scan(&keyActive); err != nil {
					return domain.Task{}, uuid.Nil, false, err
				}
				capacityAvailable = keyActive < keyConcurrency
				capacityReason = "API key execution capacity is full"
			}
		}
		if !capacityAvailable {
			if err := deferQueuedTaskTx(ctx, tx, id, capacityReason, 30*time.Second); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			return task, uuid.Nil, false, nil
		}
		systemAvailable, systemReason, err := lockSystemExecution(ctx, tx)
		if err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
		if !systemAvailable {
			if err := deferQueuedTaskTx(ctx, tx, id, systemReason, 30*time.Second); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return domain.Task{}, uuid.Nil, false, err
			}
			return task, uuid.Nil, false, nil
		}
	}

	err = scanTask(tx.QueryRow(ctx, `UPDATE tasks
		SET status=CASE WHEN status='queued' THEN 'reserving' ELSE status END,
			progress=CASE WHEN status='queued' THEN GREATEST(progress,5) ELSE progress END,
			started_at=CASE WHEN status='queued' THEN COALESCE(started_at,now()) ELSE started_at END,
			execution_lease_id=$2,execution_lease_expires_at=$3,
			execution_attempt=execution_attempt+1,updated_at=now()
		WHERE id=$1 AND cancel_requested=false
		  AND (status='queued' OR (status IN ('submitted','polling') AND generation_id<>''))
		  AND (execution_lease_id IS NULL OR execution_lease_expires_at<=now())
		RETURNING `+taskColumnsWithHash, id, leaseID, expiresAt), &task, &requestHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, uuid.Nil, false, nil
	}
	if err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations
		SET expires_at=$2,updated_at=now()
		WHERE task_id=$1 AND state='held'`, id, expiresAt); err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1 AND delivered_at IS NULL`, id); err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	if task.AccountID != nil {
		if err := wakeQueuedTasksTx(ctx, tx, *task.AccountID, task.APIKeyID); err != nil {
			return domain.Task{}, uuid.Nil, false, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,$2,$3)`, id, task.Status, "worker claimed task lease"); err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, uuid.Nil, false, err
	}
	return task, leaseID, true, nil
}

func (s *Store) RenewTaskLease(ctx context.Context, id, leaseID uuid.UUID, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = 16 * time.Minute
	}
	expiresAt := time.Now().Add(lease)
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE tasks SET execution_lease_expires_at=$3,updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND status IN ('reserving','uploading','submitted','polling')`, id, leaseID, expiresAt)
	if err != nil || ct.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations
		SET expires_at=$2,updated_at=now()
		WHERE task_id=$1 AND state='held'`, id, expiresAt); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) YieldTaskLease(ctx context.Context, id, leaseID uuid.UUID, retryAt time.Time) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var accountID *uuid.UUID
	var apiKeyID *uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE tasks
		SET status=CASE WHEN generation_id='' THEN 'queued' ELSE 'polling' END,
			execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND (status IN ('reserving','uploading','polling')
		    OR (status='submitted' AND generation_id<>''))
		RETURNING account_id,api_key_id`, id, leaseID).Scan(&accountID, &apiKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations
		SET expires_at=GREATEST(expires_at,$2),updated_at=now()
		WHERE task_id=$1 AND state='held'`, id, retryAt.Add(20*time.Minute)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_outbox(task_id,kind,delivered_at,next_attempt_at)
		SELECT id,kind,NULL,$2 FROM tasks WHERE id=$1
		ON CONFLICT(task_id) DO UPDATE SET delivered_at=NULL,next_attempt_at=excluded.next_attempt_at,updated_at=now()`, id, retryAt); err != nil {
		return false, err
	}
	if accountID != nil {
		if err := wakeQueuedTasksTx(ctx, tx, *accountID, apiKeyID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UpdateTaskOwned(ctx context.Context, id, leaseID uuid.UUID, status string, progress int, accountID *uuid.UUID, generationID string, result any, code, message string) (bool, error) {
	return s.updateTaskOwned(ctx, id, leaseID, status, progress, accountID, generationID, result, code, message, nil, nil)
}

// PrepareTaskSubmissionOwned records the exact provider request and advances
// the leased task to submitted in the same transaction.
func (s *Store) PrepareTaskSubmissionOwned(ctx context.Context, id, leaseID, accountID uuid.UUID, upstreamRequest any) (bool, error) {
	return s.PrepareTaskSubmissionWithDeadlineOwned(ctx, id, leaseID, accountID, upstreamRequest, time.Now().Add(30*time.Minute))
}

func (s *Store) PrepareTaskSubmissionWithDeadlineOwned(ctx context.Context, id, leaseID, accountID uuid.UUID, upstreamRequest any, upstreamDeadline time.Time) (bool, error) {
	return s.updateTaskOwned(ctx, id, leaseID, domain.TaskSubmitted, 20, &accountID, "", nil, "", "", upstreamRequest, &upstreamDeadline)
}

func (s *Store) updateTaskOwned(ctx context.Context, id, leaseID uuid.UUID, status string, progress int, accountID *uuid.UUID, generationID string, result any, code, message string, upstreamRequest any, upstreamDeadline *time.Time) (bool, error) {
	resultJSON := []byte(`{}`)
	if result != nil {
		var err error
		resultJSON, err = json.Marshal(result)
		if err != nil {
			return false, err
		}
	}
	upstreamRequestJSON := []byte(`{}`)
	if upstreamRequest != nil {
		var err error
		upstreamRequestJSON, err = json.Marshal(upstreamRequest)
		if err != nil {
			return false, err
		}
	}
	terminal := status == domain.TaskSucceeded || status == domain.TaskFailed || status == domain.TaskCancelled || status == domain.TaskSubmissionUncertain
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE tasks SET status=$3,progress=$4,account_id=COALESCE($5,account_id),
		generation_id=CASE WHEN $6='' THEN generation_id ELSE $6 END,
		submitted_at=CASE WHEN $3='submitted' THEN COALESCE(submitted_at,now()) ELSE submitted_at END,
		result=CASE WHEN $7='{}'::jsonb THEN result ELSE $7 END,error_code=$8,error_message=$9,
		upstream_request=CASE WHEN $11='{}'::jsonb THEN upstream_request ELSE $11 END,
		upstream_deadline_at=COALESCE(upstream_deadline_at,$12),
		last_upstream_status_at=CASE WHEN $3 IN ('submitted','polling') AND $6<>'' THEN now() ELSE last_upstream_status_at END,
		unknown_status_count=CASE WHEN $3='polling' AND $6<>'' THEN unknown_status_count+1 ELSE unknown_status_count END,
		reconciliation_reason=CASE WHEN $3='submission_uncertain' THEN $9 ELSE reconciliation_reason END,
		completed_at=CASE WHEN $10 THEN now() ELSE completed_at END,
		execution_lease_id=CASE WHEN $10 THEN NULL ELSE execution_lease_id END,
		execution_lease_expires_at=CASE WHEN $10 THEN NULL ELSE execution_lease_expires_at END,
		updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND status NOT IN ('succeeded','failed','cancelled','submission_uncertain')
		  AND ($3<>'submitted' OR tasks.generation_id<>'' OR $6<>'' OR EXISTS(
			SELECT 1 FROM model_cost_rules r
			WHERE r.id=tasks.pricing_rule_id AND r.provider_id=tasks.provider_id
			  AND r.enabled=true AND r.drifted=false
		  ))`, id, leaseID, status, progress, accountID, generationID, resultJSON, code, message, terminal, upstreamRequestJSON, upstreamDeadline)
	if err != nil || ct.RowsAffected() == 0 {
		return false, err
	}
	if status == domain.TaskSubmitted && accountID != nil {
		if _, err := tx.Exec(ctx, `UPDATE accounts SET last_submitted_at=now(),updated_at=now() WHERE id=$1`, *accountID); err != nil {
			return false, err
		}
	}
	if terminal {
		if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
			return false, err
		}
		var terminalAccountID *uuid.UUID
		var terminalAPIKeyID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT account_id,api_key_id FROM tasks WHERE id=$1`, id).Scan(&terminalAccountID, &terminalAPIKeyID); err != nil {
			return false, err
		}
		if terminalAccountID != nil {
			if err := wakeQueuedTasksTx(ctx, tx, *terminalAccountID, terminalAPIKeyID); err != nil {
				return false, err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,$2,$3)`, id, status, message); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UpdateTaskTokensOwned(ctx context.Context, id, leaseID uuid.UUID, before, after *int64) (bool, error) {
	ct, err := s.DB.Exec(ctx, `UPDATE tasks
		SET tokens_before=COALESCE($3,tokens_before),tokens_after=COALESCE($4,tokens_after),updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND status IN ('reserving','uploading','submitted','polling')`, id, leaseID, before, after)
	return ct.RowsAffected() == 1, err
}

// RecordTaskSubmissionOwned atomically stores the upstream generation identity
// and the provider-reported cost. The reported value is observational only;
// reservation settlement continues to use the immutable local price rule.
func (s *Store) RecordTaskSubmissionOwned(ctx context.Context, id, leaseID, accountID uuid.UUID, generationID string, upstreamReportedCost *float64) (bool, error) {
	if generationID == "" {
		return false, errors.New("generation id is required")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE tasks SET status='submitted',progress=0,account_id=$3,
		generation_id=$4,upstream_reported_cost=COALESCE($5,upstream_reported_cost),updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND status='submitted'`, id, leaseID, accountID, generationID, upstreamReportedCost)
	if err != nil || ct.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'submitted',$2)`, id, "upstream submission accepted"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// CompleteTaskOwned records a completed upstream generation and atomically
// settles its held credits against the local account ledger.
func (s *Store) CompleteTaskOwned(ctx context.Context, id, leaseID uuid.UUID, accountID uuid.UUID, generationID string, result any, tokensAfter *int64, reconciled bool) (bool, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE tasks
		SET status='succeeded',progress=100,account_id=$3,
			generation_id=CASE WHEN $4='' THEN generation_id ELSE $4 END,
			result=$5,tokens_after=COALESCE($6,tokens_after),
			error_code='',error_message='',completed_at=now(),
			execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND status IN ('submitted','polling')`, id, leaseID, accountID, generationID, resultJSON, tokensAfter)
	if err != nil || ct.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `WITH consumed AS (
		UPDATE account_reservations
		SET state='consumed',settled_tokens=estimated_tokens,
			reconciled_snapshot_version=0,release_reason='local_ledger_consumed',
			released_at=now(),updated_at=now()
		WHERE task_id=$1 AND account_id=$2 AND state='held'
		RETURNING estimated_tokens
	), charge AS (
		SELECT COALESCE(sum(estimated_tokens),0)::bigint AS tokens FROM consumed
	)
	UPDATE accounts a SET
		subscription_tokens=GREATEST(0,a.subscription_tokens-charge.tokens),
		rollover_tokens=GREATEST(0,a.rollover_tokens-GREATEST(0,charge.tokens-a.subscription_tokens)),
		paid_tokens=GREATEST(0,a.paid_tokens-GREATEST(0,charge.tokens-a.subscription_tokens-a.rollover_tokens)),
		updated_at=now()
	FROM charge WHERE a.id=$2 AND charge.tokens>0`, id, accountID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
		return false, err
	}
	var apiKeyID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT api_key_id FROM tasks WHERE id=$1`, id).Scan(&apiKeyID); err != nil {
		return false, err
	}
	if err := wakeQueuedTasksTx(ctx, tx, accountID, apiKeyID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'succeeded',$2)`, id, "generation completed"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) FailTaskOwned(ctx context.Context, id, leaseID uuid.UUID, code, message string, details any, tokensAfter *int64, reconciled bool) (bool, error) {
	detailsJSON := []byte(`{}`)
	if details != nil {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return false, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var generationID string
	var accountID *uuid.UUID
	var apiKeyID *uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE tasks SET status='failed',progress=100,error_code=$3,error_message=$4,error_details=$5,
		tokens_after=COALESCE($6,tokens_after),
		completed_at=now(),execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND generation_id=''
		  AND status NOT IN ('succeeded','failed','cancelled','submission_uncertain')
		RETURNING generation_id,account_id,api_key_id`, id, leaseID, code, message, detailsJSON, tokensAfter).Scan(&generationID, &accountID, &apiKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
		reconciled_snapshot_version=0,release_reason=$2,released_at=now(),updated_at=now()
		WHERE task_id=$1 AND state='held'`, id, code); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
		return false, err
	}
	if accountID != nil {
		if err := wakeQueuedTasksTx(ctx, tx, *accountID, apiKeyID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'failed',$2)`, id, message); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// FailSubmittedTaskOwned is reserved for an explicit upstream terminal FAILED
// status. Transient authentication, polling and result-read errors must yield
// the lease or enter submission_uncertain without releasing the reservation.
func (s *Store) FailSubmittedTaskOwned(ctx context.Context, id, leaseID uuid.UUID, code, message string, details any) (bool, error) {
	detailsJSON := []byte(`{}`)
	if details != nil {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return false, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var accountID uuid.UUID
	var apiKeyID *uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE tasks SET status='failed',progress=100,error_code=$3,error_message=$4,error_details=$5,
		completed_at=now(),execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND generation_id<>'' AND status IN ('submitted','polling')
		RETURNING account_id,api_key_id`, id, leaseID, code, message, detailsJSON).Scan(&accountID, &apiKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
		reconciled_snapshot_version=0,release_reason=$2,released_at=now(),updated_at=now()
		WHERE task_id=$1 AND state='held'`, id, code); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
		return false, err
	}
	if err := wakeQueuedTasksTx(ctx, tx, accountID, apiKeyID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'failed',$2)`, id, message); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ConfirmSubmissionNotCreated records an administrator's reconciliation
// decision for a task whose upstream mutation result was unknown. The task and
// any held reservation are settled atomically so public reads cannot observe a
// failed task with credits still reserved.
func (s *Store) ConfirmSubmissionNotCreated(ctx context.Context, id uuid.UUID) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var accountID *uuid.UUID
	var apiKeyID *uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE tasks SET status='failed',progress=100,
		error_code='submission_not_created',
		error_message='administrator confirmed that the upstream task was not created',
		reconciliation_reason='administrator confirmed no upstream generation',
		completed_at=COALESCE(completed_at,now()),updated_at=now()
		WHERE id=$1 AND status='submission_uncertain'
		RETURNING account_id,api_key_id`, id).Scan(&accountID, &apiKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
		reconciled_snapshot_version=0,release_reason='submission_not_created',
		released_at=now(),updated_at=now()
		WHERE task_id=$1 AND state='held'`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
		return false, err
	}
	if accountID != nil {
		if err := wakeQueuedTasksTx(ctx, tx, *accountID, apiKeyID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message)
		VALUES($1,'failed','administrator confirmed that no upstream generation was created')`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) RecordTaskUpstreamStatusOwned(ctx context.Context, id, leaseID uuid.UUID, known bool) (bool, error) {
	ct, err := s.DB.Exec(ctx, `UPDATE tasks SET last_upstream_status_at=now(),
		unknown_status_count=CASE WHEN $3 THEN unknown_status_count ELSE unknown_status_count+1 END,
		updated_at=now()
		WHERE id=$1 AND execution_lease_id=$2 AND execution_lease_expires_at>now()
		  AND generation_id<>'' AND status IN ('submitted','polling')`, id, leaseID, known)
	return ct.RowsAffected() == 1, err
}

func (s *Store) CancelQueuedTaskAndRelease(ctx context.Context, id, keyID uuid.UUID) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var accountID uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE tasks SET status='cancelled',cancel_requested=true,progress=100,completed_at=now(),updated_at=now()
		WHERE id=$1 AND api_key_id=$2 AND status='queued'
		  AND (execution_lease_id IS NULL OR execution_lease_expires_at<=now())
		RETURNING account_id`, id, keyID).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
		reconciled_snapshot_version=0,release_reason='cancelled',released_at=now(),updated_at=now()
		WHERE task_id=$1 AND state='held'`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, id); err != nil {
		return false, err
	}
	if err := wakeQueuedTasksTx(ctx, tx, accountID, &keyID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'cancelled','cancelled before worker claim')`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ClaimDispatchableOutbox(ctx context.Context, limit int) ([]TaskOutboxMessage, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `WITH candidates AS (
		SELECT o.task_id,t.status IN ('submitted','polling') AS recovery FROM task_outbox o
		JOIN tasks t ON t.id=o.task_id
		WHERE o.delivered_at IS NULL AND o.next_attempt_at<=now()
		  AND ((t.status='queued' AND (t.execution_lease_id IS NULL OR t.execution_lease_expires_at<=now()))
		    OR (t.status IN ('submitted','polling') AND t.generation_id<>'' AND
		        (t.execution_lease_id IS NULL OR t.execution_lease_expires_at<=now())))
		ORDER BY o.next_attempt_at,o.created_at
		FOR UPDATE OF o SKIP LOCKED
		LIMIT $1
	)
	UPDATE task_outbox o SET last_attempt_at=now(),next_attempt_at=now()+interval '30 seconds',attempt_count=o.attempt_count+1,updated_at=now()
	FROM candidates c WHERE o.task_id=c.task_id
	RETURNING o.task_id,o.kind,o.attempt_count,c.recovery`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TaskOutboxMessage, 0, limit)
	for rows.Next() {
		var item TaskOutboxMessage
		if err := rows.Scan(&item.TaskID, &item.Kind, &item.AttemptCount, &item.Recovery); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordOutboxError(ctx context.Context, taskID uuid.UUID, message string, retryAfter time.Duration) error {
	if retryAfter <= 0 {
		retryAfter = 5 * time.Second
	}
	_, err := s.DB.Exec(ctx, `UPDATE task_outbox SET last_error=$2,next_attempt_at=$3,updated_at=now() WHERE task_id=$1 AND delivered_at IS NULL`, taskID, message, time.Now().Add(retryAfter))
	return err
}
