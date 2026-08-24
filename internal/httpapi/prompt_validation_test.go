package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/modelconstraints"
)

func TestValidateModelPromptBoundaries(t *testing.T) {
	for model, limit := range modelconstraints.AllPromptLimits() {
		t.Run(model, func(t *testing.T) {
			if err := validateModelPrompt(model, strings.Repeat("生", limit)); err != nil {
				t.Fatalf("exact limit rejected: %v", err)
			}
			err := validateModelPrompt(model, strings.Repeat("😀", limit+1))
			var requestErr *requestError
			if !errors.As(err, &requestErr) || requestErr.Code != "prompt_too_long" {
				t.Fatalf("over limit error=%#v", err)
			}
			details := requestErr.Details.(map[string]any)
			if details["model"] != model || details["actual_characters"] != limit+1 || details["max_characters"] != limit {
				t.Fatalf("unexpected details: %+v", details)
			}
		})
	}
}

func TestPromptLimitsRunBeforeTaskAdmission(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	for _, test := range []struct {
		provider string
		model    string
		kind     string
	}{
		{model: "leonardo/gpt-image-2", kind: "image"}, {model: "leonardo/nano-banana-2", kind: "image"}, {model: "leonardo/nano-banana-pro", kind: "image"}, {model: "leonardo/seedream-5.0-pro", kind: "image"},
		{model: "leonardo/flux-3-video", kind: "video"}, {model: "leonardo/seedance-2.0", kind: "video"}, {model: "leonardo/seedance-2.0-fast", kind: "video"}, {model: "leonardo/seedance-2.0-mini", kind: "video"}, {model: "leonardo/seedance-2.5", kind: "video"}, {model: "leonardo/veo-3.1", kind: "video"}, {model: "leonardo/veo-3.1-fast", kind: "video"}, {model: "adobe/kling-3.0-omni", kind: "video"}, {model: "leonardo/kling-o3-omni", kind: "video"}, {model: "leonardo/minimax-h3", kind: "video"}, {model: "leonardo/grok-imagine-1.5", kind: "video"},
		{model: "leonardo/dialogue-v3", kind: "audio"}, {model: "leonardo/music-v1", kind: "audio"}, {model: "leonardo/sound-effects-v2", kind: "audio"},
	} {
		t.Run(test.model, func(t *testing.T) {
			modelName := test.model
			if slash := strings.IndexByte(modelName, '/'); slash >= 0 {
				modelName = modelName[slash+1:]
			}
			limit, _ := modelconstraints.PromptLimit(modelName)
			prompt := strings.Repeat("长", limit+1)
			var err error
			switch test.kind {
			case "image":
				_, _, err = server.createTask(request, domain.ImageRequest{Provider: test.provider, Model: test.model, Prompt: prompt})
			case "video":
				_, _, err = server.createVideoTask(request, domain.VideoRequest{Provider: test.provider, Model: test.model, Prompt: prompt})
			case "audio":
				_, _, err = server.createAudioTask(request, domain.AudioRequest{Model: test.model, Prompt: prompt})
			}
			var requestErr *requestError
			if !errors.As(err, &requestErr) || requestErr.Code != "prompt_too_long" {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

func TestPromptTooLongResponseIncludesDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeCreateTaskError(recorder, validateModelPrompt("minimax-h3", strings.Repeat("😀", 2001)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "prompt_too_long" || response.Error.Details["model"] != "minimax-h3" || response.Error.Details["actual_characters"] != float64(2001) || response.Error.Details["max_characters"] != float64(2000) {
		t.Fatalf("unexpected response: %+v", response)
	}
}
