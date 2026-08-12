package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestConcurrentReservationsRespectBalanceAndCapacity(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resetSystemCapacity(t, ctx, st)
	if _, err := st.DB.Exec(ctx, `TRUNCATE account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var keyID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO api_keys(name,key_prefix,key_hash,concurrency_limit,allowed_models) VALUES('test','test',decode('00','hex'),20,'["*"]') RETURNING id`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,status,access_token_expires_at,last_checked_at) VALUES($1,'cipher',200,2,'active',now()+interval '1 hour',now())`, fmt.Sprintf("account-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var ruleID int64
	if err := st.DB.QueryRow(ctx, `SELECT id FROM model_cost_rules WHERE provider_id='leonardo' LIMIT 1`).Scan(&ruleID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "test", domain.ImageRequest{Model: "gpt-image-2", Prompt: "test"}, fmt.Sprintf("idem-%d", index), 80, ruleID, time.Minute)
			if err == nil && created {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if succeeded != 4 {
		t.Fatalf("expected 4 reservations, got %d", succeeded)
	}
	rows, err := st.DB.Query(ctx, `SELECT a.subscription_tokens,COALESCE(sum(r.estimated_tokens),0),count(r.task_id) FROM accounts a LEFT JOIN account_reservations r ON r.account_id=a.id AND r.state='held' GROUP BY a.id,a.subscription_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var balance, reserved int64
		var slots int
		if err := rows.Scan(&balance, &reserved, &slots); err != nil {
			t.Fatal(err)
		}
		if reserved > balance || slots > 2 {
			t.Fatalf("balance=%d reserved=%d slots=%d", balance, reserved, slots)
		}
	}
}

func TestAPIKeyCapacityBoundsExecutionAndWaitingTasks(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET image_concurrency=10,subscription_tokens=10000`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE api_keys SET concurrency_limit=1 WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	tasks := make([]domain.Task, 0, 3)
	for i := 0; i < 3; i++ {
		task, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "key-capacity", domain.ImageRequest{Model: "gpt-image-2", Prompt: "key-capacity"}, fmt.Sprintf("key-capacity-%d", i), 8, ruleID, time.Minute)
		if err != nil || !created {
			t.Fatalf("create %d: created=%v err=%v", i, created, err)
		}
		tasks = append(tasks, task)
	}
	if _, _, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "key-capacity", domain.ImageRequest{Model: "gpt-image-2", Prompt: "key-capacity"}, "key-capacity-full", 8, ruleID, time.Minute); !errors.Is(err, ErrAPIKeyCapacity) {
		t.Fatalf("fourth task error=%v, want ErrAPIKeyCapacity", err)
	}
	claimed := 0
	for _, task := range tasks {
		_, _, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed=%d, want API key execution concurrency 1", claimed)
	}
}

func TestSubmittedGenerationRejectsGenericFailureRelease(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "submitted-safety", domain.ImageRequest{Model: "gpt-image-2", Prompt: "submitted-safety"}, "submitted-safety", 8, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	claimed, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim: task=%+v ok=%v err=%v", claimed, ok, err)
	}
	if ok, err := st.PrepareTaskSubmissionOwned(ctx, task.ID, leaseID, *claimed.AccountID, map[string]any{"model": "gpt-image-2"}); err != nil || !ok {
		t.Fatalf("prepare: ok=%v err=%v", ok, err)
	}
	if ok, err := st.RecordTaskSubmissionOwned(ctx, task.ID, leaseID, *claimed.AccountID, "generation-safe", nil); err != nil || !ok {
		t.Fatalf("record submission: ok=%v err=%v", ok, err)
	}
	if ok, err := st.FailTaskOwned(ctx, task.ID, leaseID, "result_failed", "temporary result read error", nil, nil, false); err != nil || ok {
		t.Fatalf("generic failure after submission: ok=%v err=%v", ok, err)
	}
	stored, err := st.GetTask(ctx, task.ID)
	if err != nil || stored.Status != domain.TaskSubmitted || stored.GenerationID != "generation-safe" {
		t.Fatalf("stored task=%+v err=%v", stored, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "held" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	if ok, err := st.FailSubmittedTaskOwned(ctx, task.ID, leaseID, "upstream_failed", "confirmed upstream failure", nil); err != nil || !ok {
		t.Fatalf("confirmed failure: ok=%v err=%v", ok, err)
	}
	reservation, err = st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "released" {
		t.Fatalf("released reservation=%+v err=%v", reservation, err)
	}
}

func TestConfirmSubmissionNotCreatedFailsTaskAndReleasesReservation(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task, created, err := st.CreateReservedTask(ctx, keyID, "video", "minimax-h3", "uncertain", map[string]any{"model": "minimax-h3"}, "confirm-not-created", 2100, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	claimed, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim: task=%+v ok=%v err=%v", claimed, ok, err)
	}
	if ok, err := st.PrepareTaskSubmissionOwned(ctx, task.ID, leaseID, *claimed.AccountID, map[string]any{"model": "hailuo-03"}); err != nil || !ok {
		t.Fatalf("prepare: ok=%v err=%v", ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskSubmissionUncertain, 100, claimed.AccountID, "", nil, "submission_uncertain", "fixture timeout"); err != nil || !ok {
		t.Fatalf("mark uncertain: ok=%v err=%v", ok, err)
	}
	if ok, err := st.ConfirmSubmissionNotCreated(ctx, task.ID); err != nil || !ok {
		t.Fatalf("confirm not created: ok=%v err=%v", ok, err)
	}
	stored, err := st.GetTask(ctx, task.ID)
	if err != nil || stored.Status != domain.TaskFailed || stored.ErrorCode != "submission_not_created" {
		t.Fatalf("stored task=%+v err=%v", stored, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "released" || reservation.ReleaseReason != "submission_not_created" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	if ok, err := st.ConfirmSubmissionNotCreated(ctx, task.ID); err != nil || ok {
		t.Fatalf("duplicate confirmation: ok=%v err=%v", ok, err)
	}
}

func TestRecoverAbsoluteTaskDeadlines(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	queued, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "queue-deadline", domain.ImageRequest{Model: "gpt-image-2", Prompt: "queue-deadline"}, "queue-deadline", 8, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("create queued: created=%v err=%v", created, err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE tasks SET queue_deadline_at=now()-interval '1 second' WHERE id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecoverStaleTasks(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	queuedStored, err := st.GetTask(ctx, queued.ID)
	if err != nil || queuedStored.Status != domain.TaskFailed || queuedStored.ErrorCode != "queue_timeout" {
		t.Fatalf("queued task=%+v err=%v", queuedStored, err)
	}
	queuedReservation, err := st.ReservationForTask(ctx, queued.ID)
	if err != nil || queuedReservation.State != "released" {
		t.Fatalf("queued reservation=%+v err=%v", queuedReservation, err)
	}

	submitted, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "upstream-deadline", domain.ImageRequest{Model: "gpt-image-2", Prompt: "upstream-deadline"}, "upstream-deadline", 8, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("create submitted: created=%v err=%v", created, err)
	}
	claimed, leaseID, ok, err := st.ClaimTask(ctx, submitted.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim submitted: ok=%v err=%v", ok, err)
	}
	if ok, err := st.PrepareTaskSubmissionWithDeadlineOwned(ctx, submitted.ID, leaseID, *claimed.AccountID, map[string]any{"model": "gpt-image-2"}, time.Now().Add(-time.Second)); err != nil || !ok {
		t.Fatalf("prepare submitted: ok=%v err=%v", ok, err)
	}
	if ok, err := st.RecordTaskSubmissionOwned(ctx, submitted.ID, leaseID, *claimed.AccountID, "generation-deadline", nil); err != nil || !ok {
		t.Fatalf("record submitted: ok=%v err=%v", ok, err)
	}
	if _, err := st.RecoverStaleTasks(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	submittedStored, err := st.GetTask(ctx, submitted.ID)
	if err != nil || submittedStored.Status != domain.TaskSubmissionUncertain || submittedStored.ErrorCode != "upstream_deadline_exceeded" {
		t.Fatalf("submitted task=%+v err=%v", submittedStored, err)
	}
	submittedReservation, err := st.ReservationForTask(ctx, submitted.ID)
	if err != nil || submittedReservation.State != "held" {
		t.Fatalf("submitted reservation=%+v err=%v", submittedReservation, err)
	}
}

func TestAccountCapacityQueuesAndCancelReleasesCredits(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET image_concurrency=2,queue_capacity=5,subscription_tokens=700`); err != nil {
		t.Fatal(err)
	}
	tasks := make([]domain.Task, 0, 7)
	for i := 0; i < 7; i++ {
		task, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "queued", domain.ImageRequest{Model: "gpt-image-2", Prompt: "queued"}, fmt.Sprintf("queued-%d", i), 80, ruleID, time.Minute)
		if err != nil || !created {
			t.Fatalf("create %d: created=%v err=%v", i, created, err)
		}
		tasks = append(tasks, task)
	}
	if _, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "queue-full", domain.ImageRequest{Model: "gpt-image-2", Prompt: "queue-full"}, "queue-full", 8, ruleID, time.Minute); !errors.Is(err, ErrAccountQueueCapacity) || created {
		t.Fatalf("eighth task created=%v err=%v, want account queue capacity error", created, err)
	}
	claimed := 0
	var firstClaimedTask, firstLease uuid.UUID
	for _, task := range tasks {
		_, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			claimed++
			if firstClaimedTask == uuid.Nil {
				firstClaimedTask, firstLease = task.ID, leaseID
			}
		}
	}
	if claimed != 2 {
		t.Fatalf("claimed=%d, want account execution concurrency 2", claimed)
	}
	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if accounts[0].ActiveReservations != 2 || accounts[0].QueuedTasks != 5 {
		t.Fatalf("active=%d queued=%d, want 2 executing and 5 waiting", accounts[0].ActiveReservations, accounts[0].QueuedTasks)
	}
	if ok, err := st.FailTaskOwned(ctx, firstClaimedTask, firstLease, "fixture_done", "release execution slot", nil, nil, false); err != nil || !ok {
		t.Fatalf("release slot ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := st.ClaimTask(ctx, tasks[2].ID, time.Minute); err != nil || !ok {
		t.Fatalf("next queued task did not acquire released slot: ok=%v err=%v", ok, err)
	}
	queued, err := st.GetTask(ctx, tasks[3].ID)
	if err != nil || queued.Status != domain.TaskQueued || queued.QueuePosition == nil {
		t.Fatalf("queued task=%+v err=%v", queued, err)
	}
	ok, err := st.CancelQueuedTaskAndRelease(ctx, queued.ID, keyID)
	if err != nil || !ok {
		t.Fatalf("cancel ok=%v err=%v", ok, err)
	}
	reservation, err := st.ReservationForTask(ctx, queued.ID)
	if err != nil || reservation.State != "released" || reservation.ReleaseReason != "cancelled" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
}

func TestUpdateAccountConfigRespectsCurrentCapacityUsage(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	tasks := make([]domain.Task, 0, 4)
	for i := 0; i < 4; i++ {
		tasks = append(tasks, createFixtureTask(t, ctx, st, keyID, ruleID, fmt.Sprintf("account-update-%d", i)))
	}
	for i := 0; i < 2; i++ {
		if _, _, ok, err := st.ClaimTask(ctx, tasks[i].ID, time.Minute); err != nil || !ok {
			t.Fatalf("claim task %d ok=%v err=%v", i, ok, err)
		}
	}
	accountID := *tasks[0].AccountID

	concurrency := 1
	_, err := st.UpdateAccountConfig(ctx, accountID, AccountConfigPatch{ImageConcurrency: &concurrency})
	var capacityErr *AccountCapacityInUseError
	if !errors.As(err, &capacityErr) || capacityErr.ExecutingTasks != 2 || capacityErr.QueuedTasks != 2 {
		t.Fatalf("concurrency shrink error=%v details=%+v", err, capacityErr)
	}

	queueCapacity := 1
	_, err = st.UpdateAccountConfig(ctx, accountID, AccountConfigPatch{QueueCapacity: &queueCapacity})
	if !errors.As(err, &capacityErr) || capacityErr.QueuedTasks != 2 {
		t.Fatalf("queue shrink error=%v details=%+v", err, capacityErr)
	}

	name, email := "renamed-account", "routing@example.test"
	proxyURL, workerGroup := "http://127.0.0.1:7890", "proxy-us-1"
	concurrency, queueCapacity = 2, 2
	updated, err := st.UpdateAccountConfig(ctx, accountID, AccountConfigPatch{
		Name: &name, Email: &email, ProxyURL: &proxyURL,
		ImageConcurrency: &concurrency, QueueCapacity: &queueCapacity,
		BrowserWorkerGroup: &workerGroup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Email != email || updated.ProxyURL != proxyURL ||
		updated.ImageConcurrency != concurrency || updated.QueueCapacity != queueCapacity ||
		updated.BrowserWorkerGroup != workerGroup {
		t.Fatalf("unexpected updated account: %+v", updated)
	}
}

func TestListAdminTasksPageUsesStableOrdering(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	for i := 0; i < 5; i++ {
		createFixtureTask(t, ctx, st, keyID, ruleID, fmt.Sprintf("page-%d", i))
	}
	if _, err := st.DB.Exec(ctx, `UPDATE tasks SET created_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}

	var expected []uuid.UUID
	rows, err := st.DB.Query(ctx, `SELECT id FROM tasks ORDER BY created_at DESC,id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		expected = append(expected, id)
	}
	rows.Close()

	var actual []uuid.UUID
	for page, wantSize := range []int{2, 2, 1} {
		items, total, err := st.ListAdminTasksPage(ctx, page+1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 || len(items) != wantSize {
			t.Fatalf("page=%d total=%d len=%d, want total=5 len=%d", page+1, total, len(items), wantSize)
		}
		for _, item := range items {
			if item.EstimatedTokens != 80 || item.ReservationState != "held" {
				t.Fatalf("missing reservation details: %+v", item)
			}
			actual = append(actual, item.ID)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("actual=%v expected=%v", actual, expected)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("stable order mismatch at %d: got %s want %s", i, actual[i], expected[i])
		}
	}
	from := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	filtered, total, err := st.ListAdminTasksPageFiltered(ctx, 1, 3, TaskPageFilter{
		Search: "fixture", Status: "queued", Kind: "image", Model: "gpt-image-2", CreatedFrom: &from, CreatedTo: &to,
	})
	if err != nil || total != 5 || len(filtered) != 3 {
		t.Fatalf("filtered task items=%+v total=%d err=%v", filtered, total, err)
	}
	detail, err := st.GetAdminTask(ctx, filtered[0].ID)
	if err != nil || len(detail.Events) == 0 || detail.Events[0].Status != "queued" {
		t.Fatalf("task events=%+v err=%v", detail.Events, err)
	}
}

func TestListAuditLogsPageUsesStableOrdering(t *testing.T) {
	ctx, st, _, _ := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `TRUNCATE audit_logs RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := st.WriteAudit(ctx, "admin", fmt.Sprintf("audit.action.%d", i), fmt.Sprintf("target-%d", i), map[string]any{"index": i}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB.Exec(ctx, `UPDATE audit_logs SET created_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}

	var expected []int64
	rows, err := st.DB.Query(ctx, `SELECT id FROM audit_logs ORDER BY created_at DESC,id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		expected = append(expected, id)
	}
	rows.Close()

	var actual []int64
	for page, wantSize := range []int{2, 2, 1} {
		items, total, err := st.ListAuditLogsPage(ctx, page+1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 || len(items) != wantSize {
			t.Fatalf("page=%d total=%d len=%d, want total=5 len=%d", page+1, total, len(items), wantSize)
		}
		for _, item := range items {
			actual = append(actual, item.ID)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("actual=%v expected=%v", actual, expected)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("stable audit order mismatch at %d: got %d want %d", i, actual[i], expected[i])
		}
	}
	filtered, total, err := st.ListAuditLogsPageFiltered(ctx, 1, 20, AuditLogPageFilter{Search: "target-3", Action: "audit.action.3"})
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].Target != "target-3" {
		t.Fatalf("filtered audit items=%+v total=%d err=%v", filtered, total, err)
	}
}

func TestListAPIRequestLogsPageFiltersAndUsesStableOrdering(t *testing.T) {
	ctx, st, keyID, _ := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `TRUNCATE api_request_logs RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `SELECT id FROM accounts WHERE name='fixture'`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	entries := []domain.APIRequestLog{
		{RequestID: "request-success", APIKeyID: &keyID, APIKeyPrefix: "fixture", AccountID: &accountID, Method: "POST", Path: "/v1/images/generations", Kind: "image", Model: "gpt-image-2", Parameters: []byte(`{"size":"1024x1024"}`), Status: 200, DurationMS: 10},
		{RequestID: "request-cost", APIKeyID: &keyID, APIKeyPrefix: "fixture", Method: "POST", Path: "/v1/images/generations", Kind: "image", Model: "nano-banana-2", Parameters: []byte(`{"size":"768x1344"}`), Status: 422, ErrorCode: "cost_unavailable", DurationMS: 2},
		{RequestID: "request-poll", APIKeyID: &keyID, APIKeyPrefix: "fixture", AccountID: &accountID, Method: "GET", Path: "/v1/images/fixture", Status: 200, DurationMS: 1},
	}
	for _, entry := range entries {
		if err := st.WriteAPIRequestLog(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB.Exec(ctx, `UPDATE api_request_logs SET created_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	items, total, err := st.ListAPIRequestLogsPage(ctx, 1, 2, "", 0)
	if err != nil || total != 3 || len(items) != 2 || items[0].RequestID != "request-poll" || items[1].RequestID != "request-cost" {
		t.Fatalf("items=%+v total=%d err=%v", items, total, err)
	}
	items, total, err = st.ListAPIRequestLogsPage(ctx, 1, 20, "banana", 422)
	if err != nil || total != 1 || len(items) != 1 || items[0].ErrorCode != "cost_unavailable" {
		t.Fatalf("filtered items=%+v total=%d err=%v", items, total, err)
	}
	items, total, err = st.ListAPIRequestLogsPage(ctx, 1, 20, "fixture", 200)
	if err != nil || total != 2 || len(items) != 2 || items[0].AccountName != "fixture" {
		t.Fatalf("account search items=%+v total=%d err=%v", items, total, err)
	}
	from := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	items, total, err = st.ListAPIRequestLogsPageFiltered(ctx, 1, 20, APIRequestLogPageFilter{
		Method: "GET", Path: "/v1/tasks", APIKey: "fixture", ClientIP: "", CreatedFrom: &from, CreatedTo: &to,
	})
	if err != nil || total != 1 || len(items) != 1 || items[0].RequestID != "request-poll" {
		t.Fatalf("advanced request filter items=%+v total=%d err=%v", items, total, err)
	}
}

func newRoutingFixture(t *testing.T) (context.Context, *Store, uuid.UUID, int64) {
	t.Helper()
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	resetSystemCapacity(t, ctx, st)
	if _, err := st.DB.Exec(ctx, `TRUNCATE account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var keyID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO api_keys(name,key_prefix,key_hash,concurrency_limit,allowed_models) VALUES('fixture','fixture',decode('01','hex'),20,'["*"]') RETURNING id`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,status,access_token_expires_at,last_checked_at) VALUES('fixture','cipher',500,2,'active',now()+interval '1 hour',now())`); err != nil {
		t.Fatal(err)
	}
	var ruleID int64
	if err := st.DB.QueryRow(ctx, `SELECT id FROM model_cost_rules WHERE provider_id='leonardo' LIMIT 1`).Scan(&ruleID); err != nil {
		t.Fatal(err)
	}
	return ctx, st, keyID, ruleID
}

func resetSystemCapacity(t *testing.T, ctx context.Context, st *Store) {
	t.Helper()
	if _, err := st.DB.Exec(ctx, `UPDATE system_capacity_config SET
		max_executing=100,max_queued=1000,queue_high_watermark=900,queue_resume_watermark=700,
		queue_timeout_seconds=1800,maintenance_mode=false,execution_paused=false,
		overload_active=false,revision=revision+1,updated_at=now() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
}

func createFixtureTask(t *testing.T, ctx context.Context, st *Store, keyID uuid.UUID, ruleID int64, idem string) domain.Task {
	t.Helper()
	task, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "fixture", domain.ImageRequest{Model: "gpt-image-2", Prompt: "fixture"}, idem, 80, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("CreateReservedTask created=%v err=%v", created, err)
	}
	return task
}

func TestSystemQueueCapacityIsAtomicAcrossConcurrentAdmissions(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET image_concurrency=10,queue_capacity=100,subscription_tokens=100000`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE api_keys SET concurrency_limit=100 WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	config, err := st.GetSystemCapacity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxQueued = 5
	config.QueueHighWatermark = 5
	config.QueueResumeWatermark = 2
	if _, err := st.UpdateSystemCapacity(ctx, config.SystemCapacityConfig); err != nil {
		t.Fatal(err)
	}

	const attempts = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	rejectedCount := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "system-capacity", domain.ImageRequest{Model: "gpt-image-2", Prompt: "system-capacity"}, fmt.Sprintf("system-capacity-%d", index), 8, ruleID, time.Minute)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && created:
				createdCount++
			case errors.Is(err, ErrSystemOverloaded) || errors.Is(err, ErrSystemQueueCapacity):
				rejectedCount++
			default:
				t.Errorf("attempt %d created=%v err=%v", index, created, err)
			}
		}(i)
	}
	wg.Wait()
	if createdCount != 5 || rejectedCount != attempts-5 {
		t.Fatalf("created=%d rejected=%d, want 5/%d", createdCount, rejectedCount, attempts-5)
	}
	var queued int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE status='queued'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 5 {
		t.Fatalf("queued=%d, want 5", queued)
	}
}

func TestSystemMaintenancePreservesIdempotentRetries(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	req := domain.ImageRequest{Model: "gpt-image-2", Prompt: "maintenance"}
	created, wasCreated, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "maintenance", req, "maintenance-existing", 8, ruleID, time.Minute)
	if err != nil || !wasCreated {
		t.Fatalf("create existing: created=%v err=%v", wasCreated, err)
	}
	config, err := st.GetSystemCapacity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.MaintenanceMode = true
	if _, err := st.UpdateSystemCapacity(ctx, config.SystemCapacityConfig); err != nil {
		t.Fatal(err)
	}
	replayed, replayCreated, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "maintenance", req, "maintenance-existing", 8, ruleID, time.Minute)
	if err != nil || replayCreated || replayed.ID != created.ID {
		t.Fatalf("idempotent replay task=%s created=%v err=%v", replayed.ID, replayCreated, err)
	}
	if _, _, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "maintenance", req, "maintenance-new", 8, ruleID, time.Minute); !errors.Is(err, ErrSystemMaintenance) {
		t.Fatalf("new task err=%v, want ErrSystemMaintenance", err)
	}
}

func TestSystemExecutionLimitIsAtomicAcrossWorkers(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET image_concurrency=2,queue_capacity=10,subscription_tokens=10000`); err != nil {
		t.Fatal(err)
	}
	config, err := st.GetSystemCapacity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxExecuting = 1
	if _, err := st.UpdateSystemCapacity(ctx, config.SystemCapacityConfig); err != nil {
		t.Fatal(err)
	}
	tasks := []domain.Task{
		createFixtureTask(t, ctx, st, keyID, ruleID, "system-execution-1"),
		createFixtureTask(t, ctx, st, keyID, ruleID, "system-execution-2"),
	}
	type claimResult struct {
		task    domain.Task
		leaseID uuid.UUID
		claimed bool
		err     error
	}
	results := make(chan claimResult, len(tasks))
	start := make(chan struct{})
	for _, task := range tasks {
		go func(task domain.Task) {
			<-start
			claimedTask, leaseID, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
			results <- claimResult{task: claimedTask, leaseID: leaseID, claimed: claimed, err: err}
		}(task)
	}
	close(start)
	claimedCount := 0
	var claimed claimResult
	for range tasks {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.claimed {
			claimedCount++
			claimed = result
		}
	}
	if claimedCount != 1 {
		t.Fatalf("claimed=%d, want 1", claimedCount)
	}
	if ok, err := st.FailTaskOwned(ctx, claimed.task.ID, claimed.leaseID, "fixture", "release system slot", nil, nil, false); err != nil || !ok {
		t.Fatalf("release claimed task ok=%v err=%v", ok, err)
	}
	for _, task := range tasks {
		if task.ID == claimed.task.ID {
			continue
		}
		if _, _, ok, err := st.ClaimTask(ctx, task.ID, time.Minute); err != nil || !ok {
			t.Fatalf("claim after release ok=%v err=%v", ok, err)
		}
	}
}

func TestGetIdempotentTaskValidatesPayloadWithoutCreatingTask(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	req := domain.ImageRequest{Model: "gpt-image-2", Prompt: "fixture"}
	created := createFixtureTask(t, ctx, st, keyID, ruleID, "idem-read")

	found, err := st.GetIdempotentTask(ctx, keyID, "image", req, "idem-read")
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != created.ID {
		t.Fatalf("got task %s, want %s", found.ID, created.ID)
	}

	if _, err := st.GetIdempotentTask(ctx, keyID, "image", domain.ImageRequest{Model: "gpt-image-2", Prompt: "different"}, "idem-read"); err == nil {
		t.Fatal("expected idempotency payload conflict")
	}
	if _, err := st.GetIdempotentTask(ctx, keyID, "image", req, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	var taskCount int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("idempotency lookup created tasks: count=%d", taskCount)
	}
}

func TestClaimTaskIsExclusive(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "claim-exclusive")

	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
			if err != nil {
				t.Errorf("ClaimTask: %v", err)
				return
			}
			if ok {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("expected one claimed worker, got %d", claimed)
	}
	got, err := st.GetTask(ctx, task.ID)
	if err != nil || got.Status != domain.TaskReserving {
		t.Fatalf("task=%+v err=%v", got, err)
	}
}

func TestPrepareTaskSubmissionRecordsAdminOnlyUpstreamRequest(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "upstream-request")
	claimedTask, leaseID, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed || claimedTask.AccountID == nil {
		t.Fatalf("ClaimTask task=%+v claimed=%v err=%v", claimedTask, claimed, err)
	}
	request := map[string]any{
		"model":  "gpt-image-2",
		"public": false,
		"parameters": map[string]any{
			"prompt": "fixture", "width": 1024, "height": 1024, "quantity": 1,
		},
	}
	if ok, err := st.PrepareTaskSubmissionOwned(ctx, task.ID, uuid.New(), *claimedTask.AccountID, request); err != nil || ok {
		t.Fatalf("foreign lease prepare ok=%v err=%v", ok, err)
	}
	if ok, err := st.PrepareTaskSubmissionOwned(ctx, task.ID, leaseID, *claimedTask.AccountID, request); err != nil || !ok {
		t.Fatalf("PrepareTaskSubmissionOwned ok=%v err=%v", ok, err)
	}

	detail, err := st.GetAdminTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(detail.UpstreamRequest, &decoded); err != nil {
		t.Fatalf("invalid upstream request JSON %s: %v", detail.UpstreamRequest, err)
	}
	if decoded["model"] != "gpt-image-2" || decoded["public"] != false {
		t.Fatalf("unexpected upstream request: %+v", decoded)
	}
	list, total, err := st.ListAdminTasksPage(ctx, 1, 20)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListAdminTasksPage total=%d len=%d err=%v", total, len(list), err)
	}
	if len(list[0].UpstreamRequest) != 0 {
		t.Fatalf("paginated task list should not include upstream request: %s", list[0].UpstreamRequest)
	}
}

func TestCancelAndClaimAreMutuallyExclusive(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "cancel-claim")

	start := make(chan struct{})
	var wg sync.WaitGroup
	var claimOK, cancelOK bool
	var claimErr, cancelErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, claimOK, claimErr = st.ClaimTask(ctx, task.ID, time.Minute)
	}()
	go func() {
		defer wg.Done()
		<-start
		cancelOK, cancelErr = st.CancelQueuedTaskAndRelease(ctx, task.ID, keyID)
	}()
	close(start)
	wg.Wait()
	if claimErr != nil || cancelErr != nil {
		t.Fatalf("claimErr=%v cancelErr=%v", claimErr, cancelErr)
	}
	if claimOK == cancelOK {
		t.Fatalf("claim=%v cancel=%v, expected exactly one winner", claimOK, cancelOK)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelOK && reservation.State != "released" {
		t.Fatalf("cancelled task reservation=%s, want released", reservation.State)
	}
	if claimOK && reservation.State != "held" {
		t.Fatalf("claimed task reservation=%s, want held", reservation.State)
	}
}

func TestExpiredLeaseRecoversThroughOutbox(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "recover-outbox")
	_, _, claimed, err := st.ClaimTask(ctx, task.ID, time.Millisecond)
	if err != nil || !claimed {
		t.Fatalf("ClaimTask claimed=%v err=%v", claimed, err)
	}
	time.Sleep(10 * time.Millisecond)
	recovered, err := st.RecoverStaleTasks(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].ID != task.ID || recovered[0].Status != domain.TaskQueued {
		t.Fatalf("unexpected recovered tasks: %+v", recovered)
	}
	got, err := st.GetTask(ctx, task.ID)
	if err != nil || got.Status != domain.TaskQueued {
		t.Fatalf("task=%+v err=%v", got, err)
	}
	var deliveredAt *time.Time
	if err := st.DB.QueryRow(ctx, `SELECT delivered_at FROM task_outbox WHERE task_id=$1`, task.ID).Scan(&deliveredAt); err != nil {
		t.Fatal(err)
	}
	if deliveredAt != nil {
		t.Fatalf("outbox delivered_at=%v, want pending", deliveredAt)
	}
}

func TestExpiredSubmittedLeaseRecoversThroughOutbox(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "recover-submitted-outbox")
	_, leaseID, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("ClaimTask claimed=%v err=%v", claimed, err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE tasks
		SET status='submitted',generation_id='generation-fixture',
			execution_lease_expires_at=now()-interval '1 second'
		WHERE id=$1 AND execution_lease_id=$2`, task.ID, leaseID); err != nil {
		t.Fatal(err)
	}
	recovered, err := st.RecoverStaleTasks(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].ID != task.ID || recovered[0].Status != domain.TaskSubmitted {
		t.Fatalf("unexpected recovered tasks: %+v", recovered)
	}
	got, err := st.GetTask(ctx, task.ID)
	if err != nil || got.Status != domain.TaskSubmitted || got.GenerationID != "generation-fixture" {
		t.Fatalf("task=%+v err=%v", got, err)
	}
	items, err := st.ClaimDispatchableOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TaskID != task.ID || items[0].Kind != "image" || !items[0].Recovery {
		t.Fatalf("unexpected outbox items: %+v", items)
	}
	claimedTask, _, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("recovered submitted ClaimTask claimed=%v err=%v", claimed, err)
	}
	if claimedTask.Status != domain.TaskSubmitted || claimedTask.GenerationID != "generation-fixture" {
		t.Fatalf("claimed task=%+v", claimedTask)
	}
}

func TestYieldSubmittedTaskLeaseQueuesRecovery(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "yield-submitted-outbox")
	claimedTask, leaseID, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed || claimedTask.AccountID == nil {
		t.Fatalf("ClaimTask task=%+v claimed=%v err=%v", claimedTask, claimed, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskSubmitted, 20, claimedTask.AccountID, "generation-fixture", nil, "", ""); err != nil || !ok {
		t.Fatalf("UpdateTaskOwned ok=%v err=%v", ok, err)
	}
	if ok, err := st.YieldTaskLease(ctx, task.ID, leaseID, time.Now().Add(-time.Second)); err != nil || !ok {
		t.Fatalf("YieldTaskLease ok=%v err=%v", ok, err)
	}
	got, err := st.GetTask(ctx, task.ID)
	if err != nil || got.Status != domain.TaskPolling || got.GenerationID != "generation-fixture" {
		t.Fatalf("task=%+v err=%v", got, err)
	}
	var leaseCleared, outboxPending bool
	if err := st.DB.QueryRow(ctx, `SELECT execution_lease_id IS NULL,
		EXISTS(SELECT 1 FROM task_outbox WHERE task_id=$1 AND delivered_at IS NULL)
		FROM tasks WHERE id=$1`, task.ID).Scan(&leaseCleared, &outboxPending); err != nil {
		t.Fatal(err)
	}
	if !leaseCleared || !outboxPending {
		t.Fatalf("leaseCleared=%v outboxPending=%v", leaseCleared, outboxPending)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "held" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	items, err := st.ClaimDispatchableOutbox(ctx, 10)
	if err != nil || len(items) != 1 || items[0].TaskID != task.ID || !items[0].Recovery {
		t.Fatalf("outbox items=%+v err=%v", items, err)
	}
}

func TestOwnedFailureBeforeSubmissionReleasesReservation(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "failure-release")
	_, leaseID, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("ClaimTask claimed=%v err=%v", claimed, err)
	}
	ok, err := st.FailTaskOwned(ctx, task.ID, leaseID, "validation_failed", "fixture failure", nil, nil, false)
	if err != nil || !ok {
		t.Fatalf("FailTaskOwned ok=%v err=%v", ok, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "released" || reservation.ReleaseReason != "validation_failed" {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	var ledgerState, ledgerReason string
	var ledgerEstimated, ledgerSettled int64
	if err := st.DB.QueryRow(ctx, `SELECT settlement_state,release_reason,estimated_tokens,settled_tokens
		FROM api_key_usage_ledger WHERE task_id=$1`, task.ID).Scan(
		&ledgerState, &ledgerReason, &ledgerEstimated, &ledgerSettled,
	); err != nil {
		t.Fatal(err)
	}
	if ledgerState != "released" || ledgerReason != "validation_failed" || ledgerEstimated != 80 || ledgerSettled != 0 {
		t.Fatalf("unexpected usage ledger state=%s reason=%s estimated=%d settled=%d", ledgerState, ledgerReason, ledgerEstimated, ledgerSettled)
	}
	if err := st.ReleaseReservation(ctx, task.ID, "duplicate_release"); err != nil {
		t.Fatal(err)
	}
	var ledgerCount int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM api_key_usage_ledger WHERE task_id=$1`, task.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 {
		t.Fatalf("usage ledger rows=%d, want exactly one", ledgerCount)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE api_key_usage_ledger SET settled_tokens=1 WHERE task_id=$1`, task.ID); err == nil {
		t.Fatal("usage ledger accepted a mutation")
	}
	if _, err := st.DB.Exec(ctx, `UPDATE account_reservations SET release_reason='tampered' WHERE task_id=$1`, task.ID); err == nil {
		t.Fatal("settled reservation accepted a mutation")
	}
}

func TestBalanceRefreshDoesNotClearLiveCooldown(t *testing.T) {
	ctx, st, _, _ := newRoutingFixture(t)
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(10 * time.Minute)
	if err := st.SetAccountError(ctx, accountID, "rate_limited", "fixture 429", &until); err != nil {
		t.Fatal(err)
	}
	version, err := st.BeginAccountBalanceRefresh(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := st.UpdateAccountTokens(ctx, accountID, version, time.Now(), "BASIC", 123, 0, 0)
	if err != nil || !updated {
		t.Fatal(err)
	}
	account, err := st.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != "rate_limited" || account.CooldownUntil == nil || account.CooldownUntil.Before(time.Now()) || account.LastError != "fixture 429" {
		t.Fatalf("cooldown was overwritten: %+v", account)
	}
	shorter := time.Now().Add(time.Minute)
	if err := st.SetAccountError(ctx, accountID, "cooldown", "ordinary failure", &shorter); err != nil {
		t.Fatal(err)
	}
	account, err = st.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != "rate_limited" || account.CooldownUntil.Before(until.Add(-time.Second)) || account.LastError != "fixture 429" {
		t.Fatalf("429 cooldown was downgraded: %+v", account)
	}
}

func TestBalanceRefreshFencingRejectsStaleWriter(t *testing.T) {
	ctx, st, _, _ := newRoutingFixture(t)
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	oldVersion, err := st.BeginAccountBalanceRefresh(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	newVersion, err := st.BeginAccountBalanceRefresh(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := st.UpdateAccountTokens(ctx, accountID, newVersion, time.Now(), "BASIC", 100, 0, 0)
	if err != nil || !updated {
		t.Fatalf("new refresh updated=%v err=%v", updated, err)
	}
	updated, err = st.UpdateAccountTokens(ctx, accountID, oldVersion, time.Now(), "BASIC", 500, 0, 0)
	if err != nil || updated {
		t.Fatalf("stale refresh updated=%v err=%v", updated, err)
	}
	account, err := st.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.TotalTokens() != 100 {
		t.Fatalf("stale balance overwrote current balance: %d", account.TotalTokens())
	}
	updated, err = st.UpdateAccountSession(ctx, accountID, oldVersion, "newer-session-cipher", "newer-cookie-cipher", time.Now().Add(time.Hour),
		time.Now().Add(-time.Minute), "hasura", "sub", "fixture@example.test", "BASIC", 900, 0, 0, "")
	if err != nil || updated {
		t.Fatalf("stale session balance updated=%v err=%v", updated, err)
	}
	account, err = st.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.TotalTokens() != 100 || account.AccessTokenCiphertext != "newer-session-cipher" || account.CookieCiphertext != "newer-cookie-cipher" {
		t.Fatalf("stale session refresh balance=%d token=%q cookie=%q", account.TotalTokens(), account.AccessTokenCiphertext, account.CookieCiphertext)
	}
}

func TestCostRuleUpdateCreatesImmutableVersion(t *testing.T) {
	ctx, st, _, _ := newRoutingFixture(t)
	model := "fixture-" + uuid.NewString()
	original, err := st.CreateModelCostRule(ctx, domain.ModelCostRule{Kind: "image", Model: model, Size: "1024x1024", UnitTokens: 10, Enabled: true, PriceVersion: "v1", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.DB.Exec(context.Background(), `DELETE FROM model_cost_rules WHERE model=$1`, model)
	})
	replacement, err := st.UpdateModelCostRule(ctx, domain.ModelCostRule{ID: original.ID, Kind: "image", Model: model, Size: "1024x1024", UnitTokens: 20, Enabled: true, PriceVersion: "v2", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == original.ID {
		t.Fatal("price update mutated the historical row")
	}
	var oldTokens int64
	var oldEnabled bool
	if err := st.DB.QueryRow(ctx, `SELECT unit_tokens,enabled FROM model_cost_rules WHERE id=$1`, original.ID).Scan(&oldTokens, &oldEnabled); err != nil {
		t.Fatal(err)
	}
	if oldTokens != 10 || oldEnabled || replacement.UnitTokens != 20 || !replacement.Enabled {
		t.Fatalf("old=(%d,%v) replacement=%+v", oldTokens, oldEnabled, replacement)
	}
}

func TestProviderReservationNeverCrossesAccountPools(t *testing.T) {
	ctx, st, keyID, _ := newRoutingFixture(t)
	providerID := "fixture-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `INSERT INTO providers(id,display_name,auth_type,credit_unit,capabilities) VALUES($1,'Fixture','api_key','credits','["video"]')`, providerID); err != nil {
		t.Fatal(err)
	}
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(provider_id,name,cookie_ciphertext,subscription_tokens,image_concurrency,status,access_token_expires_at,last_checked_at) VALUES($1,'fixture-provider-account','cipher',1000,2,'active',now()+interval '1 hour',now()) RETURNING id`, providerID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	rule, err := st.CreateModelCostRule(ctx, domain.ModelCostRule{ProviderID: providerID, Kind: "video", Model: "fixture-video", Resolution: "720p", Duration: 5, UnitTokens: 300, Enabled: true, PriceVersion: "v1", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	task, created, err := st.CreateReservedTaskForProvider(ctx, keyID, providerID, "video", "fixture-video", "fixture", domain.VideoRequest{Model: "fixture-video", Prompt: "fixture", Resolution: "720p", Duration: 5}, "provider-isolation", 300, rule.ID, time.Minute)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if task.ProviderID != providerID || task.AccountID == nil || *task.AccountID != accountID {
		t.Fatalf("task routed across provider pools: %+v", task)
	}
}

func TestRoutingSearchesBeyondTwentyInsufficientAccounts(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `DELETE FROM accounts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		SELECT 'insufficient-'||n,'cipher',7,1,1,'active',now()+interval '1 hour',now() FROM generate_series(1,20) n`); err != nil {
		t.Fatal(err)
	}
	var goodAccountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		VALUES('good-account','cipher',100,5,5,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&goodAccountID); err != nil {
		t.Fatal(err)
	}
	dummyID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO tasks(id,api_key_id,account_id,status,model,prompt,request,request_hash,estimated_tokens,pricing_rule_id)
		VALUES($1,$2,$3,'queued','gpt-image-2','dummy','{}',decode('00','hex'),1,$4)`, dummyID, keyID, goodAccountID, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO account_reservations(task_id,account_id,estimated_tokens,expires_at)
		VALUES($1,$2,1,now()+interval '1 hour')`, dummyID, goodAccountID); err != nil {
		t.Fatal(err)
	}
	task, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "route-all-accounts",
		domain.ImageRequest{Model: "gpt-image-2", Prompt: "route-all-accounts"}, "route-all-accounts", 8, ruleID, time.Minute)
	if err != nil || !created || task.AccountID == nil || *task.AccountID != goodAccountID {
		t.Fatalf("task=%+v created=%v err=%v", task, created, err)
	}
}

func TestCostAwareRoutingUsesBestFitAndPreservesVideoPool(t *testing.T) {
	ctx, st, keyID, imageRuleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `DELETE FROM accounts`); err != nil {
		t.Fatal(err)
	}
	var lowID, highID, reservedID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		VALUES('general-low','cipher',500,5,40,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&lowID); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		VALUES('general-high','cipher',8000,5,40,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&highID); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,routing_role,protected_tokens,video_reserved_slots,status,access_token_expires_at,last_checked_at)
		VALUES('video-reserved','cipher',8500,5,20,'video_reserved',6804,1,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&reservedID); err != nil {
		t.Fatal(err)
	}
	imageTask, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "best-fit",
		domain.ImageRequest{Model: "gpt-image-2", Prompt: "best-fit"}, "best-fit-image", 80, imageRuleID, time.Minute)
	if err != nil || !created || imageTask.AccountID == nil || *imageTask.AccountID != lowID {
		t.Fatalf("image task=%+v created=%v err=%v", imageTask, created, err)
	}
	highCostTask, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "high-cost",
		domain.ImageRequest{Model: "gpt-image-2", Prompt: "high-cost"}, "best-fit-high-cost", 2042, imageRuleID, time.Minute)
	if err != nil || !created || highCostTask.AccountID == nil || *highCostTask.AccountID != highID {
		t.Fatalf("high-cost task=%+v created=%v err=%v", highCostTask, created, err)
	}
	var videoRuleID int64
	if err := st.DB.QueryRow(ctx, `SELECT id FROM model_cost_rules WHERE provider_id='leonardo' AND kind='video' AND enabled=true LIMIT 1`).Scan(&videoRuleID); err != nil {
		t.Fatal(err)
	}
	videoTask, created, err := st.CreateReservedTask(ctx, keyID, "video", "seedance-2.0", "video-priority",
		domain.VideoRequest{Model: "seedance-2.0", Prompt: "video-priority", Resolution: "1080p", Duration: 10}, "best-fit-video", 6804, videoRuleID, time.Minute)
	if err != nil || !created || videoTask.AccountID == nil || *videoTask.AccountID != reservedID {
		t.Fatalf("video task=%+v created=%v err=%v", videoTask, created, err)
	}
}

func TestProtectedVideoBalanceBlocksOrdinarySpend(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `DELETE FROM accounts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,routing_role,protected_tokens,video_reserved_slots,status,access_token_expires_at,last_checked_at)
		VALUES('protected','cipher',7000,5,20,'video_reserved',6804,1,'active',now()+interval '1 hour',now())`); err != nil {
		t.Fatal(err)
	}
	_, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "protected",
		domain.ImageRequest{Model: "gpt-image-2", Prompt: "protected"}, "protected-image", 200, ruleID, time.Minute)
	if !errors.Is(err, ErrInsufficientPoolBalance) || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

func TestConcurrentImageBurstCannotConsumeProtectedVideoBalance(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `DELETE FROM accounts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		VALUES('general','cipher',1000,5,40,'active',now()+interval '1 hour',now())`); err != nil {
		t.Fatal(err)
	}
	var reservedID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,routing_role,protected_tokens,video_reserved_slots,status,access_token_expires_at,last_checked_at)
		VALUES('video-reserved','cipher',8500,5,20,'video_reserved',6804,1,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&reservedID); err != nil {
		t.Fatal(err)
	}
	const requests = 30
	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsSeen := make([]error, 0)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, _, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "burst",
				domain.ImageRequest{Model: "gpt-image-2", Prompt: "burst"}, fmt.Sprintf("protected-burst-%d", index), 80, ruleID, time.Minute)
			if err != nil {
				mu.Lock()
				errorsSeen = append(errorsSeen, err)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(errorsSeen) != 0 {
		t.Fatalf("concurrent routing errors=%v", errorsSeen)
	}
	var balance, held int64
	if err := st.DB.QueryRow(ctx, `SELECT subscription_tokens+rollover_tokens+paid_tokens,
		COALESCE((SELECT sum(estimated_tokens) FROM account_reservations WHERE account_id=$1 AND state='held'),0)
		FROM accounts WHERE id=$1`, reservedID).Scan(&balance, &held); err != nil {
		t.Fatal(err)
	}
	if balance-held < 6804 {
		t.Fatalf("protected balance was consumed: balance=%d held=%d", balance, held)
	}
}

func TestVideoPriorityBorrowsAndReclaimsExecutionSlot(t *testing.T) {
	ctx, st, keyID, imageRuleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET subscription_tokens=5000,image_concurrency=2,queue_capacity=5,
		routing_role='video_reserved',protected_tokens=1000,video_reserved_slots=1`); err != nil {
		t.Fatal(err)
	}
	first := createFixtureTask(t, ctx, st, keyID, imageRuleID, "borrow-image-1")
	if _, _, claimed, err := st.ClaimTask(ctx, first.ID, time.Minute); err != nil || !claimed {
		t.Fatalf("first image claimed=%v err=%v", claimed, err)
	}
	second := createFixtureTask(t, ctx, st, keyID, imageRuleID, "borrow-image-2")
	var videoRuleID int64
	if err := st.DB.QueryRow(ctx, `SELECT id FROM model_cost_rules WHERE provider_id='leonardo' AND kind='video' AND enabled=true LIMIT 1`).Scan(&videoRuleID); err != nil {
		t.Fatal(err)
	}
	video, created, err := st.CreateReservedTask(ctx, keyID, "video", "seedance-2.0", "priority",
		domain.VideoRequest{Model: "seedance-2.0", Prompt: "priority", Resolution: "720p", Duration: 4}, "priority-video", 100, videoRuleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("video created=%v err=%v", created, err)
	}
	if _, _, claimed, err := st.ClaimTask(ctx, second.ID, time.Minute); err != nil || claimed {
		t.Fatalf("second image claimed=%v err=%v", claimed, err)
	}
	if _, _, claimed, err := st.ClaimTask(ctx, video.ID, time.Minute); err != nil || !claimed {
		t.Fatalf("video claimed=%v err=%v", claimed, err)
	}
}

func TestQueuedTasksClaimInAccountFIFOOrder(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET image_concurrency=1,queue_capacity=5,subscription_tokens=1000`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		createFixtureTask(t, ctx, st, keyID, ruleID, fmt.Sprintf("fifo-%d", i))
		time.Sleep(2 * time.Millisecond)
	}
	rows, err := st.DB.Query(ctx, `SELECT id FROM tasks ORDER BY created_at,id`)
	if err != nil {
		t.Fatal(err)
	}
	var ordered []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ordered = append(ordered, id)
	}
	rows.Close()
	if len(ordered) != 3 {
		t.Fatalf("ordered tasks=%d", len(ordered))
	}
	if _, _, claimed, err := st.ClaimTask(ctx, ordered[1], time.Minute); err != nil || claimed {
		t.Fatalf("later task claimed=%v err=%v", claimed, err)
	}
	_, firstLease, claimed, err := st.ClaimTask(ctx, ordered[0], time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first task claimed=%v err=%v", claimed, err)
	}
	if ok, err := st.FailTaskOwned(ctx, ordered[0], firstLease, "fixture_done", "done", nil, nil, false); err != nil || !ok {
		t.Fatalf("finish first ok=%v err=%v", ok, err)
	}
	if _, _, claimed, err := st.ClaimTask(ctx, ordered[1], time.Minute); err != nil || !claimed {
		t.Fatalf("second task claimed=%v err=%v", claimed, err)
	}
}

func TestWakeQueuedTasksSkipsLockedTaskRows(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	first := createFixtureTask(t, ctx, st, keyID, ruleID, "wake-locked-first")
	time.Sleep(2 * time.Millisecond)
	second := createFixtureTask(t, ctx, st, keyID, ruleID, "wake-locked-second")
	if first.AccountID == nil || second.AccountID == nil || *first.AccountID != *second.AccountID {
		t.Fatalf("fixture tasks were not routed to the same account: first=%v second=%v", first.AccountID, second.AccountID)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE task_outbox SET next_attempt_at=now()+interval '1 hour'
		WHERE task_id=ANY($1::uuid[])`, []uuid.UUID{first.ID, second.ID}); err != nil {
		t.Fatal(err)
	}

	lockedTx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lockedTx.Rollback(context.Background()) })
	var lockedID uuid.UUID
	if err := lockedTx.QueryRow(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, first.ID).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}

	wakeTx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wakeTx.Rollback(context.Background()) })
	wakeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := wakeQueuedTasksTx(wakeCtx, wakeTx, *first.AccountID, &keyID); err != nil {
		t.Fatalf("wake should skip a task row held by another worker: %v", err)
	}
	if err := wakeTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := lockedTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var firstReady, secondReady bool
	if err := st.DB.QueryRow(ctx, `SELECT next_attempt_at<=now() FROM task_outbox WHERE task_id=$1`, first.ID).Scan(&firstReady); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `SELECT next_attempt_at<=now() FROM task_outbox WHERE task_id=$1`, second.ID).Scan(&secondReady); err != nil {
		t.Fatal(err)
	}
	if firstReady || !secondReady {
		t.Fatalf("locked task ready=%v, unlocked task ready=%v", firstReady, secondReady)
	}
}

func TestQueuedTaskReroutesFromInvalidAccount(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	var secondAccountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		VALUES('second','cipher',500,2,5,'active',now()+interval '1 hour',now()) RETURNING id`).Scan(&secondAccountID); err != nil {
		t.Fatal(err)
	}
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "reroute-invalid")
	if task.AccountID == nil {
		t.Fatal("task has no account")
	}
	oldAccountID := *task.AccountID
	var targetAccountID uuid.UUID
	if oldAccountID == secondAccountID {
		if err := st.DB.QueryRow(ctx, `SELECT id FROM accounts WHERE id<>$1`, oldAccountID).Scan(&targetAccountID); err != nil {
			t.Fatal(err)
		}
	} else {
		targetAccountID = secondAccountID
	}
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET status='invalid',last_error='fixture invalid' WHERE id=$1`, oldAccountID); err != nil {
		t.Fatal(err)
	}
	claimedTask, _, claimed, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !claimed || claimedTask.AccountID == nil || *claimedTask.AccountID != targetAccountID {
		t.Fatalf("claimed=%v task=%+v err=%v", claimed, claimedTask, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.AccountID != targetAccountID {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
}

func completeFixtureGeneration(t *testing.T, ctx context.Context, st *Store, task domain.Task, generationID string) domain.Task {
	t.Helper()
	claimed, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim task=%+v ok=%v err=%v", claimed, ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskSubmitted, 20, claimed.AccountID, "", nil, "", ""); err != nil || !ok {
		t.Fatalf("submit state ok=%v err=%v", ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskPolling, 30, claimed.AccountID, generationID, nil, "", ""); err != nil || !ok {
		t.Fatalf("poll state ok=%v err=%v", ok, err)
	}
	if ok, err := st.CompleteTaskOwned(ctx, task.ID, leaseID, *claimed.AccountID, generationID, map[string]any{"data": []any{}}, nil, false); err != nil || !ok {
		t.Fatalf("complete state ok=%v err=%v", ok, err)
	}
	completed, err := st.GetTask(ctx, task.ID)
	if err != nil || completed.CompletedAt == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	return completed
}

func TestLocalLedgerSettlementIsAtomicAndIdempotent(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "local-ledger-success")
	accountID := *task.AccountID
	completed := completeFixtureGeneration(t, ctx, st, task, "generation-success")
	reservation, err := st.ReservationForTask(ctx, completed.ID)
	if err != nil || reservation.State != "consumed" || reservation.SettledTokens == nil || *reservation.SettledTokens != 80 {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	var balance, held int64
	if err := st.DB.QueryRow(ctx, `SELECT subscription_tokens+rollover_tokens+paid_tokens,
		COALESCE((SELECT sum(estimated_tokens) FROM account_reservations WHERE account_id=$1 AND state='held'),0)
		FROM accounts WHERE id=$1`, accountID).Scan(&balance, &held); err != nil {
		t.Fatal(err)
	}
	if balance != 420 || held != 0 {
		t.Fatalf("balance=%d held=%d, want 420/0", balance, held)
	}
	var settledTotal int64
	if err := st.DB.QueryRow(ctx, `SELECT COALESCE(sum(settled_tokens),0) FROM api_key_usage_ledger WHERE task_id=$1`, completed.ID).Scan(&settledTotal); err != nil {
		t.Fatal(err)
	}
	if settledTotal != 80 {
		t.Fatalf("settled total=%d, want 80", settledTotal)
	}
}

func TestUpstreamReportedCostIsObservationalOnly(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "upstream-reported-cost")
	claimed, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim task=%+v ok=%v err=%v", claimed, ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskSubmitted, 20, claimed.AccountID, "", nil, "", ""); err != nil || !ok {
		t.Fatalf("submit state ok=%v err=%v", ok, err)
	}
	reported := 73.5
	if ok, err := st.RecordTaskSubmissionOwned(ctx, task.ID, leaseID, *claimed.AccountID, "generation-reported", &reported); err != nil || !ok {
		t.Fatalf("record submission ok=%v err=%v", ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskPolling, 30, claimed.AccountID, "generation-reported", nil, "", ""); err != nil || !ok {
		t.Fatalf("poll state ok=%v err=%v", ok, err)
	}
	if ok, err := st.CompleteTaskOwned(ctx, task.ID, leaseID, *claimed.AccountID, "generation-reported", map[string]any{"data": []any{}}, nil, false); err != nil || !ok {
		t.Fatalf("complete state ok=%v err=%v", ok, err)
	}

	adminTasks, err := st.ListAdminTasks(ctx, 10)
	if err != nil || len(adminTasks) != 1 || adminTasks[0].UpstreamReportedCost == nil || *adminTasks[0].UpstreamReportedCost != reported {
		t.Fatalf("admin tasks=%+v err=%v", adminTasks, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.SettledTokens == nil || *reservation.SettledTokens != 80 {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	var balance int64
	if err := st.DB.QueryRow(ctx, `SELECT subscription_tokens+rollover_tokens+paid_tokens FROM accounts WHERE id=$1`, *claimed.AccountID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 420 {
		t.Fatalf("balance=%d, want local estimate settlement of 420", balance)
	}
	costs, err := st.ListModelCosts(ctx, 10)
	if err != nil || len(costs) != 1 || costs[0].Average != 80 || costs[0].UpstreamReportedAverage == nil || *costs[0].UpstreamReportedAverage != reported {
		t.Fatalf("costs=%+v err=%v", costs, err)
	}
}

func TestFailedSubmittedTaskReleasesLocalReservationWithoutCharge(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	task := createFixtureTask(t, ctx, st, keyID, ruleID, "local-ledger-failure")
	accountID := *task.AccountID
	claimed, leaseID, ok, err := st.ClaimTask(ctx, task.ID, time.Minute)
	if err != nil || !ok || claimed.AccountID == nil {
		t.Fatalf("claim task=%+v ok=%v err=%v", claimed, ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskSubmitted, 20, claimed.AccountID, "", nil, "", ""); err != nil || !ok {
		t.Fatalf("submit task ok=%v err=%v", ok, err)
	}
	if ok, err := st.UpdateTaskOwned(ctx, task.ID, leaseID, domain.TaskPolling, 30, claimed.AccountID, "generation-failed", nil, "", ""); err != nil || !ok {
		t.Fatalf("poll task ok=%v err=%v", ok, err)
	}
	if ok, err := st.FailSubmittedTaskOwned(ctx, task.ID, leaseID, "upstream_failed", "fixture failure", nil); err != nil || !ok {
		t.Fatalf("fail task ok=%v err=%v", ok, err)
	}
	reservation, err := st.ReservationForTask(ctx, task.ID)
	if err != nil || reservation.State != "released" || reservation.SettledTokens == nil || *reservation.SettledTokens != 0 {
		t.Fatalf("reservation=%+v err=%v", reservation, err)
	}
	var balance, held int64
	if err := st.DB.QueryRow(ctx, `SELECT subscription_tokens+rollover_tokens+paid_tokens,
		COALESCE((SELECT sum(estimated_tokens) FROM account_reservations WHERE account_id=$1 AND state='held'),0)
		FROM accounts WHERE id=$1`, accountID).Scan(&balance, &held); err != nil {
		t.Fatal(err)
	}
	if balance != 500 || held != 0 {
		t.Fatalf("balance=%d held=%d, want 500/0", balance, held)
	}
}

func TestThousandAccountPoolConcurrentReservations(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	capacity, err := st.GetSystemCapacity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capacity.MaxQueued = 2000
	capacity.QueueHighWatermark = 1800
	capacity.QueueResumeWatermark = 1500
	if _, err := st.UpdateSystemCapacity(ctx, capacity.SystemCapacityConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE api_keys SET concurrency_limit=1000 WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `DELETE FROM accounts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,subscription_tokens,image_concurrency,queue_capacity,status,access_token_expires_at,last_checked_at)
		SELECT 'scale-'||n,'cipher',100,1,1,'active',now()+interval '1 hour',now() FROM generate_series(1,1000) n`); err != nil {
		t.Fatal(err)
	}
	const requests = 1000
	var wg sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	errorsSeen := make([]error, 0)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "scale",
				domain.ImageRequest{Model: "gpt-image-2", Prompt: "scale"}, fmt.Sprintf("scale-%d", index), 8, ruleID, time.Minute)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsSeen = append(errorsSeen, err)
			} else if created {
				createdCount++
			}
		}(i)
	}
	wg.Wait()
	if len(errorsSeen) > 0 || createdCount != requests {
		t.Fatalf("created=%d errors=%v", createdCount, errorsSeen)
	}
	var violations int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT a.id FROM accounts a JOIN account_reservations r ON r.account_id=a.id AND r.state='held'
		GROUP BY a.id,a.subscription_tokens,a.image_concurrency,a.queue_capacity
		HAVING sum(r.estimated_tokens)>a.subscription_tokens OR count(*)>a.image_concurrency+a.queue_capacity
	) invalid`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("violations=%d err=%v", violations, err)
	}
}
