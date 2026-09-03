package jobs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

// processCreativeFabricaImage owns the asynchronous Flow lifecycle.  The
// HTTP layer only persists a task; this worker is the only place that submits
// the provider mutation and settles the reservation.
func (w *Worker) processCreativeFabricaImage(ctx context.Context, id, leaseID uuid.UUID, task domain.Task, req domain.ImageRequest, start time.Time) error {
	if isSubmittedGenerationTask(task) {
		account, token, client, err := w.resumeCreativeFabricaTask(ctx, id, leaseID, task)
		if err != nil {
			return err
		}
		return w.pollCreativeFabricaImage(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
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
	account, token, err := w.Accounts.CreativeFabricaToken(ctx, account)
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
	if _, err := w.Store.GetModelProviderConfig(ctx, providers.CreativeFabrica, req.Model); err != nil {
		return w.fail(ctx, id, leaseID, "model_not_found", err)
	}
	client, err := w.creativeFabricaTaskClient(ctx, account)
	if err != nil {
		return w.fail(ctx, id, leaseID, "client_error", err)
	}
	size := strings.ToLower(strings.TrimSpace(req.Size))
	if size == "" || size == "auto" {
		size = "1024x1024"
	}
	if _, _, err := creativeFabricaDimensions(size); err != nil {
		return w.fail(ctx, id, leaseID, "invalid_size", err)
	}
	quantity := req.N
	if quantity == 0 {
		quantity = 1
	}
	if quantity < 1 || quantity > 4 {
		return w.fail(ctx, id, leaseID, "invalid_quantity", errors.New("n must be between 1 and 4"))
	}

	sourceURL, referenceURLs, err := w.uploadCreativeFabricaImageInputs(ctx, id, leaseID, account, token, client, req)
	if err != nil {
		return err
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForProviderAccountSubmit(ctx, providers.CreativeFabrica, account.ID); err != nil {
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
	upstreamRequest, err := creativefabrica.BuildImageFlowRequest(req.Model, req.Prompt, size, quantity, sourceURL, referenceURLs)
	if err != nil {
		return w.fail(ctx, id, leaseID, "unsupported_model", err)
	}
	deadline := time.Now().Add(w.Config.TaskTimeout)
	if err := w.prepareSubmission(ctx, id, leaseID, account.ID, upstreamRequest, deadline); err != nil {
		return err
	}
	job, _, err := client.CreateFlow(ctx, token, upstreamRequest)
	if err != nil {
		w.recordAccountFailure(ctx, account, err)
		if submissionUncertain(err) {
			return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "submission_uncertain", err.Error())
		}
		return w.fail(ctx, id, leaseID, "generate_failed", err)
	}
	w.clearAccountFailures(ctx, account.ID)
	if err := w.recordCreativeFabricaSubmission(ctx, id, leaseID, account.ID, job); err != nil {
		return err
	}
	return w.pollCreativeFabricaImage(ctx, id, leaseID, creativeFabricaJobID(job), account, token, client, req, start, &deadline)
}

// processCreativeFabricaVideo owns the asynchronous Media Matrix session
// lifecycle, including signed input uploads and session polling.
func (w *Worker) processCreativeFabricaVideo(ctx context.Context, id, leaseID uuid.UUID, task domain.Task, req domain.VideoRequest, start time.Time) error {
	if isSubmittedGenerationTask(task) {
		account, token, client, err := w.resumeCreativeFabricaTask(ctx, id, leaseID, task)
		if err != nil {
			return err
		}
		return w.pollCreativeFabricaVideo(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
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
	account, token, err := w.Accounts.CreativeFabricaToken(ctx, account)
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
	if _, err := w.Store.GetModelProviderConfig(ctx, providers.CreativeFabrica, req.Model); err != nil {
		return w.fail(ctx, id, leaseID, "model_not_found", err)
	}
	spec, ok := videospec.GetForProvider(providers.CreativeFabrica, req.Model)
	if !ok {
		return w.fail(ctx, id, leaseID, "invalid_model", fmt.Errorf("unknown Creative Fabrica video model %s", req.Model))
	}
	if err := normalizeCreativeFabricaVideoOptions(&req, spec); err != nil {
		return w.fail(ctx, id, leaseID, "invalid_request", err)
	}
	client, err := w.creativeFabricaTaskClient(ctx, account)
	if err != nil {
		return w.fail(ctx, id, leaseID, "client_error", err)
	}
	inputs, err := w.collectCreativeFabricaVideoInputs(ctx, id, leaseID, account, req)
	if err != nil {
		return err
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForProviderAccountSubmit(ctx, providers.CreativeFabrica, account.ID); err != nil {
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
	frameRequests := make([]creativefabrica.VideoFrameRequest, 0, len(inputs))
	for _, input := range inputs {
		frameRequests = append(frameRequests, input.Request)
	}
	upstreamRequest, err := creativefabrica.BuildVideoSessionRequest(req.Model, req.Prompt, req.Size, req.Resolution, req.Duration, frameRequests)
	if err != nil {
		return w.fail(ctx, id, leaseID, "unsupported_model", err)
	}
	deadline := time.Now().Add(w.Config.TaskTimeout)
	if err := w.prepareSubmission(ctx, id, leaseID, account.ID, upstreamRequest, deadline); err != nil {
		return err
	}
	job, response, err := client.InitiateSession(ctx, token, upstreamRequest)
	if err != nil {
		w.recordAccountFailure(ctx, account, err)
		if submissionUncertain(err) {
			return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "submission_uncertain", err.Error())
		}
		return w.fail(ctx, id, leaseID, "generate_failed", err)
	}
	w.clearAccountFailures(ctx, account.ID)
	if err := w.recordCreativeFabricaSubmission(ctx, id, leaseID, account.ID, job); err != nil {
		return err
	}
	if err := w.uploadCreativeFabricaVideoFrames(ctx, id, leaseID, account, client, inputs, response); err != nil {
		w.recordAccountFailure(ctx, account, err)
		return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "frame_upload_uncertain", err.Error())
	}
	return w.pollCreativeFabricaVideo(ctx, id, leaseID, creativeFabricaJobID(job), account, token, client, req, start, &deadline)
}

func (w *Worker) resumeCreativeFabricaTask(ctx context.Context, id, leaseID uuid.UUID, task domain.Task) (domain.Account, string, *creativefabrica.Client, error) {
	if task.UpstreamDeadlineAt != nil && !task.UpstreamDeadlineAt.After(time.Now()) {
		return domain.Account{}, "", nil, w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "upstream_deadline_exceeded", "Creative Fabrica generation deadline exceeded")
	}
	if task.AccountID == nil {
		return domain.Account{}, "", nil, w.markSubmissionUncertain(ctx, id, leaseID, nil, "account_not_found", "submitted task has no account")
	}
	account, err := w.Store.GetAccount(ctx, *task.AccountID)
	if err != nil {
		return account, "", nil, w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "account_not_found", err.Error())
	}
	if account.Status == "rate_limited" || account.Status == "cooldown" || (account.CooldownUntil != nil && account.CooldownUntil.After(time.Now())) {
		return account, "", nil, w.yieldForCooldown(ctx, id, leaseID, account)
	}
	account, token, err := w.Accounts.CreativeFabricaToken(ctx, account)
	if err != nil {
		return account, "", nil, w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "session_error", err.Error(), 30*time.Second)
	}
	client, err := w.creativeFabricaTaskClient(ctx, account)
	if err != nil {
		return account, "", nil, w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "client_error", err.Error(), 30*time.Second)
	}
	return account, token, client, nil
}

func (w *Worker) creativeFabricaTaskClient(ctx context.Context, account domain.Account) (*creativefabrica.Client, error) {
	client, err := creativefabrica.New(account.ProxyURL, account.UserAgent)
	if err != nil {
		return nil, err
	}
	if w.Accounts == nil {
		return client, nil
	}
	cookie, err := w.Accounts.CreativeFabricaCookieHeader(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	client.CookieHeader = cookie
	return client, nil
}

func (w *Worker) uploadCreativeFabricaImageInputs(ctx context.Context, id, leaseID uuid.UUID, account domain.Account, token string, client *creativefabrica.Client, req domain.ImageRequest) (string, []string, error) {
	sources := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		sources = append([]domain.SourceImage{*req.SourceImage}, sources...)
	}
	total := len(sources) + len(req.ReferenceImages)
	if total == 0 {
		return "", nil, nil
	}
	if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
		return "", nil, err
	}
	var sourceURL string
	references := make([]string, 0, total)
	uploaded := 0
	upload := func(filename, mediaType string, raw []byte, asSource bool) error {
		uploadURL, err := client.CreateUploadURL(ctx, token, filename, mediaType, int64(len(raw)))
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		if err := client.UploadToURL(ctx, uploadURL, mediaType, raw); err != nil {
			w.recordAccountFailure(ctx, account, err)
			return w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		if asSource && sourceURL == "" {
			sourceURL = uploadURL.URL
		} else {
			references = append(references, uploadURL.URL)
		}
		uploaded++
		return w.update(ctx, id, leaseID, domain.TaskUploading, 15+uploaded*4/total, &account.ID, "", nil, "", "")
	}
	for _, source := range sources {
		raw, err := base64.StdEncoding.DecodeString(source.Data)
		if err != nil {
			return "", nil, w.fail(ctx, id, leaseID, "invalid_source_image", err)
		}
		if err := upload(source.Filename, source.MediaType, raw, sourceURL == ""); err != nil {
			return "", nil, err
		}
	}
	for _, source := range req.ReferenceImages {
		raw, err := w.readTaskAsset(source, w.Config.MaxImageBytes)
		if err != nil {
			return "", nil, w.fail(ctx, id, leaseID, "invalid_source_image", err)
		}
		if err := upload(source.Filename, source.MediaType, raw, false); err != nil {
			return "", nil, err
		}
	}
	return sourceURL, references, nil
}

type creativeFabricaVideoInput struct {
	Request   creativefabrica.VideoFrameRequest
	MediaType string
	Data      []byte
}

func (w *Worker) collectCreativeFabricaVideoInputs(ctx context.Context, id, leaseID uuid.UUID, account domain.Account, req domain.VideoRequest) ([]creativeFabricaVideoInput, error) {
	inputs := make([]creativeFabricaVideoInput, 0)
	total := len(req.SourceImages) + len(req.ReferenceImages) + len(req.ReferenceVideos) + len(req.AudioReferences())
	if req.SourceImage != nil {
		total++
	}
	if req.StartFrame != nil {
		total++
	}
	if req.EndFrame != nil {
		total++
	}
	if total == 0 {
		return inputs, nil
	}
	if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
		return nil, err
	}
	collected := 0
	collect := func(source domain.SourceMedia, frameType, referenceType string, limit int64) error {
		raw, err := w.readTaskAsset(source, limit)
		if err != nil {
			return w.fail(ctx, id, leaseID, "invalid_source_media", err)
		}
		frame := creativefabricaVideoFrameRequest(frameType, referenceType, source.Filename, source.MediaType, raw)
		inputs = append(inputs, creativeFabricaVideoInput{
			Request:   frame,
			MediaType: source.MediaType,
			Data:      raw,
		})
		collected++
		return w.update(ctx, id, leaseID, domain.TaskUploading, 15+collected*4/total, &account.ID, "", nil, "", "")
	}
	legacyImages := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		legacyImages = append([]domain.SourceImage{*req.SourceImage}, legacyImages...)
	}
	for _, source := range legacyImages {
		raw, err := base64.StdEncoding.DecodeString(source.Data)
		if err != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", err)
		}
		inputs = append(inputs, creativeFabricaVideoInput{
			Request:   creativefabricaVideoFrameRequest("VIDEO_FRAME_TYPE_REFERENCE", "VIDEO_FRAME_REFERENCE_TYPE_SUBJECT", source.Filename, source.MediaType, raw),
			MediaType: source.MediaType,
			Data:      raw,
		})
		collected++
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15+collected*4/total, &account.ID, "", nil, "", ""); err != nil {
			return nil, err
		}
	}
	for _, source := range req.ReferenceImages {
		if err := collect(source, "VIDEO_FRAME_TYPE_REFERENCE", "VIDEO_FRAME_REFERENCE_TYPE_SUBJECT", w.Config.MaxImageBytes); err != nil {
			return nil, err
		}
	}
	if req.StartFrame != nil {
		if err := collect(*req.StartFrame, "VIDEO_FRAME_TYPE_FIRST", "", w.Config.MaxImageBytes); err != nil {
			return nil, err
		}
	}
	if req.EndFrame != nil {
		if err := collect(*req.EndFrame, "VIDEO_FRAME_TYPE_LAST", "", w.Config.MaxImageBytes); err != nil {
			return nil, err
		}
	}
	for _, source := range req.ReferenceVideos {
		if err := collect(source, "VIDEO_FRAME_TYPE_REFERENCE", "VIDEO_FRAME_REFERENCE_TYPE_SUBJECT", w.Config.MaxVideoBytes); err != nil {
			return nil, err
		}
	}
	for _, source := range req.AudioReferences() {
		if err := collect(source, "VIDEO_FRAME_TYPE_ATTACHMENT", "", w.Config.MaxAudioBytes); err != nil {
			return nil, err
		}
	}
	return inputs, nil
}

func creativefabricaVideoFrameRequest(frameType, referenceType, filename, mediaType string, raw []byte) creativefabrica.VideoFrameRequest {
	request := creativefabrica.VideoFrameRequest{
		Type: frameType, FileSize: int64(len(raw)), FileName: filename, ReferenceType: referenceType,
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(mediaType, "image/") || mediaType == "application/octet-stream" {
		request.Width, request.Height = creativeFabricaImageDimensions(raw, mediaType, filename)
	}
	return request
}

func (w *Worker) uploadCreativeFabricaVideoFrames(ctx context.Context, id, leaseID uuid.UUID, account domain.Account, client *creativefabrica.Client, inputs []creativeFabricaVideoInput, response map[string]any) error {
	if len(inputs) == 0 {
		return nil
	}
	uploadURLs, err := creativefabrica.VideoFrameUploadURLs(response)
	if err != nil {
		return err
	}
	if len(uploadURLs) != len(inputs) {
		return fmt.Errorf("Creative Fabrica returned %d video frame upload URLs for %d inputs", len(uploadURLs), len(inputs))
	}
	for index, input := range inputs {
		if err := client.UploadVideoFrameToURL(ctx, uploadURLs[index], input.MediaType, input.Data); err != nil {
			return fmt.Errorf("upload Creative Fabrica video frame %d: %w", index+1, err)
		}
		progress := 40 + (index+1)*20/len(inputs)
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, progress, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) pollCreativeFabricaImage(ctx context.Context, id, leaseID uuid.UUID, flowID string, account domain.Account, token string, client *creativefabrica.Client, req domain.ImageRequest, start time.Time, deadline *time.Time) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "Creative Fabrica image generation deadline exceeded")
			}
			result, err := client.GetFlow(ctx, token, flowID)
			if err != nil {
				w.Log.Warn("Creative Fabrica image poll failed", "task_id", id, "error", err)
				if accounts.IsCreativeFabricaAuthenticationRejected(err) {
					w.recordAccountFailure(ctx, account, err)
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "session_error", err.Error(), 30*time.Second)
				}
				if retryAt := w.recordAccountFailure(ctx, account, err); retryAt != nil {
					return w.yieldAt(ctx, id, leaseID, *retryAt)
				}
				continue
			}
			known := result.Status == "PENDING" || result.Status == "COMPLETE" || result.Status == "FAILED"
			if ok, statusErr := w.Store.RecordTaskUpstreamStatusOwned(ctx, id, leaseID, known); statusErr != nil {
				return statusErr
			} else if !ok {
				return errors.New("task execution lease is no longer owned")
			}
			switch result.Status {
			case "FAILED":
				message := strings.TrimSpace(result.Error)
				if message == "" {
					message = "Creative Fabrica image generation failed"
				}
				return w.failAfterSubmission(ctx, id, leaseID, "upstream_failed", message, map[string]any{"source": providers.CreativeFabrica, "upstream_status": result.Status}, account, token)
			case "COMPLETE":
				output := domain.ImageResult{Created: time.Now().Unix()}
				for _, item := range result.Outputs {
					if item.URL == "" {
						continue
					}
					output.Data = append(output.Data, domain.ImageOutput{ID: item.ID, URL: item.URL, RevisedPrompt: req.Prompt})
				}
				if len(output.Data) == 0 {
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Creative Fabrica returned no images", 15*time.Second)
				}
				return w.complete(ctx, id, leaseID, flowID, account, token, output, start)
			default:
				progress := result.Progress
				if progress < 1 {
					progress = 20
				}
				if err := w.update(ctx, id, leaseID, domain.TaskPolling, progress, &account.ID, flowID, nil, "", ""); err != nil {
					return err
				}
			}
		}
	}
}

func (w *Worker) pollCreativeFabricaVideo(ctx context.Context, id, leaseID uuid.UUID, sessionID string, account domain.Account, token string, client *creativefabrica.Client, req domain.VideoRequest, start time.Time, deadline *time.Time) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "Creative Fabrica video generation deadline exceeded")
			}
			result, err := client.GetSession(ctx, token, sessionID)
			if err != nil {
				w.Log.Warn("Creative Fabrica video poll failed", "task_id", id, "error", err)
				if accounts.IsCreativeFabricaAuthenticationRejected(err) {
					w.recordAccountFailure(ctx, account, err)
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "session_error", err.Error(), 30*time.Second)
				}
				if retryAt := w.recordAccountFailure(ctx, account, err); retryAt != nil {
					return w.yieldAt(ctx, id, leaseID, *retryAt)
				}
				continue
			}
			if result.Status == "COMPLETE" && len(result.Outputs) == 0 {
				if fallback, fallbackErr := client.GetSessionMedia(ctx, token, sessionID); fallbackErr == nil && len(fallback.Outputs) > 0 {
					result = fallback
				} else if fallback, fallbackErr := client.GetSessionIterations(ctx, token, sessionID); fallbackErr == nil && len(fallback.Outputs) > 0 {
					result = fallback
				}
			}
			known := result.Status == "PENDING" || result.Status == "COMPLETE" || result.Status == "FAILED"
			if ok, statusErr := w.Store.RecordTaskUpstreamStatusOwned(ctx, id, leaseID, known); statusErr != nil {
				return statusErr
			} else if !ok {
				return errors.New("task execution lease is no longer owned")
			}
			switch result.Status {
			case "FAILED":
				message := strings.TrimSpace(result.Error)
				if message == "" {
					message = "Creative Fabrica video generation failed"
				}
				return w.failAfterSubmission(ctx, id, leaseID, "upstream_failed", message, map[string]any{"source": providers.CreativeFabrica, "upstream_status": result.Status}, account, token)
			case "COMPLETE":
				output := domain.VideoResult{Created: time.Now().Unix()}
				for _, item := range result.Outputs {
					if item.URL == "" {
						continue
					}
					width, height, _ := creativeFabricaDimensions(req.Size)
					output.Data = append(output.Data, domain.VideoOutput{ID: item.ID, URL: item.URL, MediaType: "video/mp4", Duration: req.Duration, Width: width, Height: height})
				}
				if len(output.Data) == 0 {
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Creative Fabrica returned no video", 15*time.Second)
				}
				return w.complete(ctx, id, leaseID, sessionID, account, token, output, start)
			default:
				progress := result.Progress
				if progress < 1 {
					progress = 20
				}
				if err := w.update(ctx, id, leaseID, domain.TaskPolling, progress, &account.ID, sessionID, nil, "", ""); err != nil {
					return err
				}
			}
		}
	}
}

func creativeFabricaJobID(job creativefabrica.Job) string {
	if job.ID != "" {
		return job.ID
	}
	if job.FlowID != "" {
		return job.FlowID
	}
	return job.SessionID
}

func creativeFabricaDimensions(size string) (int, int, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid Creative Fabrica size %q", size)
	}
	width, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid Creative Fabrica size %q", size)
	}
	height, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid Creative Fabrica size %q", size)
	}
	if width < 64 || height < 64 || width > 8192 || height > 8192 {
		return 0, 0, fmt.Errorf("invalid Creative Fabrica size %q", size)
	}
	return width, height, nil
}

func normalizeCreativeFabricaVideoOptions(req *domain.VideoRequest, spec videospec.Spec) error {
	if req == nil {
		return errors.New("video request is required")
	}
	if req.GenerateAudio == nil && spec.SupportsGenerateAudio {
		value := true
		req.GenerateAudio = &value
	}
	if spec.AlwaysGenerateAudio && req.GenerateAudio != nil && !*req.GenerateAudio {
		return fmt.Errorf("%s always generates native audio; generate_audio cannot be false", req.Model)
	}
	if req.Size == "" {
		if size, ok := spec.DefaultSizeForResolution(req.Resolution); ok {
			req.Size = size
		} else {
			req.Size = spec.DefaultSize
		}
	}
	if !spec.SupportsSize(req.Size) {
		return fmt.Errorf("unsupported size %s for %s", req.Size, req.Model)
	}
	if req.Duration == 0 {
		req.Duration = spec.DefaultDuration
	}
	if !spec.SupportsDuration(req.Duration) {
		return fmt.Errorf("unsupported duration %d for %s", req.Duration, req.Model)
	}
	if req.Resolution == "" {
		if resolution, ok := spec.ResolutionForSize(req.Size); ok {
			req.Resolution = resolution
		} else {
			req.Resolution = spec.DefaultResolution
		}
	}
	if !spec.SupportsResolution(req.Resolution) {
		return fmt.Errorf("unsupported resolution %s for %s", req.Resolution, req.Model)
	}
	if resolution, ok := spec.ResolutionForSize(req.Size); ok && resolution != req.Resolution {
		return fmt.Errorf("size %s requires resolution %s for %s", req.Size, resolution, req.Model)
	}
	return nil
}
