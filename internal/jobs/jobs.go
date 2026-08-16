package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/circuit"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"math"
	"strings"
	"time"
)

type Worker struct {
	Store     *store.Store
	Redis     *redis.Client
	Accounts  *accounts.Service
	Assets    *taskassets.Store
	Config    config.Config
	Log       *slog.Logger
	Providers *providers.Registry
	Circuit   circuit.Breaker
}

func (w *Worker) Handler() httpHandler { return httpHandler{w: w} }

type httpHandler struct{ w *Worker }

func (h httpHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p Payload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	switch t.Type() {
	case TypeImage:
		return h.w.processImage(ctx, p.TaskID)
	case TypeVideo:
		return h.w.processVideo(ctx, p.TaskID)
	case TypeAudio:
		return h.w.processAudio(ctx, p.TaskID)
	default:
		return fmt.Errorf("unknown task type %s", t.Type())
	}
}

func (w *Worker) taskLease() time.Duration {
	lease := w.Config.TaskLease
	if lease < 30*time.Second {
		lease = 60 * time.Second
	}
	return lease
}

func (w *Worker) startLeaseHeartbeat(ctx context.Context, cancel context.CancelFunc, id, leaseID uuid.UUID) func() {
	interval := w.taskLease() / 3
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(context.Background(), 10*time.Second)
				ok, err := w.Store.RenewTaskLease(renewCtx, id, leaseID, w.taskLease())
				renewCancel()
				if err != nil || !ok {
					w.Log.Warn("task lease lost", "task_id", id, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

func (w *Worker) update(ctx context.Context, id, leaseID uuid.UUID, status string, progress int, accountID *uuid.UUID, generationID string, result any, code, message string) error {
	ok, err := w.Store.UpdateTaskOwned(ctx, id, leaseID, status, progress, accountID, generationID, result, code, message)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) updateTokens(ctx context.Context, id, leaseID uuid.UUID, before, after *int64) error {
	ok, err := w.Store.UpdateTaskTokensOwned(ctx, id, leaseID, before, after)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) prepareSubmission(ctx context.Context, id, leaseID, accountID uuid.UUID, request any, upstreamDeadline time.Time) error {
	ok, err := w.Store.PrepareTaskSubmissionWithDeadlineOwned(ctx, id, leaseID, accountID, request, upstreamDeadline)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) recordAdobeSubmission(ctx context.Context, id, leaseID, accountID uuid.UUID, response adobe.Job) error {
	ok, err := w.Store.RecordTaskSubmissionOwned(ctx, id, leaseID, accountID, response.PollURL, nil)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) recordSubmission(ctx context.Context, id, leaseID uuid.UUID, accountID uuid.UUID, response leonardo.GenerateResponse) error {
	cost := normalizeUpstreamReportedCost(response.APICreditCost)
	ok, err := w.Store.RecordTaskSubmissionOwned(ctx, id, leaseID, accountID, response.GenerationID, cost)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func normalizeUpstreamReportedCost(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return value
}

func (w *Worker) validateTaskAPIKey(ctx context.Context, task domain.Task) error {
	if task.APIKeyID == nil {
		return nil
	}
	_, err := w.Store.GetAPIKey(ctx, *task.APIKeyID)
	return err
}

func (w *Worker) yieldForCooldown(ctx context.Context, id, leaseID uuid.UUID, account domain.Account) error {
	retryAt := time.Now().Add(30 * time.Second)
	if account.CooldownUntil != nil && account.CooldownUntil.After(retryAt) {
		retryAt = *account.CooldownUntil
	}
	ok, err := w.Store.YieldTaskLease(ctx, id, leaseID, retryAt)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) yieldForReroute(ctx context.Context, id, leaseID uuid.UUID) error {
	ok, err := w.Store.YieldTaskLease(ctx, id, leaseID, time.Now().Add(time.Second))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) yieldAt(ctx context.Context, id, leaseID uuid.UUID, retryAt time.Time) error {
	ok, err := w.Store.YieldTaskLease(ctx, id, leaseID, retryAt)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("task execution lease is no longer owned")
	}
	return nil
}

func (w *Worker) markSubmissionUncertain(ctx context.Context, id, leaseID uuid.UUID, accountID *uuid.UUID, code, message string) error {
	if err := w.update(ctx, id, leaseID, domain.TaskSubmissionUncertain, 100, accountID, "", nil, code, message); err != nil {
		return err
	}
	_ = w.Store.ClearTerminalTaskSourceImage(context.Background(), id)
	w.recordTerminalTaskMetrics(id, domain.TaskSubmissionUncertain, 0)
	_ = w.Redis.Publish(context.Background(), "leo:task:"+id.String(), "done").Err()
	return nil
}

func (w *Worker) yieldSubmittedRetry(ctx context.Context, id, leaseID uuid.UUID, accountID uuid.UUID, deadline *time.Time, code, message string, delay time.Duration) error {
	if deadline != nil && !deadline.After(time.Now()) {
		return w.markSubmissionUncertain(ctx, id, leaseID, &accountID, "upstream_deadline_exceeded", message)
	}
	if delay < time.Second {
		delay = 10 * time.Second
	}
	retryAt := time.Now().Add(delay)
	if deadline != nil && retryAt.After(*deadline) {
		retryAt = *deadline
	}
	return w.yieldAt(ctx, id, leaseID, retryAt)
}

func submissionAccountRouteable(account domain.Account) bool {
	now := time.Now()
	return account.Status == "active" &&
		(account.CooldownUntil == nil || !account.CooldownUntil.After(now)) &&
		account.AccessTokenExpiresAt != nil && account.AccessTokenExpiresAt.After(now) &&
		account.LastCheckedAt != nil &&
		account.TotalTokens() >= account.ReservedTokens
}

func upstreamTaskState(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PENDING":
		return domain.TaskSubmitted
	default:
		// Leonardo's generation query is verified for PENDING, COMPLETE and
		// FAILED. COMPLETE/FAILED are handled by the caller; any unexpected
		// non-terminal value remains in the polling phase without inventing a
		// provider queue state.
		return domain.TaskPolling
	}
}

func (w *Worker) pollInterval(id uuid.UUID, deadline *time.Time) time.Duration {
	interval := w.Config.PollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	if deadline != nil {
		elapsed := w.Config.TaskTimeout - time.Until(*deadline)
		if elapsed >= 5*time.Minute && interval < 10*time.Second {
			interval = 10 * time.Second
		} else if elapsed >= time.Minute && interval < 5*time.Second {
			interval = 5 * time.Second
		}
	}
	jitter := time.Duration(int64(interval) * int64(id[0]%50) / 100)
	return interval + jitter
}

func isSubmittedGenerationTask(task domain.Task) bool {
	return (task.Status == domain.TaskSubmitted || task.Status == domain.TaskPolling) && task.GenerationID != "" && task.AccountID != nil
}

func (w *Worker) enforceSubmissionFence(ctx context.Context, id, leaseID, accountID uuid.UUID) (domain.Account, bool, error) {
	account, err := w.Store.GetAccount(ctx, accountID)
	if err != nil {
		return account, false, w.fail(ctx, id, leaseID, "account_not_found", err)
	}
	if !submissionAccountRouteable(account) {
		return account, false, w.yieldForReroute(ctx, id, leaseID)
	}
	if w.Circuit.Redis != nil {
		until, open, circuitErr := w.Circuit.Check(ctx, account.ProviderID, account.ProxyURL)
		if circuitErr != nil {
			return account, false, circuitErr
		}
		if open {
			return account, false, w.yieldAt(ctx, id, leaseID, until)
		}
	}
	active, err := w.Store.IsTaskPricingRuleActive(ctx, id)
	if err != nil {
		return account, false, err
	}
	if !active {
		return account, false, w.fail(ctx, id, leaseID, "cost_rule_unavailable", errors.New("price rule was disabled before upstream submission"))
	}
	return account, true, nil
}
