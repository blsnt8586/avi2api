package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"strings"
	"time"
)

func (s *Store) GetTask(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	var t domain.Task
	err := s.DB.QueryRow(ctx, `SELECT id,provider_id,api_key_id,account_id,kind,status,progress,model,prompt,request,upstream_request,generation_id,result,error_code,error_message,error_details,retry_count,tokens_before,tokens_after,cancel_requested,created_at,updated_at,started_at,completed_at,queue_deadline_at,upstream_deadline_at,last_upstream_status_at,unknown_status_count,reconciliation_reason FROM tasks WHERE id=$1`, id).Scan(
		&t.ID, &t.ProviderID, &t.APIKeyID, &t.AccountID, &t.Kind, &t.Status, &t.Progress, &t.Model, &t.Prompt, &t.Request, &t.UpstreamRequest, &t.GenerationID, &t.Result, &t.ErrorCode, &t.ErrorMessage, &t.ErrorDetails, &t.RetryCount, &t.TokensBefore, &t.TokensAfter, &t.CancelRequested, &t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt, &t.QueueDeadlineAt, &t.UpstreamDeadlineAt, &t.LastUpstreamStatusAt, &t.UnknownStatusCount, &t.ReconciliationReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err == nil {
		tasks := []domain.Task{t}
		err = s.populateTaskQueuePositions(ctx, tasks)
		t = tasks[0]
	}
	return t, err
}

func (s *Store) GetPublicTask(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	var t domain.Task
	err := s.DB.QueryRow(ctx, `SELECT id,provider_id,api_key_id,account_id,kind,status,progress,model,prompt,
		generation_id,result,error_code,error_message,error_details,retry_count,cancel_requested,
		created_at,updated_at,started_at,completed_at,queue_deadline_at,upstream_deadline_at,
		last_upstream_status_at,unknown_status_count,reconciliation_reason
		FROM tasks WHERE id=$1`, id).Scan(
		&t.ID, &t.ProviderID, &t.APIKeyID, &t.AccountID, &t.Kind, &t.Status, &t.Progress, &t.Model, &t.Prompt,
		&t.GenerationID, &t.Result, &t.ErrorCode, &t.ErrorMessage, &t.ErrorDetails, &t.RetryCount, &t.CancelRequested,
		&t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt, &t.QueueDeadlineAt, &t.UpstreamDeadlineAt,
		&t.LastUpstreamStatusAt, &t.UnknownStatusCount, &t.ReconciliationReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err == nil {
		tasks := []domain.Task{t}
		err = s.populateTaskQueuePositions(ctx, tasks)
		t = tasks[0]
	}
	return t, err
}

func (s *Store) ListTasks(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	return s.listTasksPage(ctx, limit, 0)
}

func (s *Store) GetTaskOverview(ctx context.Context) (TaskOverview, error) {
	overview := TaskOverview{Counts: map[string]int{}}
	rows, err := s.DB.Query(ctx, `SELECT status,count(*) FROM tasks
		WHERE status IN ('queued','reserving','uploading','submitted','polling')
		   OR status='submission_uncertain'
		   OR (status IN ('succeeded','failed') AND completed_at>=now()-interval '1 hour')
		GROUP BY status`)
	if err != nil {
		return TaskOverview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return TaskOverview{}, err
		}
		overview.Counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return TaskOverview{}, err
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status='failed' AND completed_at>=now()-interval '1 hour') FROM tasks`).Scan(&overview.Total, &overview.FailedLastHour); err != nil {
		return TaskOverview{}, err
	}
	return overview, nil
}

func (s *Store) ListTasksPage(ctx context.Context, page, pageSize int) ([]domain.Task, int64, error) {
	return s.ListTasksPageFiltered(ctx, page, pageSize, TaskPageFilter{})
}

func (s *Store) ListTasksPageFiltered(ctx context.Context, page, pageSize int, filter TaskPageFilter) ([]domain.Task, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Kind = strings.TrimSpace(filter.Kind)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.ProviderID = strings.TrimSpace(filter.ProviderID)
	const where = ` WHERE ($1='' OR t.id::text ILIKE '%'||$1||'%' OR t.prompt ILIKE '%'||$1||'%' OR t.model ILIKE '%'||$1||'%' OR t.provider_id ILIKE '%'||$1||'%')
		AND ($2='' OR ($2='active' AND t.status=ANY(ARRAY['reserving','uploading','submitted','polling'])) OR ($2<>'active' AND t.status=$2))
		AND ($3='' OR t.kind=$3) AND ($4='' OR t.model=$4)
		AND ($5='' OR t.provider_id=$5)
		AND ($6::timestamptz IS NULL OR t.created_at>=$6) AND ($7::timestamptz IS NULL OR t.created_at<$7)`
	args := []any{filter.Search, filter.Status, filter.Kind, filter.Model, filter.ProviderID, filter.CreatedFrom, filter.CreatedTo}
	var total int64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM tasks t`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id,provider_id,api_key_id,account_id,kind,status,progress,model,prompt,request,generation_id,result,error_code,error_message,error_details,retry_count,tokens_before,tokens_after,cancel_requested,created_at,updated_at,started_at,completed_at,queue_deadline_at,upstream_deadline_at,last_upstream_status_at,unknown_status_count,reconciliation_reason FROM tasks t`+where+` ORDER BY created_at DESC,id DESC LIMIT $8 OFFSET $9`, append(args, pageSize, int64(page-1)*int64(pageSize))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0, pageSize)
	for rows.Next() {
		var t domain.Task
		if err := rows.Scan(&t.ID, &t.ProviderID, &t.APIKeyID, &t.AccountID, &t.Kind, &t.Status, &t.Progress, &t.Model, &t.Prompt, &t.Request, &t.GenerationID, &t.Result, &t.ErrorCode, &t.ErrorMessage, &t.ErrorDetails, &t.RetryCount, &t.TokensBefore, &t.TokensAfter, &t.CancelRequested, &t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt, &t.QueueDeadlineAt, &t.UpstreamDeadlineAt, &t.LastUpstreamStatusAt, &t.UnknownStatusCount, &t.ReconciliationReason); err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := s.populateTaskQueuePositions(ctx, tasks); err != nil {
		return nil, 0, err
	}
	return tasks, total, err
}

func (s *Store) listTasksPage(ctx context.Context, limit int, offset int64) ([]domain.Task, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,provider_id,api_key_id,account_id,kind,status,progress,model,prompt,request,generation_id,result,error_code,error_message,error_details,retry_count,tokens_before,tokens_after,cancel_requested,created_at,updated_at,started_at,completed_at,queue_deadline_at,upstream_deadline_at,last_upstream_status_at,unknown_status_count,reconciliation_reason FROM tasks ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Task, 0, limit)
	for rows.Next() {
		var t domain.Task
		if err := rows.Scan(&t.ID, &t.ProviderID, &t.APIKeyID, &t.AccountID, &t.Kind, &t.Status, &t.Progress, &t.Model, &t.Prompt, &t.Request, &t.GenerationID, &t.Result, &t.ErrorCode, &t.ErrorMessage, &t.ErrorDetails, &t.RetryCount, &t.TokensBefore, &t.TokensAfter, &t.CancelRequested, &t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt, &t.QueueDeadlineAt, &t.UpstreamDeadlineAt, &t.LastUpstreamStatusAt, &t.UnknownStatusCount, &t.ReconciliationReason); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.populateTaskQueuePositions(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) populateTaskQueuePositions(ctx context.Context, tasks []domain.Task) error {
	byID := make(map[uuid.UUID]*domain.Task, len(tasks))
	ids := make([]uuid.UUID, 0, len(tasks))
	for i := range tasks {
		if tasks[i].Status == domain.TaskQueued && tasks[i].AccountID != nil {
			byID[tasks[i].ID] = &tasks[i]
			ids = append(ids, tasks[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.DB.Query(ctx, `SELECT t.id,1+(
		SELECT count(*) FROM tasks earlier
		WHERE earlier.account_id=t.account_id AND earlier.status='queued'
		  AND (earlier.created_at,earlier.id)<(t.created_at,t.id)
	) FROM tasks t WHERE t.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var position int
		if err := rows.Scan(&id, &position); err != nil {
			return err
		}
		if task := byID[id]; task != nil {
			task.QueuePosition = &position
		}
	}
	return rows.Err()
}

func (s *Store) ClearTerminalTaskSourceImage(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE tasks
		SET request=request-'source_image'-'source_images'-'reference_images'-'start_frame'-'end_frame'-'reference_videos'-'reference_audio'-'reference_audios',updated_at=now()
		WHERE id=$1 AND status IN ('succeeded','failed','cancelled','submission_uncertain')
		  AND (request ? 'source_image' OR request ? 'source_images' OR request ? 'reference_images'
		       OR request ? 'start_frame' OR request ? 'end_frame' OR request ? 'reference_videos' OR request ? 'reference_audio' OR request ? 'reference_audios')`, id)
	return err
}

type RecoveryTask struct {
	ID     uuid.UUID
	Kind   string
	Status string
}

// RecoverStaleTasks only touches expired execution leases. A task that was
// never picked up may remain queued until its reservation expires, at which
// point it is failed and released instead of being submitted without capacity.

func (s *Store) RecoverStaleTasks(ctx context.Context, _ time.Time) ([]RecoveryTask, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var out []RecoveryTask

	type expiredQueuedTask struct {
		id        uuid.UUID
		kind      string
		accountID uuid.UUID
		apiKeyID  *uuid.UUID
	}
	deadlineRows, err := tx.Query(ctx, `UPDATE tasks SET status='failed',progress=100,
		error_code='queue_timeout',error_message='task exceeded its queue deadline before submission',
		completed_at=now(),updated_at=now()
		WHERE status='queued' AND queue_deadline_at IS NOT NULL AND queue_deadline_at<=now()
		  AND (execution_lease_id IS NULL OR execution_lease_expires_at<=now())
		RETURNING id,kind,account_id,api_key_id`)
	if err != nil {
		return nil, err
	}
	var expiredQueued []expiredQueuedTask
	for deadlineRows.Next() {
		var task expiredQueuedTask
		if err := deadlineRows.Scan(&task.id, &task.kind, &task.accountID, &task.apiKeyID); err != nil {
			deadlineRows.Close()
			return nil, err
		}
		expiredQueued = append(expiredQueued, task)
	}
	if err := deadlineRows.Err(); err != nil {
		deadlineRows.Close()
		return nil, err
	}
	deadlineRows.Close()
	for _, task := range expiredQueued {
		if _, err := tx.Exec(ctx, `UPDATE account_reservations SET state='released',settled_tokens=0,
			reconciled_snapshot_version=0,release_reason='queue_timeout',released_at=now(),updated_at=now()
			WHERE task_id=$1 AND state='held'`, task.id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, task.id); err != nil {
			return nil, err
		}
		if err := wakeQueuedTasksTx(ctx, tx, task.accountID, task.apiKeyID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'failed','queue deadline exceeded')`, task.id); err != nil {
			return nil, err
		}
		out = append(out, RecoveryTask{ID: task.id, Kind: task.kind, Status: domain.TaskFailed})
	}

	upstreamDeadlineRows, err := tx.Query(ctx, `UPDATE tasks SET status='submission_uncertain',progress=100,
		error_code='upstream_deadline_exceeded',error_message='upstream generation did not reach a verifiable terminal result before its deadline',
		reconciliation_reason='upstream_deadline_exceeded',completed_at=now(),
		execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE status IN ('submitted','polling') AND generation_id<>''
		  AND upstream_deadline_at IS NOT NULL AND upstream_deadline_at<=now()
		RETURNING id,kind,account_id,api_key_id`)
	if err != nil {
		return nil, err
	}
	type upstreamExpiredTask struct {
		RecoveryTask
		accountID uuid.UUID
		apiKeyID  *uuid.UUID
	}
	var upstreamExpired []upstreamExpiredTask
	for upstreamDeadlineRows.Next() {
		var task upstreamExpiredTask
		if err := upstreamDeadlineRows.Scan(&task.ID, &task.Kind, &task.accountID, &task.apiKeyID); err != nil {
			upstreamDeadlineRows.Close()
			return nil, err
		}
		task.Status = domain.TaskSubmissionUncertain
		upstreamExpired = append(upstreamExpired, task)
	}
	if err := upstreamDeadlineRows.Err(); err != nil {
		upstreamDeadlineRows.Close()
		return nil, err
	}
	upstreamDeadlineRows.Close()
	for _, task := range upstreamExpired {
		if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, task.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'submission_uncertain','upstream deadline exceeded; manual reconciliation required')`, task.ID); err != nil {
			return nil, err
		}
		if err := wakeQueuedTasksTx(ctx, tx, task.accountID, task.apiKeyID); err != nil {
			return nil, err
		}
		out = append(out, task.RecoveryTask)
	}

	requeue := func(query string) error {
		rows, queryErr := tx.Query(ctx, query)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		var recovered []RecoveryTask
		for rows.Next() {
			var task RecoveryTask
			if scanErr := rows.Scan(&task.ID, &task.Kind, &task.Status); scanErr != nil {
				return scanErr
			}
			recovered = append(recovered, task)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for _, task := range recovered {
			if _, execErr := tx.Exec(ctx, `UPDATE account_reservations
				SET expires_at=GREATEST(expires_at,now()+interval '20 minutes'),updated_at=now()
				WHERE task_id=$1 AND state='held'`, task.ID); execErr != nil {
				return execErr
			}
			if _, execErr := tx.Exec(ctx, `INSERT INTO task_outbox(task_id,kind,delivered_at,next_attempt_at,last_error)
				VALUES($1,$2,NULL,now(),'lease expired; redispatch requested')
				ON CONFLICT(task_id) DO UPDATE SET delivered_at=NULL,next_attempt_at=now(),last_error=excluded.last_error,updated_at=now()`, task.ID, task.Kind); execErr != nil {
				return execErr
			}
			if _, execErr := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,$2,'expired worker lease recovered')`, task.ID, task.Status); execErr != nil {
				return execErr
			}
			out = append(out, task)
		}
		return nil
	}
	if err := requeue(`UPDATE tasks
		SET status='queued',progress=0,error_code='',error_message='',
			execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE status IN ('reserving','uploading')
		  AND execution_lease_expires_at IS NOT NULL AND execution_lease_expires_at<=now()
		RETURNING id,kind,status`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks
		SET status='submission_uncertain',progress=100,error_code='submission_uncertain',
			error_message='worker stopped after submission began; task was not resubmitted',
			completed_at=now(),execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE status='submitted' AND generation_id=''
		  AND execution_lease_expires_at IS NOT NULL AND execution_lease_expires_at<=now()`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE task_outbox o SET delivered_at=now(),updated_at=now()
		FROM tasks t WHERE o.task_id=t.id AND t.status='submission_uncertain'`); err != nil {
		return nil, err
	}
	if err := requeue(`UPDATE tasks
		SET execution_lease_id=NULL,execution_lease_expires_at=NULL,updated_at=now()
		WHERE status IN ('submitted','polling') AND generation_id<>''
		  AND execution_lease_expires_at IS NOT NULL AND execution_lease_expires_at<=now()
		RETURNING id,kind,status`); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `UPDATE tasks t
		SET status='failed',progress=100,error_code='reservation_expired',
			error_message='task was not submitted before its account capacity reservation expired',
			completed_at=now(),updated_at=now()
		WHERE t.status='queued' AND t.execution_attempt=0
		  AND EXISTS (SELECT 1 FROM account_reservations r WHERE r.task_id=t.id AND r.state='held' AND r.expires_at<=now())
		RETURNING t.id,t.kind,t.status`)
	if err != nil {
		return nil, err
	}
	var expired []RecoveryTask
	for rows.Next() {
		var task RecoveryTask
		if err := rows.Scan(&task.ID, &task.Kind, &task.Status); err != nil {
			rows.Close()
			return nil, err
		}
		expired = append(expired, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, task := range expired {
		if _, err := tx.Exec(ctx, `UPDATE account_reservations
			SET state='released',settled_tokens=0,reconciled_snapshot_version=0,
				release_reason='reservation_expired',released_at=now(),updated_at=now()
			WHERE task_id=$1 AND state='held'`, task.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE task_outbox SET delivered_at=now(),updated_at=now() WHERE task_id=$1`, task.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO task_events(task_id,status,message) VALUES($1,'failed','reservation expired before worker claim')`, task.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
