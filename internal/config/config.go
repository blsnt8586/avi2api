package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Mode                       string
	HTTPAddr                   string
	DatabaseURL                string
	DatabaseMaxConns           int
	RedisAddr                  string
	RedisPassword              string
	RedisDB                    int
	MasterKey                  []byte
	AdminUsername              string
	AdminPassword              string
	SessionSyncToken           string
	PublicBaseURL              string
	SchemaVersion              string
	SyncTimeout                time.Duration
	PollInterval               time.Duration
	TaskTimeout                time.Duration
	TaskLease                  time.Duration
	QueueTimeout               time.Duration
	BalanceReconcileDelay      time.Duration
	BalanceRefreshWorkers      int
	BalanceRefreshPerProxy     int
	ProxyControlMinInterval    time.Duration
	ProxyControlLease          time.Duration
	TaskDispatcherConcurrency  int
	APIKeyQueueMultiplier      int
	UncertainAccountLimit      int
	UncertainProxyLimit        int
	UncertainProviderLimit     int
	SharedCircuitWindow        time.Duration
	SharedCircuitCooldown      time.Duration
	ProviderCircuitFailures    int
	ProxyCircuitFailures       int
	GenerationInFlight         int
	MultipartInFlight          int
	SyncInFlight               int
	RequestLogWorkers          int
	RequestLogBuffer           int
	SessionRefreshAhead        time.Duration
	SessionRefreshScanInterval time.Duration
	SessionRefreshMinFresh     time.Duration
	SessionRefreshLease        time.Duration
	SessionRefreshBrowserLease time.Duration
	SessionRefreshIdlePoll     time.Duration
	SessionRefreshWorkers      int
	SessionRefreshBatch        int
	SessionWorkerAllowRemote   bool
	MaxImageBytes              int64
	MaxVideoBytes              int64
	MaxAudioBytes              int64
	MaxMultipartBytes          int64
	TaskAssetDir               string
	HistoryCleanupInterval     time.Duration
	HistoryCleanupBatch        int
	TaskEventRetention         time.Duration
	OutboxRetention            time.Duration
	SessionJobRetention        time.Duration
	APIRequestLogRetention     time.Duration
	AuditLogRetention          time.Duration
	ReconciliationRetention    time.Duration
	LogLevel                   string
	CookieSecure               bool
	RateLimitPerMinute         int
	RateLimitPerHour           int
	DailyImageLimit            int
	IPRateLimitPerMinute       int
	AccountSubmitInterval      time.Duration
	CircuitFailures            int
	CircuitCooldown            time.Duration
	Upstream429Cooldown        time.Duration
}

func Load() (Config, error) {
	c := Config{
		Mode:             env("LEO_MODE", "all"),
		HTTPAddr:         env("LEO_HTTP_ADDR", ":8080"),
		DatabaseURL:      env("LEO_DATABASE_URL", "postgres://leonardo:leonardo@localhost:5432/leonardo?sslmode=disable"),
		RedisAddr:        env("LEO_REDIS_ADDR", "localhost:6379"),
		RedisPassword:    os.Getenv("LEO_REDIS_PASSWORD"),
		AdminUsername:    env("LEO_ADMIN_USERNAME", "admin"),
		AdminPassword:    os.Getenv("LEO_ADMIN_PASSWORD"),
		SessionSyncToken: os.Getenv("LEO_SESSION_SYNC_TOKEN"),
		PublicBaseURL:    strings.TrimRight(env("LEO_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		SchemaVersion:    env("LEO_SCHEMA_VERSION", "1.255.2"),
		TaskAssetDir:     env("LEO_TASK_ASSET_DIR", "./data/task-assets"),
		LogLevel:         env("LEO_LOG_LEVEL", "info"),
	}
	var err error
	if c.RedisDB, err = intEnv("LEO_REDIS_DB", 0); err != nil {
		return Config{}, err
	}
	if c.DatabaseMaxConns, err = intEnv("LEO_DATABASE_MAX_CONNS", 64); err != nil {
		return Config{}, err
	}
	if c.SyncTimeout, err = durationEnv("LEO_SYNC_TIMEOUT", 180*time.Second); err != nil {
		return Config{}, err
	}
	if c.PollInterval, err = durationEnv("LEO_POLL_INTERVAL", 3*time.Second); err != nil {
		return Config{}, err
	}
	if c.TaskTimeout, err = durationEnv("LEO_TASK_TIMEOUT", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if c.TaskLease, err = durationEnv("LEO_TASK_LEASE", 60*time.Second); err != nil {
		return Config{}, err
	}
	if c.QueueTimeout, err = durationEnv("LEO_QUEUE_TIMEOUT", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if c.BalanceReconcileDelay, err = durationEnv("LEO_BALANCE_RECONCILE_DELAY", 2*time.Second); err != nil {
		return Config{}, err
	}
	if c.BalanceRefreshWorkers, err = intEnv("LEO_BALANCE_REFRESH_WORKERS", 32); err != nil {
		return Config{}, err
	}
	if c.BalanceRefreshPerProxy, err = intEnv("LEO_BALANCE_REFRESH_PER_PROXY", 1); err != nil {
		return Config{}, err
	}
	if c.ProxyControlMinInterval, err = durationEnv("LEO_PROXY_CONTROL_MIN_INTERVAL", 20*time.Second); err != nil {
		return Config{}, err
	}
	if c.ProxyControlLease, err = durationEnv("LEO_PROXY_CONTROL_LEASE", 90*time.Second); err != nil {
		return Config{}, err
	}
	if c.TaskDispatcherConcurrency, err = intEnv("LEO_TASK_DISPATCHER_CONCURRENCY", 128); err != nil {
		return Config{}, err
	}
	if c.APIKeyQueueMultiplier, err = intEnv("LEO_API_KEY_QUEUE_MULTIPLIER", 2); err != nil {
		return Config{}, err
	}
	if c.UncertainAccountLimit, err = intEnv("LEO_UNCERTAIN_ACCOUNT_LIMIT", 1); err != nil {
		return Config{}, err
	}
	if c.UncertainProxyLimit, err = intEnv("LEO_UNCERTAIN_PROXY_LIMIT", 3); err != nil {
		return Config{}, err
	}
	if c.UncertainProviderLimit, err = intEnv("LEO_UNCERTAIN_PROVIDER_LIMIT", 5); err != nil {
		return Config{}, err
	}
	if c.SharedCircuitWindow, err = durationEnv("LEO_SHARED_CIRCUIT_WINDOW", 30*time.Second); err != nil {
		return Config{}, err
	}
	if c.SharedCircuitCooldown, err = durationEnv("LEO_SHARED_CIRCUIT_COOLDOWN", time.Minute); err != nil {
		return Config{}, err
	}
	if c.ProviderCircuitFailures, err = intEnv("LEO_PROVIDER_CIRCUIT_FAILURES", 5); err != nil {
		return Config{}, err
	}
	if c.ProxyCircuitFailures, err = intEnv("LEO_PROXY_CIRCUIT_FAILURES", 3); err != nil {
		return Config{}, err
	}
	if c.GenerationInFlight, err = intEnv("LEO_GENERATION_INFLIGHT", 200); err != nil {
		return Config{}, err
	}
	if c.MultipartInFlight, err = intEnv("LEO_MULTIPART_INFLIGHT", 8); err != nil {
		return Config{}, err
	}
	if c.SyncInFlight, err = intEnv("LEO_SYNC_INFLIGHT", 10); err != nil {
		return Config{}, err
	}
	if c.RequestLogWorkers, err = intEnv("LEO_REQUEST_LOG_WORKERS", 4); err != nil {
		return Config{}, err
	}
	if c.RequestLogBuffer, err = intEnv("LEO_REQUEST_LOG_BUFFER", 4096); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshAhead, err = durationEnv("LEO_SESSION_REFRESH_AHEAD", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshScanInterval, err = durationEnv("LEO_SESSION_REFRESH_SCAN_INTERVAL", time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshMinFresh, err = durationEnv("LEO_SESSION_REFRESH_MIN_FRESH", 20*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshLease, err = durationEnv("LEO_SESSION_REFRESH_LEASE", 2*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshBrowserLease, err = durationEnv("LEO_SESSION_BROWSER_LEASE", 8*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshIdlePoll, err = durationEnv("LEO_SESSION_REFRESH_IDLE_POLL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshWorkers, err = intEnv("LEO_SESSION_REFRESH_WORKERS", 32); err != nil {
		return Config{}, err
	}
	if c.SessionRefreshBatch, err = intEnv("LEO_SESSION_REFRESH_BATCH", 1000); err != nil {
		return Config{}, err
	}
	if c.SessionWorkerAllowRemote, err = boolEnv("LEO_SESSION_WORKER_ALLOW_REMOTE", false); err != nil {
		return Config{}, err
	}
	if c.MaxImageBytes, err = int64Env("LEO_MAX_IMAGE_BYTES", 25<<20); err != nil {
		return Config{}, err
	}
	if c.MaxVideoBytes, err = int64Env("LEO_MAX_VIDEO_BYTES", 200<<20); err != nil {
		return Config{}, err
	}
	if c.MaxAudioBytes, err = int64Env("LEO_MAX_AUDIO_BYTES", 50<<20); err != nil {
		return Config{}, err
	}
	if c.MaxMultipartBytes, err = int64Env("LEO_MAX_MULTIPART_BYTES", 400<<20); err != nil {
		return Config{}, err
	}
	if c.HistoryCleanupInterval, err = durationEnv("LEO_HISTORY_CLEANUP_INTERVAL", time.Hour); err != nil {
		return Config{}, err
	}
	if c.HistoryCleanupBatch, err = intEnv("LEO_HISTORY_CLEANUP_BATCH", 10000); err != nil {
		return Config{}, err
	}
	if c.TaskEventRetention, err = durationEnv("LEO_TASK_EVENT_RETENTION", 90*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.OutboxRetention, err = durationEnv("LEO_OUTBOX_RETENTION", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.SessionJobRetention, err = durationEnv("LEO_SESSION_JOB_RETENTION", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.APIRequestLogRetention, err = durationEnv("LEO_API_REQUEST_LOG_RETENTION", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.AuditLogRetention, err = durationEnv("LEO_AUDIT_LOG_RETENTION", 365*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.ReconciliationRetention, err = durationEnv("LEO_RECONCILIATION_RETENTION", 730*24*time.Hour); err != nil {
		return Config{}, err
	}
	if c.CookieSecure, err = boolEnv("LEO_COOKIE_SECURE", true); err != nil {
		return Config{}, err
	}
	if c.RateLimitPerMinute, err = intEnv("LEO_RATE_LIMIT_PER_MINUTE", 60); err != nil {
		return Config{}, err
	}
	if c.RateLimitPerHour, err = intEnv("LEO_RATE_LIMIT_PER_HOUR", 1000); err != nil {
		return Config{}, err
	}
	if c.DailyImageLimit, err = intEnv("LEO_DAILY_IMAGE_LIMIT", 100); err != nil {
		return Config{}, err
	}
	if c.IPRateLimitPerMinute, err = intEnv("LEO_IP_RATE_LIMIT_PER_MINUTE", 120); err != nil {
		return Config{}, err
	}
	if c.AccountSubmitInterval, err = durationEnv("LEO_ACCOUNT_SUBMIT_INTERVAL", 8*time.Second); err != nil {
		return Config{}, err
	}
	if c.CircuitFailures, err = intEnv("LEO_CIRCUIT_FAILURES", 3); err != nil {
		return Config{}, err
	}
	if c.CircuitCooldown, err = durationEnv("LEO_CIRCUIT_COOLDOWN", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if c.Upstream429Cooldown, err = durationEnv("LEO_UPSTREAM_429_COOLDOWN", 2*time.Minute); err != nil {
		return Config{}, err
	}
	keyText := os.Getenv("LEO_MASTER_KEY")
	if keyText == "" {
		return Config{}, errors.New("LEO_MASTER_KEY is required")
	}
	c.MasterKey, err = base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(c.MasterKey) != 32 {
		return Config{}, errors.New("LEO_MASTER_KEY must be base64 for exactly 32 bytes")
	}
	if c.AdminPassword == "" {
		return Config{}, errors.New("LEO_ADMIN_PASSWORD is required")
	}
	if c.Mode != "api" && c.Mode != "worker" && c.Mode != "all" {
		return Config{}, fmt.Errorf("invalid LEO_MODE %q", c.Mode)
	}
	if c.CircuitFailures < 1 {
		return Config{}, errors.New("LEO_CIRCUIT_FAILURES must be at least 1")
	}
	if c.SessionRefreshWorkers < 1 || c.SessionRefreshWorkers > 256 {
		return Config{}, errors.New("LEO_SESSION_REFRESH_WORKERS must be between 1 and 256")
	}
	if c.BalanceRefreshWorkers < 1 || c.BalanceRefreshWorkers > 256 {
		return Config{}, errors.New("LEO_BALANCE_REFRESH_WORKERS must be between 1 and 256")
	}
	if c.BalanceRefreshPerProxy < 1 || c.BalanceRefreshPerProxy > 32 {
		return Config{}, errors.New("LEO_BALANCE_REFRESH_PER_PROXY must be between 1 and 32")
	}
	if c.ProxyControlMinInterval < time.Second || c.ProxyControlMinInterval > 10*time.Minute {
		return Config{}, errors.New("LEO_PROXY_CONTROL_MIN_INTERVAL must be between 1s and 10m")
	}
	if c.ProxyControlLease < c.ProxyControlMinInterval || c.ProxyControlLease > 30*time.Minute {
		return Config{}, errors.New("LEO_PROXY_CONTROL_LEASE must be at least the control interval and at most 30m")
	}
	if c.TaskDispatcherConcurrency < 1 || c.TaskDispatcherConcurrency > 10000 {
		return Config{}, errors.New("LEO_TASK_DISPATCHER_CONCURRENCY must be between 1 and 10000")
	}
	if c.TaskLease < 30*time.Second || c.TaskLease > 5*time.Minute {
		return Config{}, errors.New("LEO_TASK_LEASE must be between 30s and 5m")
	}
	if c.QueueTimeout < time.Minute || c.QueueTimeout > 24*time.Hour {
		return Config{}, errors.New("LEO_QUEUE_TIMEOUT must be between 1m and 24h")
	}
	if c.APIKeyQueueMultiplier < 1 || c.APIKeyQueueMultiplier > 20 {
		return Config{}, errors.New("LEO_API_KEY_QUEUE_MULTIPLIER must be between 1 and 20")
	}
	if c.UncertainAccountLimit < 1 || c.UncertainProxyLimit < 1 || c.UncertainProviderLimit < 1 {
		return Config{}, errors.New("submission uncertainty limits must be at least 1")
	}
	if c.UncertainAccountLimit > c.UncertainProxyLimit || c.UncertainProxyLimit > c.UncertainProviderLimit {
		return Config{}, errors.New("submission uncertainty limits must satisfy account <= proxy <= provider")
	}
	if c.ProviderCircuitFailures < 1 || c.ProxyCircuitFailures < 1 || c.SharedCircuitWindow < time.Second || c.SharedCircuitCooldown < time.Second {
		return Config{}, errors.New("shared circuit breaker settings are invalid")
	}
	if c.GenerationInFlight < 1 || c.MultipartInFlight < 1 || c.SyncInFlight < 1 {
		return Config{}, errors.New("HTTP in-flight limits must be at least 1")
	}
	if c.MultipartInFlight > c.GenerationInFlight || c.SyncInFlight > c.GenerationInFlight {
		return Config{}, errors.New("specialized HTTP in-flight limits cannot exceed the generation limit")
	}
	if c.RequestLogWorkers < 1 || c.RequestLogWorkers > 64 || c.RequestLogBuffer < 100 || c.RequestLogBuffer > 100000 {
		return Config{}, errors.New("request log worker or buffer setting is invalid")
	}
	if c.MaxMultipartBytes < c.MaxVideoBytes || c.MaxMultipartBytes > 2<<30 {
		return Config{}, errors.New("LEO_MAX_MULTIPART_BYTES must be at least one video limit and at most 2 GiB")
	}
	if c.DatabaseMaxConns < 4 || c.DatabaseMaxConns > 500 {
		return Config{}, errors.New("LEO_DATABASE_MAX_CONNS must be between 4 and 500")
	}
	if c.Mode == "all" && c.DatabaseMaxConns < 16 {
		return Config{}, errors.New("LEO_DATABASE_MAX_CONNS must be at least 16 in all mode")
	}
	if c.Mode == "worker" && c.DatabaseMaxConns < 8 {
		return Config{}, errors.New("LEO_DATABASE_MAX_CONNS must be at least 8 in worker mode")
	}
	if c.BalanceReconcileDelay < 0 || c.BalanceReconcileDelay > time.Minute {
		return Config{}, errors.New("LEO_BALANCE_RECONCILE_DELAY must be between 0 and 1m")
	}
	if c.SessionRefreshBatch < 1 || c.SessionRefreshBatch > 10000 {
		return Config{}, errors.New("LEO_SESSION_REFRESH_BATCH must be between 1 and 10000")
	}
	if c.HistoryCleanupInterval < 10*time.Minute || c.HistoryCleanupInterval > 24*time.Hour {
		return Config{}, errors.New("LEO_HISTORY_CLEANUP_INTERVAL must be between 10m and 24h")
	}
	if c.HistoryCleanupBatch < 100 || c.HistoryCleanupBatch > 100000 {
		return Config{}, errors.New("LEO_HISTORY_CLEANUP_BATCH must be between 100 and 100000")
	}
	if c.TaskEventRetention < 24*time.Hour || c.OutboxRetention < 24*time.Hour || c.SessionJobRetention < 24*time.Hour || c.APIRequestLogRetention < 24*time.Hour {
		return Config{}, errors.New("task event, outbox, session job, and API request log retention must be at least 24h")
	}
	if c.AuditLogRetention < 7*24*time.Hour || c.ReconciliationRetention < 30*24*time.Hour {
		return Config{}, errors.New("audit retention must be at least 7d and reconciliation retention at least 30d")
	}
	return c, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func intEnv(k string, d int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	return strconv.Atoi(v)
}
func int64Env(k string, d int64) (int64, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
func boolEnv(k string, d bool) (bool, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	return strconv.ParseBool(v)
}
func durationEnv(k string, d time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	return time.ParseDuration(v)
}
