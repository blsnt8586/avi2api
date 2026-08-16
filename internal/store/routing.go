package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

var (
	ErrInsufficientPoolBalance = errors.New("insufficient pool balance")
	ErrAccountQueueCapacity    = errors.New("account queue capacity exhausted")
	ErrAPIKeyCapacity          = errors.New("API key capacity exhausted")
	ErrNoHealthyAccount        = errors.New("no healthy provider account")
	ErrCostRuleUnavailable     = errors.New("cost rule is no longer available")
	ErrSubmissionCircuitOpen   = errors.New("provider submission uncertainty budget exhausted")
	ErrSystemMaintenance       = errors.New("system is in maintenance drain mode")
	ErrSystemQueueCapacity     = errors.New("system queue capacity exhausted")
	ErrSystemOverloaded        = errors.New("system queue overload protection is active")
)

const queuedReservationTTL = 24 * time.Hour

type taskScanner interface{ Scan(...any) error }

func scanTask(row taskScanner, task *domain.Task, requestHash *[]byte) error {
	return row.Scan(&task.ID, &task.ProviderID, &task.APIKeyID, &task.AccountID, &task.Kind, &task.Status, &task.Progress, &task.Model, &task.Prompt, &task.Request, &task.UpstreamRequest, &task.GenerationID, &task.Result, &task.ErrorCode, &task.ErrorMessage, &task.ErrorDetails, &task.RetryCount, &task.TokensBefore, &task.TokensAfter, &task.CancelRequested, &task.CreatedAt, &task.UpdatedAt, &task.StartedAt, &task.CompletedAt, &task.QueueDeadlineAt, &task.UpstreamDeadlineAt, &task.LastUpstreamStatusAt, &task.UnknownStatusCount, &task.ReconciliationReason, requestHash)
}

const taskColumnsWithHash = `id,provider_id,api_key_id,account_id,kind,status,progress,model,prompt,request,upstream_request,generation_id,result,error_code,error_message,error_details,retry_count,tokens_before,tokens_after,cancel_requested,created_at,updated_at,started_at,completed_at,queue_deadline_at,upstream_deadline_at,last_upstream_status_at,unknown_status_count,reconciliation_reason,request_hash`

func (s *Store) GetIdempotentTask(ctx context.Context, keyID uuid.UUID, kind string, request any, idempotencyKey string) (domain.Task, error) {
	return s.GetIdempotentTaskWithHashRequest(ctx, keyID, kind, request, idempotencyKey)
}

func (s *Store) GetIdempotentTaskWithHashRequest(ctx context.Context, keyID uuid.UUID, kind string, hashRequest any, idempotencyKey string) (domain.Task, error) {
	body, err := json.Marshal(hashRequest)
	if err != nil {
		return domain.Task{}, err
	}
	hash := sha256.Sum256(append([]byte(kind+":"), body...))
	var task domain.Task
	var storedHash []byte
	err = scanTask(s.DB.QueryRow(ctx, `SELECT `+taskColumnsWithHash+` FROM tasks WHERE api_key_id=$1 AND idempotency_key=$2`, keyID, idempotencyKey), &task, &storedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err != nil {
		return domain.Task{}, err
	}
	if string(storedHash) != string(hash[:]) {
		return domain.Task{}, fmt.Errorf("idempotency key reused with different payload")
	}
	return task, nil
}

func (s *Store) CreateReservedTask(ctx context.Context, keyID uuid.UUID, kind, model, prompt string, request any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	return s.CreateReservedTaskWithHashRequest(ctx, keyID, kind, model, prompt, request, request, idempotencyKey, estimatedTokens, pricingRuleID, lease)
}

func (s *Store) CreateReservedTaskWithHashRequest(ctx context.Context, keyID uuid.UUID, kind, model, prompt string, request, hashRequest any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	return s.createReservedTaskForProvider(ctx, keyID, "leonardo", kind, model, prompt, request, hashRequest, idempotencyKey, estimatedTokens, pricingRuleID, lease)
}

func (s *Store) CreateReservedTaskForProvider(ctx context.Context, keyID uuid.UUID, providerID, kind, model, prompt string, request any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	return s.createReservedTaskForProvider(ctx, keyID, providerID, kind, model, prompt, request, request, idempotencyKey, estimatedTokens, pricingRuleID, lease)
}

func (s *Store) CreateReservedTaskForProviderWithHashRequest(ctx context.Context, keyID uuid.UUID, providerID, kind, model, prompt string, request, hashRequest any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	return s.createReservedTaskForProvider(ctx, keyID, providerID, kind, model, prompt, request, hashRequest, idempotencyKey, estimatedTokens, pricingRuleID, lease)
}

func (s *Store) createReservedTaskForProvider(ctx context.Context, keyID uuid.UUID, providerID, kind, model, prompt string, request, hashRequest any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		task, created, err := s.createReservedTaskForProviderOnce(ctx, keyID, providerID, kind, model, prompt, request, hashRequest, idempotencyKey, estimatedTokens, pricingRuleID, lease)
		if err == nil || !retryableClaimTransactionError(err) || attempt == maxAttempts-1 {
			return task, created, err
		}
		delay := (10 * time.Millisecond) << attempt
		delay += time.Duration(keyID[0]%10) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return domain.Task{}, false, ctx.Err()
		case <-timer.C:
		}
	}
	return domain.Task{}, false, errors.New("task admission retry loop exhausted")
}

func (s *Store) createReservedTaskForProviderOnce(ctx context.Context, keyID uuid.UUID, providerID, kind, model, prompt string, request, hashRequest any, idempotencyKey string, estimatedTokens, pricingRuleID int64, lease time.Duration) (domain.Task, bool, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return domain.Task{}, false, err
	}
	hashBody, err := json.Marshal(hashRequest)
	if err != nil {
		return domain.Task{}, false, err
	}
	hash := sha256.Sum256(append([]byte(kind+":"), hashBody...))
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Task{}, false, err
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		lockKey := keyID.String() + ":" + idempotencyKey
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return domain.Task{}, false, err
		}
		var existing domain.Task
		var existingHash []byte
		err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumnsWithHash+` FROM tasks WHERE api_key_id=$1 AND idempotency_key=$2`, keyID, idempotencyKey), &existing, &existingHash)
		if err == nil {
			if string(existingHash) != string(hash[:]) {
				return domain.Task{}, false, fmt.Errorf("idempotency key reused with different payload")
			}
			if err := tx.Commit(ctx); err != nil {
				return domain.Task{}, false, err
			}
			return existing, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Task{}, false, err
		}
	}
	var providerUncertain int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks uncertain
		JOIN account_reservations uncertain_reservation ON uncertain_reservation.task_id=uncertain.id AND uncertain_reservation.state='held'
		WHERE uncertain.provider_id=$1 AND uncertain.status='submission_uncertain'`, providerID).Scan(&providerUncertain); err != nil {
		return domain.Task{}, false, err
	}
	if providerUncertain >= s.SchedulingPolicy.UncertainProviderLimit {
		return domain.Task{}, false, ErrSubmissionCircuitOpen
	}
	var costRuleExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM model_cost_rules WHERE id=$1 AND provider_id=$2 AND enabled=true AND drifted=false
		FOR KEY SHARE)`, pricingRuleID, providerID).Scan(&costRuleExists); err != nil {
		return domain.Task{}, false, err
	}
	if !costRuleExists {
		return domain.Task{}, false, ErrCostRuleUnavailable
	}
	var accountID uuid.UUID
	candidateSQL := `WITH usage AS (
		SELECT r.account_id,COALESCE(sum(r.estimated_tokens),0) AS reserved,
			count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS slots,
			count(*) AS total
		FROM account_reservations r JOIN tasks t ON t.id=r.task_id
		WHERE r.state='held' GROUP BY r.account_id
	)
	SELECT a.id FROM accounts a LEFT JOIN usage u ON u.account_id=a.id
	WHERE a.archived_at IS NULL AND a.provider_id=$2 AND a.status='active' AND (a.cooldown_until IS NULL OR a.cooldown_until<=now())
	  AND a.access_token_expires_at IS NOT NULL AND a.access_token_expires_at>now()
	  AND a.last_checked_at IS NOT NULL
	  AND (SELECT count(*) FROM tasks uncertain
	       JOIN account_reservations uncertain_reservation ON uncertain_reservation.task_id=uncertain.id AND uncertain_reservation.state='held'
	       WHERE uncertain.account_id=a.id AND uncertain.status='submission_uncertain')<$5
	  AND (SELECT count(*) FROM tasks uncertain
	       JOIN account_reservations uncertain_reservation ON uncertain_reservation.task_id=uncertain.id AND uncertain_reservation.state='held'
	       JOIN accounts uncertain_account ON uncertain_account.id=uncertain.account_id
	       WHERE uncertain.provider_id=$2 AND uncertain.status='submission_uncertain'
	         AND COALESCE(uncertain_account.proxy_url,'')=COALESCE(a.proxy_url,''))<$6
	  AND COALESCE(u.total,0)<a.image_concurrency+a.queue_capacity-
	    CASE WHEN $4<>'video' AND a.routing_role='video_reserved' THEN a.video_reserved_slots ELSE 0 END
	  AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)-
	    CASE WHEN $4<>'video'
	      AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)>=a.protected_tokens
	      THEN a.protected_tokens ELSE 0 END >=$1
	  AND NOT (a.id=ANY($3::uuid[]))
	ORDER BY CASE WHEN $4='video' THEN CASE WHEN a.routing_role='video_reserved' THEN 0 ELSE 1 END
	              ELSE CASE WHEN a.routing_role='general' THEN 0 ELSE 1 END END ASC,
	  COALESCE(u.slots,0)::numeric/a.image_concurrency ASC,
	  COALESCE(u.total,0)::numeric/(a.image_concurrency+a.queue_capacity) ASC,
	  COALESCE(a.last_submitted_at,'epoch'::timestamptz) ASC,
	  a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)-
	    CASE WHEN $4<>'video'
	      AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)>=a.protected_tokens
	      THEN a.protected_tokens ELSE 0 END-$1 ASC,
	  CASE WHEN a.last_error='' THEN 0 ELSE 1 END ASC,a.id`
	skipLockedSQL := candidateSQL + ` FOR UPDATE OF a SKIP LOCKED LIMIT 1`
	var candidateErr error = pgx.ErrNoRows
	excluded := make([]uuid.UUID, 0)
	for {
		if _, err := tx.Exec(ctx, `SAVEPOINT account_candidate`); err != nil {
			return domain.Task{}, false, err
		}
		accountID = uuid.Nil
		for attempt := 0; attempt < 10; attempt++ {
			candidateErr = tx.QueryRow(ctx, skipLockedSQL, estimatedTokens, providerID, excluded, kind, s.SchedulingPolicy.UncertainAccountLimit, s.SchedulingPolicy.UncertainProxyLimit).Scan(&accountID)
			if candidateErr == nil || !errors.Is(candidateErr, pgx.ErrNoRows) {
				break
			}
			select {
			case <-ctx.Done():
				return domain.Task{}, false, ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
		if errors.Is(candidateErr, pgx.ErrNoRows) {
			candidateErr = tx.QueryRow(ctx, candidateSQL+` FOR UPDATE OF a LIMIT 1`, estimatedTokens, providerID, excluded, kind, s.SchedulingPolicy.UncertainAccountLimit, s.SchedulingPolicy.UncertainProxyLimit).Scan(&accountID)
		}
		if candidateErr != nil {
			_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT account_candidate`)
			break
		}
		var reserved int64
		var reservationCount int
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(estimated_tokens),0)
			FROM account_reservations WHERE account_id=$1 AND state='held'`, accountID).Scan(&reservationCount, &reserved); err != nil {
			return domain.Task{}, false, err
		}
		var balance, protectedTokens int64
		var concurrency, queueCapacity, videoReservedSlots int
		var routingRole string
		if err := tx.QueryRow(ctx, `SELECT subscription_tokens+rollover_tokens+paid_tokens,
			image_concurrency,queue_capacity,routing_role,protected_tokens,video_reserved_slots
			FROM accounts WHERE id=$1`, accountID).Scan(&balance, &concurrency, &queueCapacity, &routingRole, &protectedTokens, &videoReservedSlots); err != nil {
			return domain.Task{}, false, err
		}
		available := balance - reserved
		spendable := available
		capacity := concurrency + queueCapacity
		if kind != "video" {
			if available >= protectedTokens {
				spendable -= protectedTokens
			}
			if routingRole == "video_reserved" {
				capacity -= videoReservedSlots
			}
		}
		if reservationCount < capacity && spendable >= estimatedTokens {
			if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT account_candidate`); err != nil {
				return domain.Task{}, false, err
			}
			break
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT account_candidate`); err != nil {
			return domain.Task{}, false, err
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT account_candidate`); err != nil {
			return domain.Task{}, false, err
		}
		excluded = append(excluded, accountID)
		candidateErr = pgx.ErrNoRows
	}
	if candidateErr != nil {
		if !errors.Is(candidateErr, pgx.ErrNoRows) {
			return domain.Task{}, false, candidateErr
		}
		var healthy int
		var enoughBalance bool
		diagnosticSQL := `WITH usage AS (
			SELECT account_id,COALESCE(sum(estimated_tokens),0) AS reserved,count(*) AS total
			FROM account_reservations WHERE state='held' GROUP BY account_id
		)
		SELECT count(*),COALESCE(bool_or(
		  a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)-
		  CASE WHEN $3<>'video'
		    AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(u.reserved,0)>=a.protected_tokens
		    THEN a.protected_tokens ELSE 0 END >=$1),false)
		FROM accounts a LEFT JOIN usage u ON u.account_id=a.id
		WHERE a.archived_at IS NULL AND a.provider_id=$2 AND a.status='active' AND (a.cooldown_until IS NULL OR a.cooldown_until<=now())
		  AND a.access_token_expires_at IS NOT NULL AND a.access_token_expires_at>now()
		  AND a.last_checked_at IS NOT NULL`
		if scanErr := tx.QueryRow(ctx, diagnosticSQL, estimatedTokens, providerID, kind).Scan(&healthy, &enoughBalance); scanErr != nil {
			return domain.Task{}, false, scanErr
		}
		if healthy == 0 {
			return domain.Task{}, false, ErrNoHealthyAccount
		}
		if !enoughBalance {
			return domain.Task{}, false, ErrInsufficientPoolBalance
		}
		return domain.Task{}, false, ErrAccountQueueCapacity
	}

	// ClaimTask locks account -> API key -> system capacity. Admission must use
	// the same order so task creation cannot deadlock with a worker claim.
	var keyConcurrency, keyNonTerminal int
	if err := tx.QueryRow(ctx, `SELECT concurrency_limit FROM api_keys
		WHERE id=$1 AND enabled=true AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>now())
		FOR UPDATE`, keyID).Scan(&keyConcurrency); err != nil {
		return domain.Task{}, false, err
	}
	if keyConcurrency < 1 {
		keyConcurrency = 1
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE api_key_id=$1
		AND status IN ('queued','reserving','uploading','submitted','polling')`, keyID).Scan(&keyNonTerminal); err != nil {
		return domain.Task{}, false, err
	}
	if keyNonTerminal >= keyConcurrency*(1+s.SchedulingPolicy.APIKeyQueueMultiplier) {
		return domain.Task{}, false, ErrAPIKeyCapacity
	}

	capacityConfig, err := lockSystemAdmission(ctx, tx)
	if err != nil {
		return domain.Task{}, false, err
	}
	id := uuid.New()
	var task domain.Task
	var storedHash []byte
	insert := `INSERT INTO tasks(id,provider_id,api_key_id,account_id,kind,status,model,prompt,request,request_hash,idempotency_key,estimated_tokens,pricing_rule_id,queue_deadline_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14) RETURNING ` + taskColumnsWithHash
	queueDeadline := time.Now().Add(time.Duration(capacityConfig.QueueTimeoutSeconds) * time.Second)
	if err := scanTask(tx.QueryRow(ctx, insert, id, providerID, keyID, accountID, kind, domain.TaskQueued, model, prompt, body, hash[:], idempotencyKey, estimatedTokens, pricingRuleID, queueDeadline), &task, &storedHash); err != nil {
		return domain.Task{}, false, err
	}
	if lease < queuedReservationTTL {
		lease = queuedReservationTTL
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_reservations(task_id,account_id,estimated_tokens,expires_at) VALUES($1,$2,$3,$4)`, id, accountID, estimatedTokens, time.Now().Add(lease)); err != nil {
		return domain.Task{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_outbox(task_id,kind) VALUES($1,$2)`, id, kind); err != nil {
		return domain.Task{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'queued','task admitted and credits reserved')`, id); err != nil {
		return domain.Task{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, false, err
	}
	task, err = s.GetTask(ctx, id)
	return task, true, err
}

func (s *Store) ReleaseReservation(ctx context.Context, taskID uuid.UUID, reason string) error {
	_, err := s.DB.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
		reconciled_snapshot_version=0,release_reason=$2,released_at=now(),updated_at=now()
		WHERE task_id=$1 AND state='held'`, taskID, reason)
	return err
}

func (s *Store) ConsumeTerminalReservationsForAccount(ctx context.Context, accountID uuid.UUID) error {
	_, err := s.ReconcileTerminalReservationsForAccount(ctx, accountID)
	return err
}

func (s *Store) ReservationForTask(ctx context.Context, taskID uuid.UUID) (domain.AccountReservation, error) {
	var reservation domain.AccountReservation
	err := s.DB.QueryRow(ctx, `SELECT task_id,account_id,estimated_tokens,settled_tokens,reconciled_snapshot_version,
		state,expires_at,release_reason,created_at,updated_at,released_at
		FROM account_reservations WHERE task_id=$1`, taskID).Scan(&reservation.TaskID, &reservation.AccountID,
		&reservation.EstimatedTokens, &reservation.SettledTokens, &reservation.ReconciledSnapshotVersion,
		&reservation.State, &reservation.ExpiresAt, &reservation.ReleaseReason, &reservation.CreatedAt,
		&reservation.UpdatedAt, &reservation.ReleasedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return reservation, ErrNotFound
	}
	return reservation, err
}

func (s *Store) ListAdminTasks(ctx context.Context, limit int) ([]domain.AdminTask, error) {
	tasks, err := s.ListTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	return s.adminTasksWithReservationDetails(ctx, tasks)
}

func (s *Store) ListAdminTasksPage(ctx context.Context, page, pageSize int) ([]domain.AdminTask, int64, error) {
	return s.ListAdminTasksPageFiltered(ctx, page, pageSize, TaskPageFilter{})
}

func (s *Store) ListAdminTasksPageFiltered(ctx context.Context, page, pageSize int, filter TaskPageFilter) ([]domain.AdminTask, int64, error) {
	tasks, total, err := s.ListTasksPageFiltered(ctx, page, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	adminTasks, err := s.adminTasksWithReservationDetails(ctx, tasks)
	return adminTasks, total, err
}

func (s *Store) GetAdminTask(ctx context.Context, id uuid.UUID) (domain.AdminTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return domain.AdminTask{}, err
	}
	tasks, err := s.adminTasksWithReservationDetails(ctx, []domain.Task{task})
	if err != nil {
		return domain.AdminTask{}, err
	}
	if len(tasks) != 1 {
		return domain.AdminTask{}, ErrNotFound
	}
	events, err := s.ListTaskEvents(ctx, id)
	if err != nil {
		return domain.AdminTask{}, err
	}
	tasks[0].Events = events
	return tasks[0], nil
}

func (s *Store) ListTaskEvents(ctx context.Context, id uuid.UUID) ([]domain.TaskEvent, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,status,message,created_at FROM task_events WHERE task_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]domain.TaskEvent, 0)
	for rows.Next() {
		var event domain.TaskEvent
		if err := rows.Scan(&event.ID, &event.Status, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) adminTasksWithReservationDetails(ctx context.Context, tasks []domain.Task) ([]domain.AdminTask, error) {
	if len(tasks) == 0 {
		return []domain.AdminTask{}, nil
	}
	details := make(map[uuid.UUID]struct {
		estimated int64
		settled   *int64
		upstream  *float64
		state     string
		reason    string
	}, len(tasks))
	ids := make([]uuid.UUID, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	rows, err := s.DB.Query(ctx, `SELECT t.id,t.estimated_tokens,r.settled_tokens,t.upstream_reported_cost,
		COALESCE(r.state,''),COALESCE(r.release_reason,'')
		FROM tasks t LEFT JOIN account_reservations r ON r.task_id=t.id
		WHERE t.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var detail struct {
			estimated int64
			settled   *int64
			upstream  *float64
			state     string
			reason    string
		}
		if err := rows.Scan(&id, &detail.estimated, &detail.settled, &detail.upstream, &detail.state, &detail.reason); err != nil {
			return nil, err
		}
		details[id] = detail
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.AdminTask, 0, len(tasks))
	for _, task := range tasks {
		detail := details[task.ID]
		adminTask := domain.AdminTask{Task: task, ProviderID: task.ProviderID,
			EstimatedTokens: detail.estimated, SettledTokens: detail.settled,
			UpstreamReportedCost: detail.upstream,
			ReservationState:     detail.state, ReservationReleaseReason: detail.reason}
		if len(task.UpstreamRequest) > 2 {
			adminTask.UpstreamRequest = task.UpstreamRequest
		}
		out = append(out, adminTask)
	}
	return out, nil
}
