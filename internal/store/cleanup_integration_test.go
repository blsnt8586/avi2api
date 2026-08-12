package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestCleanupHistoryOnlyDeletesExpiredTerminalRecords(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE api_request_logs,session_refresh_jobs,account_reconciliation_batches,audit_logs,task_outbox,task_events,tasks,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour)
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status) VALUES('cleanup-account','cipher','active') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	var terminalTaskID, activeTaskID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO tasks(status,kind,model,prompt,request,request_hash,created_at,updated_at,completed_at)
		VALUES('succeeded','image','fixture','fixture','{}',decode('00','hex'),$1,$1,$1) RETURNING id`, old).Scan(&terminalTaskID); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `INSERT INTO tasks(status,kind,model,prompt,request,request_hash,created_at,updated_at)
		VALUES('queued','image','fixture','fixture','{}',decode('01','hex'),$1,$1) RETURNING id`, old).Scan(&activeTaskID); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO task_events(task_id,status,created_at) VALUES($1,'succeeded',$3),($2,'queued',$3)`, []any{terminalTaskID, activeTaskID, old}},
		{`INSERT INTO task_outbox(task_id,kind,delivered_at,created_at,updated_at) VALUES($1,'image',$2,$2,$2)`, []any{terminalTaskID, old}},
		{`INSERT INTO session_refresh_jobs(account_id,stage,status,completed_at,created_at,updated_at) VALUES($1,'browser','succeeded',$2,$2,$2)`, []any{accountID, old}},
		{`INSERT INTO session_refresh_jobs(account_id,stage,status,completed_at,created_at,updated_at) VALUES($1,'browser','failed',$2,$2,$2)`, []any{accountID, old}},
		{`INSERT INTO audit_logs(actor,action,created_at) VALUES('fixture','expired',$1),('fixture','current',$2)`, []any{old, now}},
		{`INSERT INTO api_request_logs(request_id,method,path,status,duration_ms,created_at)
			VALUES('expired','GET','/v1/models',200,1,$1),('current','GET','/v1/models',200,1,$2)`, []any{old, now}},
		{`INSERT INTO account_reconciliation_batches(account_id,snapshot_version,snapshot_started_at,baseline_tokens,observed_tokens,observed_spend,expected_tokens,reservation_count,unresolved_tokens,unresolved_count,candidate_signature,status,created_at)
			VALUES($1,1,$2,100,100,0,0,0,0,0,'fixture','ok',$2)`, []any{accountID, old}},
	}
	for _, fixture := range fixtures {
		if _, err := st.DB.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}

	result, err := st.CleanupHistory(ctx, now, HistoryRetention{
		TaskEvents: 90 * 24 * time.Hour, Outbox: 30 * 24 * time.Hour,
		SessionJobs: 30 * 24 * time.Hour, APIRequests: 30 * 24 * time.Hour, AuditLogs: 90 * 24 * time.Hour,
		Reconciliations: 90 * 24 * time.Hour, BatchSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskEvents != 1 || result.Outbox != 1 || result.SessionJobs != 2 || result.APIRequests != 1 || result.AuditLogs != 1 || result.Reconciliations != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	var activeEvents, currentRequests, currentAudits, tasks int
	if err := st.DB.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM task_events WHERE task_id=$1),
		(SELECT count(*) FROM api_request_logs WHERE request_id='current'),
		(SELECT count(*) FROM audit_logs WHERE action='current'),
		(SELECT count(*) FROM tasks)`, activeTaskID).Scan(&activeEvents, &currentRequests, &currentAudits, &tasks); err != nil {
		t.Fatal(err)
	}
	if activeEvents != 1 || currentRequests != 1 || currentAudits != 1 || tasks != 2 {
		t.Fatalf("cleanup removed protected records: active_events=%d current_requests=%d current_audits=%d tasks=%d", activeEvents, currentRequests, currentAudits, tasks)
	}
}
