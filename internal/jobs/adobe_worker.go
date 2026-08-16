package jobs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/providers"
)

func (w *Worker) adobeClient(account domain.Account) (*adobe.Client, error) {
	if strings.TrimSpace(w.Config.AdobeSubmitAPIKey) == "" || strings.TrimSpace(w.Config.AdobeCreditsAPIKey) == "" {
		return nil, errors.New("Adobe provider API keys are not configured")
	}
	return adobe.New(account.ProxyURL, w.Config.AdobeSubmitAPIKey, w.Config.AdobeCreditsAPIKey, account.UserAgent)
}

func (w *Worker) processAdobeImage(ctx context.Context, id, leaseID uuid.UUID, task domain.Task, req domain.ImageRequest, start time.Time) error {
	if isSubmittedGenerationTask(task) {
		account, token, client, err := w.resumeAdobeTask(ctx, id, leaseID, task)
		if err != nil {
			return err
		}
		return w.pollAdobeImage(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
	}
	if err := w.validateTaskAPIKey(ctx, task); err != nil {
		return w.fail(ctx, id, leaseID, "api_key_disabled", err)
	}
	if w.Providers != nil {
		if _, err := w.Providers.Get(task.ProviderID); err != nil {
			return w.fail(ctx, id, leaseID, "provider_unavailable", err)
		}
	}
	account, token, client, err := w.prepareAdobeTask(ctx, id, leaseID, task)
	if err != nil {
		return err
	}
	modelSpec, ok := adobe.Model(req.Model)
	if !ok || modelSpec.Kind != "image" {
		return w.fail(ctx, id, leaseID, "unsupported_model", fmt.Errorf("unsupported Adobe image model %q", req.Model))
	}
	width, height, err := imageopts.ParseSizeForProvider(providers.Adobe, req.Model, req.Size)
	if err != nil {
		return w.fail(ctx, id, leaseID, "invalid_size", err)
	}
	references := make([]adobe.Reference, 0, len(req.ReferenceImages)+len(req.SourceImages)+1)
	sources := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		sources = append([]domain.SourceImage{*req.SourceImage}, sources...)
	}
	totalUploads := len(sources) + len(req.ReferenceImages)
	if totalUploads > 0 {
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
	}
	uploaded := 0
	for _, source := range sources {
		raw, decodeErr := base64.StdEncoding.DecodeString(source.Data)
		if decodeErr != nil {
			return w.fail(ctx, id, leaseID, "invalid_source_image", decodeErr)
		}
		mediaID, uploadErr := client.Upload(ctx, token, source.MediaType, raw)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		references = append(references, adobeImageReference(modelSpec, mediaID))
		uploaded++
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15+uploaded*4/totalUploads, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
	}
	for _, source := range req.ReferenceImages {
		raw, readErr := w.readTaskAsset(source, w.Config.MaxImageBytes)
		if readErr != nil {
			return w.fail(ctx, id, leaseID, "invalid_source_image", readErr)
		}
		mediaID, uploadErr := client.Upload(ctx, token, source.MediaType, raw)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		references = append(references, adobeImageReference(modelSpec, mediaID))
		uploaded++
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15+uploaded*4/totalUploads, &account.ID, "", nil, "", ""); err != nil {
			return err
		}
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForProviderAccountSubmit(ctx, providers.Adobe, account.ID); err != nil {
		return w.fail(ctx, id, leaseID, "account_submit_interval", err)
	}
	upstream, err := adobeImageSubmitRequest(req, width, height, references)
	if err != nil {
		return w.fail(ctx, id, leaseID, "unsupported_model", err)
	}
	deadline := time.Now().Add(w.Config.TaskTimeout)
	if err := w.prepareSubmission(ctx, id, leaseID, account.ID, upstream, deadline); err != nil {
		return err
	}
	job, err := client.SubmitImage(ctx, token, upstream)
	if err != nil {
		if adobeSubmissionUncertain(err) {
			w.recordAccountFailure(ctx, account, err)
			return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "submission_uncertain", err.Error())
		}
		w.recordAccountFailure(ctx, account, err)
		return w.fail(ctx, id, leaseID, "generate_failed", err)
	}
	w.clearAccountFailures(ctx, account.ID)
	if err := w.recordAdobeSubmission(ctx, id, leaseID, account.ID, job); err != nil {
		return err
	}
	return w.pollAdobeImage(ctx, id, leaseID, job.PollURL, account, token, client, req, start, &deadline)
}

func (w *Worker) processAdobeVideo(ctx context.Context, id, leaseID uuid.UUID, task domain.Task, req domain.VideoRequest, start time.Time) error {
	if isSubmittedGenerationTask(task) {
		account, token, client, err := w.resumeAdobeTask(ctx, id, leaseID, task)
		if err != nil {
			return err
		}
		return w.pollAdobeVideo(ctx, id, leaseID, task.GenerationID, account, token, client, req, start, task.UpstreamDeadlineAt)
	}
	if err := w.validateTaskAPIKey(ctx, task); err != nil {
		return w.fail(ctx, id, leaseID, "api_key_disabled", err)
	}
	account, token, client, err := w.prepareAdobeTask(ctx, id, leaseID, task)
	if err != nil {
		return err
	}
	modelSpec, ok := adobe.Model(req.Model)
	if !ok || modelSpec.Kind != "video" {
		return w.fail(ctx, id, leaseID, "unsupported_model", fmt.Errorf("unsupported Adobe video model %q", req.Model))
	}
	width, height, err := parseSize(req.Size)
	if err != nil {
		return w.fail(ctx, id, leaseID, "invalid_size", err)
	}
	references, err := w.uploadAdobeVideoReferences(ctx, id, leaseID, account, token, client, req)
	if err != nil {
		return err
	}
	account, proceed, err := w.enforceSubmissionFence(ctx, id, leaseID, account.ID)
	if err != nil || !proceed {
		return err
	}
	if err := w.waitForProviderAccountSubmit(ctx, providers.Adobe, account.ID); err != nil {
		return w.fail(ctx, id, leaseID, "account_submit_interval", err)
	}
	upstream, err := adobeVideoSubmitRequest(req, width, height, references)
	if err != nil {
		return w.fail(ctx, id, leaseID, "unsupported_model", err)
	}
	deadline := time.Now().Add(w.Config.TaskTimeout)
	if err := w.prepareSubmission(ctx, id, leaseID, account.ID, upstream, deadline); err != nil {
		return err
	}
	job, err := client.SubmitVideo(ctx, token, upstream)
	if err != nil {
		if adobeSubmissionUncertain(err) {
			w.recordAccountFailure(ctx, account, err)
			return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "submission_uncertain", err.Error())
		}
		w.recordAccountFailure(ctx, account, err)
		return w.fail(ctx, id, leaseID, "generate_failed", err)
	}
	w.clearAccountFailures(ctx, account.ID)
	if err := w.recordAdobeSubmission(ctx, id, leaseID, account.ID, job); err != nil {
		return err
	}
	return w.pollAdobeVideo(ctx, id, leaseID, job.PollURL, account, token, client, req, start, &deadline)
}

func adobeImageReference(spec adobe.ModelSpec, mediaID string) adobe.Reference {
	usage := "general"
	if spec.PublicID == adobe.AdobeGPTImage2 {
		usage = "subject"
	}
	return adobe.Reference{ID: mediaID, Usage: usage}
}

func adobeImageSubmitRequest(req domain.ImageRequest, width, height int, references []adobe.Reference) (adobe.SubmitRequest, error) {
	spec, ok := adobe.Model(req.Model)
	if !ok || spec.Kind != "image" {
		return adobe.SubmitRequest{}, fmt.Errorf("unsupported Adobe image model %q", req.Model)
	}
	upstream := adobe.SubmitRequest{
		ModelID: spec.ModelID, ModelVersion: spec.ModelVersion, Prompt: req.Prompt, Width: width, Height: height, N: 1,
		References: references,
	}
	switch spec.PublicID {
	case adobe.AdobeGPTImage2:
		quality := strings.ToLower(strings.TrimSpace(req.Quality))
		if quality == "" || quality == "auto" {
			quality = "low"
		}
		upstream.OutputResolution = adobeImageResolution(width, height)
		upstream.ModelSpecific = adobeGPTImageModelSpecific(width, height, len(references) > 0)
		upstream.GenerationSettings = map[string]any{"detailLevel": map[string]int{"low": 1, "medium": 3, "high": 5}[quality]}
	case adobe.AdobeNanoBanana2:
		falseValue := false
		upstream.GroundSearch = &falseValue
		upstream.SkipCAI = &falseValue
		upstream.GenerationMetadata = map[string]any{"module": "text2image", "submodule": "ff-image-generate"}
		if len(references) > 0 {
			upstream.GenerationMetadata["module"] = "image2image"
		}
		upstream.ModelSpecific = map[string]any{
			"aspectRatio": closestAspectRatio(width, height, []aspectRatio{{"1:1", 1, 1}, {"2:3", 2, 3}, {"3:2", 3, 2}, {"3:4", 3, 4}, {"4:3", 4, 3}, {"4:5", 4, 5}, {"5:4", 5, 4}, {"9:16", 9, 16}, {"16:9", 16, 9}, {"21:9", 21, 9}}),
			"parameters":  map[string]any{"addWatermark": false},
		}
	}
	return upstream, nil
}

func adobeVideoSubmitRequest(req domain.VideoRequest, width, height int, references []adobe.Reference) (adobe.SubmitRequest, error) {
	spec, ok := adobe.Model(req.Model)
	if !ok || spec.Kind != "video" {
		return adobe.SubmitRequest{}, fmt.Errorf("unsupported Adobe video model %q", req.Model)
	}
	aspect := closestAspectRatio(width, height, []aspectRatio{{"21:9", 21, 9}, {"16:9", 16, 9}, {"4:3", 4, 3}, {"1:1", 1, 1}, {"3:4", 3, 4}, {"9:16", 9, 16}})
	upstream := adobe.SubmitRequest{
		ModelID: spec.ModelID, ModelVersion: spec.ModelVersion, Prompt: req.Prompt, Width: width, Height: height, N: 1,
		GenerateAudio: boolPointer(false), References: references, GenerationMetadata: map[string]any{"module": "text2video"},
	}
	switch spec.PublicID {
	case adobe.AdobeVeo31, adobe.AdobeVeo31Fast:
		upstream.ModelSpecific = map[string]any{"parameters": map[string]any{"durationSeconds": req.Duration, "aspectRatio": aspect, "addWaterMark": false}}
	case adobe.AdobeSeedance20, adobe.AdobeSeedance20Fast:
		upstream.Duration = req.Duration
		upstream.NegativePrompt = "cartoon, vector art, & bad aesthetics & poor aesthetic"
		upstream.GenerationSettings = map[string]any{"aspectRatio": aspect}
	case adobe.AdobeKling30Omni:
		upstream.Duration = req.Duration
		upstream.GenerationSettings = map[string]any{"aspectRatio": aspect}
		if req.StartFrame != nil || req.EndFrame != nil {
			upstream.GenerationMetadata["module"] = "image2video"
		}
	}
	return upstream, nil
}

type aspectRatio struct {
	name        string
	numerator   float64
	denominator float64
}

func closestAspectRatio(width, height int, candidates []aspectRatio) string {
	if width <= 0 || height <= 0 || len(candidates) == 0 {
		return "16:9"
	}
	ratio := float64(width) / float64(height)
	best := candidates[0]
	bestDistance := ratio - best.numerator/best.denominator
	if bestDistance < 0 {
		bestDistance = -bestDistance
	}
	for _, candidate := range candidates[1:] {
		distance := ratio - candidate.numerator/candidate.denominator
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	return best.name
}

func (w *Worker) uploadAdobeVideoReferences(ctx context.Context, id, leaseID uuid.UUID, account domain.Account, token string, client *adobe.Client, req domain.VideoRequest) ([]adobe.Reference, error) {
	legacyImages := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		legacyImages = append([]domain.SourceImage{*req.SourceImage}, legacyImages...)
	}
	total := len(legacyImages) + len(req.ReferenceImages) + len(req.ReferenceVideos) + len(req.AudioReferences())
	if req.StartFrame != nil {
		total++
	}
	if req.EndFrame != nil {
		total++
	}
	if total == 0 {
		return nil, nil
	}
	if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
		return nil, err
	}
	references := make([]adobe.Reference, 0, total)
	uploaded := 0
	upload := func(kind, mediaType string, raw []byte, index int) error {
		mediaID, uploadErr := client.Upload(ctx, token, mediaType, raw)
		if uploadErr != nil {
			w.recordAccountFailure(ctx, account, uploadErr)
			return w.fail(ctx, id, leaseID, "upload_failed", uploadErr)
		}
		references = append(references, adobeVideoReference(req.Model, kind, mediaID, index))
		uploaded++
		return w.update(ctx, id, leaseID, domain.TaskUploading, 15+uploaded*4/total, &account.ID, "", nil, "", "")
	}
	ordinaryIndex := 0
	for _, source := range legacyImages {
		raw, decodeErr := base64.StdEncoding.DecodeString(source.Data)
		if decodeErr != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", decodeErr)
		}
		ordinaryIndex++
		if err := upload("image", source.MediaType, raw, ordinaryIndex); err != nil {
			return nil, err
		}
	}
	for _, source := range req.ReferenceImages {
		raw, readErr := w.readTaskAsset(source, w.Config.MaxImageBytes)
		if readErr != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", readErr)
		}
		ordinaryIndex++
		if err := upload("image", source.MediaType, raw, ordinaryIndex); err != nil {
			return nil, err
		}
	}
	for index, source := range []*domain.SourceMedia{req.StartFrame, req.EndFrame} {
		if source == nil {
			continue
		}
		raw, readErr := w.readTaskAsset(*source, w.Config.MaxImageBytes)
		if readErr != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", readErr)
		}
		if err := upload("frame", source.MediaType, raw, index+1); err != nil {
			return nil, err
		}
	}
	for index, source := range req.ReferenceVideos {
		raw, readErr := w.readTaskAsset(source, w.Config.MaxVideoBytes)
		if readErr != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", readErr)
		}
		if err := upload("video", source.MediaType, raw, index+1); err != nil {
			return nil, err
		}
	}
	for index, source := range req.AudioReferences() {
		raw, readErr := w.readTaskAsset(source, w.Config.MaxAudioBytes)
		if readErr != nil {
			return nil, w.fail(ctx, id, leaseID, "invalid_source_media", readErr)
		}
		if err := upload("audio", source.MediaType, raw, index+1); err != nil {
			return nil, err
		}
	}
	return references, nil
}

func adobeVideoReference(model, kind, mediaID string, index int) adobe.Reference {
	reference := adobe.Reference{ID: mediaID}
	switch kind {
	case "frame":
		reference.Usage = "general"
		reference.PromptReference = index
		if model == adobe.AdobeSeedance20 || model == adobe.AdobeSeedance20Fast || model == adobe.AdobeKling30Omni {
			reference.Usage = "keyframe"
		}
	case "image":
		reference.Usage = "style"
		if model == adobe.AdobeVeo31 {
			reference.Usage = "asset"
		}
	case "video", "audio":
		reference.Usage = "source"
	}
	return reference
}

func adobeImageResolution(width, height int) string {
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	switch {
	case longEdge <= 1456:
		return "1K"
	case longEdge <= 2560:
		return "2K"
	default:
		return "4K"
	}
}

func adobeGPTImageModelSpecific(width, height int, hasReferences bool) map[string]any {
	if hasReferences {
		return map[string]any{}
	}
	return map[string]any{"size": fmt.Sprintf("%dx%d", width, height)}
}

func boolPointer(value bool) *bool { return &value }

func (w *Worker) prepareAdobeTask(ctx context.Context, id, leaseID uuid.UUID, task domain.Task) (domain.Account, string, *adobe.Client, error) {
	if task.Status != domain.TaskReserving || task.AccountID == nil {
		return domain.Account{}, "", nil, w.fail(ctx, id, leaseID, "invalid_task_state", errors.New("task has no reserved account for submission"))
	}
	account, err := w.Store.GetAccount(ctx, *task.AccountID)
	if err != nil {
		return account, "", nil, w.fail(ctx, id, leaseID, "account_not_found", err)
	}
	if !submissionAccountRouteable(account) {
		return account, "", nil, w.yieldForReroute(ctx, id, leaseID)
	}
	account, token, err := w.Accounts.AdobeToken(ctx, account)
	if err != nil {
		return account, "", nil, w.yieldForReroute(ctx, id, leaseID)
	}
	client, err := w.adobeClient(account)
	if err != nil {
		return account, "", nil, w.fail(ctx, id, leaseID, "client_error", err)
	}
	tokensBefore := account.TotalTokens()
	if err := w.updateTokens(ctx, id, leaseID, &tokensBefore, nil); err != nil {
		return account, "", nil, err
	}
	return account, token, client, nil
}

func (w *Worker) resumeAdobeTask(ctx context.Context, id, leaseID uuid.UUID, task domain.Task) (domain.Account, string, *adobe.Client, error) {
	if task.UpstreamDeadlineAt != nil && !task.UpstreamDeadlineAt.After(time.Now()) {
		return domain.Account{}, "", nil, w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "upstream_deadline_exceeded", "Adobe generation deadline exceeded")
	}
	account, err := w.Store.GetAccount(ctx, *task.AccountID)
	if err != nil {
		return account, "", nil, w.markSubmissionUncertain(ctx, id, leaseID, task.AccountID, "account_not_found", err.Error())
	}
	account, token, err := w.Accounts.AdobeToken(ctx, account)
	if err != nil {
		return account, "", nil, w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "session_error", err.Error(), 30*time.Second)
	}
	client, err := w.adobeClient(account)
	if err != nil {
		return account, "", nil, w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, task.UpstreamDeadlineAt, "client_error", err.Error(), 30*time.Second)
	}
	return account, token, client, nil
}

func (w *Worker) pollAdobeImage(ctx context.Context, id, leaseID uuid.UUID, pollURL string, account domain.Account, token string, client *adobe.Client, req domain.ImageRequest, start time.Time, deadline *time.Time) error {
	return w.pollAdobe(ctx, id, leaseID, pollURL, account, token, client, deadline, func(result adobe.PollResult) error {
		output := domain.ImageResult{Created: time.Now().Unix()}
		for _, item := range result.Outputs {
			if item.MediaType == "image" || item.MediaType == "" {
				output.Data = append(output.Data, domain.ImageOutput{ID: item.ID, URL: item.URL, RevisedPrompt: req.Prompt})
			}
		}
		if len(output.Data) == 0 {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Adobe returned no images", 15*time.Second)
		}
		return w.complete(ctx, id, leaseID, pollURL, account, token, output, start)
	})
}

func (w *Worker) pollAdobeVideo(ctx context.Context, id, leaseID uuid.UUID, pollURL string, account domain.Account, token string, client *adobe.Client, req domain.VideoRequest, start time.Time, deadline *time.Time) error {
	return w.pollAdobe(ctx, id, leaseID, pollURL, account, token, client, deadline, func(result adobe.PollResult) error {
		output := domain.VideoResult{Created: time.Now().Unix()}
		for _, item := range result.Outputs {
			if item.MediaType == "video" || item.MediaType == "" {
				output.Data = append(output.Data, domain.VideoOutput{ID: item.ID, URL: item.URL, MediaType: "video/mp4", Duration: req.Duration, Width: item.Width, Height: item.Height})
			}
		}
		if len(output.Data) == 0 {
			return w.yieldSubmittedRetry(ctx, id, leaseID, account.ID, deadline, "empty_result", "Adobe returned no video", 15*time.Second)
		}
		return w.complete(ctx, id, leaseID, pollURL, account, token, output, start)
	})
}

func (w *Worker) pollAdobe(ctx context.Context, id, leaseID uuid.UUID, pollURL string, account domain.Account, token string, client *adobe.Client, deadline *time.Time, complete func(adobe.PollResult) error) error {
	ticker := time.NewTicker(w.pollInterval(id, deadline))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, err := w.Store.YieldTaskLease(context.Background(), id, leaseID, time.Now().Add(time.Minute))
			return err
		case <-ticker.C:
			if deadline != nil && !deadline.After(time.Now()) {
				return w.markSubmissionUncertain(ctx, id, leaseID, &account.ID, "upstream_deadline_exceeded", "Adobe generation deadline exceeded")
			}
			result, err := client.Poll(ctx, token, pollURL)
			if err != nil {
				w.Log.Warn("Adobe poll failed", "task_id", id, "error", err)
				var httpErr *adobe.HTTPError
				if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
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
					message = "Adobe upstream generation failed"
				}
				return w.failAfterSubmission(ctx, id, leaseID, "upstream_failed", message, map[string]any{"source": "adobe", "upstream_status": result.Status}, account, token)
			case "COMPLETE":
				w.clearAccountFailures(ctx, account.ID)
				return complete(result)
			default:
				progress := result.Progress
				if progress < 1 {
					progress = 20
				}
				if err := w.update(ctx, id, leaseID, domain.TaskPolling, progress, &account.ID, pollURL, nil, "", ""); err != nil {
					return err
				}
			}
		}
	}
}

func (w *Worker) readTaskAsset(source domain.SourceMedia, limit int64) ([]byte, error) {
	if w.Assets == nil {
		return nil, errors.New("task asset storage is not configured")
	}
	file, err := w.Assets.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("stored task asset exceeds configured size limit")
	}
	return raw, nil
}

func adobeSubmissionUncertain(err error) bool {
	var httpErr *adobe.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == 408 || httpErr.Status >= 500
	}
	return true
}
