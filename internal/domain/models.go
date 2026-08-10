package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	TaskQueued              = "queued"
	TaskReserving           = "reserving"
	TaskUploading           = "uploading"
	TaskSubmitted           = "submitted"
	TaskPolling             = "polling"
	TaskSucceeded           = "succeeded"
	TaskFailed              = "failed"
	TaskCancelled           = "cancelled"
	TaskSubmissionUncertain = "submission_uncertain"
)

type Account struct {
	ID                             uuid.UUID  `json:"id"`
	ProviderID                     string     `json:"provider_id"`
	Name                           string     `json:"name"`
	Email                          string     `json:"email"`
	CookieCiphertext               string     `json:"-"`
	AccessTokenCiphertext          string     `json:"-"`
	AccessTokenExpiresAt           *time.Time `json:"access_token_expires_at,omitempty"`
	HasuraUserID                   string     `json:"hasura_user_id"`
	CognitoSub                     string     `json:"cognito_sub"`
	TeamID                         string     `json:"team_id"`
	Plan                           string     `json:"plan"`
	SubscriptionTokens             int64      `json:"subscription_tokens"`
	RolloverTokens                 int64      `json:"rollover_tokens"`
	PaidTokens                     int64      `json:"paid_tokens"`
	ProxyURL                       string     `json:"proxy_url"`
	UserAgent                      string     `json:"user_agent"`
	ImageConcurrency               int        `json:"image_concurrency"`
	QueueCapacity                  int        `json:"queue_capacity"`
	RoutingRole                    string     `json:"routing_role"`
	ProtectedTokens                int64      `json:"protected_tokens"`
	VideoReservedSlots             int        `json:"video_reserved_slots"`
	Status                         string     `json:"status"`
	CooldownUntil                  *time.Time `json:"cooldown_until,omitempty"`
	LastError                      string     `json:"last_error,omitempty"`
	LastCheckedAt                  *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
	ReservedTokens                 int64      `json:"reserved_tokens"`
	AvailableTokens                int64      `json:"available_tokens"`
	ActiveReservations             int        `json:"active_reservations"`
	QueuedTasks                    int        `json:"queued_tasks"`
	SessionRefreshEnabled          bool       `json:"session_refresh_enabled"`
	BrowserProfileKey              string     `json:"browser_profile_key"`
	BrowserWorkerGroup             string     `json:"browser_worker_group"`
	HasLoginCredentials            bool       `json:"has_login_credentials"`
	SessionRefreshJitterSeconds    int        `json:"session_refresh_jitter_seconds"`
	SessionRefreshNotBefore        *time.Time `json:"session_refresh_not_before,omitempty"`
	SessionRefreshLastAt           *time.Time `json:"session_refresh_last_at,omitempty"`
	SessionRefreshLastMethod       string     `json:"session_refresh_last_method,omitempty"`
	SessionRefreshLastDurationMS   *int       `json:"session_refresh_last_duration_ms,omitempty"`
	SessionRefreshFailures         int        `json:"session_refresh_failures"`
	SessionRefreshJobStage         string     `json:"session_refresh_job_stage,omitempty"`
	SessionRefreshJobStatus        string     `json:"session_refresh_job_status,omitempty"`
	SessionRefreshJobNextAttemptAt *time.Time `json:"session_refresh_job_next_attempt_at,omitempty"`
}

func (a Account) TotalTokens() int64 { return a.SubscriptionTokens + a.RolloverTokens + a.PaidTokens }

type APIKey struct {
	ID               uuid.UUID
	Name             string
	Prefix           string
	Enabled          bool
	ConcurrencyLimit int
	AllowedModels    []string
}

type APIKeyRecord struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	Prefix           string     `json:"prefix"`
	Enabled          bool       `json:"enabled"`
	ConcurrencyLimit int        `json:"concurrency_limit"`
	AllowedModels    []string   `json:"allowed_models"`
	RequestCount     int64      `json:"request_count"`
	CreatedAt        time.Time  `json:"created_at"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type AuditLog struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

type TaskEvent struct {
	ID        int64     `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type APIRequestLog struct {
	ID              int64           `json:"id"`
	RequestID       string          `json:"request_id"`
	APIKeyID        *uuid.UUID      `json:"api_key_id,omitempty"`
	APIKeyPrefix    string          `json:"api_key_prefix"`
	AccountID       *uuid.UUID      `json:"account_id,omitempty"`
	AccountName     string          `json:"account_name,omitempty"`
	TaskID          *uuid.UUID      `json:"task_id,omitempty"`
	Method          string          `json:"method"`
	Path            string          `json:"path"`
	Kind            string          `json:"kind,omitempty"`
	Model           string          `json:"model,omitempty"`
	Parameters      json.RawMessage `json:"parameters"`
	PromptChars     int             `json:"prompt_chars"`
	EstimatedTokens *int64          `json:"estimated_tokens,omitempty"`
	Status          int             `json:"status"`
	ErrorCode       string          `json:"error_code,omitempty"`
	DurationMS      int64           `json:"duration_ms"`
	ClientIP        string          `json:"client_ip"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ModelConfig struct {
	ID            string          `json:"id"`
	UpstreamModel string          `json:"upstream_model"`
	DisplayName   string          `json:"display_name"`
	Capabilities  []string        `json:"capabilities"`
	Defaults      json.RawMessage `json:"defaults"`
}

type ModelCostRecord struct {
	Model                   string    `json:"model"`
	Kind                    string    `json:"kind"`
	Size                    string    `json:"size,omitempty"`
	Quality                 string    `json:"quality,omitempty"`
	Resolution              string    `json:"resolution,omitempty"`
	Duration                string    `json:"duration,omitempty"`
	Samples                 int       `json:"samples"`
	Average                 float64   `json:"average"`
	Minimum                 int64     `json:"minimum"`
	Maximum                 int64     `json:"maximum"`
	UpstreamReportedSamples int       `json:"upstream_reported_samples"`
	UpstreamReportedAverage *float64  `json:"upstream_reported_average,omitempty"`
	UpstreamReportedMinimum *float64  `json:"upstream_reported_minimum,omitempty"`
	UpstreamReportedMaximum *float64  `json:"upstream_reported_maximum,omitempty"`
	LastUsedAt              time.Time `json:"last_used_at"`
}

type ModelCostRule struct {
	ID           int64     `json:"id"`
	ProviderID   string    `json:"provider_id"`
	Kind         string    `json:"kind"`
	Model        string    `json:"model"`
	Size         string    `json:"size"`
	Quality      string    `json:"quality"`
	Resolution   string    `json:"resolution"`
	Duration     int       `json:"duration"`
	UnitTokens   int64     `json:"unit_tokens"`
	Enabled      bool      `json:"enabled"`
	PriceVersion string    `json:"price_version"`
	Source       string    `json:"source"`
	Drifted      bool      `json:"drifted"`
	DriftReason  string    `json:"drift_reason"`
	VerifiedAt   time.Time `json:"verified_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AccountReservation struct {
	TaskID                    uuid.UUID  `json:"task_id"`
	AccountID                 uuid.UUID  `json:"account_id"`
	EstimatedTokens           int64      `json:"estimated_tokens"`
	SettledTokens             *int64     `json:"settled_tokens,omitempty"`
	ReconciledSnapshotVersion *int64     `json:"reconciled_snapshot_version,omitempty"`
	State                     string     `json:"state"`
	ExpiresAt                 time.Time  `json:"expires_at"`
	ReleaseReason             string     `json:"release_reason"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	ReleasedAt                *time.Time `json:"released_at,omitempty"`
}

type SessionRefreshJob struct {
	ID             uuid.UUID  `json:"id"`
	AccountID      uuid.UUID  `json:"account_id"`
	Stage          string     `json:"stage"`
	Status         string     `json:"status"`
	Priority       int        `json:"priority"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	AttemptCount   int        `json:"attempt_count"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseToken     *uuid.UUID `json:"lease_token,omitempty"`
	LeaseStartedAt *time.Time `json:"lease_started_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type AdminTask struct {
	Task
	ProviderID               string          `json:"provider_id"`
	UpstreamRequest          json.RawMessage `json:"upstream_request,omitempty"`
	EstimatedTokens          int64           `json:"estimated_tokens"`
	SettledTokens            *int64          `json:"settled_tokens,omitempty"`
	UpstreamReportedCost     *float64        `json:"upstream_reported_cost,omitempty"`
	ReservationState         string          `json:"reservation_state"`
	ReservationReleaseReason string          `json:"reservation_release_reason"`
	Events                   []TaskEvent     `json:"events,omitempty"`
}

type ImageRequest struct {
	Model             string        `json:"model"`
	Prompt            string        `json:"prompt"`
	N                 int           `json:"n,omitempty"`
	Size              string        `json:"size,omitempty"`
	ResponseFormat    string        `json:"response_format,omitempty"`
	Quality           string        `json:"quality,omitempty"`
	OutputFormat      string        `json:"output_format,omitempty"`
	OutputCompression *int          `json:"output_compression,omitempty"`
	Background        string        `json:"background,omitempty"`
	Moderation        string        `json:"moderation,omitempty"`
	Public            *bool         `json:"public,omitempty"`
	StyleIDs          []string      `json:"style_ids,omitempty"`
	ReferenceIDs      []string      `json:"reference_ids,omitempty"`
	SourceImage       *SourceImage  `json:"source_image,omitempty"`
	SourceImages      []SourceImage `json:"source_images,omitempty"`
	ReferenceStrength string        `json:"reference_strength,omitempty"`
	ImageStrength     *float64      `json:"image_strength,omitempty"`
}

type SourceImage struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type SourceMedia struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

type ImageResult struct {
	Created                   int64         `json:"created"`
	Data                      []ImageOutput `json:"data"`
	NSFW                      bool          `json:"nsfw,omitempty"`
	ModerationClassifications []string      `json:"moderation_classifications,omitempty"`
}

type VideoRequest struct {
	Model             string        `json:"model"`
	Prompt            string        `json:"prompt"`
	Duration          int           `json:"duration,omitempty"`
	Size              string        `json:"size,omitempty"`
	Resolution        string        `json:"resolution,omitempty"`
	Public            *bool         `json:"public,omitempty"`
	SourceImage       *SourceImage  `json:"source_image,omitempty"`
	SourceImages      []SourceImage `json:"source_images,omitempty"`
	ReferenceStrength string        `json:"reference_strength,omitempty"`
	ReferenceImages   []SourceMedia `json:"reference_images,omitempty"`
	StartFrame        *SourceMedia  `json:"start_frame,omitempty"`
	EndFrame          *SourceMedia  `json:"end_frame,omitempty"`
	ReferenceVideos   []SourceMedia `json:"reference_videos,omitempty"`
	ReferenceAudio    *SourceMedia  `json:"reference_audio,omitempty"`
	ReferenceAudios   []SourceMedia `json:"reference_audios,omitempty"`
	GenerateAudio     *bool         `json:"generate_audio,omitempty"`
}

// AudioReferences preserves compatibility with tasks stored before repeated
// audio references were introduced.
func (r VideoRequest) AudioReferences() []SourceMedia {
	references := make([]SourceMedia, 0, len(r.ReferenceAudios)+1)
	if r.ReferenceAudio != nil {
		references = append(references, *r.ReferenceAudio)
	}
	return append(references, r.ReferenceAudios...)
}

type VideoResult struct {
	Created                   int64         `json:"created"`
	Data                      []VideoOutput `json:"data"`
	NSFW                      bool          `json:"nsfw,omitempty"`
	ModerationClassifications []string      `json:"moderation_classifications,omitempty"`
}

type VideoOutput struct {
	ID                        string   `json:"id,omitempty"`
	URL                       string   `json:"url"`
	MediaType                 string   `json:"media_type"`
	Duration                  int      `json:"duration,omitempty"`
	Width                     int      `json:"width,omitempty"`
	Height                    int      `json:"height,omitempty"`
	NSFW                      bool     `json:"nsfw,omitempty"`
	ModerationClassifications []string `json:"moderation_classifications,omitempty"`
}

type AudioRequest struct {
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
	Public            *bool    `json:"public,omitempty"`
}

type AudioResult struct {
	Created int64         `json:"created"`
	Data    []AudioOutput `json:"data"`
}

type AudioOutput struct {
	ID        string `json:"id,omitempty"`
	URL       string `json:"url"`
	MediaType string `json:"media_type"`
	Duration  int    `json:"duration,omitempty"`
}

type ImageOutput struct {
	URL                       string   `json:"url,omitempty"`
	B64JSON                   string   `json:"b64_json,omitempty"`
	RevisedPrompt             string   `json:"revised_prompt,omitempty"`
	ID                        string   `json:"id,omitempty"`
	NSFW                      bool     `json:"nsfw,omitempty"`
	ModerationClassifications []string `json:"moderation_classifications,omitempty"`
}

type Task struct {
	ID                   uuid.UUID       `json:"id"`
	ProviderID           string          `json:"-"`
	APIKeyID             *uuid.UUID      `json:"-"`
	AccountID            *uuid.UUID      `json:"account_id,omitempty"`
	Kind                 string          `json:"kind"`
	Status               string          `json:"status"`
	Progress             int             `json:"progress"`
	QueuePosition        *int            `json:"queue_position,omitempty"`
	Model                string          `json:"model"`
	Prompt               string          `json:"prompt"`
	Request              json.RawMessage `json:"-"`
	UpstreamRequest      json.RawMessage `json:"-"`
	GenerationID         string          `json:"generation_id,omitempty"`
	Result               json.RawMessage `json:"result,omitempty"`
	ErrorCode            string          `json:"error_code,omitempty"`
	ErrorMessage         string          `json:"error_message,omitempty"`
	ErrorDetails         json.RawMessage `json:"error_details,omitempty"`
	RetryCount           int             `json:"retry_count"`
	TokensBefore         *int64          `json:"tokens_before,omitempty"`
	TokensAfter          *int64          `json:"tokens_after,omitempty"`
	CancelRequested      bool            `json:"cancel_requested"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	StartedAt            *time.Time      `json:"started_at,omitempty"`
	CompletedAt          *time.Time      `json:"completed_at,omitempty"`
	QueueDeadlineAt      *time.Time      `json:"queue_deadline_at,omitempty"`
	UpstreamDeadlineAt   *time.Time      `json:"upstream_deadline_at,omitempty"`
	LastUpstreamStatusAt *time.Time      `json:"last_upstream_status_at,omitempty"`
	UnknownStatusCount   int             `json:"unknown_status_count"`
	ReconciliationReason string          `json:"reconciliation_reason,omitempty"`
}

type Provider struct {
	ID           string          `json:"id"`
	DisplayName  string          `json:"display_name"`
	Enabled      bool            `json:"enabled"`
	AuthType     string          `json:"auth_type"`
	CreditUnit   string          `json:"credit_unit"`
	Priority     int             `json:"priority"`
	Capabilities []string        `json:"capabilities"`
	Settings     json.RawMessage `json:"settings,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (t Task) Terminal() bool {
	return t.Status == TaskSucceeded || t.Status == TaskFailed || t.Status == TaskCancelled || t.Status == TaskSubmissionUncertain
}
