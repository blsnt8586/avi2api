package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/pricing"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"
)

type imageEstimateRequest struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Size     string `json:"size,omitempty"`
	Quality  string `json:"quality,omitempty"`
	N        int    `json:"n,omitempty"`
}

type imageEstimateResponse struct {
	Provider          string   `json:"provider"`
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
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model"`
	Sizes    []string `json:"sizes"`
}

type imageCostMatrixRow struct {
	Size          string           `json:"size"`
	Costs         map[string]int64 `json:"costs"`
	PricingTier   string           `json:"pricing_tier,omitempty"`
	PricingAnchor string           `json:"pricing_anchor,omitempty"`
}

type imageCostMatrixResponse struct {
	Provider      string               `json:"provider"`
	Model         string               `json:"model"`
	Qualities     []string             `json:"qualities"`
	Rows          []imageCostMatrixRow `json:"rows"`
	PriceVersions []string             `json:"price_versions"`
	Sources       []string             `json:"sources"`
}

type videoEstimateRequest struct {
	Provider          string `json:"provider,omitempty"`
	Model             string `json:"model"`
	Duration          int    `json:"duration,omitempty"`
	Size              string `json:"size,omitempty"`
	Resolution        string `json:"resolution,omitempty"`
	GenerateAudio     *bool  `json:"generate_audio,omitempty"`
	HasVideoReference bool   `json:"has_video_reference,omitempty"`
	ReferenceMode     string `json:"reference_mode,omitempty"`
}

type videoEstimateResponse struct {
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Duration          int      `json:"duration"`
	Size              string   `json:"size"`
	Resolution        string   `json:"resolution"`
	GenerateAudio     *bool    `json:"generate_audio,omitempty"`
	HasVideoReference bool     `json:"has_video_reference"`
	ReferenceMode     string   `json:"reference_mode,omitempty"`
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
	s.createAsyncImage(w, r)
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
	route, err := s.resolveMediaModel("image", req.Provider, req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Provider, req.Model = route.Provider, route.InternalModel
	_, estimateStore, err := s.providerModelContext(r.Context(), route.Provider, route.InternalModel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "model provider is unavailable")
		return
	}
	response, err := estimateImageCostMatrix(r.Context(), estimateStore, req)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "image cost matrix is temporarily unavailable")
		return
	}
	response.Provider, response.Model = route.Provider, route.PublicModel
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
	if imageopts.IsGPTImage2(model) {
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
			estimate, err := estimateImageCost(ctx, cached, imageEstimateRequest{Provider: req.Provider, Model: model, Size: size, Quality: estimateQuality, N: 1}, nil, false)
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
	response := imageCostMatrixResponse{Provider: req.Provider, Model: model, Qualities: qualities, Rows: rows}
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
	route, providerErr := s.resolveMediaModel("image", req.Provider, req.Model)
	if providerErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", providerErr.Error())
		return
	}
	if enforceAPIKeyModels && !allowedMediaModel(allowedModels, route) {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is not allowed for this API key")
		return
	}
	req.Provider, req.Model = route.Provider, route.InternalModel
	_, estimateStore, providerErr := s.providerModelContext(r.Context(), route.Provider, route.InternalModel)
	if providerErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "model provider is unavailable")
		return
	}
	estimate, err := estimateImageCost(r.Context(), estimateStore, req, nil, false)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "image cost estimate is temporarily unavailable")
		return
	}
	estimate.Provider, estimate.Model = route.Provider, route.PublicModel
	noteImageRequest(r, domain.ImageRequest{Provider: route.Provider, Model: route.InternalModel, Size: estimate.Size, Quality: estimate.Quality, N: estimate.Quantity})
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
	if imageopts.IsGPTImage2(req.Model) || imageopts.IsAdobeImageModel(req.Provider, req.Model) {
		if req.N != 1 {
			return domain.ImageRequest{}, errors.New("selected image route currently supports n=1")
		}
	}
	if imageopts.IsGPTImage2(req.Model) {
		if req.Quality == "" || req.Quality == "auto" {
			req.Quality = "low"
		}
	} else if req.Quality != "" {
		return domain.ImageRequest{}, errors.New("quality is supported only by gpt-image-2")
	}
	return domain.ImageRequest{Provider: req.Provider, Model: req.Model, Size: req.Size, Quality: req.Quality, N: req.N}, nil
}

func estimateImageCost(ctx context.Context, rules imageEstimateStore, req imageEstimateRequest, allowedModels []string, enforceAPIKeyModels bool) (imageEstimateResponse, error) {
	normalized, err := normalizeImageEstimateRequest(req)
	if err != nil {
		return imageEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	}
	if enforceAPIKeyModels && !allowed(allowedModels, mediaModelPermission(normalized.Provider, normalized.Model)) {
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
	estimate, err := pricing.ImageForProvider(ctx, rules, normalized.Provider, normalized)
	if errors.Is(err, pricing.ErrCostUnavailable) {
		return imageEstimateResponse{}, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
	}
	if err != nil {
		return imageEstimateResponse{}, err
	}
	response := imageEstimateResponse{
		Provider: normalized.Provider, Model: normalized.Model, Size: normalized.Size, Quality: normalized.Quality,
		Quantity: normalized.N, UnitTokens: estimate.UnitTokens, EstimatedTokens: estimate.Tokens,
		PricingAnchor: estimate.RuleSize, PriceVersion: estimate.PriceVersion, Source: estimate.Source,
	}
	if imageopts.IsAdobeImageModel(normalized.Provider, normalized.Model) {
		response.PricingBasis = "provider_rule"
		response.Formula = "adobe_bks_unit_tokens * n"
		response.CostParameters = []string{"model", "size", "n"}
		if imageopts.IsAdobeGPTImage2(normalized.Provider, normalized.Model) {
			response.CostParameters = []string{"model", "size", "quality", "n"}
		}
		return response, nil
	}
	switch imageopts.BaseModel(normalized.Model) {
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
	route, providerErr := s.resolveMediaModel("video", req.Provider, req.Model)
	if providerErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", providerErr.Error())
		return
	}
	if enforceAPIKeyModels && !allowedMediaModel(allowedModels, route) {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is not allowed for this API key")
		return
	}
	req.Provider, req.Model = route.Provider, route.InternalModel
	_, estimateStore, providerErr := s.providerModelContext(r.Context(), route.Provider, route.InternalModel)
	if providerErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "model provider is unavailable")
		return
	}
	estimate, err := estimateVideoCost(r.Context(), estimateStore, req, nil, false)
	if err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			writeCreateTaskError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "estimate_failed", "video cost estimate is temporarily unavailable")
		return
	}
	estimate.Provider, estimate.Model = route.Provider, route.PublicModel
	noteVideoRequest(r, domain.VideoRequest{
		Provider: estimate.Provider, Model: estimate.Model, Duration: estimate.Duration, Size: estimate.Size,
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
	if enforceAPIKeyModels && !allowed(allowedModels, mediaModelPermission(req.Provider, req.Model)) {
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
	spec, ok := videospec.GetForProvider(req.Provider, req.Model)
	if !ok {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "unknown video model"}
	}
	request := domain.VideoRequest{
		Provider: req.Provider, Model: req.Model, Duration: req.Duration, Size: strings.ToLower(strings.TrimSpace(req.Size)),
		Resolution: strings.ToLower(strings.TrimSpace(req.Resolution)), GenerateAudio: req.GenerateAudio,
	}
	referenceMode := strings.ToLower(strings.TrimSpace(req.ReferenceMode))
	if referenceMode == "" {
		referenceMode = "text"
	}
	if req.Provider == "adobe" && req.Model == adobe.AdobeKling30Omni {
		switch referenceMode {
		case "text":
		case "frame":
			request.StartFrame = &domain.SourceMedia{Filename: "cost-estimate-frame"}
		case "image":
			request.ReferenceImages = []domain.SourceMedia{{Filename: "cost-estimate-image"}}
		default:
			return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "reference_mode must be text, frame or image for kling-3.0-omni on provider adobe"}
		}
	} else if req.ReferenceMode != "" {
		return videoEstimateResponse{}, &requestError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "reference_mode is supported only by kling-3.0-omni on provider adobe"}
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
	estimate, err := pricing.VideoForProvider(ctx, rules, request.Provider, request)
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
	if request.Provider == "adobe" {
		formula = "active Adobe BKS price rule selected by normalized duration and size/resolution"
		costParameters = []string{"model", "duration", "size", "resolution"}
		if request.Model == adobe.AdobeKling30Omni {
			formula += ", including the text/frame/image workflow"
			costParameters = append(costParameters, "reference_mode")
		}
	}
	return videoEstimateResponse{
		Provider: request.Provider, Model: request.Model, Duration: request.Duration, Size: request.Size,
		Resolution: request.Resolution, GenerateAudio: request.GenerateAudio,
		HasVideoReference: req.HasVideoReference, BaseTokens: estimate.UnitTokens,
		ReferenceMode:   referenceMode,
		EstimatedTokens: estimate.Tokens, AppliedModifiers: modifiers,
		Formula:        formula,
		CostParameters: costParameters,
		PriceVersion:   estimate.PriceVersion, Source: estimate.Source,
	}, nil
}

func (s *Server) createAsyncImage(w http.ResponseWriter, r *http.Request) {
	var req domain.ImageRequest
	var err error
	created := false
	defer func() {
		if !created && s.Assets != nil {
			_ = s.Assets.CleanupImageRequest(req)
		}
	}()
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		req, err = s.parseAsyncImageMultipart(r)
	} else if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		err = decodeErr
	} else {
		req, err = normalizeAsyncImageRequest(req)
	}
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
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
		return errors.New("reference images must be uploaded with multipart/form-data using image or image[]")
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
	route, err := s.resolveMediaModel("image", req.Provider, req.Model)
	if err != nil {
		return domain.Task{}, false, err
	}
	req.Provider, req.Model = route.Provider, route.InternalModel
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
	if imageopts.IsGPTImage2(req.Model) || imageopts.IsAdobeImageModel(req.Provider, req.Model) {
		if req.N > 1 {
			return domain.Task{}, false, errors.New("selected image route currently supports n=1")
		}
	}
	if imageopts.IsGPTImage2(req.Model) {
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
		req.ResponseFormat = "url"
	}
	if req.ResponseFormat != "url" || req.OutputFormat != "" || req.OutputCompression != nil {
		return domain.Task{}, false, errors.New("asynchronous image tasks only support response_format=url and do not support output_format or output_compression")
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if !allowedMediaModel(key.AllowedModels, route) {
		return domain.Task{}, false, errors.New("model is not allowed for this API key")
	}
	model, err := s.Store.GetModel(r.Context(), route.InternalModel)
	if err != nil {
		return domain.Task{}, false, errors.New("unknown model")
	}
	if !allowed(model.Capabilities, "text-to-image") {
		return domain.Task{}, false, errors.New("selected model is not an image model")
	}
	if err := validateImageOptions(req, model); err != nil {
		return domain.Task{}, false, err
	}
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
	if err := s.admitProviderCircuit(r.Context(), route.Provider); err != nil {
		return domain.Task{}, false, err
	}
	images := req.N
	if images == 0 {
		images = 1
	}
	providerConfig, rules, err := s.providerModelContext(r.Context(), route.Provider, route.InternalModel)
	if err != nil {
		return domain.Task{}, false, errors.New("model provider is unavailable")
	}
	estimate, err := pricing.ImageForProvider(r.Context(), rules, route.Provider, req)
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
	task, created, err := s.Store.CreateReservedTaskForProviderWithHashRequest(r.Context(), key.ID, providerConfig.ProviderID, "image", route.PublicModel, req.Prompt, req, imageIdempotencyRequest(req), idem, estimate.Tokens, estimate.RuleID, s.Config.TaskTimeout+time.Minute)
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
	if _, _, err := imageopts.ParseSizeForProvider(req.Provider, req.Model, req.Size); err != nil {
		return err
	}
	if imageopts.IsGPTImage2(req.Model) {
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
	if req.Background != "" && req.Background != "auto" && req.Background != "opaque" {
		return errors.New("background must be auto or opaque; transparent is not supported")
	}
	if req.Moderation != "" && req.Moderation != "auto" {
		return errors.New("the selected provider does not expose adjustable moderation; moderation must be auto")
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
	w.Header().Set("Location", "/v1/images/"+task.ID.String())
	w.Header().Set("Retry-After", "3")
	writeJSON(w, http.StatusAccepted, newPublicTaskResponse(task))
}

func (s *Server) getImageTask(w http.ResponseWriter, r *http.Request) {
	s.getTaskForKind(w, r, "image")
}

func (s *Server) getVideoTask(w http.ResponseWriter, r *http.Request) {
	s.getTaskForKind(w, r, "video")
}

func (s *Server) getAudioTask(w http.ResponseWriter, r *http.Request) {
	s.getTaskForKind(w, r, "audio")
}

func (s *Server) getTaskForKind(w http.ResponseWriter, r *http.Request, kind string) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_task_id", "invalid task id")
		return
	}
	task, err := s.Store.GetPublicTask(r.Context(), id)
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if err != nil || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.Kind != kind {
		writeError(w, 404, "not_found", "task not found")
		return
	}
	noteRequestTask(r, task)
	w.Header().Set("Retry-After", "3")
	writeJSON(w, 200, newPublicTaskResponse(task))
}

func (s *Server) cancelImageTask(w http.ResponseWriter, r *http.Request) {
	s.cancelTaskForKind(w, r, "image")
}

func (s *Server) cancelVideoTask(w http.ResponseWriter, r *http.Request) {
	s.cancelTaskForKind(w, r, "video")
}

func (s *Server) cancelAudioTask(w http.ResponseWriter, r *http.Request) {
	s.cancelTaskForKind(w, r, "audio")
}

func (s *Server) cancelTaskForKind(w http.ResponseWriter, r *http.Request, kind string) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid_task_id", "invalid task id")
		return
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	task, taskErr := s.Store.GetTask(r.Context(), id)
	if taskErr != nil || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.Kind != kind {
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
		Provider: r.FormValue("provider"), Model: r.FormValue("model"), Prompt: r.FormValue("prompt"), Size: r.FormValue("size"),
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
