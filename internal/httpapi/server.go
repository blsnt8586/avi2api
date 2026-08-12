package httpapi

import (
	"context"
	"crypto/sha256"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/circuit"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/jobs"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Store           *store.Store
	Redis           *redis.Client
	Queue           *jobs.Client
	Accounts        *accounts.Service
	Assets          *taskassets.Store
	Config          config.Config
	Log             *slog.Logger
	adminHash       []byte
	adminSalt       []byte
	static          http.Handler
	Circuit         circuit.Breaker
	generationSlots chan struct{}
	multipartSlots  chan struct{}
	syncSlots       chan struct{}
	requestLogQueue chan domain.APIRequestLog
}

type contextKey string

const apiKeyContext contextKey = "api_key"

func New(s *store.Store, r *redis.Client, q *jobs.Client, a *accounts.Service, assets *taskassets.Store, c config.Config, log *slog.Logger, static http.Handler) *Server {
	salt := sha256.Sum256([]byte("leonardo2api:" + c.AdminUsername))
	server := &Server{
		Store: s, Redis: r, Queue: q, Accounts: a, Assets: assets, Config: c, Log: log,
		adminSalt: salt[:], adminHash: argon2.IDKey([]byte(c.AdminPassword), salt[:], 1, 64*1024, 4, 32), static: static,
		Circuit:         circuit.Breaker{Redis: r, Window: c.SharedCircuitWindow, Cooldown: c.SharedCircuitCooldown, ProviderFailures: c.ProviderCircuitFailures, ProxyFailures: c.ProxyCircuitFailures},
		generationSlots: make(chan struct{}, maxInt(1, c.GenerationInFlight)),
		multipartSlots:  make(chan struct{}, maxInt(1, c.MultipartInFlight)),
		syncSlots:       make(chan struct{}, maxInt(1, c.SyncInFlight)),
		requestLogQueue: make(chan domain.APIRequestLog, maxInt(100, c.RequestLogBuffer)),
	}
	workers := maxInt(1, c.RequestLogWorkers)
	for i := 0; i < workers; i++ {
		go server.runRequestLogWorker()
	}
	return server
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.securityHeaders, middleware.RequestID, middleware.Recoverer, middleware.Timeout(4*time.Minute), s.accessLog)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	r.Get("/readyz", s.ready)
	r.Get("/openapi.json", openAPISpec)
	r.Get("/api-docs/cost-rules", s.publicCostRules)
	r.Post("/api-docs/image-estimate", s.publicImageEstimate)
	r.Post("/api-docs/image-cost-matrix", s.publicImageCostMatrix)
	r.Post("/api-docs/video-estimate", s.publicVideoEstimate)
	r.Handle("/metrics", promhttp.Handler())
	r.Route("/v1", func(r chi.Router) {
		r.Use(s.apiRequestLog, s.preAuthIPAdmission, s.apiAuth, s.apiKeyAdmission, s.generationBulkhead)
		r.Get("/models", s.models)
		r.Post("/images/estimate", s.imageEstimate)
		r.Post("/videos/estimate", s.videoEstimate)
		r.Post("/images/generations", s.imageGeneration)
		r.Get("/images/{id:[0-9a-fA-F-]+}", s.getImageTask)
		r.Post("/images/{id:[0-9a-fA-F-]+}/cancel", s.cancelImageTask)
		r.Post("/videos/generations", s.videoGeneration)
		r.Get("/videos/{id}", s.getVideoTask)
		r.Post("/videos/{id}/cancel", s.cancelVideoTask)
		r.Post("/audio/generations", s.audioGeneration)
		r.Get("/audio/{id}", s.getAudioTask)
		r.Post("/audio/{id}/cancel", s.cancelAudioTask)
		r.Post("/chat/completions", s.chat)
	})
	r.Post("/admin/login", s.adminLogin)
	r.Put("/internal/session-sync/accounts/{id}", s.internalImportAccountSession)
	r.Post("/internal/session-refresh/claim", s.internalClaimSessionRefresh)
	r.Post("/internal/session-refresh/jobs/{id}/heartbeat", s.internalHeartbeatSessionRefresh)
	r.Post("/internal/session-refresh/jobs/{id}/complete", s.internalCompleteSessionRefresh)
	r.Post("/internal/session-refresh/jobs/{id}/fail", s.internalFailSessionRefresh)
	r.Route("/admin/api", func(r chi.Router) {
		r.Use(s.adminAuth, s.adminCSRF)
		r.Get("/overview", s.adminOverview)
		r.Get("/providers", s.adminProviders)
		r.Get("/accounts", s.adminAccounts)
		r.Post("/accounts", s.adminCreateAccount)
		r.Patch("/accounts/{id}", s.adminUpdateAccount)
		r.Post("/accounts/{id}/refresh", s.adminRefreshAccount)
		r.Post("/accounts/{id}/session-refresh", s.adminEnqueueSessionRefresh)
		r.Put("/accounts/{id}/session", s.adminImportAccountSession)
		r.Patch("/accounts/{id}/status", s.adminSetAccountStatus)
		r.Patch("/accounts/{id}/session-worker-group", s.adminSetAccountSessionWorkerGroup)
		r.Get("/tasks", s.adminTasks)
		r.Get("/tasks/{id}", s.adminTask)
		r.Post("/tasks/{id}/reservation/release", s.adminReleaseTaskReservation)
		r.Post("/tasks/{id}/submission/not-created", s.adminConfirmTaskNotSubmitted)
		r.Get("/model-costs", s.adminModelCosts)
		r.Get("/cost-rules", s.adminCostRules)
		r.Post("/cost-rules", s.adminCreateCostRule)
		r.Put("/cost-rules/{id}", s.adminUpdateCostRule)
		r.Delete("/cost-rules/{id}", s.adminDeleteCostRule)
		r.Get("/api-keys", s.adminAPIKeys)
		r.Post("/api-keys", s.adminCreateAPIKey)
		r.Patch("/api-keys/{id}", s.adminSetAPIKeyEnabled)
		r.Delete("/api-keys/{id}", s.adminDeleteAPIKey)
		r.Get("/models", s.adminModels)
		r.Get("/platform-models", s.adminPlatformModels)
		r.Get("/platform-video-models", s.adminPlatformVideoModels)
		r.Get("/platform-audio-models", s.adminPlatformAudioModels)
		r.Post("/platform-models/sync", s.adminSyncPlatformModels)
		r.Get("/request-logs", s.adminRequestLogs)
		r.Get("/audit-logs", s.adminAuditLogs)
		r.Get("/session-refresh/jobs", s.adminSessionRefreshJobs)
		r.Get("/system-capacity", s.adminSystemCapacity)
		r.Put("/system-capacity", s.adminUpdateSystemCapacity)
		r.Get("/sale-pricing", s.adminSalePricing)
		r.Put("/sale-pricing", s.adminUpdateSalePricing)
		r.Post("/sale-pricing/quote", s.adminSalePricingQuote)
		r.Get("/settings", s.adminSettings)
		r.Put("/settings/schema-version", s.adminSetSchemaVersion)
		r.Put("/settings/password", s.adminChangePassword)
	})
	if s.static != nil {
		r.NotFound(s.static.ServeHTTP)
	}
	return r
}

func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.Store.ListProviders(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, providers)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.Log.Info("http request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'; img-src 'self' data: https:; media-src 'self' blob: data: https:; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeAudit(ctx context.Context, actor, action, target string, metadata any) {
	if err := s.Store.WriteAudit(ctx, actor, action, target, metadata); err != nil {
		s.Log.Error("write audit log", "actor", actor, "action", action, "target", target, "error", err)
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		writeError(w, 503, "postgres_unavailable", err.Error())
		return
	}
	if err := s.Redis.Ping(ctx).Err(); err != nil {
		writeError(w, 503, "redis_unavailable", err.Error())
		return
	}
	if s.Config.Mode == "all" {
		if err := s.Redis.Get(ctx, "aiv2api:scheduler:heartbeat").Err(); err != nil {
			writeError(w, 503, "scheduler_unavailable", "task scheduler heartbeat is stale")
			return
		}
	}
	health, err := s.Store.GetSchedulingHealth(ctx)
	if err != nil {
		writeError(w, 503, "scheduler_health_unavailable", err.Error())
		return
	}
	if health.OutboxPending > 0 && health.OutboxOldest > 30*time.Second {
		writeError(w, 503, "task_dispatch_delayed", "task dispatch Outbox is delayed")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ready"})
}

func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r.Header.Get("Authorization"))
		if raw == "" {
			writeError(w, 401, "invalid_api_key", "missing Bearer API key")
			return
		}
		key, err := s.Store.VerifyAPIKey(r.Context(), raw)
		if err != nil {
			writeError(w, 401, "invalid_api_key", "invalid API key")
			return
		}
		noteRequestAPIKey(r, key)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContext, key)))
	})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	models, err := s.Store.ListModels(r.Context())
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	key := r.Context().Value(apiKeyContext).(domain.APIKey)
	data := modelListForKey(models, key.AllowedModels)
	writeJSON(w, 200, map[string]any{"object": "list", "data": data})
}

func modelListForKey(models []domain.ModelConfig, allowedModels []string) []map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if !allowed(allowedModels, model.ID) {
			continue
		}
		data = append(data, map[string]any{"id": model.ID, "object": "model", "created": 0, "owned_by": "aiv2api"})
	}
	return data
}
