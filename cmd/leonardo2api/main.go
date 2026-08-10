package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/circuit"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/cryptox"
	"github.com/leonardo2api/leonardo2api/internal/httpapi"
	"github.com/leonardo2api/leonardo2api/internal/jobs"
	"github.com/leonardo2api/leonardo2api/internal/metrics"
	"github.com/leonardo2api/leonardo2api/internal/migrate"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/sessionrefresh"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
	"github.com/leonardo2api/leonardo2api/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := migrate.Up(ctx, cfg.DatabaseURL); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	apiConns, workerConns, controlConns := databasePoolBudgets(cfg.Mode, cfg.DatabaseMaxConns)
	openedStores := make([]*store.Store, 0, 3)
	openStore := func(name string, maxConns int) *store.Store {
		st, openErr := store.NewWithMaxConns(ctx, cfg.DatabaseURL, int32(maxConns))
		if openErr != nil {
			log.Error("database connect failed", "pool", name, "max_conns", maxConns, "error", openErr)
			for _, opened := range openedStores {
				opened.Close()
			}
			os.Exit(1)
		}
		st.ConfigureScheduling(store.SchedulingPolicy{
			QueueTimeout: cfg.QueueTimeout, APIKeyQueueMultiplier: cfg.APIKeyQueueMultiplier,
			UncertainAccountLimit: cfg.UncertainAccountLimit, UncertainProxyLimit: cfg.UncertainProxyLimit,
			UncertainProviderLimit: cfg.UncertainProviderLimit,
		})
		openedStores = append(openedStores, st)
		return st
	}
	var apiStore, workerStore, controlStore *store.Store
	if apiConns > 0 {
		apiStore = openStore("api", apiConns)
	}
	if workerConns > 0 {
		workerStore = openStore("worker", workerConns)
	}
	if controlConns > 0 {
		controlStore = openStore("control", controlConns)
	}
	defer func() {
		for _, opened := range openedStores {
			opened.Close()
		}
	}()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("redis connect failed", "error", err)
		os.Exit(1)
	}
	cipher, err := cryptox.New(cfg.MasterKey)
	if err != nil {
		panic(err)
	}
	var apiAccountService, workerAccountService, controlAccountService *accounts.Service
	if apiStore != nil {
		apiAccountService = &accounts.Service{Store: apiStore, Redis: rdb, Cipher: cipher, Config: cfg}
	}
	if workerStore != nil {
		workerAccountService = &accounts.Service{Store: workerStore, Redis: rdb, Cipher: cipher, Config: cfg}
	}
	if controlStore != nil {
		controlAccountService = &accounts.Service{Store: controlStore, Redis: rdb, Cipher: cipher, Config: cfg}
	}
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}
	queue := jobs.NewClient(redisOpt)
	defer queue.Close()
	metrics.Register()
	providerRegistry := providers.NewRegistry()
	assets, err := taskassets.New(cfg.TaskAssetDir)
	if err != nil {
		log.Error("task asset store initialization failed", "error", err)
		os.Exit(1)
	}
	sharedCircuit := circuit.Breaker{Redis: rdb, Window: cfg.SharedCircuitWindow, Cooldown: cfg.SharedCircuitCooldown, ProviderFailures: cfg.ProviderCircuitFailures, ProxyFailures: cfg.ProxyCircuitFailures}
	var asynqServer *asynq.Server
	if cfg.Mode == "worker" || cfg.Mode == "all" {
		worker := &jobs.Worker{Store: workerStore, Redis: rdb, Accounts: workerAccountService, Assets: assets, Config: cfg, Log: log, Providers: providerRegistry, Circuit: sharedCircuit}
		scheduler := &jobs.Scheduler{Accounts: controlAccountService, Store: controlStore, Queue: queue, Assets: assets, Config: cfg, Log: log, Redis: rdb}
		asynqServer = asynq.NewServer(redisOpt, asynq.Config{Concurrency: cfg.TaskDispatcherConcurrency, Queues: jobs.WorkerQueues(), Logger: asynqLogger{log}})
		go func() {
			if err := asynqServer.Run(worker.Handler()); err != nil && !errors.Is(err, asynq.ErrServerClosed) {
				log.Error("worker stopped", "error", err)
				stop()
			}
		}()
		go scheduler.Run(ctx)
		sessionManager := &sessionrefresh.Manager{Store: controlStore, Accounts: controlAccountService, Config: cfg, Log: log}
		go sessionManager.Run(ctx)
	}
	var server *http.Server
	if cfg.Mode == "api" || cfg.Mode == "all" {
		api := httpapi.New(apiStore, rdb, queue, apiAccountService, assets, cfg, log, web.Handler())
		server = &http.Server{Addr: cfg.HTTPAddr, Handler: otelhttp.NewHandler(api.Router(), "http.server"), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 4 * time.Minute, WriteTimeout: 4*time.Minute + 30*time.Second, IdleTimeout: 90 * time.Second}
		go func() {
			log.Info("HTTP server listening", "addr", cfg.HTTPAddr, "mode", cfg.Mode)
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("HTTP server stopped", "error", err)
				stop()
			}
		}()
	}
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if server != nil {
		_ = server.Shutdown(shutdownCtx)
	}
	if asynqServer != nil {
		asynqServer.Shutdown()
	}
}

func databasePoolBudgets(mode string, total int) (api, worker, control int) {
	if total < 4 {
		total = 4
	}
	switch mode {
	case "api":
		return total, 0, 0
	case "worker":
		if total < 8 {
			return 0, 4, 4
		}
		control = total / 4
		worker = total - control
		return 0, worker, control
	default:
		api = total * 3 / 8
		worker = total / 2
		control = total - api - worker
		return api, worker, control
	}
}

type asynqLogger struct{ l *slog.Logger }

func (a asynqLogger) Debug(args ...interface{}) { a.l.Debug("asynq", "args", args) }
func (a asynqLogger) Info(args ...interface{})  { a.l.Info("asynq", "args", args) }
func (a asynqLogger) Warn(args ...interface{})  { a.l.Warn("asynq", "args", args) }
func (a asynqLogger) Error(args ...interface{}) { a.l.Error("asynq", "args", args) }
func (a asynqLogger) Fatal(args ...interface{}) { a.l.Error("asynq fatal", "args", args) }
