package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
	"io"
	"math"
	"path/filepath"
	"strings"
	"time"
)

func (w *Worker) processVideo(parent context.Context, id uuid.UUID) error {
	start := time.Now()
	metrics.TasksActive.Inc()
	defer metrics.TasksActive.Dec()
	ctx, cancel := context.WithTimeout(parent, w.Config.TaskTimeout)
	defer cancel()
	task, leaseID, claimed, err := w.Store.ClaimTask(ctx, id, w.taskLease())
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	stopLease := w.startLeaseHeartbeat(ctx, cancel, id, leaseID)
	defer stopLease()
	var req domain.VideoRequest
	if err := json.Unmarshal(task.Request, &req); err != nil {
		if isSubmittedGenerationTask(task) {
			return w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "invalid_request_state", err.Error())
		}
		return w.fail(ctx, id, leaseID, "invalid_request", err)
	}
	defer w.cleanupTerminalVideoAssets(id, req)
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
		return w.pollVideo(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
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
	spec, ok := videospec.Get(req.Model)
	if !ok {
		return w.fail(ctx, id, leaseID, "invalid_model", fmt.Errorf("unknown video model %s", req.Model))
	}
	width, height, err := resolveVideoDimensions(spec, req.Size, req.Resolution)
	if err != nil {
		return w.fail(ctx, id, leaseID, "invalid_size", err)
	}
	if len(req.ReferenceVideos) > 0 && spec.OmitDurationWithVideo {
		width, height = 0, 0
	}
	public := false
	mode := ""
	if spec.UsesResolutionMode {
		mode = "RESOLUTION_" + strings.TrimSuffix(strings.ToUpper(req.Resolution), "P")
	}
	resolution := ""
	if req.Model == "flux-3-video" {
		resolution = req.Resolution
	}
	client, err := leonardo.New(account.ProxyURL, account.UserAgent, w.Store.GetSettingString(ctx, "schema_version", w.Config.SchemaVersion))
	if err != nil {
		return w.fail(ctx, id, leaseID, "client_error", err)
	}
	sources := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		sources = append([]domain.SourceImage{*req.SourceImage}, sources...)
	}
	imageReferences := make([]leonardo.ImageReference, 0, len(sources))
	audioSources := req.AudioReferences()
	totalUploads := len(sources) + len(req.ReferenceImages) + len(req.ReferenceVideos) + len(audioSources)
	if req.StartFrame != nil {
		totalUploads++
	}
	if req.EndFrame != nil {
		totalUploads++
	}
	uploadedCount := 0
	updateUploadProgress := func() error {
		uploadedCount++
		progress := 15 + (uploadedCount * 4 / totalUploads)
		return w.update(ctx, id, leaseID, domain.TaskUploading, progress, &account.ID, "", nil, "", "")
	}
	if totalUploads > 0 {
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
	}
	if len(sources) > 0 {
		for _, source := range sources {
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
			if err := updateUploadProgress(); err != nil {
				return err
			}
		}
	}
	for _, source := range req.ReferenceImages {
		uploaded, uploadErr := w.uploadTaskImage(ctx, client, token, account.TeamID, source)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		imageReferences = append(imageReferences, leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED", Strength: req.ReferenceStrength})
		if err := updateUploadProgress(); err != nil {
			return err
		}
	}
	var startFrame, endFrame *leonardo.ImageReference
	if req.StartFrame != nil {
		uploaded, uploadErr := w.uploadTaskImage(ctx, client, token, account.TeamID, *req.StartFrame)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		startFrame = &leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED"}
		if err := updateUploadProgress(); err != nil {
			return err
		}
	}
	if req.EndFrame != nil {
		uploaded, uploadErr := w.uploadTaskImage(ctx, client, token, account.TeamID, *req.EndFrame)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		endFrame = &leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED"}
		if err := updateUploadProgress(); err != nil {
			return err
		}
	}
	videoReferences := make([]leonardo.MediaReference, 0, len(req.ReferenceVideos))
	totalReferenceDuration := 0.0
	for _, source := range req.ReferenceVideos {
		uploaded, uploadErr := w.uploadTaskMedia(ctx, client, token, account.TeamID, source)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		if uploaded.Duration <= 0 || uploaded.Width <= 0 || uploaded.Height <= 0 {
			return w.fail(ctx, id, leaseID, "invalid_reference_video", errors.New("Leonardo did not return duration and dimensions for the reference video"))
		}
		if (spec.MinReferenceVideoDimension > 0 || spec.MaxReferenceVideoDimension > 0) && !spec.SupportsReferenceVideoDimensions(uploaded.Width, uploaded.Height) {
			return w.fail(ctx, id, leaseID, "invalid_reference_video_dimensions", fmt.Errorf("reference video is %dx%d; %s requires each edge between %d and %d pixels", uploaded.Width, uploaded.Height, req.Model, spec.MinReferenceVideoDimension, spec.MaxReferenceVideoDimension))
		}
		totalReferenceDuration += uploaded.Duration
		videoReferences = append(videoReferences, leonardo.MediaReference{ID: uploaded.ID, Type: "UPLOADED", Duration: uploaded.Duration, Width: uploaded.Width, Height: uploaded.Height, MotionHasAudio: true})
		if err := updateUploadProgress(); err != nil {
			return err
		}
	}
	if spec.MaxVideoDuration > 0 && totalReferenceDuration > spec.MaxVideoDuration+0.0001 {
		return w.fail(ctx, id, leaseID, "reference_video_too_long", fmt.Errorf("reference videos total %.2f seconds; %s allows at most %.1f seconds", totalReferenceDuration, req.Model, spec.MaxVideoDuration))
	}
	if spec.MinVideoDuration > 0 && totalReferenceDuration+0.0001 < spec.MinVideoDuration {
		return w.fail(ctx, id, leaseID, "reference_video_too_short", fmt.Errorf("reference video is %.2f seconds; %s requires at least %.1f seconds", totalReferenceDuration, req.Model, spec.MinVideoDuration))
	}
	if len(videoReferences) > 0 && spec.OmitDurationWithVideo && math.Abs(totalReferenceDuration-float64(req.Duration)) > 0.051 {
		return w.fail(ctx, id, leaseID, "reference_video_duration_mismatch", fmt.Errorf("reference video is %.2f seconds; duration must match it within 0.05 seconds for exact pricing", totalReferenceDuration))
	}
	audioReferences := make([]leonardo.MediaReference, 0, len(audioSources))
	totalAudioDuration := 0.0
	for _, source := range audioSources {
		uploaded, uploadErr := w.uploadTaskMedia(ctx, client, token, account.TeamID, source)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		if uploaded.Duration <= 0 {
			return w.fail(ctx, id, leaseID, "invalid_reference_audio", errors.New("Leonardo did not return a duration for the reference audio"))
		}
		totalAudioDuration += uploaded.Duration
		audioReferences = append(audioReferences, leonardo.MediaReference{ID: uploaded.ID, Type: "UPLOADED", Duration: uploaded.Duration})
		if err := updateUploadProgress(); err != nil {
			return err
		}
	}
	if spec.MaxAudioDuration > 0 && totalAudioDuration > spec.MaxAudioDuration+0.0001 {
		return w.fail(ctx, id, leaseID, "reference_audio_too_long", fmt.Errorf("reference audio totals %.2f seconds; %s allows at most %.1f seconds", totalAudioDuration, req.Model, spec.MaxAudioDuration))
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForAccountSubmit(ctx, account.ID); err != nil {
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
	upstreamDuration := req.Duration
	if len(videoReferences) > 0 && spec.OmitDurationWithVideo {
		upstreamDuration = 0
	}
	upstreamRequest := leonardo.BuildVideoGenerationRequest(leonardo.GenerateVideoRequest{
		Model: model.UpstreamModel, Prompt: req.Prompt, Width: width, Height: height,
		Duration: upstreamDuration, ResolutionMode: mode, Resolution: resolution, Public: public,
		ImageReferences: imageReferences, StartFrame: startFrame, EndFrame: endFrame,
		VideoReferences: videoReferences, AudioReferences: audioReferences, GenerateAudio: req.GenerateAudio,
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
	return w.pollVideo(ctx, id, leaseID, gen.GenerationID, account, token, client, req, start, &upstreamDeadline)
}

func (w *Worker) uploadTaskImage(ctx context.Context, client *leonardo.Client, token, teamID string, source domain.SourceMedia) (leonardo.UploadResult, error) {
	if w.Assets == nil {
		return leonardo.UploadResult{}, errors.New("task asset storage is not configured")
	}
	file, err := w.Assets.Open(source)
	if err != nil {
		return leonardo.UploadResult{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, w.Config.MaxImageBytes+1))
	if err != nil {
		return leonardo.UploadResult{}, err
	}
	if int64(len(raw)) > w.Config.MaxImageBytes {
		return leonardo.UploadResult{}, errors.New("stored image exceeds configured size limit")
	}
	return client.UploadInitImage(ctx, token, teamID, source.Filename, source.MediaType, raw)
}

func (w *Worker) uploadTaskMedia(ctx context.Context, client *leonardo.Client, token, teamID string, source domain.SourceMedia) (leonardo.UploadedMedia, error) {
	if w.Assets == nil {
		return leonardo.UploadedMedia{}, errors.New("task asset storage is not configured")
	}
	file, err := w.Assets.Open(source)
	if err != nil {
		return leonardo.UploadedMedia{}, err
	}
	defer file.Close()
	return client.UploadMedia(ctx, token, teamID, source.Filename, filepath.Ext(source.Filename), file)
}

func (w *Worker) cleanupTerminalVideoAssets(id uuid.UUID, request domain.VideoRequest) {
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
		if err := w.Assets.CleanupVideoRequest(request); err != nil {
			w.Log.Warn("cleanup task media assets", "task_id", id, "error", err)
		}
	}
}

func (w *Worker) pollVideo(ctx context.Context, id, leaseID uuid.UUID, generationID string, account domain.Account, token string, client *leonardo.Client, req domain.VideoRequest, start time.Time, deadline *time.Time) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "video generation deadline exceeded")
			}
			status, err := client.Status(ctx, token, account.TeamID, generationID)
			if err != nil {
				w.Log.Warn("video poll failed", "task_id", id, "error", err)
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
				return w.failUpstreamGeneration(ctx, id, leaseID, generationID, account, token, client, "video")
			case "COMPLETE":
				generation, err := client.Result(ctx, token, account.TeamID, generationID)
				if err != nil {
					w.recordAccountFailure(ctx, account, err)
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "result_failed", err.Error(), 15*time.Second)
				}
				result := domain.VideoResult{
					Created: time.Now().Unix(), NSFW: generation.NSFW,
					ModerationClassifications: generation.ModerationClassifications,
				}
				for _, output := range generation.Outputs {
					url := output.MotionMP4URL
					if url == "" && strings.HasSuffix(strings.ToLower(output.URL), ".mp4") {
						url = output.URL
					}
					if url != "" {
						result.Data = append(result.Data, domain.VideoOutput{
							ID: output.ID, URL: url, MediaType: "video/mp4", Duration: req.Duration,
							Width: output.Width, Height: output.Height, NSFW: output.NSFW,
							ModerationClassifications: output.ModerationClassifications,
						})
					}
				}
				if len(result.Data) == 0 {
					return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Leonardo returned no MP4 video", 15*time.Second)
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

func parseVideoSize(size, resolution string) (int, int, error) {
	portrait := false
	switch size {
	case "1280x720":
	case "720x1280":
		portrait = true
	default:
		return 0, 0, errors.New("video size must be 1280x720 or 720x1280")
	}
	dimensions := map[string][2]int{
		"480p":  {864, 496},
		"720p":  {1280, 720},
		"1080p": {1920, 1080},
		"2160p": {3840, 2160},
	}
	dimension, ok := dimensions[resolution]
	if !ok {
		return 0, 0, errors.New("unsupported video resolution")
	}
	if portrait {
		return dimension[1], dimension[0], nil
	}
	return dimension[0], dimension[1], nil
}

func resolveVideoDimensions(spec videospec.Spec, size, resolution string) (int, int, error) {
	if !spec.SupportsSize(size) {
		return 0, 0, errors.New("unsupported video size")
	}
	if !spec.SupportsResolution(resolution) {
		return 0, 0, errors.New("unsupported video resolution")
	}
	if expected, ok := spec.ResolutionForSize(size); ok && expected != resolution {
		return 0, 0, fmt.Errorf("video size %s requires resolution %s", size, expected)
	}
	if spec.UsesExactDimensions {
		return parseSize(size)
	}
	return parseVideoSize(size, resolution)
}
