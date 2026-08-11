package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/pricing"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
	_ "golang.org/x/image/webp"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"
)

type imageEstimateRequest struct {
	Model   string `json:"model"`
	Size    string `json:"size,omitempty"`
	Quality string `json:"quality,omitempty"`
	N       int    `json:"n,omitempty"`
}

type imageEstimateResponse struct {
	Model             string   `json:"model"`
	Size              string   `json:"size"`
	Quality           string   `json:"quality,omitempty"`
	Quantity          int      `json:"quantity"`
	UnitTokens        int64    `json:"unit_tokens"`
	EstimatedTokens   int64    `json:"estimated_tokens"`
	PricingBasis      string   `json:"pricing_basis"`
	PricingTier       string   `json:"pricing_tier,omitempty"`
	PricingAnchor     string   `json:"pricing_anchor,omitempty"`
	QualityMultiplier *float64 `json:"quality_multiplier,omitempty"`
	Formula           string   `json:"formula"`
	CostParameters    []string `json:"cost_parameters"`
	PriceVersion      string   `json:"price_version"`
	Source            string   `json:"source"`
}

type imageCostMatrixRequest struct {
	Model string   `json:"model"`
	Sizes []string `json:"sizes"`
}

type imageCostMatrixRow struct {
	Size          string           `json:"size"`
	Costs         map[string]int64 `json:"costs"`
	PricingTier   string           `json:"pricing_tier,omitempty"`
	PricingAnchor string           `json:"pricing_anchor,omitempty"`
}

type imageCostMatrixResponse struct {
	Model         string               `json:"model"`
	Qualities     []string             `json:"qualities"`
	Rows          []imageCostMatrixRow `json:"rows"`
	PriceVersions []string             `json:"price_versions"`
	Sources       []string             `json:"sources"`
}

type videoEstimateRequest struct {
	Model             string `json:"model"`
	Duration          int    `json:"duration,omitempty"`
	Size              string `json:"size,omitempty"`
	Resolution        string `json:"resolution,omitempty"`
	GenerateAudio     *bool  `json:"generate_audio,omitempty"`
	HasVideoReference bool   `json:"has_video_reference,omitempty"`
}

type videoEstimateResponse struct {
	Model             string   `json:"model"`
	Duration          int      `json:"duration"`
	Size              string   `json:"size"`
	Resolution        string   `json:"resolution"`
	GenerateAudio     *bool    `json:"generate_audio,omitempty"`
	HasVideoReference bool     `json:"has_video_reference"`
	BaseTokens        int64    `json:"base_tokens"`
	EstimatedTokens   int64    `json:"estimated_tokens"`
	AppliedModifiers  []string `json:"applied_modifiers"`
	Formula           string   `json:"formula"`
	CostParameters    []string `json:"cost_parameters"`
	PriceVersion      string   `json:"price_version"`
	Source            string   `json:"source"`
}

type imageEstimateStore interface {
	pricing.RuleStore
	GetModel(context.Context, string) (domain.ModelConfig, error)
}

func (s *Server) imageGeneration(w http.ResponseWriter, r *http.Request) {
	s.createAsyncImage(w, r, false)
}

func (s *Server) imageEstimate(w http.ResponseWriter, r *http.Request) {
	s.writeImageEstimate(w, r, true)
}

func (s *Server) publicImageEstimate(w http.ResponseWriter, r *http.Request) {
	s.writeImageEstimate(w, r, false)
}

func (s *Server) publicImageCostMatrix(w http.ResponseWriter, r *http.Request) {
	var req imageCostMatrixRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	response, err := estimateImageCostMatrix(r.Context(), s.Store, req)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "image cost matrix is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type cachedImageEstimateStore struct {
	source imageEstimateStore
	models map[string]domain.ModelConfig
	rules  map[string]domain.ModelCostRule
}

func newCachedImageEstimateStore(source imageEstimateStore) *cachedImageEstimateStore {
	return &cachedImageEstimateStore{
		source: source,
		models: make(map[string]domain.ModelConfig),
		rules:  make(map[string]domain.ModelCostRule),
	}
}

func (c *cachedImageEstimateStore) GetModel(ctx context.Context, id string) (domain.ModelConfig, error) {
	if model, ok := c.models[id]; ok {
		return model, nil
	}
	model, err := c.source.GetModel(ctx, id)
	if err == nil {
		c.models[id] = model
	}
	return model, err
}

func (c *cachedImageEstimateStore) FindModelCostRule(ctx context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d", kind, model, size, quality, resolution, duration)
	if rule, ok := c.rules[key]; ok {
		return rule, nil
	}
	rule, err := c.source.FindModelCostRule(ctx, kind, model, size, quality, resolution, duration)
	if err == nil {
		c.rules[key] = rule
	}
	return rule, err
}

func estimateImageCostMatrix(ctx context.Context, source imageEstimateStore, req imageCostMatrixRequest) (imageCostMatrixResponse, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return imageCostMatrixResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "model is required"}
	}
	if len(req.Sizes) < 1 || len(req.Sizes) > 100 {
		return imageCostMatrixResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "sizes must contain between 1 and 100 entries"}
	}
	qualities := []string{"fixed"}
	if model == imageopts.GPTImage2 {
		qualities = []string{"low", "medium", "high"}
	}
	cached := newCachedImageEstimateStore(source)
	rows := make([]imageCostMatrixRow, 0, len(req.Sizes))
	seen := make(map[string]struct{}, len(req.Sizes))
	versions := make(map[string]struct{})
	sources := make(map[string]struct{})
	for _, rawSize := range req.Sizes {
		size := strings.ToLower(strings.TrimSpace(rawSize))
		if size == "" {
			return imageCostMatrixResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "sizes cannot contain an empty value"}
		}
		if _, ok := seen[size]; ok {
			continue
		}
		seen[size] = struct{}{}
		row := imageCostMatrixRow{Size: size, Costs: make(map[string]int64, len(qualities))}
		for _, quality := range qualities {
			estimateQuality := quality
			if quality == "fixed" {
				estimateQuality = ""
			}
			estimate, err := estimateImageCost(ctx, cached, imageEstimateRequest{Model: model, Size: size, Quality: estimateQuality, N: 1}, nil, false)
			if err != nil {
				return imageCostMatrixResponse{}, err
			}
			row.Costs[quality] = estimate.UnitTokens
			row.PricingTier = estimate.PricingTier
			row.PricingAnchor = estimate.PricingAnchor
			versions[estimate.PriceVersion] = struct{}{}
			sources[estimate.Source] = struct{}{}
		}
		rows = append(rows, row)
	}
	response := imageCostMatrixResponse{Model: model, Qualities: qualities, Rows: rows}
	for version := range versions {
		response.PriceVersions = append(response.PriceVersions, version)
	}
	for source := range sources {
		response.Sources = append(response.Sources, source)
	}
	sort.Strings(response.PriceVersions)
	sort.Strings(response.Sources)
	return response, nil
}

func (s *Server) writeImageEstimate(w http.ResponseWriter, r *http.Request, enforceAPIKeyModels bool) {
	var req imageEstimateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var allowedModels []string
	if enforceAPIKeyModels {
		key, ok := r.Context().Value(apiKeyContext).(domain.APIKey)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key context is missing")
			return
		}
		allowedModels = key.AllowedModels
	}
	estimate, err := estimateImageCost(r.Context(), s.Store, req, allowedModels, enforceAPIKeyModels)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "image cost estimate is temporarily unavailable")
		return
	}
	noteImageRequest(r, domain.ImageRequest{Model: estimate.Model, Size: estimate.Size, Quality: estimate.Quality, N: estimate.Quantity})
	noteRequestEstimate(r, estimate.EstimatedTokens)
	writeJSON(w, http.StatusOK, estimate)
}

func normalizeImageEstimateRequest(req imageEstimateRequest) (domain.ImageRequest, error) {
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		return domain.ImageRequest{}, errors.New("model is required")
	}
	req.Size = strings.ToLower(strings.TrimSpace(req.Size))
	if req.Size == "" || req.Size == "auto" {
		req.Size = "1024x1024"
	}
	req.Quality = strings.ToLower(strings.TrimSpace(req.Quality))
	if req.N == 0 {
		req.N = 1
	}
	if req.N < 1 || req.N > 4 {
		return domain.ImageRequest{}, errors.New("n must be between 1 and 4")
	}
	if req.Model == imageopts.GPTImage2 {
		if req.N != 1 {
			return domain.ImageRequest{}, errors.New("gpt-image-2 currently supports n=1 on this Leonardo account")
		}
		if req.Quality == "" || req.Quality == "auto" {
			req.Quality = "low"
		}
	} else if req.Quality != "" {
		return domain.ImageRequest{}, errors.New("quality is supported only by gpt-image-2")
	}
	return domain.ImageRequest{Model: req.Model, Size: req.Size, Quality: req.Quality, N: req.N}, nil
}

func estimateImageCost(ctx context.Context, rules imageEstimateStore, req imageEstimateRequest, allowedModels []string, enforceAPIKeyModels bool) (imageEstimateResponse, error) {
	normalized, err := normalizeImageEstimateRequest(req)
	if err != nil {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	}
	if enforceAPIKeyModels && !allowed(allowedModels, normalized.Model) {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "model is not allowed for this API key"}
	}
	model, err := rules.GetModel(ctx, normalized.Model)
	if errors.Is(err, store.ErrNotFound) {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "unknown model"}
	}
	if err != nil {
		return imageEstimateResponse{}, err
	}
	if !allowed(model.Capabilities, "text-to-image") {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "selected model is not an image model"}
	}
	if err := validateImageOptions(normalized, model); err != nil {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	}
	estimate, err := pricing.Image(ctx, rules, normalized)
	if errors.Is(err, pricing.ErrCostUnavailable) {
		return imageEstimateResponse{}, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
	}
	if err != nil {
		return imageEstimateResponse{}, err
	}
	response := imageEstimateResponse{
		Model: normalized.Model, Size: normalized.Size, Quality: normalized.Quality,
		Quantity: normalized.N, UnitTokens: estimate.UnitTokens, EstimatedTokens: estimate.Tokens,
		PricingAnchor: estimate.RuleSize, PriceVersion: estimate.PriceVersion, Source: estimate.Source,
	}
	switch normalized.Model {
	case imageopts.GPTImage2:
		multipliers := map[string]float64{"low": 1, "medium": 8.833, "high": 35.167}
		multiplier := multipliers[normalized.Quality]
		response.PricingBasis = "pixel_formula"
		response.QualityMultiplier = &multiplier
		response.Formula = "ceil(width * height / 1000000 * 7 * quality_multiplier) * n"
		response.CostParameters = []string{"model", "size", "quality", "n"}
	case imageopts.NanoBanana2, imageopts.NanoBananaPro:
		response.PricingBasis = "size_tier"
		response.PricingTier = map[string]string{"1024x1024": "small", "2048x2048": "medium", "4096x4096": "large"}[estimate.RuleSize]
		response.Formula = "size_tier_unit_tokens * n"
		response.CostParameters = []string{"model", "size", "n"}
	case imageopts.Seedream50Pro:
		response.PricingBasis = "size_threshold"
		response.PricingTier = map[string]string{"1024x1024": "standard", "2048x2048": "2k"}[estimate.RuleSize]
		response.Formula = "(45 + 45 when width>=2016 and height>=1153, or width>=1153 and height>=2016) * n"
		response.CostParameters = []string{"model", "size", "n"}
	}
	return response, nil
}

func (s *Server) videoEstimate(w http.ResponseWriter, r *http.Request) {
	s.writeVideoEstimate(w, r, true)
}

func (s *Server) publicVideoEstimate(w http.ResponseWriter, r *http.Request) {
	s.writeVideoEstimate(w, r, false)
}

func (s *Server) writeVideoEstimate(w http.ResponseWriter, r *http.Request, enforceAPIKeyModels bool) {
	var req videoEstimateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var allowedModels []string
	if enforceAPIKeyModels {
		key, ok := r.Context().Value(apiKeyContext).(domain.APIKey)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key context is missing")
			return
		}
		allowedModels = key.AllowedModels
	}
	estimate, err := estimateVideoCost(r.Context(), s.Store, req, allowedModels, enforceAPIKeyModels)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "video cost estimate is temporarily unavailable")
		return
	}
	noteVideoRequest(r, domain.VideoRequest{
		Model: estimate.Model, Duration: estimate.Duration, Size: estimate.Size,
		Resolution: estimate.Resolution, GenerateAudio: estimate.GenerateAudio,
	})
	noteRequestEstimate(r, estimate.EstimatedTokens)
	writeJSON(w, http.StatusOK, estimate)
}

func estimateVideoCost(ctx context.Context, rules imageEstimateStore, req videoEstimateRequest, allowedModels []string, enforceAPIKeyModels bool) (videoEstimateResponse, error) {
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "model is required"}
	}
	if enforceAPIKeyModels && !allowed(allowedModels, req.Model) {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "model is not allowed for this API key"}
	}
	model, err := rules.GetModel(ctx, req.Model)
	if errors.Is(err, store.ErrNotFound) {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "unknown model"}
	}
	if err != nil {
		return videoEstimateResponse{}, err
	}
	if !supportsVideoGeneration(model) {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "selected model is not a video model"}
	}
	spec, ok := videospec.Get(req.Model)
	if !ok {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "unknown video model"}
	}
	request := domain.VideoRequest{
		Model: req.Model, Duration: req.Duration, Size: strings.ToLower(strings.TrimSpace(req.Size)),
		Resolution: strings.ToLower(strings.TrimSpace(req.Resolution)), GenerateAudio: req.GenerateAudio,
	}
	if req.HasVideoReference {
		if spec.MaxReferenceVideos == 0 {
			return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: fmt.Sprintf("%s does not support video references", req.Model)}
		}
		request.ReferenceVideos = []domain.SourceMedia{{Filename: "cost-estimate-reference"}}
	}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	}
	estimate, err := pricing.Video(ctx, rules, request)
	if errors.Is(err, pricing.ErrCostUnavailable) {
		return videoEstimateResponse{}, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
	}
	if err != nil {
		return videoEstimateResponse{}, err
	}
	modifiers := make([]string, 0, 2)
	if req.HasVideoReference {
		if request.Model == "kling-o3-omni" {
			modifiers = append(modifiers, "kling_o3_video_reference")
		} else if request.Model == "flux-3-video" {
			modifiers = append(modifiers, "flux_video_reference")
		} else {
			modifiers = append(modifiers, "seedance_video_reference")
		}
	}
	if request.GenerateAudio != nil && !*request.GenerateAudio {
		hasAudioPriceModifier := request.Model == "veo-3.1" || request.Model == "veo-3.1-fast" || (request.Model == "kling-o3-omni" && !req.HasVideoReference && request.Resolution != "2160p")
		if hasAudioPriceModifier {
			modifiers = append(modifiers, "native_audio_disabled")
		}
	}
	formula := "active_price_rule, then schema modifiers for video reference and native audio"
	costParameters := []string{"model", "duration", "resolution", "generate_audio", "has_video_reference"}
	if len(spec.ResolutionBySize) > 0 {
		formula = "active_price_rule selected by the normalized size/resolution tier and duration"
		costParameters = []string{"model", "duration", "size", "resolution"}
		if spec.MaxReferenceVideos > 0 {
			formula += ", then the schema video-reference modifier"
			costParameters = append(costParameters, "has_video_reference")
		}
	}
	return videoEstimateResponse{
		Model: request.Model, Duration: request.Duration, Size: request.Size,
		Resolution: request.Resolution, GenerateAudio: request.GenerateAudio,
		HasVideoReference: req.HasVideoReference, BaseTokens: estimate.UnitTokens,
		EstimatedTokens: estimate.Tokens, AppliedModifiers: modifiers,
		Formula:        formula,
		CostParameters: costParameters,
		PriceVersion:   estimate.PriceVersion, Source: estimate.Source,
	}, nil
}

func (s *Server) asyncImage(w http.ResponseWriter, r *http.Request) {
	s.createAsyncImage(w, r, false)
}

func (s *Server) createAsyncImage(w http.ResponseWriter, r *http.Request, requireReference bool) {
	var req domain.ImageRequest
	var err error
	created := false
	defer func() {
		if !created && s.Assets != nil {
			_ = s.Assets.CleanupImageRequest(req)
		}
	}()
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") && (requireReference || r.URL.Path == "/v1/tasks/images") {
		req, err = s.parseAsyncImageMultipart(r)
	} else if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		err = errors.New("text-to-image compatibility endpoint requires application/json; use multipart /v1/tasks/images for reference images")
	} else if requireReference {
		err = errors.New("image edits require multipart/form-data with image or image[]")
	} else if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		err = decodeErr
	} else {
		req, err = normalizeAsyncImageRequest(req)
	}
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if requireReference && len(req.ReferenceImages) == 0 {
		writeError(w, 400, "invalid_request", "image is required")
		return
	}
	task, taskCreated, err := s.createTask(r, req)
	if err != nil {
		writeCreateTaskError(w, err)
		return
	}
	created = taskCreated
	writeJSON(w, map[bool]int{true: 202, false: 200}[taskCreated], newPublicTaskResponse(task))
}

func validatePublicImageRequest(req domain.ImageRequest) error {
	if req.Public != nil || len(req.StyleIDs) > 0 || len(req.ReferenceIDs) > 0 || req.SourceImage != nil || len(req.SourceImages) > 0 || len(req.ReferenceImages) > 0 || req.ImageStrength != nil || req.ReferenceStrength != "" {
		return errors.New("this endpoint accepts only documented public image parameters; use multipart /v1/tasks/images for reference images")
	}
	return nil
}

func normalizeAsyncImageRequest(req domain.ImageRequest) (domain.ImageRequest, error) {
	if err := validatePublicImageRequest(req); err != nil {
		return domain.ImageRequest{}, err
	}
	return normalizeAsyncImageDelivery(req)
}

func normalizeAsyncImageDelivery(req domain.ImageRequest) (domain.ImageRequest, error) {
	if req.ResponseFormat == "" {
		req.ResponseFormat = "url"
	}
	if req.ResponseFormat != "url" || req.OutputFormat != "" || req.OutputCompression != nil {
		return domain.ImageRequest{}, errors.New("asynchronous image tasks only support response_format=url and do not support output_format or output_compression")
	}
	return req, nil
}

func (s *Server) createTask(r *http.Request, req domain.ImageRequest) (domain.Task, bool, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return domain.Task{}, false, errors.New("prompt is required")
	}
	if req.Model == "" {
		req.Model = "leonardo-auto"
	}
	if err := validateModelPrompt(req.Model, req.Prompt); err != nil {
		return domain.Task{}, false, err
	}
	notPublic := false
	req.Public = &notPublic
	if req.N < 0 || req.N > 4 {
		return domain.Task{}, false, errors.New("n must be between 1 and 4")
	}
	if len(req.StyleIDs) > 0 || len(req.ReferenceIDs) > 0 {
		return domain.Task{}, false, errors.New("style_ids and reference_ids are not exposed by this API")
	}
	if req.Model == imageopts.GPTImage2 {
		if req.N > 1 {
			return domain.Task{}, false, errors.New("gpt-image-2 currently supports n=1 on this Leonardo account")
		}
		if req.Quality == "" || req.Quality == "auto" {
			req.Quality = "low"
		}
	}
	imageCount := len(req.SourceImages) + len(req.ReferenceImages)
	if req.SourceImage != nil {
		imageCount++
	}
	if imageCount > 6 {
		return domain.Task{}, false, errors.New("image edits support at most 6 reference images")
	}
	if imageCount > 0 && req.ReferenceStrength == "" {
		req.ReferenceStrength = "MID"
	} else if req.ReferenceStrength != "" {
		req.ReferenceStrength = strings.ToUpper(req.ReferenceStrength)
		if req.ReferenceStrength != "LOW" && req.ReferenceStrength != "MID" && req.ReferenceStrength != "HIGH" {
			return domain.Task{}, false, errors.New("reference_strength must be LOW, MID or HIGH")
		}
	}
	if req.ResponseFormat == "" {
		if req.Model == imageopts.GPTImage2 {
			req.ResponseFormat = "b64_json"
		} else {
			req.ResponseFormat = "url"
		}
	}
	if req.ResponseFormat != "url" && req.ResponseFormat != "b64_json" {
		return domain.Task{}, false, errors.New("response_format must be url or b64_json")
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if !allowed(key.AllowedModels, req.Model) {
		return domain.Task{}, false, errors.New("model is not allowed for this API key")
	}
	model, err := s.Store.GetModel(r.Context(), req.Model)
	if err != nil {
		return domain.Task{}, false, errors.New("unknown model")
	}
	if !allowed(model.Capabilities, "text-to-image") {
		return domain.Task{}, false, errors.New("selected model is not an image model")
	}
	if err := validateImageOptions(req, model); err != nil {
		return domain.Task{}, false, err
	}
	req.OutputFormat = imageopts.NormalizeOutputFormat(req.OutputFormat)
	noteImageRequest(r, req)
	idem := r.Header.Get("Idempotency-Key")
	if idem != "" {
		if task, err := s.Store.GetIdempotentTaskWithHashRequest(r.Context(), key.ID, "image", imageIdempotencyRequest(req), idem); err == nil {
			noteRequestTask(r, task)
			return task, false, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return domain.Task{}, false, err
		}
	}
	images := req.N
	if images == 0 {
		images = 1
	}
	estimate, err := pricing.Image(r.Context(), s.Store, req)
	if err != nil {
		if errors.Is(err, pricing.ErrCostUnavailable) {
			return domain.Task{}, false, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
		}
		return domain.Task{}, false, err
	}
	noteRequestEstimate(r, estimate.Tokens)
	if err := s.admitDailyQuota(r.Context(), key.ID, images); err != nil {
		return domain.Task{}, false, err
	}
	task, created, err := s.Store.CreateReservedTaskWithHashRequest(r.Context(), key.ID, "image", req.Model, req.Prompt, req, imageIdempotencyRequest(req), idem, estimate.Tokens, estimate.RuleID, s.Config.TaskTimeout+time.Minute)
	noteRequestTask(r, task)
	if err != nil {
		s.rollbackDailyQuota(r.Context(), key.ID, images)
		return task, false, routingRequestError(err)
	}
	if !created {
		s.rollbackDailyQuota(r.Context(), key.ID, images)
		return task, false, nil
	}
	s.enqueueTask(r.Context(), task)
	return task, created, nil
}

func imageIdempotencyRequest(request domain.ImageRequest) domain.ImageRequest {
	clone := request
	clone.ReferenceImages = canonicalMedia(request.ReferenceImages)
	return clone
}

// enqueueTask is intentionally best effort. The task and its account
// reservation are already durable in PostgreSQL; Scheduler.dispatch retries
// any Outbox row that Redis did not accept.

func (s *Server) enqueueTask(ctx context.Context, task domain.Task) {
	if err := s.Queue.EnqueueKind(ctx, task.ID, task.Kind); err != nil {
		_ = s.Store.RecordOutboxError(context.Background(), task.ID, err.Error(), 5*time.Second)
		s.Log.Warn("initial task enqueue failed; outbox will retry", "task_id", task.ID, "kind", task.Kind, "error", err)
	}
}

func validateImageOptions(req domain.ImageRequest, model domain.ModelConfig) error {
	if _, _, err := imageopts.ParseSize(req.Model, req.Size); err != nil {
		return err
	}
	if req.Model == imageopts.GPTImage2 {
		quality := req.Quality
		if quality == "" {
			quality = "auto"
		}
		if quality != "auto" && quality != "low" && quality != "medium" && quality != "high" {
			return errors.New("quality must be auto, low, medium or high")
		}
		if !allowed(model.Capabilities, "quality") {
			return errors.New("quality is not supported by the selected model")
		}
	} else if req.Quality != "" {
		return errors.New("quality is supported only by gpt-image-2")
	}
	format := imageopts.NormalizeOutputFormat(req.OutputFormat)
	if format != "png" && format != "jpeg" {
		return errors.New("output_format must be png or jpeg")
	}
	if req.OutputCompression != nil {
		if *req.OutputCompression < 0 || *req.OutputCompression > 100 {
			return errors.New("output_compression must be between 0 and 100")
		}
		if format != "jpeg" {
			return errors.New("output_compression is only supported for jpeg")
		}
	}
	if req.Background != "" && req.Background != "auto" && req.Background != "opaque" {
		return errors.New("background must be auto or opaque; transparent is not supported")
	}
	if req.Moderation != "" && req.Moderation != "auto" {
		return errors.New("Leonardo does not expose adjustable moderation; moderation must be auto")
	}
	if req.ResponseFormat == "url" && req.OutputFormat != "" {
		return errors.New("output_format requires response_format=b64_json")
	}
	return nil
}

func (s *Server) waitTask(ctx context.Context, id uuid.UUID, timeout time.Duration) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sub := s.Redis.Subscribe(ctx, "leo:task:"+id.String())
	defer sub.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		task, err := s.Store.GetTask(ctx, id)
		if err != nil {
			return task, err
		}
		if task.Terminal() {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return task, ctx.Err()
		case <-ticker.C:
		case <-sub.Channel():
		}
	}
}

func (s *Server) writePendingTask(w http.ResponseWriter, task domain.Task) {
	w.Header().Set("Location", "/v1/tasks/"+task.ID.String())
	w.Header().Set("Retry-After", "3")
	writeJSON(w, http.StatusAccepted, newPublicTaskResponse(task))
}

func (s *Server) writeImageResult(w http.ResponseWriter, ctx context.Context, task domain.Task) {
	if task.Status != domain.TaskSucceeded {
		writeError(w, 502, task.ErrorCode, task.ErrorMessage)
		return
	}
	var result domain.ImageResult
	if err := json.Unmarshal(task.Result, &result); err != nil {
		writeError(w, 500, "invalid_result", err.Error())
		return
	}
	var request domain.ImageRequest
	if err := json.Unmarshal(task.Request, &request); err != nil {
		writeError(w, 500, "invalid_request_state", err.Error())
		return
	}
	if request.ResponseFormat == "b64_json" {
		for i := range result.Data {
			b, err := s.download(ctx, result.Data[i].URL)
			if err != nil {
				writeError(w, 502, "image_download_failed", err.Error())
				return
			}
			b, err = transcodeImage(b, request.OutputFormat, request.OutputCompression)
			if err != nil {
				writeError(w, 422, "image_conversion_failed", err.Error())
				return
			}
			result.Data[i].B64JSON = base64.StdEncoding.EncodeToString(b)
			result.Data[i].URL = ""
		}
	}
	writeJSON(w, 200, result)
}

func transcodeImage(raw []byte, outputFormat string, compression *int) ([]byte, error) {
	target := imageopts.NormalizeOutputFormat(outputFormat)
	img, source, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}
	if target == "webp" {
		if source == "webp" && compression == nil {
			return raw, nil
		}
		return nil, errors.New("webp transcoding with compression is not available; request png/jpeg or omit output_compression when Leonardo returns WebP")
	}
	if target == "jpeg" && source == "jpeg" && compression == nil {
		return raw, nil
	}
	if target == "png" && source == "png" {
		return raw, nil
	}
	var out bytes.Buffer
	switch target {
	case "jpeg":
		quality := 90
		if compression != nil {
			quality = *compression
			if quality == 0 {
				quality = 1
			}
		}
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: quality})
	case "png":
		err = png.Encode(&out, img)
	default:
		return nil, fmt.Errorf("unsupported output format %q", target)
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *Server) download(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("CDN returned %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, s.Config.MaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > s.Config.MaxImageBytes {
		return nil, errors.New("CDN image exceeds configured response limit")
	}
	return b, nil
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_task_id", "invalid task id")
		return
	}
	task, err := s.Store.GetPublicTask(r.Context(), id)
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if err != nil || task.APIKeyID == nil || *task.APIKeyID != key.ID {
		writeError(w, 404, "not_found", "task not found")
		return
	}
	noteRequestTask(r, task)
	w.Header().Set("Retry-After", "3")
	writeJSON(w, 200, newPublicTaskResponse(task))
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_task_id", "invalid task id")
		return
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	task, taskErr := s.Store.GetTask(r.Context(), id)
	if taskErr != nil || task.APIKeyID == nil || *task.APIKeyID != key.ID {
		writeError(w, 404, "not_found", "task not found")
		return
	}
	noteRequestTask(r, task)
	ok, err := s.Store.CancelQueuedTaskAndRelease(r.Context(), id, key.ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !ok {
		writeError(w, 409, "not_cancellable", "only queued tasks can be cancelled")
		return
	}
	if s.Assets != nil {
		switch task.Kind {
		case "video":
			var request domain.VideoRequest
			if json.Unmarshal(task.Request, &request) == nil {
				_ = s.Assets.CleanupVideoRequest(request)
			}
		case "image":
			var request domain.ImageRequest
			if json.Unmarshal(task.Request, &request) == nil {
				_ = s.Assets.CleanupImageRequest(request)
			}
		}
	}
	_ = s.Store.ClearTerminalTaskSourceImage(r.Context(), id)
	writeJSON(w, 200, map[string]any{"id": id, "status": domain.TaskCancelled})
}

func (s *Server) imageEdit(w http.ResponseWriter, r *http.Request) {
	s.createAsyncImage(w, r, true)
}

func (s *Server) parseAsyncImageMultipart(r *http.Request) (domain.ImageRequest, error) {
	maxBody := s.Config.MaxImageBytes*6 + (1 << 20)
	if s.Config.MaxMultipartBytes > 0 && maxBody > s.Config.MaxMultipartBytes {
		maxBody = s.Config.MaxMultipartBytes
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return domain.ImageRequest{}, err
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["mask"]) > 0 {
		return domain.ImageRequest{}, errors.New("mask editing is not exposed by the selected Leonardo models")
	}
	var files []*multipart.FileHeader
	files = append(files, r.MultipartForm.File["image"]...)
	files = append(files, r.MultipartForm.File["image[]"]...)
	if len(files) == 0 {
		return domain.ImageRequest{}, errors.New("image is required")
	}
	if len(files) > 6 {
		return domain.ImageRequest{}, errors.New("at most 6 reference images are supported")
	}
	if s.Assets == nil {
		return domain.ImageRequest{}, errors.New("task asset storage is not configured")
	}
	req := domain.ImageRequest{
		Model: r.FormValue("model"), Prompt: r.FormValue("prompt"), Size: r.FormValue("size"),
		ResponseFormat: r.FormValue("response_format"), Quality: r.FormValue("quality"),
		OutputFormat: r.FormValue("output_format"), Background: r.FormValue("background"),
		Moderation: r.FormValue("moderation"), ReferenceStrength: r.FormValue("reference_strength"),
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.Assets.CleanupImageRequest(req)
		}
	}()
	for _, header := range files {
		asset, err := s.Assets.Save(header, s.Config.MaxImageBytes)
		if err != nil {
			return domain.ImageRequest{}, err
		}
		if !isImageMedia(asset.MediaType) {
			_ = s.Assets.Remove(asset)
			return domain.ImageRequest{}, errors.New("only PNG, JPEG and WebP are supported")
		}
		req.ReferenceImages = append(req.ReferenceImages, asset)
	}
	if v := r.FormValue("n"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &req.N); err != nil {
			return domain.ImageRequest{}, errors.New("n must be an integer")
		}
	}
	if r.FormValue("image_strength") != "" {
		return domain.ImageRequest{}, errors.New("image_strength is not used by the selected reference-image models; use reference_strength")
	}
	if v := r.FormValue("output_compression"); v != "" {
		var compression int
		if _, err := fmt.Sscanf(v, "%d", &compression); err != nil {
			return domain.ImageRequest{}, errors.New("output_compression must be an integer")
		}
		req.OutputCompression = &compression
	}
	var err error
	req, err = normalizeAsyncImageDelivery(req)
	if err != nil {
		return domain.ImageRequest{}, err
	}
	cleanup = false
	return req, nil
}
