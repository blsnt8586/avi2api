package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
)

const apiRequestLogContext contextKey = "api_request_log"

type apiRequestMetadata struct {
	APIKeyID        *uuid.UUID
	APIKeyPrefix    string
	Kind            string
	Model           string
	Parameters      map[string]any
	PromptChars     int
	EstimatedTokens *int64
	TaskID          *uuid.UUID
	AccountID       *uuid.UUID
}

type cappedResponseBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *cappedResponseBuffer) Write(value []byte) (int, error) {
	if remaining := buffer.limit - buffer.Len(); remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		_, _ = buffer.Buffer.Write(value[:remaining])
	}
	return len(value), nil
}

func (s *Server) apiRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata := &apiRequestMetadata{Parameters: map[string]any{}}
		if key, ok := r.Context().Value(apiKeyContext).(domain.APIKey); ok {
			noteAPIKeyMetadata(metadata, key)
		}
		ctx := context.WithValue(r.Context(), apiRequestLogContext, metadata)
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		response := &cappedResponseBuffer{limit: 8192}
		wrapped.Tee(response)
		startedAt := time.Now()
		next.ServeHTTP(wrapped, r.WithContext(ctx))
		status := wrapped.Status()
		if status == 0 {
			status = http.StatusOK
		}
		parameters, err := json.Marshal(metadata.Parameters)
		if err != nil {
			parameters = []byte(`{}`)
		}
		entry := domain.APIRequestLog{
			RequestID: middleware.GetReqID(r.Context()), APIKeyID: metadata.APIKeyID, APIKeyPrefix: metadata.APIKeyPrefix,
			AccountID: metadata.AccountID, TaskID: metadata.TaskID, Method: r.Method, Path: r.URL.Path,
			Kind: metadata.Kind, Model: metadata.Model, Parameters: parameters,
			PromptChars: metadata.PromptChars, EstimatedTokens: metadata.EstimatedTokens,
			Status: status, ErrorCode: responseErrorCode(status, response.Bytes()),
			DurationMS: time.Since(startedAt).Milliseconds(), ClientIP: clientIPFromRequest(r),
		}
		if s.requestLogQueue == nil {
			logCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := s.Store.WriteAPIRequestLog(logCtx, entry)
			cancel()
			if err != nil {
				s.Log.Warn("write API request log", "request_id", entry.RequestID, "error", err)
			}
		} else {
			select {
			case s.requestLogQueue <- entry:
			default:
				metrics.RequestLogDropped.Inc()
				s.Log.Warn("drop API request log because buffer is full", "request_id", entry.RequestID)
			}
		}
	})
}

func (s *Server) runRequestLogWorker() {
	for first := range s.requestLogQueue {
		batch := make([]domain.APIRequestLog, 1, 100)
		batch[0] = first
		timer := time.NewTimer(100 * time.Millisecond)
	collect:
		for len(batch) < cap(batch) {
			select {
			case entry, ok := <-s.requestLogQueue:
				if !ok {
					break collect
				}
				batch = append(batch, entry)
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		logCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.Store.WriteAPIRequestLogs(logCtx, batch)
		cancel()
		if err != nil {
			s.Log.Warn("write API request log batch", "count", len(batch), "error", err)
		}
	}
}

func noteRequestAPIKey(r *http.Request, key domain.APIKey) {
	if metadata := requestMetadata(r); metadata != nil {
		noteAPIKeyMetadata(metadata, key)
	}
}

func noteAPIKeyMetadata(metadata *apiRequestMetadata, key domain.APIKey) {
	keyID := key.ID
	metadata.APIKeyID = &keyID
	metadata.APIKeyPrefix = key.Prefix
}

func responseErrorCode(status int, body []byte) string {
	if status < 400 {
		return ""
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &response)
	return response.Error.Code
}

func requestMetadata(r *http.Request) *apiRequestMetadata {
	metadata, _ := r.Context().Value(apiRequestLogContext).(*apiRequestMetadata)
	return metadata
}

func noteImageRequest(r *http.Request, request domain.ImageRequest) {
	metadata := requestMetadata(r)
	if metadata == nil {
		return
	}
	metadata.Kind = "image"
	metadata.Model = request.Model
	metadata.PromptChars = utf8.RuneCountInString(request.Prompt)
	metadata.Parameters = map[string]any{
		"size": request.Size, "quality": request.Quality, "n": request.N,
		"response_format": request.ResponseFormat, "output_format": request.OutputFormat,
		"reference_images": len(request.ReferenceImages) + len(request.SourceImages) + boolInt(request.SourceImage != nil),
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func noteVideoRequest(r *http.Request, request domain.VideoRequest) {
	metadata := requestMetadata(r)
	if metadata == nil {
		return
	}
	metadata.Kind = "video"
	metadata.Model = request.Model
	metadata.PromptChars = utf8.RuneCountInString(request.Prompt)
	metadata.Parameters = map[string]any{
		"size": request.Size, "resolution": request.Resolution, "duration": request.Duration,
		"generate_audio": request.GenerateAudio, "reference_images": len(request.ReferenceImages) + len(request.SourceImages),
		"reference_videos": len(request.ReferenceVideos), "start_frame": request.StartFrame != nil,
		"end_frame": request.EndFrame != nil, "reference_audios": len(request.AudioReferences()),
	}
}

func noteAudioRequest(r *http.Request, request domain.AudioRequest) {
	metadata := requestMetadata(r)
	if metadata == nil {
		return
	}
	metadata.Kind = "audio"
	metadata.Model = request.Model
	metadata.PromptChars = utf8.RuneCountInString(request.Prompt)
	metadata.Parameters = map[string]any{
		"n": request.N, "duration": request.Duration, "duration_minutes": request.DurationMinutes,
		"voice": request.Voice, "language": request.Language, "loop": request.Loop,
		"force_instrumental": request.ForceInstrumental,
	}
}

func noteRequestEstimate(r *http.Request, tokens int64) {
	if metadata := requestMetadata(r); metadata != nil {
		value := tokens
		metadata.EstimatedTokens = &value
	}
}

func noteRequestTask(r *http.Request, task domain.Task) {
	metadata := requestMetadata(r)
	if metadata == nil || task.ID == uuid.Nil {
		return
	}
	taskID := task.ID
	metadata.TaskID = &taskID
	if task.AccountID != nil {
		accountID := *task.AccountID
		metadata.AccountID = &accountID
	}
}
