package jobs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

type leonardoVideoGuidance struct {
	imageReferences []leonardo.ImageReference
	startFrame      *leonardo.ImageReference
	endFrame        *leonardo.ImageReference
	videoReferences []leonardo.MediaReference
	audioReferences []leonardo.MediaReference
}

func (w *Worker) uploadLeonardoVideoGuidance(ctx context.Context, id, leaseID uuid.UUID, account domain.Account, token string, client *leonardo.Client, req domain.VideoRequest, spec videospec.Spec) (leonardoVideoGuidance, error) {
	var guidance leonardoVideoGuidance
	sources := append([]domain.SourceImage(nil), req.SourceImages...)
	if req.SourceImage != nil {
		sources = append([]domain.SourceImage{*req.SourceImage}, sources...)
	}
	guidance.imageReferences = make([]leonardo.ImageReference, 0, len(sources)+len(req.ReferenceImages))
	audioSources := req.AudioReferences()
	totalUploads := len(sources) + len(req.ReferenceImages) + len(req.ReferenceVideos) + len(audioSources)
	if req.StartFrame != nil {
		totalUploads++
	}
	if req.EndFrame != nil {
		totalUploads++
	}
	uploadedCount := 0
	updateProgress := func() error {
		uploadedCount++
		progress := 15 + (uploadedCount * 4 / totalUploads)
		return w.update(ctx, id, leaseID, domain.TaskUploading, progress, &account.ID, "", nil, "", "")
	}
	if totalUploads > 0 {
		if err := w.update(ctx, id, leaseID, domain.TaskUploading, 15, &account.ID, "", nil, "", ""); err != nil {
			return guidance, err
		}
	}
	for _, source := range sources {
		raw, err := base64.StdEncoding.DecodeString(source.Data)
		if err != nil {
			return guidance, w.fail(ctx, id, leaseID, "invalid_source_image", err)
		}
		uploaded, err := client.UploadInitImage(ctx, token, account.TeamID, source.Filename, source.MediaType, raw)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		guidance.imageReferences = append(guidance.imageReferences, leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED", Strength: req.ReferenceStrength})
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}
	for _, source := range req.ReferenceImages {
		uploaded, err := w.uploadTaskImage(ctx, client, token, account.TeamID, source)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		guidance.imageReferences = append(guidance.imageReferences, leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED", Strength: req.ReferenceStrength})
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}
	if req.StartFrame != nil {
		uploaded, err := w.uploadTaskImage(ctx, client, token, account.TeamID, *req.StartFrame)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		guidance.startFrame = &leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED"}
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}
	if req.EndFrame != nil {
		uploaded, err := w.uploadTaskImage(ctx, client, token, account.TeamID, *req.EndFrame)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		guidance.endFrame = &leonardo.ImageReference{ID: uploaded.InitImageID, Type: "UPLOADED"}
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}

	guidance.videoReferences = make([]leonardo.MediaReference, 0, len(req.ReferenceVideos))
	totalVideoDuration := 0.0
	for _, source := range req.ReferenceVideos {
		uploaded, err := w.uploadTaskMedia(ctx, client, token, account.TeamID, source)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		if uploaded.Duration <= 0 || uploaded.Width <= 0 || uploaded.Height <= 0 {
			return guidance, w.fail(ctx, id, leaseID, "invalid_reference_video", errors.New("Leonardo did not return duration and dimensions for the reference video"))
		}
		if (spec.MinReferenceVideoDimension > 0 || spec.MaxReferenceVideoDimension > 0) && !spec.SupportsReferenceVideoDimensions(uploaded.Width, uploaded.Height) {
			return guidance, w.fail(ctx, id, leaseID, "invalid_reference_video_dimensions", fmt.Errorf("reference video is %dx%d; %s requires each edge between %d and %d pixels", uploaded.Width, uploaded.Height, req.Model, spec.MinReferenceVideoDimension, spec.MaxReferenceVideoDimension))
		}
		totalVideoDuration += uploaded.Duration
		guidance.videoReferences = append(guidance.videoReferences, leonardo.MediaReference{ID: uploaded.ID, Type: "UPLOADED", Duration: uploaded.Duration, Width: uploaded.Width, Height: uploaded.Height, MotionHasAudio: true})
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}
	if spec.MaxVideoDuration > 0 && totalVideoDuration > spec.MaxVideoDuration+0.0001 {
		return guidance, w.fail(ctx, id, leaseID, "reference_video_too_long", fmt.Errorf("reference videos total %.2f seconds; %s allows at most %.1f seconds", totalVideoDuration, req.Model, spec.MaxVideoDuration))
	}
	if spec.MinVideoDuration > 0 && totalVideoDuration+0.0001 < spec.MinVideoDuration {
		return guidance, w.fail(ctx, id, leaseID, "reference_video_too_short", fmt.Errorf("reference video is %.2f seconds; %s requires at least %.1f seconds", totalVideoDuration, req.Model, spec.MinVideoDuration))
	}
	if len(guidance.videoReferences) > 0 && spec.OmitDurationWithVideo && math.Abs(totalVideoDuration-float64(req.Duration)) > 0.051 {
		return guidance, w.fail(ctx, id, leaseID, "reference_video_duration_mismatch", fmt.Errorf("reference video is %.2f seconds; duration must match it within 0.05 seconds for exact pricing", totalVideoDuration))
	}

	guidance.audioReferences = make([]leonardo.MediaReference, 0, len(audioSources))
	totalAudioDuration := 0.0
	for _, source := range audioSources {
		uploaded, err := w.uploadTaskMedia(ctx, client, token, account.TeamID, source)
		if err != nil {
			w.recordAccountFailure(ctx, account, err)
			return guidance, w.fail(ctx, id, leaseID, "upload_failed", err)
		}
		if uploaded.Duration <= 0 {
			return guidance, w.fail(ctx, id, leaseID, "invalid_reference_audio", errors.New("Leonardo did not return a duration for the reference audio"))
		}
		totalAudioDuration += uploaded.Duration
		guidance.audioReferences = append(guidance.audioReferences, leonardo.MediaReference{ID: uploaded.ID, Type: "UPLOADED", Duration: uploaded.Duration})
		if err := updateProgress(); err != nil {
			return guidance, err
		}
	}
	if spec.MaxAudioDuration > 0 && totalAudioDuration > spec.MaxAudioDuration+0.0001 {
		return guidance, w.fail(ctx, id, leaseID, "reference_audio_too_long", fmt.Errorf("reference audio totals %.2f seconds; %s allows at most %.1f seconds", totalAudioDuration, req.Model, spec.MaxAudioDuration))
	}
	return guidance, nil
}
