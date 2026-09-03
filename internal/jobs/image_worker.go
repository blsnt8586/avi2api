package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"strings"
	"time"
)

func (w *Worker) processImage(parent context.Context, id uuid.UUID) error {
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
	stopLease := w.startLeaseHeartbeat(ctx, cancel, id, leaseID)
	defer stopLease()
	var req domain.ImageRequest
	if err := json.Unmarshal(task.Request, &req); err != nil {
		if isSubmittedGenerationTask(task) {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "invalid_request_state", err.Error())
		}
		return w.fail(ctx, id, leaseID, "invalid_request", err)
	}
	defer w.cleanupTerminalImageAssets(id, req)
	switch task.ProviderID {
	case providers.Adobe:
		return w.processAdobeImage(ctx, id, leaseID, task, req, start)
	case providers.CreativeFabrica:
		return w.processCreativeFabricaImage(ctx, id, leaseID, task, req, start)
	case providers.Leonardo:
	default:
		return w.fail(ctx, id, leaseID, "provider_unavailable", providers.ErrUnsupported)
	}
	if isSubmittedGenerationTask(task) {
		if task.UpstreamDeadlineAt != nil && !task.UpstreamDeadlineAt.After(time.Now()) {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "upstream_deadline_exceeded", "upstream generation deadline exceeded")
		}
		account, err := w.Store.GetAccount(ctx, *task.AccountID)
		if err != nil {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "account_not_found", err.Error())
		}
		if account.Status == "rate_limited" || account.Status == "cooldown" || (account.CooldownUntil != nil && account.CooldownUntil.After(time.Now())) {
			return w.yieldForCooldown(ctx, id, leaseID, account)
		}
		account, token, err := w.Accounts.Token(ctx, account)
		if err != nil {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "session_error", err.Error(), 30*time.Second)
		}
		client, err := leonardo.New(account.ProxyURL, account.UserAgent, w.Store.GetSettingString(ctx, "schema_version", w.Config.SchemaVersion))
		if err != nil {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "client_error", err.Error(), 30*time.Second)
		}
		return w.pollGeneration(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
	}
	if err := w.validateTaskAPIKey(ctx, task); err != nil {
		return w.fail(ctx, id, leaseID, "api_key_disabled", err)
	}
	if w.Providers != nil {
		if _, err := w.Providers.Get(task.ProviderID); err != nil {
			return w.fail(ctx, id, leaseID, "provider_unavailable", err)
		}
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
	width, height, err := imageopts.ParseSize(req.Model, req.Size)
	if err != nil {
		return w.fail(ctx, id, leaseID, "invalid_size", err)
	}
	quantity := req.N
	if quantity == 0 {
		quantity = 1
	}
	public := false
	var defaults struct {
		Width, Height, Quantity int
		StyleIDs                []string `json:"style_ids"`
		PromptEnhance           string   `json:"prompt_enhance"`
	}
	_ = json.Unmarshal(model.Defaults, &defaults)
	if req.Size == "" && defaults.Width > 0 {
		width, height = defaults.Width, defaults.Height
	}
	if req.N == 0 && defaults.Quantity > 0 {
		quantity = defaults.Quantity
	}
	styles := req.StyleIDs
	if len(styles) == 0 {
		styles = defaults.StyleIDs
	}
	promptEnhance := defaults.PromptEnhance
	if promptEnhance == "" && strings.EqualFold(model.UpstreamModel, "gpt-image-2") {
		promptEnhance = "AUTO"
	}
	schemaVersion := w.Store.GetSettingString(ctx, "schema_version", w.Config.SchemaVersion)
	client, err := leonardo.New(account.ProxyURL, account.UserAgent, schemaVersion)
	if err != nil {
		return w.fail(ctx, id, leaseID, "client_error", err)
	}
	sources := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		sources = append([]domain.SourceImage{*req.SourceImage}, sources...)
	}
	imageReferences := make([]leonardo.ImageReference, 0, len(sources)+len(req.ReferenceImages))
	totalSources := len(sources) + len(req.ReferenceImages)
	if totalSources > 0 {
		if err = w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
		for i, source := range sources {
			raw, decodeErr := base64.StdEncoding.DecodeString(source.Data)
			if decodeErr != nil {
				return w.fail(ctx, id, leaseID, "invalid_source_image", decodeErr)
			}
			uploaded, uploadErr := client.UploadInitImage(ctx, token, account.TeamID, source.Filename, source.MediaType, raw)
			if uploadErr != nil {
				w.recordAccountFailure(ctx, account, uploadErr)
				return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
			}
			imageReferences = append(imageReferences, leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED", Strength: req.ReferenceStrength})
			progress := 15 + ((i + 1) * 4 / totalSources)
			if err := w.update(ctx, id, leaseID, domain.TaskUploading, progress, &account.ID, "", nil, "", ""); err != nil {
				return err
			}
		}
		for i, source := range req.ReferenceImages {
			uploaded, uploadErr := w.uploadTaskImage(ctx, client, token, account.TeamID, source)
			if uploadErr != nil {
				w.recordAccountFailure(ctx, account, uploadErr)
				return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
			}
			imageReferences = append(imageReferences, leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED", Strength: req.ReferenceStrength})
			progress := 15 + ((len(sources) + i + 1) * 4 / totalSources)
			if err := w.update(ctx, id, leaseID, domain.TaskUploading, progress, &account.ID, "", nil, "", ""); err != nil {
				return err
			}
		}
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
	upstreamRequest := leonardo.BuildImageGenerationRequest(leonardo.GenerateRequest{
		Model: model.UpstreamModel, Prompt: req.Prompt, Width: width, Height: height,
		Quantity: quantity, Public: public, StyleIDs: styles, ReferenceIDs: req.ReferenceIDs,
		Quality: req.Quality, PromptEnhance: promptEnhance, ImageReferences: imageReferences,
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
	return w.pollGeneration(ctx, id, leaseID, gen.GenerationID, account, token, client, req, start, &upstreamDeadline)
}

func (w *Worker) pollGeneration(ctx context.Context, id, leaseID uuid.UUID, generationID string, account domain.Account, token string, client *leonardo.Client, req domain.ImageRequest, start time.Time, deadline *time.Time) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "image generation deadline exceeded")
			}
			status, err := client.Status(ctx, token, account.TeamID, generationID)
			if err != nil {
				w.Log.Warn("poll failed", "task_id", id, "error", err)
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
				return w.failUpstreamGeneration(ctx, id, leaseID, generationID, account, token, client, "image")
			case "COMPLETE":
				generation, err := client.Result(ctx, token, account.TeamID, generationID)
				if err != nil {
					w.recordAccountFailure(ctx, account, err)
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "result_failed", err.Error(), 15*time.Second)
				}
				result := domain.ImageResult{
					Created: time.Now().Unix(), NSFW: generation.NSFW,
					ModerationClassifications: generation.ModerationClassifications,
				}
				for _, output := range generation.Outputs {
					result.Data = append(result.Data, domain.ImageOutput{
						ID: output.ID, URL: output.URL, RevisedPrompt: req.Prompt, NSFW: output.NSFW,
						ModerationClassifications: output.ModerationClassifications,
					})
				}
				if len(result.Data) == 0 {
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Leonardo returned no images", 15*time.Second)
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

func parseSize(size string) (int, int, error) { return imageopts.ParseSize("", size) }

func (w *Worker) cleanupTerminalImageAssets(id uuid.UUID, request domain.ImageRequest) {
	if w.Assets == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	task, err := w.Store.GetTask(ctx, id)
	if err != nil {
		return
	}
	switch task.Status {
	case domain.TaskSucceeded, domain.TaskFailed, domain.TaskCancelled, domain.TaskSubmissionUncertain:
		if err := w.Assets.CleanupImageRequest(request); err != nil {
			w.Log.Warn("cleanup task image assets", "task_id", id, "error", err)
		}
	}
}
