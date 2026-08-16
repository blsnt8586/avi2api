package sessionrefresh

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type Manager struct {
	Store    *store.Store
	Accounts *accounts.Service
	Config   config.Config
	Log      *slog.Logger
}

func (m *Manager) Run(ctx context.Context) {
	if recovered, err := m.Store.RecoverExpiredSessionRefreshJobs(ctx); err != nil {
		m.Log.Error("session refresh lease recovery failed", "error", err)
	} else if recovered > 0 {
		m.Log.Warn("recovered expired session refresh leases", "count", recovered)
	}
	m.schedule(ctx)

	var workers sync.WaitGroup
	for i := 0; i < m.Config.SessionRefreshWorkers; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			m.runCookieWorker(ctx, fmt.Sprintf("cookie-%d", index+1))
		}(i)
	}

	ticker := time.NewTicker(m.Config.SessionRefreshScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return
		case <-ticker.C:
			if recovered, err := m.Store.RecoverExpiredSessionRefreshJobs(ctx); err != nil {
				m.Log.Warn("session refresh lease recovery failed", "error", err)
			} else if recovered > 0 {
				m.Log.Warn("recovered expired session refresh leases", "count", recovered)
			}
			m.schedule(ctx)
		}
	}
}

func (m *Manager) schedule(ctx context.Context) {
	if restored, err := m.Store.ActivateExpiredAccountCooldowns(ctx); err != nil {
		m.Log.Warn("expired account cooldown recovery failed", "error", err)
	} else if restored > 0 {
		m.Log.Info("restored accounts after cooldown", "count", restored)
	}
	created, err := m.Store.EnqueueDueSessionRefreshJobs(ctx, m.Config.SessionRefreshAhead, m.Config.SessionRefreshBatch)
	if err != nil {
		m.Log.Error("session refresh scheduling failed", "error", err)
		return
	}
	if created > 0 {
		m.Log.Info("scheduled session refresh jobs", "count", created)
	}
}

func (m *Manager) runCookieWorker(ctx context.Context, owner string) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, ok, err := m.Store.ClaimSessionRefreshJob(ctx, "cookie", owner, "", m.Config.SessionRefreshLease)
		if err != nil {
			m.Log.Warn("cookie session refresh claim failed", "worker", owner, "error", err)
			if !wait(ctx, m.Config.SessionRefreshIdlePoll) {
				return
			}
			continue
		}
		if !ok {
			if !wait(ctx, m.Config.SessionRefreshIdlePoll) {
				return
			}
			continue
		}

		started := time.Now()
		refreshCtx, cancel := context.WithTimeout(ctx, m.Config.SyncTimeout)
		account, refreshErr := m.Accounts.RefreshForScheduler(refreshCtx, job.AccountID)
		cancel()
		if refreshErr != nil && account.ProviderID == providers.Adobe {
			if accounts.IsAdobeAuthenticationRejected(refreshErr) {
				if err := m.Store.TerminalFailSessionRefreshJob(ctx, job.ID, *job.LeaseToken, accounts.SanitizedUpstreamError(refreshErr), ""); err != nil {
					m.Log.Warn("Adobe session refresh terminal failure update failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
				}
				continue
			}
			retryAfter := 5 * time.Minute
			if accounts.IsUpstreamRateLimited(refreshErr) {
				retryAfter = m.Config.Upstream429Cooldown
			}
			if err := m.Store.DeferSessionRefreshJob(ctx, job.ID, *job.LeaseToken, jitteredRetry(job.ID, retryAfter), accounts.SanitizedUpstreamError(refreshErr)); err != nil {
				m.Log.Warn("Adobe session refresh defer failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
			}
			continue
		}
		if accounts.IsBrowserSessionRequired(refreshErr) {
			if err := m.Store.RequireBrowserSessionRefresh(ctx, job.ID, *job.LeaseToken, refreshErr.Error()); err != nil {
				m.Log.Warn("browser session refresh handoff failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
			}
			continue
		}
		if refreshErr != nil && (accounts.IsProxyControlUnavailable(refreshErr) || accounts.IsUpstreamRateLimited(refreshErr)) {
			retryAfter := accounts.ProxyControlRetryAfter(refreshErr)
			if retryAfter <= 0 {
				retryAfter = m.Config.Upstream429Cooldown
			}
			retryAfter = jitteredRetry(job.ID, retryAfter)
			if err := m.Store.DeferSessionRefreshJob(ctx, job.ID, *job.LeaseToken, retryAfter, accounts.SanitizedUpstreamError(refreshErr)); err != nil {
				m.Log.Warn("session refresh defer failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
			}
			continue
		}
		if refreshErr == nil && account.AccessTokenExpiresAt != nil && account.AccessTokenExpiresAt.After(time.Now().Add(m.Config.SessionRefreshMinFresh)) {
			if err := m.Store.CompleteSessionRefreshJob(ctx, job.ID, *job.LeaseToken, "cookie", time.Since(started), m.Config.SessionRefreshMinFresh); err != nil {
				m.Log.Warn("cookie session refresh completion failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
			}
			continue
		}

		message := "cookie refresh returned a token with insufficient remaining lifetime"
		if refreshErr != nil {
			message = accounts.SanitizedUpstreamError(refreshErr)
		}
		if err := m.Store.RequireBrowserSessionRefresh(ctx, job.ID, *job.LeaseToken, message); err != nil {
			m.Log.Warn("browser session refresh handoff failed", "job_id", job.ID, "account_id", job.AccountID, "error", err)
		}
	}
}

func jitteredRetry(id [16]byte, base time.Duration) time.Duration {
	if base <= 0 {
		return time.Minute
	}
	spread := base / 4
	if spread > 10*time.Minute {
		spread = 10 * time.Minute
	}
	if spread < time.Second {
		return base
	}
	seed := int(id[0])<<8 | int(id[1])
	return base + time.Duration(seed%int(spread/time.Millisecond))*time.Millisecond
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
