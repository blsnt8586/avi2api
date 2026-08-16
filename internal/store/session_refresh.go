package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

const sessionRefreshJobColumns = `id,account_id,stage,status,priority,next_attempt_at,attempt_count,lease_owner,lease_token,lease_started_at,lease_expires_at,last_error,completed_at,created_at,updated_at`

const sessionRefreshBalanceRetry = 30 * time.Second
const sessionRefreshRotationRetryAhead = 5 * time.Minute

func scanSessionRefreshJob(row pgx.Row) (domain.SessionRefreshJob, error) {
	var job domain.SessionRefreshJob
	err := row.Scan(&job.ID, &job.AccountID, &job.Stage, &job.Status, &job.Priority, &job.NextAttemptAt, &job.AttemptCount, &job.LeaseOwner, &job.LeaseToken, &job.LeaseStartedAt, &job.LeaseExpiresAt, &job.LastError, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func (s *Store) EnqueueDueSessionRefreshJobs(ctx context.Context, ahead time.Duration, limit int) (int64, error) {
	if limit < 1 {
		return 0, nil
	}
	command, err := s.DB.Exec(ctx, `WITH due AS (
		SELECT a.id
		FROM accounts a
		WHERE a.archived_at IS NULL AND a.session_refresh_enabled=true
		  AND a.status NOT IN ('disabled','invalid')
		  AND (a.session_refresh_not_before IS NULL OR a.session_refresh_not_before<=now())
		  AND (a.access_token_expires_at IS NULL OR a.access_token_expires_at<=now()+$1::interval-make_interval(secs=>a.session_refresh_jitter_seconds))
		  AND NOT EXISTS (
			SELECT 1 FROM session_refresh_jobs j
			WHERE j.account_id=a.id AND j.status IN ('pending','leased')
		  )
		ORDER BY a.access_token_expires_at+make_interval(secs=>a.session_refresh_jitter_seconds) ASC NULLS FIRST,a.id
		LIMIT $2
	)
	INSERT INTO session_refresh_jobs(account_id,stage,status,priority)
	SELECT id,'cookie','pending',0 FROM due
	ON CONFLICT DO NOTHING`, ahead.String(), limit)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}

func (s *Store) EnqueueSessionRefreshJob(ctx context.Context, accountID uuid.UUID, priority int) (domain.SessionRefreshJob, error) {
	return s.enqueueSessionRefreshJob(ctx, accountID, priority, "cookie")
}

func (s *Store) EnqueueBrowserSessionRefreshJob(ctx context.Context, accountID uuid.UUID, priority int) (domain.SessionRefreshJob, error) {
	return s.enqueueSessionRefreshJob(ctx, accountID, priority, "browser")
}

func (s *Store) enqueueSessionRefreshJob(ctx context.Context, accountID uuid.UUID, priority int, stage string) (domain.SessionRefreshJob, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return domain.SessionRefreshJob{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO session_refresh_jobs(account_id,stage,status,priority)
		SELECT id,$2,'pending',$3 FROM accounts
		WHERE id=$1 AND archived_at IS NULL AND session_refresh_enabled=true
		ON CONFLICT DO NOTHING`, accountID, stage, priority)
	if err != nil {
		return domain.SessionRefreshJob{}, err
	}
	job, err := scanSessionRefreshJob(tx.QueryRow(ctx, `SELECT `+sessionRefreshJobColumns+` FROM session_refresh_jobs
		WHERE account_id=$1 AND status IN ('pending','leased')`, accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SessionRefreshJob{}, ErrNotFound
	}
	if err != nil {
		return domain.SessionRefreshJob{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE session_refresh_jobs SET
		stage=CASE WHEN $3='browser' THEN 'browser' ELSE stage END,
		priority=GREATEST(priority,$2),next_attempt_at=LEAST(next_attempt_at,now()),updated_at=now()
		WHERE id=$1 AND status='pending'`, job.ID, priority, stage); err != nil {
		return domain.SessionRefreshJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SessionRefreshJob{}, err
	}
	return s.GetSessionRefreshJob(ctx, job.ID)
}

func (s *Store) RecoverExpiredSessionRefreshJobs(ctx context.Context) (int64, error) {
	command, err := s.DB.Exec(ctx, `UPDATE session_refresh_jobs SET
		status='pending',lease_owner='',lease_token=NULL,lease_started_at=NULL,lease_expires_at=NULL,
		next_attempt_at=now(),last_error=CASE WHEN last_error='' THEN 'worker lease expired' ELSE last_error END,updated_at=now()
		WHERE status='leased' AND lease_expires_at<=now()`)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}

func (s *Store) ClaimSessionRefreshJob(ctx context.Context, stage, owner, workerGroup string, lease time.Duration) (domain.SessionRefreshJob, bool, error) {
	leaseToken := uuid.New()
	job, err := scanSessionRefreshJob(s.DB.QueryRow(ctx, `WITH candidate AS (
		SELECT j.id FROM session_refresh_jobs j JOIN accounts a ON a.id=j.account_id
		WHERE j.status='pending' AND j.stage=$1 AND j.next_attempt_at<=now()
		  AND a.archived_at IS NULL AND a.session_refresh_enabled=true
		  AND ($2='' OR a.browser_worker_group=$2)
		ORDER BY j.priority DESC,j.next_attempt_at,j.created_at
		LIMIT 1 FOR UPDATE SKIP LOCKED
	)
	UPDATE session_refresh_jobs j SET
		status='leased',lease_owner=$3,lease_token=$4,lease_started_at=now(),lease_expires_at=now()+$5::interval,
		attempt_count=attempt_count+1,updated_at=now()
	FROM candidate WHERE j.id=candidate.id
	RETURNING j.`+strings.ReplaceAll(sessionRefreshJobColumns, ",", ",j."), stage, workerGroup, owner, leaseToken, lease.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SessionRefreshJob{}, false, nil
	}
	return job, err == nil, err
}

func (s *Store) GetSessionRefreshJob(ctx context.Context, id uuid.UUID) (domain.SessionRefreshJob, error) {
	job, err := scanSessionRefreshJob(s.DB.QueryRow(ctx, `SELECT `+sessionRefreshJobColumns+` FROM session_refresh_jobs WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SessionRefreshJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ExtendSessionRefreshLease(ctx context.Context, id, leaseToken uuid.UUID, lease time.Duration) (bool, error) {
	command, err := s.DB.Exec(ctx, `UPDATE session_refresh_jobs SET lease_expires_at=now()+$3::interval,updated_at=now()
		WHERE id=$1 AND status='leased' AND lease_token=$2 AND lease_expires_at>now()`, id, leaseToken, lease.String())
	return command.RowsAffected() == 1, err
}

func (s *Store) RequireBrowserSessionRefresh(ctx context.Context, id, leaseToken uuid.UUID, message string) error {
	message = truncateRefreshError(message)
	command, err := s.DB.Exec(ctx, `UPDATE session_refresh_jobs SET
		stage='browser',status='pending',next_attempt_at=now(),lease_owner='',lease_token=NULL,
		lease_started_at=NULL,lease_expires_at=NULL,last_error=$3,updated_at=now()
		WHERE id=$1 AND status='leased' AND lease_token=$2`, id, leaseToken, message)
	if err == nil && command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) CompleteSessionRefreshJob(ctx context.Context, id, leaseToken uuid.UUID, method string, duration, minFresh time.Duration) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountID uuid.UUID
	var jobStage string
	var tokenExpiresAt time.Time
	var leaseStartedAt time.Time
	var lastCheckedAt *time.Time
	var pendingCookieJSON string
	err = tx.QueryRow(ctx, `SELECT j.account_id,j.stage,a.access_token_expires_at,j.lease_started_at,a.last_checked_at,a.pending_cookie_json_ciphertext
		FROM session_refresh_jobs j JOIN accounts a ON a.id=j.account_id
		WHERE j.id=$1 AND j.status='leased' AND j.lease_token=$2
		FOR UPDATE OF j,a`, id, leaseToken).Scan(&accountID, &jobStage, &tokenExpiresAt, &leaseStartedAt, &lastCheckedAt, &pendingCookieJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	now := time.Now()
	if !tokenExpiresAt.After(now) {
		return ErrNotFound
	}

	// A valid Better Auth session may legitimately return the existing JWT
	// until Leonardo's own rotation window opens. Keep the browser result and
	// retry close to expiry instead of treating this as a failed login.
	if pendingCookieJSON != "" {
		command, updateErr := tx.Exec(ctx, `UPDATE session_refresh_jobs SET
			stage='browser',status='pending',next_attempt_at=now(),lease_owner='',lease_token=NULL,
			lease_started_at=NULL,lease_expires_at=NULL,attempt_count=GREATEST(0,attempt_count-1),
			last_error='complete Cookie JSON pending browser validation',completed_at=NULL,updated_at=now()
			WHERE id=$1 AND status='leased' AND lease_token=$2`, id, leaseToken)
		if updateErr != nil {
			return updateErr
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
	} else if !tokenExpiresAt.After(now.Add(minFresh)) {
		retryAt := tokenExpiresAt.Add(-sessionRefreshRotationRetryAhead)
		if minimum := now.Add(time.Minute); retryAt.Before(minimum) {
			retryAt = minimum
		}
		command, updateErr := tx.Exec(ctx, `UPDATE session_refresh_jobs SET
			stage='browser',status='pending',next_attempt_at=$3,lease_owner='',lease_token=NULL,
			lease_started_at=NULL,lease_expires_at=NULL,attempt_count=GREATEST(0,attempt_count-1),
			last_error='browser authenticated; JWT rotation deferred by upstream',completed_at=NULL,updated_at=now()
			WHERE id=$1 AND status='leased' AND lease_token=$2`, id, leaseToken, retryAt)
		if updateErr != nil {
			return updateErr
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
	} else {
		balanceFresh := lastCheckedAt != nil && !lastCheckedAt.Before(leaseStartedAt)
		status := "pending"
		stage := "cookie"
		nextAttemptAt := now.Add(sessionRefreshBalanceRetry)
		lastError := "session restored; balance refresh deferred"
		var completedAt *time.Time
		if balanceFresh {
			status = "succeeded"
			stage = jobStage
			nextAttemptAt = now
			lastError = ""
			completedAt = &now
		}
		command, updateErr := tx.Exec(ctx, `UPDATE session_refresh_jobs SET
			stage=$3,status=$4,next_attempt_at=$5,lease_owner='',lease_token=NULL,
			lease_started_at=NULL,lease_expires_at=NULL,last_error=$6,completed_at=$7,updated_at=now()
			WHERE id=$1 AND status='leased' AND lease_token=$2`,
			id, leaseToken, stage, status, nextAttemptAt, lastError, completedAt)
		if updateErr != nil {
			return updateErr
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	durationMS := max(0, duration.Milliseconds())
	if _, err := tx.Exec(ctx, `UPDATE accounts SET
		session_refresh_last_at=now(),session_refresh_last_method=$2,session_refresh_last_duration_ms=$3,
		session_refresh_failures=0,session_refresh_not_before=NULL,updated_at=now()
		WHERE id=$1`, accountID, method, durationMS); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) FailSessionRefreshJob(ctx context.Context, id, leaseToken uuid.UUID, retryAfter time.Duration, message string) error {
	message = truncateRefreshError(message)
	if retryAfter < time.Minute {
		retryAfter = time.Minute
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountID uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE session_refresh_jobs SET
		status='pending',next_attempt_at=now()+$3::interval,lease_owner='',lease_token=NULL,
		lease_started_at=NULL,lease_expires_at=NULL,last_error=$4,updated_at=now()
		WHERE id=$1 AND status='leased' AND lease_token=$2
		RETURNING account_id`, id, leaseToken, retryAfter.String(), message).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE accounts SET
		session_refresh_failures=session_refresh_failures+1,session_refresh_not_before=now()+$2::interval,
		last_error=CASE WHEN access_token_expires_at IS NULL OR access_token_expires_at<=now() THEN $3 ELSE last_error END,
		status=CASE WHEN status<>'disabled' AND (access_token_expires_at IS NULL OR access_token_expires_at<=now()) THEN 'cooldown' ELSE status END,
		cooldown_until=CASE WHEN status<>'disabled' AND (access_token_expires_at IS NULL OR access_token_expires_at<=now()) THEN now()+$2::interval ELSE cooldown_until END,
		updated_at=now() WHERE id=$1`, accountID, retryAfter.String(), message)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) TerminalFailSessionRefreshJob(ctx context.Context, id, leaseToken uuid.UUID, message, attemptedFingerprint string) error {
	message = truncateRefreshError(message)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountID uuid.UUID
	var pendingCookieJSON string
	var pendingFingerprint string
	var tokenExpiresAt *time.Time
	err = tx.QueryRow(ctx, `SELECT j.account_id,a.pending_cookie_json_ciphertext,a.pending_cookie_json_fingerprint,a.access_token_expires_at
		FROM session_refresh_jobs j JOIN accounts a ON a.id=j.account_id
		WHERE j.id=$1 AND j.status='leased' AND j.lease_token=$2
		FOR UPDATE OF j,a`, id, leaseToken).Scan(&accountID, &pendingCookieJSON, &pendingFingerprint, &tokenExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if pendingCookieJSON != "" && (attemptedFingerprint == "" || pendingFingerprint != attemptedFingerprint) {
		_, err = tx.Exec(ctx, `UPDATE session_refresh_jobs SET
			stage='browser',status='pending',next_attempt_at=now(),lease_owner='',lease_token=NULL,
			lease_started_at=NULL,lease_expires_at=NULL,attempt_count=GREATEST(0,attempt_count-1),
			last_error='complete Cookie JSON pending browser validation',completed_at=NULL,updated_at=now()
			WHERE id=$1`, id)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE session_refresh_jobs SET
		status='failed',lease_owner='',lease_token=NULL,lease_started_at=NULL,lease_expires_at=NULL,
		last_error=$2,completed_at=now(),updated_at=now()
		WHERE id=$1`, id, message)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE accounts SET
		session_refresh_failures=session_refresh_failures+1,session_refresh_not_before=NULL,
		last_error=CASE WHEN access_token_expires_at IS NULL OR access_token_expires_at<=now() THEN $2 ELSE last_error END,
		status=CASE WHEN status='disabled' THEN status WHEN access_token_expires_at IS NULL OR access_token_expires_at<=now() THEN 'invalid' ELSE status END,
		cooldown_until=CASE WHEN access_token_expires_at IS NULL OR access_token_expires_at<=now() THEN NULL ELSE cooldown_until END,
		updated_at=now() WHERE id=$1`, accountID, message)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeferSessionRefreshJob reschedules work because shared egress capacity is
// unavailable. Unlike a real refresh failure, it does not penalize the account.
func (s *Store) DeferSessionRefreshJob(ctx context.Context, id, leaseToken uuid.UUID, retryAfter time.Duration, message string) error {
	message = truncateRefreshError(message)
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	command, err := s.DB.Exec(ctx, `UPDATE session_refresh_jobs SET
		status='pending',next_attempt_at=now()+$3::interval,lease_owner='',lease_token=NULL,
		lease_started_at=NULL,lease_expires_at=NULL,attempt_count=GREATEST(0,attempt_count-1),
		last_error=$4,updated_at=now()
		WHERE id=$1 AND status='leased' AND lease_token=$2`, id, leaseToken, retryAfter.String(), message)
	if err == nil && command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) ListSessionRefreshJobs(ctx context.Context, limit int) ([]domain.SessionRefreshJob, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `SELECT `+sessionRefreshJobColumns+` FROM session_refresh_jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]domain.SessionRefreshJob, 0, limit)
	for rows.Next() {
		job, err := scanSessionRefreshJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func truncateRefreshError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		message = message[:1000]
	}
	return message
}
