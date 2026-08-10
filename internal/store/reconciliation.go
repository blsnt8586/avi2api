package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrBalanceSnapshotUnavailable = errors.New("account balance snapshot is unavailable")

type ReconciliationOutcome struct {
	SnapshotVersion    int64
	Status             string
	BaselineTokens     int64
	ObservedTokens     int64
	ObservedSpend      int64
	ExpectedTokens     int64
	ReservationCount   int
	UnresolvedTokens   int64
	UnresolvedCount    int
	CandidateSignature string
}

// ReconcileTerminalReservationsForAccount settles only work that reached a
// terminal state before the upstream balance request started. A first mismatch
// is held for another snapshot; a repeated mismatch disables the involved
// price rules before the account can route more work.
func (s *Store) ReconcileTerminalReservationsForAccount(ctx context.Context, accountID uuid.UUID) (ReconciliationOutcome, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ReconciliationOutcome{}, err
	}
	defer tx.Rollback(ctx)

	var outcome ReconciliationOutcome
	var snapshotStartedAt *time.Time
	var accountStatus string
	err = tx.QueryRow(ctx, `SELECT balance_snapshot_version,balance_snapshot_started_at,
		subscription_tokens+rollover_tokens+paid_tokens,status
		FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(
		&outcome.SnapshotVersion, &snapshotStartedAt, &outcome.ObservedTokens, &accountStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome, ErrNotFound
	}
	if err != nil {
		return outcome, err
	}
	if outcome.SnapshotVersion < 1 || snapshotStartedAt == nil {
		return outcome, ErrBalanceSnapshotUnavailable
	}

	var existing ReconciliationOutcome
	err = tx.QueryRow(ctx, `SELECT snapshot_version,status,baseline_tokens,observed_tokens,
		observed_spend,expected_tokens,reservation_count,unresolved_tokens,unresolved_count,candidate_signature
		FROM account_reconciliation_batches
		WHERE account_id=$1 AND snapshot_version=$2`, accountID, outcome.SnapshotVersion).Scan(
		&existing.SnapshotVersion, &existing.Status, &existing.BaselineTokens,
		&existing.ObservedTokens, &existing.ObservedSpend, &existing.ExpectedTokens,
		&existing.ReservationCount, &existing.UnresolvedTokens, &existing.UnresolvedCount,
		&existing.CandidateSignature,
	)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return existing, commitErr
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return outcome, err
	}

	var previousBaseline, previousObserved, previousExpected int64
	var previousUnresolvedCount int
	var previousCandidateSignature string
	var previousStatus string
	hasPrevious := true
	err = tx.QueryRow(ctx, `SELECT baseline_tokens,observed_tokens,status,expected_tokens,
		unresolved_count,candidate_signature
		FROM account_reconciliation_batches
		WHERE account_id=$1 ORDER BY snapshot_version DESC LIMIT 1`, accountID).Scan(
		&previousBaseline, &previousObserved, &previousStatus, &previousExpected,
		&previousUnresolvedCount, &previousCandidateSignature,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		hasPrevious = false
	} else if err != nil {
		return outcome, err
	}

	var ruleIDs []int64
	err = tx.QueryRow(ctx, `WITH candidates AS (
		SELECT r.task_id,r.estimated_tokens,t.status,t.pricing_rule_id,
			(t.completed_at IS NOT NULL AND t.completed_at<=$2
			 AND (t.status='succeeded' OR (t.status='failed' AND t.generation_id<>''))) AS terminal_candidate,
			(t.submitted_at IS NOT NULL AND t.submitted_at<=$2
			 AND NOT (t.completed_at IS NOT NULL AND t.completed_at<=$2
			          AND (t.status='succeeded' OR (t.status='failed' AND t.generation_id<>'')))
			 AND (t.status IN ('submitted','polling','submission_uncertain')
			      OR (t.completed_at IS NOT NULL AND t.completed_at>$2
			          AND (t.status='succeeded' OR (t.status='failed' AND t.generation_id<>''))))) AS unresolved
		FROM account_reservations r
		JOIN tasks t ON t.id=r.task_id
		WHERE r.account_id=$1 AND r.state='held'
	)
	SELECT count(*) FILTER (WHERE terminal_candidate),
		COALESCE(sum(estimated_tokens) FILTER (WHERE terminal_candidate AND status='succeeded'),0),
		COALESCE(array_agg(DISTINCT pricing_rule_id)
			FILTER (WHERE terminal_candidate AND status='succeeded' AND pricing_rule_id IS NOT NULL),'{}'::bigint[]),
		md5(COALESCE(string_agg(task_id::text,',' ORDER BY task_id) FILTER (WHERE terminal_candidate),'')),
		count(*) FILTER (WHERE unresolved),
		COALESCE(sum(estimated_tokens) FILTER (WHERE unresolved),0)
	FROM candidates`, accountID, *snapshotStartedAt).Scan(
		&outcome.ReservationCount, &outcome.ExpectedTokens, &ruleIDs,
		&outcome.CandidateSignature, &outcome.UnresolvedCount, &outcome.UnresolvedTokens,
	)
	if err != nil {
		return outcome, err
	}

	settle := false
	drifted := false
	outcome.BaselineTokens = outcome.ObservedTokens
	outcome.Status = "baseline"
	if hasPrevious {
		outcome.BaselineTokens = previousObserved
		if previousStatus == "pending" {
			outcome.BaselineTokens = previousBaseline
		}
		if outcome.ObservedTokens < outcome.BaselineTokens {
			outcome.ObservedSpend = outcome.BaselineTokens - outcome.ObservedTokens
		}
		stableMismatch := previousStatus == "pending" && previousUnresolvedCount == 0 &&
			outcome.UnresolvedCount == 0 && previousExpected == outcome.ExpectedTokens &&
			previousCandidateSignature == outcome.CandidateSignature
		switch {
		case outcome.ObservedTokens > outcome.BaselineTokens:
			// A subscription refill changes the baseline, so this snapshot is
			// authoritative but cannot be used to measure price variance.
			outcome.Status = "baseline"
			settle = true
		case outcome.ObservedSpend == outcome.ExpectedTokens:
			outcome.Status = "ok"
			settle = true
		case outcome.UnresolvedCount > 0 && outcome.ObservedSpend >= outcome.ExpectedTokens &&
			outcome.ObservedSpend <= outcome.ExpectedTokens+outcome.UnresolvedTokens:
			// A submitted generation may already be charged even though its
			// terminal state was committed after this balance request began.
			outcome.Status = "pending"
		case stableMismatch:
			outcome.Status = "drift"
			settle = true
			drifted = true
		default:
			outcome.Status = "pending"
		}
	} else {
		// Existing installations do not have a pre-migration baseline. The
		// fenced snapshot is still authoritative for available pool balance.
		settle = true
	}

	details, err := json.Marshal(map[string]any{
		"rule_ids":          ruleIDs,
		"drift":             outcome.ObservedSpend - outcome.ExpectedTokens,
		"unresolved_count":  outcome.UnresolvedCount,
		"unresolved_tokens": outcome.UnresolvedTokens,
	})
	if err != nil {
		return outcome, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_reconciliation_batches(
		account_id,snapshot_version,snapshot_started_at,baseline_tokens,observed_tokens,
		observed_spend,expected_tokens,reservation_count,unresolved_tokens,unresolved_count,
		candidate_signature,status,details)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, accountID, outcome.SnapshotVersion,
		*snapshotStartedAt, outcome.BaselineTokens, outcome.ObservedTokens,
		outcome.ObservedSpend, outcome.ExpectedTokens, outcome.ReservationCount,
		outcome.UnresolvedTokens, outcome.UnresolvedCount, outcome.CandidateSignature,
		outcome.Status, details); err != nil {
		return outcome, err
	}

	if settle && outcome.ReservationCount > 0 {
		if _, err := tx.Exec(ctx, `UPDATE account_reservations r SET
			state=CASE WHEN t.status='succeeded' THEN 'consumed' ELSE 'released' END,
			settled_tokens=CASE WHEN t.status='succeeded' THEN r.estimated_tokens ELSE 0 END,
			reconciled_snapshot_version=$3,
			release_reason=CASE WHEN t.status='succeeded' THEN 'balance_reconciled' ELSE 'upstream_failed_no_charge' END,
			released_at=now(),updated_at=now()
			FROM tasks t
			WHERE r.account_id=$1 AND r.task_id=t.id AND r.state='held'
			  AND t.completed_at IS NOT NULL AND t.completed_at<=$2
			  AND (t.status='succeeded' OR (t.status='failed' AND t.generation_id<>''))`,
			accountID, *snapshotStartedAt, outcome.SnapshotVersion); err != nil {
			return outcome, err
		}
	}

	if drifted {
		reason := fmt.Sprintf("price drift: expected %d credits, observed %d", outcome.ExpectedTokens, outcome.ObservedSpend)
		if len(ruleIDs) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,drifted=true,
				drift_reason=$2,updated_at=now() WHERE id=ANY($1::bigint[])`, ruleIDs, reason); err != nil {
				return outcome, err
			}
		}
		if accountStatus != "disabled" && accountStatus != "invalid" && accountStatus != "rate_limited" {
			if _, err := tx.Exec(ctx, `UPDATE accounts SET status='cooldown',cooldown_until=now()+interval '30 minutes',
				last_error=$2,updated_at=now() WHERE id=$1`, accountID, "balance reconciliation "+reason); err != nil {
				return outcome, err
			}
		}
	} else if outcome.Status == "pending" {
		if accountStatus != "disabled" && accountStatus != "invalid" && accountStatus != "rate_limited" {
			if _, err := tx.Exec(ctx, `UPDATE accounts SET status='cooldown',cooldown_until=now()+interval '30 seconds',
				last_error=$2,updated_at=now() WHERE id=$1`, accountID,
				fmt.Sprintf("balance reconciliation pending: expected %d credits, observed %d, unresolved up to %d", outcome.ExpectedTokens, outcome.ObservedSpend, outcome.UnresolvedTokens)); err != nil {
				return outcome, err
			}
		}
	} else if settle {
		if _, err := tx.Exec(ctx, `UPDATE accounts SET
			status=CASE WHEN status='cooldown' AND last_error LIKE 'balance reconciliation %' THEN 'active' ELSE status END,
			cooldown_until=CASE WHEN status='cooldown' AND last_error LIKE 'balance reconciliation %' THEN NULL ELSE cooldown_until END,
			last_error=CASE WHEN status='cooldown' AND last_error LIKE 'balance reconciliation %' THEN '' ELSE last_error END,
			updated_at=now() WHERE id=$1`, accountID); err != nil {
			return outcome, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return outcome, err
	}
	return outcome, nil
}
