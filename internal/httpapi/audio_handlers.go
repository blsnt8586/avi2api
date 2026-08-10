package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/leonardo2api/leonardo2api/internal/audioopts"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/pricing"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type publicAudioJSONRequest struct {
	Model             string   `json:"model"`
	Prompt            string   `json:"prompt"`
	N                 int      `json:"n,omitempty"`
	Duration          int      `json:"duration,omitempty"`
	DurationMinutes   int      `json:"duration_minutes,omitempty"`
	ForceInstrumental bool     `json:"force_instrumental,omitempty"`
	Loop              bool     `json:"loop,omitempty"`
	Voice             string   `json:"voice,omitempty"`
	Language          string   `json:"language,omitempty"`
	PromptInfluence   *float64 `json:"prompt_influence,omitempty"`
}

func (req publicAudioJSONRequest) domainRequest() domain.AudioRequest {
	return domain.AudioRequest{
		Model: req.Model, Prompt: req.Prompt, N: req.N, Duration: req.Duration,
		DurationMinutes: req.DurationMinutes, ForceInstrumental: req.ForceInstrumental,
		Loop: req.Loop, Voice: req.Voice, Language: req.Language,
		PromptInfluence: req.PromptInfluence,
	}
}

func (s *Server) audioGeneration(w http.ResponseWriter, r *http.Request) {
	var publicReq publicAudioJSONRequest
	if err := decodeJSON(r, &publicReq); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	req := publicReq.domainRequest()
	task, created, err := s.createAudioTask(r, req)
	if err != nil {
		writeCreateTaskError(w, err)
		return
	}
	writeJSON(w, map[bool]int{true: http.StatusAccepted, false: http.StatusOK}[created], newPublicTaskResponse(task))
}

func (s *Server) createAudioTask(r *http.Request, req domain.AudioRequest) (domain.Task, bool, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return domain.Task{}, false, errors.New("prompt is required")
	}
	if req.Model == "" {
		req.Model = "sound-effects-v2"
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	if !allowed(key.AllowedModels, req.Model) {
		return domain.Task{}, false, errors.New("model is not allowed for this API key")
	}
	model, err := s.Store.GetModel(r.Context(), req.Model)
	if err != nil {
		return domain.Task{}, false, errors.New("unknown audio model")
	}
	if err := applyAndValidateAudioDefaults(&req, model); err != nil {
		return domain.Task{}, false, err
	}
	notPublic := false
	req.Public = &notPublic
	noteAudioRequest(r, req)
	idem := r.Header.Get("Idempotency-Key")
	if idem != "" {
		if task, err := s.Store.GetIdempotentTask(r.Context(), key.ID, "audio", req, idem); err == nil {
			noteRequestTask(r, task)
			return task, false, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return domain.Task{}, false, err
		}
	}
	estimate, err := pricing.Audio(r.Context(), s.Store, req)
	if err != nil {
		if errors.Is(err, pricing.ErrCostUnavailable) {
			return domain.Task{}, false, &requestError{Status: http.StatusUnprocessableEntity, Code: "cost_unavailable", Message: "cost is unavailable for the selected model parameters"}
		}
		return domain.Task{}, false, err
	}
	noteRequestEstimate(r, estimate.Tokens)
	if err := s.admitDailyQuota(r.Context(), key.ID, req.N); err != nil {
		return domain.Task{}, false, err
	}
	task, created, err := s.Store.CreateReservedTask(r.Context(), key.ID, "audio", req.Model, req.Prompt, req, idem, estimate.Tokens, estimate.RuleID, s.Config.TaskTimeout+time.Minute)
	noteRequestTask(r, task)
	if err != nil {
		s.rollbackDailyQuota(r.Context(), key.ID, req.N)
		return task, false, routingRequestError(err)
	}
	if !created {
		s.rollbackDailyQuota(r.Context(), key.ID, req.N)
		return task, false, nil
	}
	s.enqueueTask(r.Context(), task)
	return task, created, nil
}

func applyAndValidateAudioDefaults(req *domain.AudioRequest, model domain.ModelConfig) error {
	var defaults struct {
		Quantity          int      `json:"quantity"`
		Duration          int      `json:"duration"`
		DurationMinutes   int      `json:"duration_minutes"`
		Voice             string   `json:"voice"`
		Language          string   `json:"language"`
		PromptInfluence   *float64 `json:"prompt_influence"`
		ForceInstrumental bool     `json:"force_instrumental"`
		Loop              bool     `json:"loop"`
	}
	_ = json.Unmarshal(model.Defaults, &defaults)
	if req.N == 0 {
		req.N = defaults.Quantity
	}
	if req.N == 0 {
		req.N = 1
	}
	if req.N < 1 || req.N > 4 {
		return errors.New("n must be between 1 and 4")
	}
	switch req.Model {
	case "dialogue-v3":
		if !allowed(model.Capabilities, "text-to-speech") {
			return errors.New("selected model is not a speech model")
		}
		if utf8.RuneCountInString(req.Prompt) > 5000 {
			return errors.New("dialogue-v3 prompt must not exceed 5000 characters")
		}
		if req.Duration != 0 || req.DurationMinutes != 0 || req.ForceInstrumental || req.Loop {
			return errors.New("dialogue-v3 only accepts voice, language and prompt_influence audio options")
		}
		if req.Voice == "" {
			req.Voice = defaults.Voice
		}
		if req.Voice == "" {
			req.Voice = "george"
		}
		req.Voice = strings.ToLower(strings.TrimSpace(req.Voice))
		if _, ok := audioopts.VoiceID(req.Voice); !ok {
			return fmt.Errorf("voice must be one of: %s", strings.Join(audioopts.VoiceNames(), ", "))
		}
		if req.Language == "" {
			req.Language = defaults.Language
		}
		if req.Language == "" {
			req.Language = "en"
		}
		req.Language = strings.ToLower(strings.TrimSpace(req.Language))
		if len(req.Language) > 16 {
			return errors.New("language must not exceed 16 characters")
		}
		if req.PromptInfluence == nil {
			req.PromptInfluence = defaults.PromptInfluence
		}
		if req.PromptInfluence == nil {
			v := 0.5
			req.PromptInfluence = &v
		}
	case "music-v1":
		if !allowed(model.Capabilities, "text-to-music") {
			return errors.New("selected model is not a music model")
		}
		if utf8.RuneCountInString(req.Prompt) > 9999 {
			return errors.New("music-v1 prompt must not exceed 9999 characters")
		}
		if req.Duration != 0 || req.Voice != "" || req.Language != "" || req.PromptInfluence != nil || req.Loop {
			return errors.New("music-v1 only accepts duration_minutes and force_instrumental audio options")
		}
		if req.DurationMinutes == 0 {
			req.DurationMinutes = defaults.DurationMinutes
		}
		if req.DurationMinutes == 0 {
			req.DurationMinutes = 1
		}
		if req.DurationMinutes < 1 || req.DurationMinutes > 10 {
			return errors.New("music-v1 duration_minutes must be between 1 and 10")
		}
	case "sound-effects-v2":
		if !allowed(model.Capabilities, "text-to-sound") {
			return errors.New("selected model is not a sound effect model")
		}
		if utf8.RuneCountInString(req.Prompt) > 9999 {
			return errors.New("sound-effects-v2 prompt must not exceed 9999 characters")
		}
		if req.DurationMinutes != 0 || req.Voice != "" || req.Language != "" || req.ForceInstrumental {
			return errors.New("sound-effects-v2 only accepts duration, loop and prompt_influence audio options")
		}
		if req.Duration == 0 {
			req.Duration = defaults.Duration
		}
		if req.Duration == 0 {
			req.Duration = 2
		}
		if req.Duration < 1 || req.Duration > 22 {
			return errors.New("sound-effects-v2 duration must be between 1 and 22 seconds")
		}
		if req.PromptInfluence == nil {
			req.PromptInfluence = defaults.PromptInfluence
		}
		if req.PromptInfluence == nil {
			v := 0.7
			req.PromptInfluence = &v
		}
	default:
		return errors.New("unknown audio model")
	}
	if req.PromptInfluence != nil && (*req.PromptInfluence < 0 || *req.PromptInfluence > 1) {
		return errors.New("prompt_influence must be between 0 and 1")
	}
	return nil
}
