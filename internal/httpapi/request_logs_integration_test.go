package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/migrate"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

func TestAPIRequestLogMiddlewarePersistsFailureContext(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(ctx, `TRUNCATE api_request_logs,api_keys RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	keyID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO api_keys(id,name,key_prefix,key_hash,allowed_models)
		VALUES($1,'request-log-test','fixture',decode('01','hex'),'["*"]')`, keyID); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := middleware.RequestID(server.apiRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noteImageRequest(r, domain.ImageRequest{Provider: "adobe", Model: "gpt-image-2", Prompt: "fixture", Size: "1536x1024", Quality: "high", N: 1})
		noteRequestEstimate(r, 1033)
		writeError(w, http.StatusUnprocessableEntity, "cost_unavailable", "fixture")
	})))
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	request = request.WithContext(context.WithValue(request.Context(), apiKeyContext, domain.APIKey{ID: keyID, Prefix: "fixture"}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d", recorder.Code)
	}
	entries, total, err := st.ListAPIRequestLogsPage(ctx, 1, 20, "cost_unavailable", 422)
	if err != nil || total != 1 || len(entries) != 1 {
		t.Fatalf("entries=%+v total=%d err=%v", entries, total, err)
	}
	entry := entries[0]
	if entry.ProviderID != "adobe" || entry.APIKeyPrefix != "fixture" || entry.Model != "gpt-image-2" || entry.EstimatedTokens == nil || *entry.EstimatedTokens != 1033 || entry.ErrorCode != "cost_unavailable" {
		t.Fatalf("entry=%+v", entry)
	}
	unauthenticated := middleware.RequestID(server.apiRequestLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "fixture")
	})))
	unauthenticated.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	entries, total, err = st.ListAPIRequestLogsPage(ctx, 1, 20, "invalid_api_key", 401)
	if err != nil || total != 1 || len(entries) != 1 || entries[0].APIKeyPrefix != "" {
		t.Fatalf("unauthenticated entries=%+v total=%d err=%v", entries, total, err)
	}
}

func TestSupportedAPIKeyModelsReadEnabledProviderCatalog(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := &Server{Store: st}
	models, err := server.supportedAPIKeyModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"leonardo:gpt-image-2", "adobe:gpt-image-2", "adobe:veo-3.1-fast"} {
		if !models[permission] {
			t.Fatalf("provider model permission %q is missing", permission)
		}
	}
}

func TestAPIRequestLogWorkerBatchesAndCountsUsage(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(ctx, `TRUNCATE api_request_logs,api_keys RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	keyID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO api_keys(id,name,key_prefix,key_hash,allowed_models)
		VALUES($1,'request-log-batch-test','batch',decode('02','hex'),'["*"]')`, keyID); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		Store:           st,
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		requestLogQueue: make(chan domain.APIRequestLog, 512),
	}
	done := make(chan struct{})
	go func() {
		server.runRequestLogWorker()
		close(done)
	}()
	handler := middleware.RequestID(server.apiRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noteImageRequest(r, domain.ImageRequest{Model: "gpt-image-2", Prompt: "fixture", Size: "1536x1024", Quality: "high", N: 1})
		w.WriteHeader(http.StatusAccepted)
	})))

	const requestCount = 250
	for range requestCount {
		request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		request = request.WithContext(context.WithValue(request.Context(), apiKeyContext, domain.APIKey{ID: keyID, Prefix: "batch"}))
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	close(server.requestLogQueue)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("request log worker did not drain the queue")
	}

	var storedLogs, storedUsage int64
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM api_request_logs`).Scan(&storedLogs); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `SELECT request_count FROM api_keys WHERE id=$1`, keyID).Scan(&storedUsage); err != nil {
		t.Fatal(err)
	}
	if storedLogs != requestCount || storedUsage != requestCount {
		t.Fatalf("stored logs=%d usage=%d want=%d", storedLogs, storedUsage, requestCount)
	}
}
