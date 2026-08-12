package store

import (
	"context"
	"time"
)

type HistoryRetention struct {
	TaskEvents      time.Duration
	Outbox          time.Duration
	SessionJobs     time.Duration
	APIRequests     time.Duration
	AuditLogs       time.Duration
	Reconciliations time.Duration
	BatchSize       int
}

type HistoryCleanupResult struct {
	TaskEvents      int64
	Outbox          int64
	SessionJobs     int64
	APIRequests     int64
	AuditLogs       int64
	Reconciliations int64
}

func (s *Store) CleanupHistory(ctx context.Context, now time.Time, retention HistoryRetention) (HistoryCleanupResult, error) {
	var result HistoryCleanupResult
	steps := []struct {
		retention time.Duration
		query     string
		count     *int64
	}{
		{retention.TaskEvents, `WITH doomed AS (
			SELECT e.id FROM task_events e
			JOIN tasks t ON t.id=e.task_id
			WHERE e.created_at<$1
			  AND t.status IN ('succeeded','failed','cancelled','submission_uncertain')
			ORDER BY e.created_at,e.id LIMIT $2 FOR UPDATE OF e SKIP LOCKED
		) DELETE FROM task_events e USING doomed d WHERE e.id=d.id`, &result.TaskEvents},
		{retention.Outbox, `WITH doomed AS (
			SELECT o.task_id FROM task_outbox o
			JOIN tasks t ON t.id=o.task_id
			WHERE o.delivered_at IS NOT NULL AND o.updated_at<$1
			  AND t.status IN ('succeeded','failed','cancelled','submission_uncertain')
			ORDER BY o.updated_at,o.task_id LIMIT $2 FOR UPDATE OF o SKIP LOCKED
		) DELETE FROM task_outbox o USING doomed d WHERE o.task_id=d.task_id`, &result.Outbox},
		{retention.SessionJobs, `WITH doomed AS (
			SELECT id FROM session_refresh_jobs
			WHERE status IN ('succeeded','failed','cancelled') AND updated_at<$1
			ORDER BY updated_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
		) DELETE FROM session_refresh_jobs j USING doomed d WHERE j.id=d.id`, &result.SessionJobs},
		{retention.APIRequests, `WITH doomed AS (
			SELECT id FROM api_request_logs WHERE created_at<$1
			ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
		) DELETE FROM api_request_logs l USING doomed d WHERE l.id=d.id`, &result.APIRequests},
		{retention.AuditLogs, `WITH doomed AS (
			SELECT id FROM audit_logs WHERE created_at<$1
			ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
		) DELETE FROM audit_logs a USING doomed d WHERE a.id=d.id`, &result.AuditLogs},
		{retention.Reconciliations, `WITH doomed AS (
			SELECT id FROM account_reconciliation_batches WHERE created_at<$1
			ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
		) DELETE FROM account_reconciliation_batches b USING doomed d WHERE b.id=d.id`, &result.Reconciliations},
	}
	for _, step := range steps {
		command, err := s.DB.Exec(ctx, step.query, now.Add(-step.retention), retention.BatchSize)
		if err != nil {
			return result, err
		}
		*step.count = command.RowsAffected()
	}
	return result, nil
}
