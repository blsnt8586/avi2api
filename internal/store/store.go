package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"time"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrAccountCapacityInUse = errors.New("account capacity is in use")
	ErrAccountInUse         = errors.New("account is in use")
)

type AccountConfigPatch struct {
	Name               *string
	Email              *string
	ProxyURL           *string
	ImageConcurrency   *int
	QueueCapacity      *int
	RoutingRole        *string
	ProtectedTokens    *int64
	VideoReservedSlots *int
	BrowserWorkerGroup *string
}

type AccountCapacityInUseError struct {
	ExecutingTasks       int
	QueuedTasks          int
	RequestedConcurrency int
	RequestedQueue       int
}

type AccountInUseError struct {
	ActiveTasks      int `json:"active_tasks"`
	HeldReservations int `json:"held_reservations"`
}

type AccountPage struct {
	Data     []domain.Account `json:"data"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type AccountPageFilter struct {
	Search     string
	Status     string
	Role       string
	ProviderID string
}

type TaskPageFilter struct {
	Search      string
	Status      string
	Kind        string
	Model       string
	ProviderID  string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

type AuditLogPageFilter struct {
	Search      string
	Action      string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

type AccountOverview struct {
	TotalAccounts  int `json:"accounts"`
	ActiveAccounts int `json:"active_accounts"`
}

type ProviderOverview struct {
	ProviderID            string         `json:"provider_id"`
	DisplayName           string         `json:"display_name"`
	Enabled               bool           `json:"enabled"`
	CreditUnit            string         `json:"credit_unit"`
	Capabilities          []string       `json:"capabilities"`
	Accounts              int            `json:"accounts"`
	ActiveAccounts        int            `json:"active_accounts"`
	AttentionAccounts     int            `json:"attention_accounts"`
	TotalCredits          int64          `json:"total_credits"`
	ReservedCredits       int64          `json:"reserved_credits"`
	AvailableCredits      int64          `json:"available_credits"`
	ExecutionSlots        int            `json:"execution_slots"`
	QueueSlots            int            `json:"queue_slots"`
	ExecutingTasks        int            `json:"executing_tasks"`
	QueuedTasks           int            `json:"queued_tasks"`
	FailedLastHour        int            `json:"failed_last_hour"`
	SubmissionUncertain   int            `json:"submission_uncertain"`
	TaskTotal             int            `json:"task_total"`
	TaskCounts            map[string]int `json:"task_counts"`
	VideoProtectedCredits int64          `json:"video_protected_credits,omitempty"`
	VideoReady720P15      int            `json:"video_ready_720p_15s,omitempty"`
	VideoReady1080P8      int            `json:"video_ready_1080p_8s,omitempty"`
	VideoReady1080P10     int            `json:"video_ready_1080p_10s,omitempty"`
}

type TaskOverview struct {
	Counts         map[string]int `json:"task_counts"`
	Total          int            `json:"task_total"`
	FailedLastHour int            `json:"failed_last_hour"`
}

func (e *AccountCapacityInUseError) Error() string {
	return fmt.Sprintf("account currently has %d executing and %d queued tasks", e.ExecutingTasks, e.QueuedTasks)
}

func (e *AccountCapacityInUseError) Unwrap() error { return ErrAccountCapacityInUse }

func (e *AccountInUseError) Error() string {
	return fmt.Sprintf("account currently has %d unfinished tasks and %d held reservations", e.ActiveTasks, e.HeldReservations)
}

func (e *AccountInUseError) Unwrap() error { return ErrAccountInUse }

type SchedulingPolicy struct {
	QueueTimeout           time.Duration
	APIKeyQueueMultiplier  int
	UncertainAccountLimit  int
	UncertainProxyLimit    int
	UncertainProviderLimit int
}

type Store struct {
	DB               *pgxpool.Pool
	SchedulingPolicy SchedulingPolicy
}

func defaultSchedulingPolicy() SchedulingPolicy {
	return SchedulingPolicy{
		QueueTimeout: 30 * time.Minute, APIKeyQueueMultiplier: 2,
		UncertainAccountLimit: 1, UncertainProxyLimit: 3, UncertainProviderLimit: 5,
	}
}

func (s *Store) ConfigureScheduling(policy SchedulingPolicy) {
	if policy.QueueTimeout <= 0 {
		policy.QueueTimeout = 30 * time.Minute
	}
	if policy.APIKeyQueueMultiplier < 1 {
		policy.APIKeyQueueMultiplier = 2
	}
	if policy.UncertainAccountLimit < 1 {
		policy.UncertainAccountLimit = 1
	}
	if policy.UncertainProxyLimit < policy.UncertainAccountLimit {
		policy.UncertainProxyLimit = 3
	}
	if policy.UncertainProviderLimit < policy.UncertainProxyLimit {
		policy.UncertainProviderLimit = 5
	}
	s.SchedulingPolicy = policy
}

func New(ctx context.Context, url string) (*Store, error) {
	return NewWithMaxConns(ctx, url, 0)
}

func NewWithMaxConns(ctx context.Context, url string, maxConns int32) (*Store, error) {
	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		poolConfig.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{DB: pool, SchedulingPolicy: defaultSchedulingPolicy()}, nil
}

func (s *Store) Close() { s.DB.Close() }

func (s *Store) GetAdminCredential(ctx context.Context, username string) ([]byte, []byte, error) {
	var salt, hash []byte
	err := s.DB.QueryRow(ctx, `SELECT password_salt,password_hash FROM admin_credentials WHERE username=$1`, username).Scan(&salt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return salt, hash, err
}

func (s *Store) SetAdminCredential(ctx context.Context, username string, salt, hash []byte) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO admin_credentials(username,password_salt,password_hash,updated_at) VALUES($1,$2,$3,now()) ON CONFLICT(username) DO UPDATE SET password_salt=excluded.password_salt,password_hash=excluded.password_hash,updated_at=now()`, username, salt, hash)
	return err
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.DB.Ping(ctx)
}
