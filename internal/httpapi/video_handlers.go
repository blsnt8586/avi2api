package httpapi

import (
	"errors"
	"fmt"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/pricing"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type publicVideoJSONRequest struct {
	Model         string `json:"model"`
	Prompt        string `json:"prompt"`
	Duration      int    `json:"duration,omitempty"`
	Size          string `json:"size,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
	GenerateAudio *bool  `json:"generate_audio,omitempty"`
}

func (req publicVideoJSONRequest) domainRequest() domain.VideoRequest {
	return domain.VideoRequest{
		Model: req.Model, Prompt: req.Prompt, Duration: req.Duration,
		Size: req.Size, Resolution: req.Resolution, GenerateAudio: req.GenerateAudio,
	}
}

func (s *Server) videoGeneration(w http.ResponseWriter, r *http.Request) {
	var req domain.VideoRequest
	var err error
	created := false
	defer func() {
		if !created && s.Assets != nil {
			_ = s.Assets.CleanupVideoRequest(req)
		}
	}()
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		req, err = s.parseVideoMultipart(r)
	} else {
		var publicReq publicVideoJSONRequest
		err = decodeJSON(r, &publicReq)
		req = publicReq.domainRequest()
	}
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	task, taskCreated, err := s.createVideoTask(r, req)
	if err != nil {
		writeCreateTaskError(w, err)
		return
	}
	created = taskCreated
	writeJSON(w, map[bool]int{true: 202, false: 200}[taskCreated], newPublicTaskResponse(task))
}

func (s *Server) parseVideoMultipart(r *http.Request) (domain.VideoRequest, error) {
	maxBody := s.Config.MaxImageBytes*30 + s.Config.MaxVideoBytes*10 + s.Config.MaxAudioBytes*10 + (4 << 20)
	if s.Config.MaxMultipartBytes > 0 && maxBody > s.Config.MaxMultipartBytes {
		maxBody = s.Config.MaxMultipartBytes
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return domain.VideoRequest{}, err
	}
	defer r.MultipartForm.RemoveAll()
	if s.Assets == nil {
		return domain.VideoRequest{}, errors.New("task asset storage is not configured")
	}
	req := domain.VideoRequest{Model: r.FormValue("model"), Prompt: r.FormValue("prompt"), Size: r.FormValue("size"), Resolution: r.FormValue("resolution"), ReferenceStrength: r.FormValue("reference_strength")}
	maxReferenceVideoBytes := s.Config.MaxVideoBytes
	if spec, ok := videospec.Get(req.Model); ok && spec.MaxReferenceVideoBytes > 0 && spec.MaxReferenceVideoBytes < maxReferenceVideoBytes {
		maxReferenceVideoBytes = spec.MaxReferenceVideoBytes
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.Assets.CleanupVideoRequest(req)
		}
	}()
	imageFiles := append([]*multipart.FileHeader{}, r.MultipartForm.File["image"]...)
	imageFiles = append(imageFiles, r.MultipartForm.File["image[]"]...)
	for _, header := range imageFiles {
		asset, err := s.Assets.Save(header, s.Config.MaxImageBytes)
		if err != nil {
			return domain.VideoRequest{}, err
		}
		if !isImageMedia(asset.MediaType) {
			_ = s.Assets.Remove(asset)
			return domain.VideoRequest{}, errors.New("reference images must be PNG, JPEG or WebP")
		}
		req.ReferenceImages = append(req.ReferenceImages, asset)
	}
	if header, err := singleMultipartFile(r.MultipartForm.File["start_frame"], "start_frame"); err != nil {
		return domain.VideoRequest{}, err
	} else if header != nil {
		asset, saveErr := s.Assets.Save(header, s.Config.MaxImageBytes)
		if saveErr != nil {
			return domain.VideoRequest{}, saveErr
		}
		if !isImageMedia(asset.MediaType) {
			_ = s.Assets.Remove(asset)
			return domain.VideoRequest{}, errors.New("start_frame must be PNG, JPEG or WebP")
		}
		req.StartFrame = &asset
	}
	if header, err := singleMultipartFile(r.MultipartForm.File["end_frame"], "end_frame"); err != nil {
		return domain.VideoRequest{}, err
	} else if header != nil {
		asset, saveErr := s.Assets.Save(header, s.Config.MaxImageBytes)
		if saveErr != nil {
			return domain.VideoRequest{}, saveErr
		}
		if !isImageMedia(asset.MediaType) {
			_ = s.Assets.Remove(asset)
			return domain.VideoRequest{}, errors.New("end_frame must be PNG, JPEG or WebP")
		}
		req.EndFrame = &asset
	}
	videoFiles := append([]*multipart.FileHeader{}, r.MultipartForm.File["video"]...)
	videoFiles = append(videoFiles, r.MultipartForm.File["video[]"]...)
	for _, header := range videoFiles {
		asset, err := s.Assets.Save(header, maxReferenceVideoBytes)
		if err != nil {
			return domain.VideoRequest{}, err
		}
		if !isVideoMedia(asset.MediaType, asset.Filename) {
			_ = s.Assets.Remove(asset)
			return domain.VideoRequest{}, errors.New("reference videos must be MP4, MOV or WebM")
		}
		req.ReferenceVideos = append(req.ReferenceVideos, asset)
	}
	audioFiles := append([]*multipart.FileHeader{}, r.MultipartForm.File["audio"]...)
	audioFiles = append(audioFiles, r.MultipartForm.File["audio[]"]...)
	for _, header := range audioFiles {
		asset, saveErr := s.Assets.Save(header, s.Config.MaxAudioBytes)
		if saveErr != nil {
			return domain.VideoRequest{}, saveErr
		}
		if !isAudioMedia(asset.MediaType, asset.Filename) {
			_ = s.Assets.Remove(asset)
			return domain.VideoRequest{}, errors.New("reference audio must be MP3, WAV, M4A, AAC or OGG")
		}
		req.ReferenceAudios = append(req.ReferenceAudios, asset)
	}
	if raw := strings.TrimSpace(r.FormValue("generate_audio")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return domain.VideoRequest{}, errors.New("generate_audio must be true or false")
		}
		req.GenerateAudio = &value
	}
	if v := r.FormValue("duration"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &req.Duration); err != nil {
			return domain.VideoRequest{}, errors.New("duration must be an integer")
		}
	}
	cleanup = false
	return req, nil
}

func singleMultipartFile(files []*multipart.FileHeader, field string) (*multipart.FileHeader, error) {
	if len(files) > 1 {
		return nil, fmt.Errorf("%s accepts only one file", field)
	}
	if len(files) == 0 {
		return nil, nil
	}
	return files[0], nil
}

func isImageMedia(mediaType string) bool {
	return mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/webp"
}

func isVideoMedia(mediaType, filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return (mediaType == "video/mp4" || mediaType == "video/quicktime" || mediaType == "video/webm" || mediaType == "application/octet-stream") && (ext == ".mp4" || ext == ".mov" || ext == ".webm")
}

func isAudioMedia(mediaType, filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	allowedExt := ext == ".mp3" || ext == ".wav" || ext == ".m4a" || ext == ".aac" || ext == ".ogg"
	return allowedExt && (strings.HasPrefix(mediaType, "audio/") || mediaType == "application/ogg" || mediaType == "application/octet-stream")
}

func (s *Server) createVideoTask(r *http.Request, req domain.VideoRequest) (domain.Task, bool, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	notPublic := false
	req.Public = &notPublic
	if req.Prompt == "" {
		return domain.Task{}, false, errors.New("prompt is required")
	}
	if req.Model == "" {
		req.Model = "seedance-2.0-fast"
	}
	spec, ok := videospec.Get(req.Model)
	if !ok {
		return domain.Task{}, false, errors.New("unknown video model")
	}
	if err := validateModelPrompt(req.Model, req.Prompt); err != nil {
		return domain.Task{}, false, err
	}
	model, err := s.Store.GetModel(r.Context(), req.Model)
	if err != nil || !supportsVideoGeneration(model) {
		return domain.Task{}, false, errors.New("unknown video model")
	}
	if req.ReferenceStrength == "" {
		req.ReferenceStrength = "MID"
	}
	if req.ReferenceStrength != "LOW" && req.ReferenceStrength != "MID" && req.ReferenceStrength != "HIGH" {
		return domain.Task{}, false, errors.New("reference_strength must be LOW, MID or HIGH")
	}
	legacyImages := len(req.SourceImages)
	if req.SourceImage != nil {
		legacyImages++
	}
	referenceImages := legacyImages + len(req.ReferenceImages)
	audioReferences := req.AudioReferences()
	advancedReferences := req.StartFrame != nil || req.EndFrame != nil || len(req.ReferenceVideos) > 0 || len(audioReferences) > 0
	if referenceImages > 0 || advancedReferences {
		if !allowed(model.Capabilities, "image-to-video") && !allowed(model.Capabilities, "video-reference") {
			return domain.Task{}, false, errors.New("selected video model does not support media references")
		}
	}
	maxReferenceImages := spec.MaxReferenceImages
	if len(req.ReferenceVideos) > 0 && spec.MaxReferenceImagesWithVideo > 0 {
		maxReferenceImages = spec.MaxReferenceImagesWithVideo
	}
	if referenceImages > maxReferenceImages {
		return domain.Task{}, false, fmt.Errorf("%s supports at most %d reference images for this reference mode", req.Model, maxReferenceImages)
	}
	if err := validateVideoFrameContract(req, spec); err != nil {
		return domain.Task{}, false, err
	}
	if referenceImages > 0 && (req.StartFrame != nil || req.EndFrame != nil) {
		return domain.Task{}, false, errors.New("reference images cannot be combined with start_frame or end_frame")
	}
	if len(req.ReferenceVideos) > 0 && (req.StartFrame != nil || req.EndFrame != nil) {
		return domain.Task{}, false, errors.New("reference videos cannot be combined with start_frame or end_frame")
	}
	if len(req.ReferenceVideos) > spec.MaxReferenceVideos {
		return domain.Task{}, false, fmt.Errorf("%s supports at most %d reference videos", req.Model, spec.MaxReferenceVideos)
	}
	if spec.MaxReferenceVideoBytes > 0 {
		for _, reference := range req.ReferenceVideos {
			if reference.Size > spec.MaxReferenceVideoBytes {
				return domain.Task{}, false, fmt.Errorf("%s reference videos must not exceed %d bytes", req.Model, spec.MaxReferenceVideoBytes)
			}
		}
	}
	if len(audioReferences) > spec.MaxReferenceAudios {
		return domain.Task{}, false, fmt.Errorf("%s supports at most %d audio references", req.Model, spec.MaxReferenceAudios)
	}
	if len(audioReferences) > 0 && referenceImages == 0 && len(req.ReferenceVideos) == 0 {
		return domain.Task{}, false, errors.New("an audio reference requires a reference image or video")
	}
	if err := normalizeVideoOptions(&req, spec); err != nil {
		return domain.Task{}, false, err
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if !allowed(key.AllowedModels, req.Model) {
		return domain.Task{}, false, errors.New("model is not allowed for this API key")
	}
	noteVideoRequest(r, req)
	idem := r.Header.Get("Idempotency-Key")
	if idem != "" {
		if task, err := s.Store.GetIdempotentTaskWithHashRequest(r.Context(), key.ID, "video", videoIdempotencyRequest(req), idem); err == nil {
			noteRequestTask(r, task)
			return task, false, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return domain.Task{}, false, err
		}
	}
	estimate, err := pricing.Video(r.Context(), s.Store, req)
	if err != nil {
		if errors.Is(err, pricing.ErrCostUnavailable) {
			return domain.Task{}, false, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
		}
		return domain.Task{}, false, err
	}
	noteRequestEstimate(r, estimate.Tokens)
	if err := s.admitDailyQuota(r.Context(), key.ID, 1); err != nil {
		return domain.Task{}, false, err
	}
	task, created, err := s.Store.CreateReservedTaskWithHashRequest(r.Context(), key.ID, "video", req.Model, req.Prompt, req, videoIdempotencyRequest(req), idem, estimate.Tokens, estimate.RuleID, s.Config.TaskTimeout+time.Minute)
	noteRequestTask(r, task)
	if err != nil {
		s.rollbackDailyQuota(r.Context(), key.ID, 1)
		return task, false, routingRequestError(err)
	}
	if !created {
		s.rollbackDailyQuota(r.Context(), key.ID, 1)
		return task, false, nil
	}
	s.enqueueTask(r.Context(), task)
	return task, created, nil
}

func normalizeVideoOptions(req *domain.VideoRequest, spec videospec.Spec) error {
	if req.GenerateAudio != nil && !spec.SupportsGenerateAudio {
		return fmt.Errorf("%s does not support generate_audio", req.Model)
	}
	if spec.AlwaysGenerateAudio && req.GenerateAudio != nil && !*req.GenerateAudio {
		return fmt.Errorf("%s always generates native audio; generate_audio cannot be false", req.Model)
	}
	if req.GenerateAudio == nil && spec.SupportsGenerateAudio {
		generateAudio := true
		req.GenerateAudio = &generateAudio
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
	if len(req.ReferenceVideos) > 0 {
		if spec.MinVideoDuration > 0 && float64(req.Duration) < spec.MinVideoDuration {
			return fmt.Errorf("reference video duration must be at least %.2f seconds for %s", spec.MinVideoDuration, req.Model)
		}
		if spec.MaxVideoDuration > 0 && float64(req.Duration) > spec.MaxVideoDuration {
			return fmt.Errorf("reference video duration must not exceed %.2f seconds for %s", spec.MaxVideoDuration, req.Model)
		}
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
	if len(req.ReferenceVideos) > 0 && !spec.SupportsVideoReferenceResolution(req.Resolution) {
		return fmt.Errorf("unsupported resolution %s for %s video references", req.Resolution, req.Model)
	}
	if resolution, ok := spec.ResolutionForSize(req.Size); ok && resolution != req.Resolution {
		return fmt.Errorf("size %s requires resolution %s for %s", req.Size, resolution, req.Model)
	}
	referenceImages := len(req.ReferenceImages) + len(req.SourceImages)
	if req.SourceImage != nil {
		referenceImages++
	}
	if req.Model == "veo-3.1" && referenceImages > 0 {
		if req.Size != "1280x720" {
			return errors.New("veo-3.1 ordinary reference images require size 1280x720 (16:9)")
		}
		if req.Duration != 8 {
			return errors.New("veo-3.1 ordinary reference images require duration 8")
		}
	}
	return nil
}

func supportsVideoGeneration(model domain.ModelConfig) bool {
	return allowed(model.Capabilities, "text-to-video") || allowed(model.Capabilities, "image-to-video")
}

func validateVideoFrameContract(req domain.VideoRequest, spec videospec.Spec) error {
	if (req.StartFrame != nil || req.EndFrame != nil) && !spec.SupportsStartEndFrame {
		return fmt.Errorf("%s does not support start_frame or end_frame", req.Model)
	}
	if req.EndFrame != nil && !spec.SupportsEndFrame {
		return fmt.Errorf("%s does not support end_frame", req.Model)
	}
	if req.EndFrame != nil && req.StartFrame == nil {
		return errors.New("end_frame requires start_frame")
	}
	if spec.RequiresStartFrame && req.StartFrame == nil {
		return fmt.Errorf("%s requires start_frame", req.Model)
	}
	return nil
}

func videoIdempotencyRequest(request domain.VideoRequest) domain.VideoRequest {
	clone := request
	clone.ReferenceImages = canonicalMedia(request.ReferenceImages)
	clone.ReferenceVideos = canonicalMedia(request.ReferenceVideos)
	clone.ReferenceAudios = canonicalMedia(request.ReferenceAudios)
	if request.StartFrame != nil {
		asset := *request.StartFrame
		asset.Path = ""
		clone.StartFrame = &asset
	}
	if request.EndFrame != nil {
		asset := *request.EndFrame
		asset.Path = ""
		clone.EndFrame = &asset
	}
	if request.ReferenceAudio != nil {
		asset := *request.ReferenceAudio
		asset.Path = ""
		clone.ReferenceAudio = &asset
	}
	return clone
}

func canonicalMedia(assets []domain.SourceMedia) []domain.SourceMedia {
	out := append([]domain.SourceMedia(nil), assets...)
	for index := range out {
		out[index].Path = ""
	}
	return out
}
