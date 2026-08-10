package jobs

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"net/http"
	"strings"
	"time"
)

func (w *Worker) complete(ctx context.Context, id, leaseID uuid.UUID, generationID string, account domain.Account, token string, result any, start time.Time) error {
	ok, err := w.Store.CompleteTaskOwned(ctx, id, leaseID, account.ID, generationID, result, nil, true)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_ = w.Store.ClearTerminalTaskSourceImage(context.Background(), id)
	metrics.TasksTotal.WithLabelValues(domain.TaskSucceeded).Inc()
	metrics.TaskDuration.Observe(time.Since(start).Seconds())
	_ = w.Redis.Publish(ctx, "leo:task:"+id.String(), "done").Err()
	return nil
}

func (w *Worker) fail(ctx context.Context, id, leaseID uuid.UUID, code string, err error) error {
	return w.failDetailedWithSettlement(ctx, id, leaseID, code, err.Error(), nil, nil, false)
}

func (w *Worker) failDetailedWithSettlement(ctx context.Context, id, leaseID uuid.UUID, code, message string, details any, tokensAfter *int64, reconciled bool) error {
	ok, err := w.Store.FailTaskOwned(ctx, id, leaseID, code, message, details, tokensAfter, reconciled)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_ = w.Store.ClearTerminalTaskSourceImage(context.Background(), id)
	metrics.TasksTotal.WithLabelValues(domain.TaskFailed).Inc()
	_ = w.Redis.Publish(context.Background(), "leo:task:"+id.String(), "done").Err()
	return nil
}

func (w *Worker) failAfterSubmission(ctx context.Context, id, leaseID uuid.UUID, code, message string, details any, account domain.Account, token string) error {
	ok, err := w.Store.FailSubmittedTaskOwned(ctx, id, leaseID, code, message, details)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_ = w.Store.ClearTerminalTaskSourceImage(context.Background(), id)
	metrics.TasksTotal.WithLabelValues(domain.TaskFailed).Inc()
	_ = w.Redis.Publish(context.Background(), "leo:task:"+id.String(), "done").Err()
	return nil
}

func (w *Worker) failUpstreamGeneration(ctx context.Context, id, leaseID uuid.UUID, generationID string, account domain.Account, token string, client *leonardo.Client, kind string) error {
	failure, err := client.FailureDetails(ctx, token, account.TeamID, generationID)
	if err != nil {
		w.Log.Warn("generation failure details unavailable", "task_id", id, "generation_id", generationID, "error", err)
		return w.failAfterSubmission(ctx, id, leaseID, "upstream_failed", "Leonardo 上游生成失败，未返回详细原因", map[string]any{
			"source": "leonardo", "generation_id": generationID, "upstream_status": "FAILED", "detail_error": err.Error(),
		}, account, token)
	}
	providerCode := failure.ProviderErrorCode()
	message := "Leonardo 上游生成失败"
	if providerCode == "PROVIDER_MODERATION_ERROR" {
		message = "上游供应商内容审核未通过"
	} else if providerCode != "" {
		message = "上游供应商生成失败：" + providerCode
	}
	details := map[string]any{
		"source": "leonardo_provider", "media_type": kind, "generation_id": generationID,
		"upstream_status": failure.Status, "provider_error_code": providerCode, "nsfw": failure.NSFW,
		"notes": failure.Notes, "prompt_moderations": failure.PromptModerations,
	}
	return w.failAfterSubmission(ctx, id, leaseID, "upstream_failed", message, details, account, token)
}

func (w *Worker) waitForAccountSubmit(ctx context.Context, accountID uuid.UUID) error {
	if w.Config.AccountSubmitInterval <= 0 {
		return nil
	}
	key := "leo:account:last-submit:" + accountID.String()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := w.Redis.SetNX(ctx, key, time.Now().UnixMilli(), w.Config.AccountSubmitInterval).Result()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) recordAccountFailure(ctx context.Context, account domain.Account, err error) *time.Time {
	var sharedRetryAt *time.Time
	if w.Circuit.Redis != nil && sharedCircuitFailure(err) {
		if until, open, circuitErr := w.Circuit.RecordFailure(ctx, account.ProviderID, account.ProxyURL); circuitErr != nil {
			w.Log.Warn("record shared circuit failure", "provider_id", account.ProviderID, "error", circuitErr)
		} else if open {
			sharedRetryAt = &until
		}
	}
	var he *leonardo.HTTPError
	if errors.As(err, &he) {
		if he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden {
			_ = w.Store.SetAccountError(ctx, account.ID, "invalid", err.Error(), nil)
			return nil
		}
		if he.Status >= 400 && he.Status < 500 && he.Status != http.StatusRequestTimeout && he.Status != http.StatusTooManyRequests {
			return nil
		}
		if he.Status == http.StatusTooManyRequests {
			until := time.Now().Add(w.Config.Upstream429Cooldown)
			if sharedRetryAt != nil && sharedRetryAt.After(until) {
				until = *sharedRetryAt
			}
			_ = w.Store.SetAccountError(ctx, account.ID, "rate_limited", err.Error(), &until)
			return &until
		}
	}
	var gqlErr *leonardo.GraphQLError
	if errors.As(err, &gqlErr) {
		code, _ := gqlErr.Extensions["code"].(string)
		switch strings.ToUpper(code) {
		case "BAD_USER_INPUT":
			return nil
		case "FORBIDDEN", "UNAUTHENTICATED":
			_ = w.Store.SetAccountError(ctx, account.ID, "invalid", err.Error(), nil)
			return nil
		case "RATE_LIMIT_EXCEEDED":
			until := time.Now().Add(w.Config.Upstream429Cooldown)
			if sharedRetryAt != nil && sharedRetryAt.After(until) {
				until = *sharedRetryAt
			}
			_ = w.Store.SetAccountError(ctx, account.ID, "rate_limited", err.Error(), &until)
			return &until
		}
	}
	key := "leo:account:failures:" + account.ID.String()
	count, redisErr := w.Redis.Incr(ctx, key).Result()
	if redisErr != nil {
		return nil
	}
	_ = w.Redis.Expire(ctx, key, 30*time.Minute).Err()
	if w.Config.CircuitFailures > 0 && count >= int64(w.Config.CircuitFailures) {
		until := time.Now().Add(w.Config.CircuitCooldown)
		if sharedRetryAt != nil && sharedRetryAt.After(until) {
			until = *sharedRetryAt
		}
		_ = w.Store.SetAccountError(ctx, account.ID, "cooldown", err.Error(), &until)
		return &until
	}
	if sharedRetryAt != nil {
		return sharedRetryAt
	}
	return nil
}

func sharedCircuitFailure(err error) bool {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		return upstream.Status == http.StatusRequestTimeout || upstream.Status == http.StatusTooManyRequests || upstream.Status >= http.StatusInternalServerError
	}
	var gqlErr *leonardo.GraphQLError
	if errors.As(err, &gqlErr) {
		if status, ok := gqlErr.Extensions["statusCode"].(float64); ok && status >= 400 && status < 500 {
			return false
		}
		code, _ := gqlErr.Extensions["code"].(string)
		switch strings.ToUpper(code) {
		case "BAD_USER_INPUT", "FORBIDDEN", "UNAUTHENTICATED":
			return false
		}
		return true
	}
	return true
}

func isRateLimited(err error) bool {
	var upstream *leonardo.HTTPError
	return errors.As(err, &upstream) && upstream.Status == 429
}

func (w *Worker) clearAccountFailures(ctx context.Context, accountID uuid.UUID) {
	_ = w.Redis.Del(ctx, "leo:account:failures:"+accountID.String()).Err()
}

func submissionUncertain(err error) bool {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		return upstream.Status == http.StatusRequestTimeout || upstream.Status >= http.StatusInternalServerError
	}
	var gqlErr *leonardo.GraphQLError
	if errors.As(err, &gqlErr) {
		if status, ok := gqlErr.Extensions["statusCode"].(float64); ok && status >= 400 && status < 500 {
			return false
		}
		code, _ := gqlErr.Extensions["code"].(string)
		switch strings.ToUpper(code) {
		case "RATE_LIMIT_EXCEEDED", "BAD_USER_INPUT", "FORBIDDEN", "UNAUTHENTICATED":
			return false
		}
		return true
	}
	// Once a mutation starts, transport, decode, empty-response and GraphQL
	// failures do not prove that Leonardo rejected the generation.
	return true
}
