package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"net/http"
	"strings"
	"time"
)

type chatRequest struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
}

func (request chatRequest) imageRequest(prompt string, source *domain.SourceImage) domain.ImageRequest {
	return domain.ImageRequest{
		Provider: request.Provider, Model: request.Model, Prompt: prompt,
		ResponseFormat: "url", SourceImage: source,
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var cr chatRequest
	maxBody := s.Config.MaxImageBytes*4/3 + (2 << 20)
	if err := decodeJSONLimit(r, &cr, maxBody); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	prompt, source, parseErr := lastChatInput(cr.Messages)
	if parseErr != nil {
		writeError(w, 400, "invalid_request", parseErr.Error())
		return
	}
	if prompt == "" {
		writeError(w, 400, "invalid_request", "no user prompt found")
		return
	}
	model := cr.Model
	if model == "" {
		model = "gpt-image-2"
	}
	cr.Model = model
	task, _, err := s.createTask(r, cr.imageRequest(prompt, source))
	if err != nil {
		writeCreateTaskError(w, err)
		return
	}
	noteChatRequest(r)
	if cr.Stream {
		s.streamChat(w, r, task.ID)
		return
	}
	task, err = s.waitTask(r.Context(), task.ID, s.Config.SyncTimeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			s.writePendingTask(w, task)
			return
		}
		writeError(w, 500, "generation_wait_failed", err.Error())
		return
	}
	if task.Status != domain.TaskSucceeded {
		writeError(w, 502, task.ErrorCode, task.ErrorMessage)
		return
	}
	var result domain.ImageResult
	_ = json.Unmarshal(task.Result, &result)
	var links []string
	for _, d := range result.Data {
		links = append(links, "![generated image]("+d.URL+")")
	}
	writeJSON(w, 200, map[string]any{"id": "chatcmpl-" + task.ID.String(), "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": strings.Join(links, "\n")}, "finish_reason": "stop"}}})
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	last := -1
	for {
		task, err := s.Store.GetTask(r.Context(), id)
		if err != nil {
			return
		}
		if task.Progress != last {
			last = task.Progress
			chunk := map[string]any{
				"id":     "chatcmpl-" + id.String(),
				"object": "chat.completion.chunk",
				"choices": []any{map[string]any{
					"index": 0,
					"delta": map[string]any{"content": fmt.Sprintf("Generation progress: %d%%\n", task.Progress)},
				}},
			}
			if err := sendSSE(w, chunk); err != nil {
				return
			}
			f.Flush()
		}
		if task.Terminal() {
			if task.Status == domain.TaskSucceeded {
				var result domain.ImageResult
				_ = json.Unmarshal(task.Result, &result)
				for _, d := range result.Data {
					if err := sendSSE(w, map[string]any{
						"id":     "chatcmpl-" + id.String(),
						"object": "chat.completion.chunk",
						"choices": []any{map[string]any{
							"index": 0,
							"delta": map[string]any{"content": "![generated image](" + d.URL + ")"},
						}},
					}); err != nil {
						return
					}
				}
			} else {
				code := task.ErrorCode
				if code == "" {
					code = "upstream_failed"
				}
				message := task.ErrorMessage
				if message == "" {
					message = "upstream generation failed"
				}
				if err := sendSSE(w, map[string]any{"error": map[string]any{
					"message": message,
					"type":    code,
					"code":    code,
				}}); err != nil {
					return
				}
			}
			if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
				return
			}
			f.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func lastChatInput(messages []struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}) (string, *domain.SourceImage, error) {
	lastUser := -1
	for index, message := range messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" {
			return "", nil, fmt.Errorf("unsupported chat role %q", message.Role)
		}
		if _, _, err := parseChatContent(message.Content, false); err != nil {
			return "", nil, fmt.Errorf("messages[%d].content: %w", index, err)
		}
		if message.Role == "user" {
			lastUser = index
		}
	}
	if lastUser < 0 {
		return "", nil, errors.New("messages must include a user message")
	}
	return parseChatContent(messages[lastUser].Content, true)
}

func parseChatContent(content any, includeImage bool) (string, *domain.SourceImage, error) {
	switch value := content.(type) {
	case string:
		if value == "" {
			return "", nil, errors.New("content must not be empty")
		}
		return value, nil, nil
	case []any:
		if len(value) == 0 {
			return "", nil, errors.New("content parts must not be empty")
		}
		texts := make([]string, 0, len(value))
		var source *domain.SourceImage
		imageCount := 0
		for index, part := range value {
			item, ok := part.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("part %d must be an object", index)
			}
			typeName, ok := item["type"].(string)
			if !ok {
				return "", nil, fmt.Errorf("part %d requires a string type", index)
			}
			switch typeName {
			case "text":
				if !hasOnlyKeys(item, "type", "text") {
					return "", nil, fmt.Errorf("text part %d contains unsupported fields", index)
				}
				text, ok := item["text"].(string)
				if !ok || text == "" {
					return "", nil, fmt.Errorf("text part %d requires non-empty text", index)
				}
				texts = append(texts, text)
			case "image_url":
				if !hasOnlyKeys(item, "type", "image_url") {
					return "", nil, fmt.Errorf("image_url part %d contains unsupported fields", index)
				}
				imageCount++
				if imageCount > 1 {
					return "", nil, errors.New("chat content accepts at most one image_url part")
				}
				imageURL, ok := item["image_url"].(map[string]any)
				if !ok || !hasOnlyKeys(imageURL, "url") {
					return "", nil, fmt.Errorf("image_url part %d requires only a url field", index)
				}
				rawURL, ok := imageURL["url"].(string)
				if !ok || !strings.HasPrefix(rawURL, "data:") {
					return "", nil, errors.New("chat image_url currently requires a data URL")
				}
				parsed, err := sourceImageFromDataURL(rawURL)
				if err != nil {
					return "", nil, err
				}
				if includeImage {
					source = parsed
				}
			default:
				return "", nil, fmt.Errorf("unsupported chat content part type %q", typeName)
			}
		}
		return strings.Join(texts, "\n"), source, nil
	default:
		return "", nil, errors.New("content must be a string or an array of content parts")
	}
}

func sourceImageFromDataURL(rawURL string) (*domain.SourceImage, error) {
	comma := strings.IndexByte(rawURL, ',')
	if comma < 0 || !strings.Contains(rawURL[:comma], ";base64") {
		return nil, errors.New("invalid image data URL")
	}
	mediaType := strings.TrimPrefix(strings.SplitN(rawURL[:comma], ";", 2)[0], "data:")
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		return nil, errors.New("chat image must be PNG, JPEG or WebP")
	}
	data, err := base64.StdEncoding.DecodeString(rawURL[comma+1:])
	if err != nil {
		return nil, errors.New("invalid image data URL")
	}
	if int64(len(data)) > 25<<20 {
		return nil, errors.New("chat image exceeds 25 MB")
	}
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}[mediaType]
	return &domain.SourceImage{Filename: "chat-image." + ext, MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func hasOnlyKeys(value map[string]any, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}
