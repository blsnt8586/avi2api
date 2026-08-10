package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
)

type Scheduler struct {
	Accounts *accounts.Service
	Store    *store.Store
	Queue    *Client
	Assets   *taskassets.Store
	Config   config.Config
	Log      *slog.Logger
	Redis    *redis.Client
}

func (s *Scheduler) Run(ctx context.Context) {
	recoveryTicker := time.NewTicker(15 * time.Second)
	dispatchTicker := time.NewTicker(2 * time.Second)
	assetTicker := time.NewTicker(6 * time.Hour)
	historyTicker := time.NewTicker(s.Config.HistoryCleanupInterval)
	defer recoveryTicker.Stop()
	defer dispatchTicker.Stop()
	defer assetTicker.Stop()
	defer historyTicker.Stop()
	s.recover(ctx)
	s.dispatch(ctx)
	s.cleanupAssets()
	s.cleanupHistory(ctx)
	s.heartbeat(ctx)
	s.observeHealth(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-recoveryTicker.C:
			s.recover(ctx)
			s.heartbeat(ctx)
			s.observeHealth(ctx)
		case <-dispatchTicker.C:
			s.dispatch(ctx)
		case <-assetTicker.C:
			s.cleanupAssets()
		case <-historyTicker.C:
			s.cleanupHistory(ctx)
		}
	}
}

func (s *Scheduler) cleanupHistory(ctx context.Context) {
	result, err := s.Store.CleanupHistory(ctx, time.Now(), store.HistoryRetention{
		TaskEvents:      s.Config.TaskEventRetention,
		Outbox:          s.Config.OutboxRetention,
		SessionJobs:     s.Config.SessionJobRetention,
		APIRequests:     s.Config.APIRequestLogRetention,
		AuditLogs:       s.Config.AuditLogRetention,
		Reconciliations: s.Config.ReconciliationRetention,
		BatchSize:       s.Config.HistoryCleanupBatch,
	})
	if err != nil {
		s.Log.Warn("cleanup database history", "error", err)
		return
	}
	if result.TaskEvents+result.Outbox+result.SessionJobs+result.APIRequests+result.AuditLogs+result.Reconciliations > 0 {
		s.Log.Info("database history cleaned", "task_events", result.TaskEvents, "outbox", result.Outbox,
			"session_jobs", result.SessionJobs, "api_requests", result.APIRequests,
			"audit_logs", result.AuditLogs, "reconciliations", result.Reconciliations)
	}
}

func (s *Scheduler) cleanupAssets() {
	if s.Assets == nil {
		return
	}
	if err := s.Assets.CleanupOlderThan(24 * time.Hour); err != nil {
		s.Log.Warn("cleanup stale task assets", "error", err)
	}
}

func (s *Scheduler) recover(ctx context.Context) {
	_, err := s.Store.RecoverStaleTasks(ctx, time.Now())
	if err != nil {
		s.Log.Error("recover stale tasks", "error", err)
	}
}

func (s *Scheduler) heartbeat(ctx context.Context) {
	if s.Redis == nil {
		return
	}
	if err := s.Redis.Set(ctx, "aiv2api:scheduler:heartbeat", time.Now().UnixMilli(), 45*time.Second).Err(); err != nil {
		s.Log.Warn("record scheduler heartbeat", "error", err)
	}
}

func (s *Scheduler) observeHealth(ctx context.Context) {
	health, err := s.Store.GetSchedulingHealth(ctx)
	if err != nil {
		s.Log.Warn("read scheduling health", "error", err)
		return
	}
	metrics.QueueOldestSeconds.Set(health.QueueOldest.Seconds())
	metrics.OutboxPending.Set(float64(health.OutboxPending))
	metrics.OutboxOldestSeconds.Set(health.OutboxOldest.Seconds())
	metrics.ExpiredTaskLeases.Set(float64(health.ExpiredLeases))
	metrics.SubmissionUncertain.Set(float64(health.UncertainTasks))
}

func (s *Scheduler) dispatch(ctx context.Context) {
	items, err := s.Store.ClaimDispatchableOutbox(ctx, 100)
	if err != nil {
		s.Log.Error("claim task outbox", "error", err)
		return
	}
	workers := 8
	if len(items) < workers {
		workers = len(items)
	}
	if workers == 0 {
		return
	}
	jobs := make(chan store.TaskOutboxMessage)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				s.dispatchItem(ctx, item)
			}
		}()
	}
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
}

func (s *Scheduler) dispatchItem(ctx context.Context, item store.TaskOutboxMessage) {
	enqueueCtx, cancel := context.WithTimeout(ctx, time.Second)
	var err error
	if item.Recovery {
		err = s.Queue.EnqueueRecovery(enqueueCtx, item.TaskID, item.Kind)
	} else {
		err = s.Queue.EnqueueKind(enqueueCtx, item.TaskID, item.Kind)
	}
	cancel()
	if err == nil {
		return
	}
	if recordErr := s.Store.RecordOutboxError(ctx, item.TaskID, err.Error(), 5*time.Second); recordErr != nil {
		s.Log.Error("record task outbox error", "task_id", item.TaskID, "error", recordErr)
	}
	s.Log.Warn("enqueue outbox task", "task_id", item.TaskID, "kind", item.Kind, "attempt", item.AttemptCount, "error", err)
}
