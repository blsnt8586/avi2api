package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/audioopts"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"strings"
	"time"
)

func (w *Worker) processAudio(parent context.Context, id uuid.UUID) error {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, w.Config.TaskTimeout)
	defer cancel()
	task, leaseID, claimed, err := w.Store.ClaimTask(ctx, id, w.taskLease())
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	metrics.TasksActive.WithLabelValues(task.ProviderID, task.Kind).Inc()
	defer metrics.TasksActive.WithLabelValues(task.ProviderID, task.Kind).Dec()
	if task.ProviderID != providers.Leonardo {
		return w.fail(ctx, id, leaseID, "provider_unavailable", providers.ErrUnsupported)
	}
	stopLease := w.startLeaseHeartbeat(ctx, cancel, id, leaseID)
	defer stopLease()
	var req domain.AudioRequest
	if err := json.Unmarshal(task.Request, &req); err != nil {
		if isSubmittedGenerationTask(task) {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "invalid_request_state", err.Error())
		}
		return w.fail(ctx, id, leaseID, "invalid_request", err)
	}
	if isSubmittedGenerationTask(task) {
		if task.UpstreamDeadlineAt != nil && !task.UpstreamDeadlineAt.After(time.Now()) {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "upstream_deadline_exceeded", "upstream generation deadline exceeded")
		}
		account, accountErr := w.Store.GetAccount(ctx, *task.AccountID)
		if accountErr != nil {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "account_not_found", accountErr.Error())
		}
		if account.Status == "rate_limited" || account.Status == "cooldown" || (account.CooldownUntil != nil && account.CooldownUntil.After(time.Now())) {
			return w.yieldForCooldown(ctx, id, leaseID, account)
		}
		account, token, tokenErr := w.Accounts.Token(ctx, account)
		if tokenErr != nil {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "session_error", tokenErr.Error(), 30*time.Second)
		}
		client, clientErr := leonardo.New(account.ProxyURL, account.UserAgent, w.Store.GetSettingString(ctx, "schema_version", w.Config.SchemaVersion))
		if clientErr != nil {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "client_error", clientErr.Error(), 30*time.Second)
		}
		return w.pollAudio(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
	}
	if err := w.validateTaskAPIKey(ctx, task); err != nil {
		return w.fail(ctx, id, leaseID, "api_key_disabled", err)
	}
	if task.Status != domain.TaskReserving || task.AccountID == nil {
		return w.fail(ctx, id, leaseID, "invalid_task_state", errors.New("task has no reserved account for submission"))
	}
	account, err := w.Store.GetAccount(ctx, *task.AccountID)
	if err != nil {
		return w.fail(ctx, id, leaseID, "account_not_found", err)
	}
	if !submissionAccountRouteable(account) {
		return w.yieldForReroute(ctx, id, leaseID)
	}
	account, token, err := w.Accounts.Token(ctx, account)
	if err != nil {
		return w.yieldForReroute(ctx, id, leaseID)
	}
	if !submissionAccountRouteable(account) {
		return w.yieldForReroute(ctx, id, leaseID)
	}
	tokensBefore := account.TotalTokens()
	if err := w.updateTokens(ctx, id, leaseID, &tokensBefore, nil); err != nil {
		return err
	}
	model, err := w.Store.GetModel(ctx, task.Model)
	if err != nil {
		return w.fail(ctx, id, leaseID, "model_not_found", err)
	}
	client, err := leonardo.New(account.ProxyURL, account.UserAgent, w.Store.GetSettingString(ctx, "schema_version", w.Config.SchemaVersion))
	if err != nil {
		return w.fail(ctx, id, leaseID, "client_error", err)
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForProviderAccountSubmit(ctx, account.ProviderID, account.ID); err != nil {
		return w.fail(ctx, id, leaseID, "account_submit_interval", err)
	}
	account, proceed, err = w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if active, err := w.Store.IsTaskPricingRuleActive(ctx, id); err != nil {
		return err
	} else if !active {
		return w.fail(ctx, id, leaseID, "cost_rule_unavailable", errors.New("price rule was disabled before upstream submission"))
	}
	voiceID, _ := audioopts.VoiceID(req.Voice)
	upstreamRequest := leonardo.BuildAudioGenerationRequest(leonardo.GenerateAudioRequest{
		Model: model.UpstreamModel, Prompt: req.Prompt, Quantity: req.N, Duration: req.Duration, DurationMinutes: req.DurationMinutes,
		ForceInstrumental: req.ForceInstrumental, Loop: req.Loop, VoiceID: voiceID, LanguageCode: req.Language,
		PromptInfluence: req.PromptInfluence, Public: false,
	})
	upstreamDeadline := time.Now().Add(w.Config.TaskTimeout)
	if err = w.prepareSubmission(ctx, id, leaseID, account.ID, upstreamRequest, upstreamDeadline); err != nil {
		return err
	}
	gen, err := client.SubmitGeneration(ctx, token, account.TeamID, upstreamRequest)
	if err != nil {
		if submissionUncertain(err) {
			w.recordAccountFailure(ctx, account, err)
			return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "submission_uncertain", err.Error())
		}
		w.recordAccountFailure(ctx, account, err)
		return w.fail(ctx, id, leaseID, "generate_failed", err)
	}
	w.clearAccountFailures(ctx, account.ID)
	if err = w.recordSubmission(ctx, id, leaseID, account.ID, gen); err != nil {
		return err
	}
	return w.pollAudio(ctx, id, leaseID, gen.GenerationID, account, token, client, req, start, &upstreamDeadline)
}

func (w *Worker) pollAudio(ctx context.Context, id, leaseID uuid.UUID, generationID string, account domain.Account, token string, client *leonardo.Client, req domain.AudioRequest, start time.Time, deadline *time.Time) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "audio generation deadline exceeded")
			}
			status, err := client.Status(ctx, token, account.TeamID, generationID)
			if err != nil {
				w.Log.Warn("audio poll failed", "task_id", id, "error", err)
				if retryAt := w.recordAccountFailure(ctx, account, err); retryAt != nil {
					_, yieldErr := w.Store.YieldTaskLease(ctx, id, leaseID, *retryAt)
					return yieldErr
				}
				continue
			}
			known := status == "PENDING" || status == "COMPLETE" || status == "FAILED"
			if ok, statusErr := w.Store.RecordTaskUpstreamStatusOwned(ctx, id, leaseID, known); statusErr != nil {
				return statusErr
			} else if !ok {
				return errors.New("task execution lease is no longer owned")
			}
			switch status {
			case "FAILED":
				return w.failUpstreamGeneration(ctx, id, leaseID, generationID, account, token, client, "audio")
			case "COMPLETE":
				outputs, err := client.AudioResult(ctx, token, account.TeamID, generationID)
				if err != nil {
					w.recordAccountFailure(ctx, account, err)
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "result_failed", err.Error(), 15*time.Second)
				}
				result := domain.AudioResult{Created: time.Now().Unix()}
				for _, output := range outputs {
					result.Data = append(result.Data, domain.AudioOutput{ID: output.ID, URL: output.URL, MediaType: audioMediaType(output.URL), Duration: audioDuration(req)})
				}
				if len(result.Data) == 0 {
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Leonardo returned no audio file URL", 15*time.Second)
				}
				if err := w.complete(ctx, id, leaseID, generationID, account, token, result, start); err != nil {
					return err
				}
				return nil
			default:
				if err := w.update(ctx, id, leaseID, upstreamTaskState(status), 0, &account.ID, generationID, nil, "", ""); err != nil {
					return err
				}
			}
		}
	}
}

func audioDuration(req domain.AudioRequest) int {
	if req.Model == "music-v1" {
		return req.DurationMinutes * 60
	}
	if req.Model == "sound-effects-v2" {
		return req.Duration
	}
	return 0
}

func audioMediaType(rawURL string) string {
	path := strings.ToLower(strings.SplitN(rawURL, "?", 2)[0])
	switch {
	case strings.HasSuffix(path, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(path, ".ogg"):
		return "audio/ogg"
	case strings.HasSuffix(path, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(path, ".flac"):
		return "audio/flac"
	case strings.HasSuffix(path, ".aac"):
		return "audio/aac"
	default:
		return "audio/mpeg"
	}
}
