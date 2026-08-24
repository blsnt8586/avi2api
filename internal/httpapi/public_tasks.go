package httpapi

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"time"
)

type publicTaskResponse struct {
	ID              uuid.UUID       `json:"id"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Progress        int             `json:"progress"`
	QueuePosition   *int            `json:"queue_position,omitempty"`
	Model           string          `json:"model"`
	Prompt          string          `json:"prompt"`
	Result          json.RawMessage `json:"result,omitempty"`
	ErrorCode       string          `json:"error_code,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	ErrorDetails    json.RawMessage `json:"error_details,omitempty"`
	RetryCount      int             `json:"retry_count"`
	CancelRequested bool            `json:"cancel_requested"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
}

func newPublicTaskResponse(task domain.Task) publicTaskResponse {
	return publicTaskResponse{
		ID: task.ID, Kind: task.Kind, Status: publicTaskStatus(task.Status), Progress: task.Progress,
		QueuePosition: task.QueuePosition, Model: publicMediaModelID(requestProvider(task.ProviderID), task.Model), Prompt: task.Prompt,
		Result: task.Result, ErrorCode: task.ErrorCode,
		ErrorMessage: task.ErrorMessage, ErrorDetails: sanitizePublicErrorDetails(task.ErrorDetails),
		RetryCount: task.RetryCount, CancelRequested: task.CancelRequested,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
		StartedAt: task.StartedAt, CompletedAt: task.CompletedAt,
	}
}

func publicTaskStatus(status string) string {
	switch status {
	case domain.TaskQueued:
		return "queued"
	case domain.TaskSucceeded:
		return "succeeded"
	case domain.TaskFailed:
		return "failed"
	case domain.TaskCancelled:
		return "cancelled"
	case domain.TaskReserving, domain.TaskUploading, domain.TaskSubmitted, domain.TaskPolling, domain.TaskSubmissionUncertain:
		return "processing"
	default:
		return "processing"
	}
}

func sanitizePublicErrorDetails(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var details map[string]json.RawMessage
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil
	}
	allowed := map[string]bool{
		"media_type": true, "upstream_status": true, "provider_error_code": true,
		"nsfw": true, "notes": true, "prompt_moderations": true,
	}
	public := make(map[string]json.RawMessage)
	for key, value := range details {
		if allowed[key] {
			public[key] = value
		}
	}
	if len(public) == 0 {
		return nil
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		return nil
	}
	return encoded
}
