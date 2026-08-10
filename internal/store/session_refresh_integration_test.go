package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestSessionRefreshQueueScalesToOneThousandAccounts(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	for start := 0; start < 1000; start += 100 {
		if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,access_token_expires_at,status,last_checked_at)
			SELECT 'refresh-'||n,'cipher',now()+interval '5 minutes','active',now()
			FROM generate_series($1::integer,$2::integer) n`, start, start+99); err != nil {
			t.Fatal(err)
		}
	}
	page, err := st.ListAccountsPage(ctx, 50, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1000 || len(page.Data) != 20 || page.Page != 50 {
		t.Fatalf("unexpected account page: total=%d rows=%d page=%d", page.Total, len(page.Data), page.Page)
	}
	filtered, err := st.ListAccountsPage(ctx, 1, 20, "refresh-777")
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].Name != "refresh-777" {
		t.Fatalf("unexpected filtered account page: %+v", filtered)
	}
	overview, err := st.GetAccountOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalAccounts != 1000 || overview.ActiveAccounts != 1000 {
		t.Fatalf("unexpected account overview: %+v", overview)
	}
	created, err := st.EnqueueDueSessionRefreshJobs(ctx, 15*time.Minute, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1000 {
		t.Fatalf("created=%d, want 1000", created)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := make(map[uuid.UUID]uuid.UUID, 1000)
	for worker := 0; worker < 64; worker++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for {
				job, ok, err := st.ClaimSessionRefreshJob(ctx, "cookie", fmt.Sprintf("worker-%d", index), "", time.Minute)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if !ok {
					return
				}
				mu.Lock()
				if prior, exists := claimed[job.AccountID]; exists {
					t.Errorf("account %s claimed twice by jobs %s and %s", job.AccountID, prior, job.ID)
				}
				claimed[job.AccountID] = job.ID
				mu.Unlock()
			}
		}(worker)
	}
	wg.Wait()
	if len(claimed) != 1000 {
		t.Fatalf("claimed=%d, want 1000", len(claimed))
	}

	var duplicateAccounts int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT account_id FROM session_refresh_jobs WHERE status='leased' GROUP BY account_id HAVING count(*)>1
	) duplicates`).Scan(&duplicateAccounts); err != nil {
		t.Fatal(err)
	}
	if duplicateAccounts != 0 {
		t.Fatalf("duplicate leased accounts=%d", duplicateAccounts)
	}
}

func TestSessionRefreshLeaseLifecycle(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status,last_checked_at)
		VALUES('lease-test','cipher','active',now()) RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueSessionRefreshJob(ctx, accountID, 10); err != nil {
		t.Fatal(err)
	}
	cookieJob, ok, err := st.ClaimSessionRefreshJob(ctx, "cookie", "cookie-worker", "", time.Minute)
	if err != nil || !ok {
		t.Fatalf("cookie claim ok=%v err=%v", ok, err)
	}
	if err := st.RequireBrowserSessionRefresh(ctx, cookieJob.ID, *cookieJob.LeaseToken, "cookie stale"); err != nil {
		t.Fatal(err)
	}
	browserJob, ok, err := st.ClaimSessionRefreshJob(ctx, "browser", "browser-worker", "default", time.Minute)
	if err != nil || !ok {
		t.Fatalf("browser claim ok=%v err=%v", ok, err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET last_checked_at=now(),access_token_expires_at=now()+interval '5 minutes' WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteSessionRefreshJob(ctx, browserJob.ID, *browserJob.LeaseToken, "browser", time.Second, 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	deferred, err := st.GetSessionRefreshJob(ctx, browserJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Status != "pending" || deferred.Stage != "browser" || deferred.AttemptCount != browserJob.AttemptCount-1 || deferred.LastError != "browser authenticated; JWT rotation deferred by upstream" {
		t.Fatalf("unexpected deferred rotation job: %+v", deferred)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE session_refresh_jobs SET next_attempt_at=now() WHERE id=$1`, browserJob.ID); err != nil {
		t.Fatal(err)
	}
	browserJob, ok, err = st.ClaimSessionRefreshJob(ctx, "browser", "browser-worker", "default", time.Minute)
	if err != nil || !ok {
		t.Fatalf("deferred browser claim ok=%v err=%v", ok, err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE accounts SET last_checked_at=now(),access_token_expires_at=now()+interval '1 hour' WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteSessionRefreshJob(ctx, browserJob.ID, *browserJob.LeaseToken, "browser", time.Second, 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	completed, err := st.GetSessionRefreshJob(ctx, browserJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("status=%s, want succeeded", completed.Status)
	}
}

func TestActivateExpiredAccountCooldowns(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status,last_error,cooldown_until) VALUES
		('expired-rate-limit','cipher','rate_limited','429',now()-interval '1 second'),
		('active-cooldown','cipher','cooldown','network',now()+interval '1 hour'),
		('invalid-account','cipher','invalid','login rejected',now()-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	restored, err := st.ActivateExpiredAccountCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatalf("restored=%d, want 1", restored)
	}
	rows, err := st.DB.Query(ctx, `SELECT name,status,last_error FROM accounts ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string][2]string{
		"expired-rate-limit": {"active", ""},
		"active-cooldown":    {"cooldown", "network"},
		"invalid-account":    {"invalid", "login rejected"},
	}
	for rows.Next() {
		var name, status, lastError string
		if err := rows.Scan(&name, &status, &lastError); err != nil {
			t.Fatal(err)
		}
		if got := [2]string{status, lastError}; got != want[name] {
			t.Fatalf("account %s=%v, want %v", name, got, want[name])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionImportPreservesGenerationRateLimit(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var generationID, sessionID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status,last_error,cooldown_until)
		VALUES('generation-limit','cipher','rate_limited','upstream generation HTTP 429',now()+interval '10 minutes') RETURNING id`).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status,last_error,cooldown_until)
		VALUES('session-limit','cipher','rate_limited','Leonardo HTTP 429: temporary Vercel security checkpoint',now()+interval '10 minutes') RETURNING id`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{generationID, sessionID} {
		if err := st.UpdateAccountSessionCredentials(ctx, id, "fresh-token", "fresh-cookie", time.Now().Add(time.Hour), "hasura", "sub", "user@example.com", "ua"); err != nil {
			t.Fatal(err)
		}
	}
	generation, err := st.GetAccount(ctx, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if generation.Status != "rate_limited" || generation.CooldownUntil == nil || generation.LastError != "upstream generation HTTP 429" {
		t.Fatalf("generation cooldown was cleared by session import: %+v", generation)
	}
	session, err := st.GetAccount(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "active" || session.CooldownUntil != nil || session.LastError != "" {
		t.Fatalf("session checkpoint was not cleared after successful import: %+v", session)
	}
}

func TestBrowserSessionCompletionDefersOnlyBalanceRefresh(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var accountID uuid.UUID
	if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,status,last_error,session_refresh_failures,last_checked_at)
		VALUES('deferred-balance','old-cookie','cooldown','old browser failure',2,now()-interval '30 minutes') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueSessionRefreshJob(ctx, accountID, 10); err != nil {
		t.Fatal(err)
	}
	cookieJob, ok, err := st.ClaimSessionRefreshJob(ctx, "cookie", "cookie-worker", "", time.Minute)
	if err != nil || !ok {
		t.Fatalf("cookie claim ok=%v err=%v", ok, err)
	}
	if err := st.RequireBrowserSessionRefresh(ctx, cookieJob.ID, *cookieJob.LeaseToken, "browser required"); err != nil {
		t.Fatal(err)
	}
	browserJob, ok, err := st.ClaimSessionRefreshJob(ctx, "browser", "browser-worker", "default", time.Minute)
	if err != nil || !ok {
		t.Fatalf("browser claim ok=%v err=%v", ok, err)
	}
	expiry := time.Now().Add(time.Hour)
	if err := st.UpdateAccountSessionCredentials(ctx, accountID, "fresh-token", "fresh-cookie", expiry, "hasura", "sub", "user@example.com", "ua"); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteSessionRefreshJob(ctx, browserJob.ID, *browserJob.LeaseToken, "browser", time.Second, 20*time.Minute); err != nil {
		t.Fatal(err)
	}

	pending, err := st.GetSessionRefreshJob(ctx, browserJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "pending" || pending.Stage != "cookie" || pending.LastError != "session restored; balance refresh deferred" {
		t.Fatalf("unexpected deferred balance job: %+v", pending)
	}
	account, err := st.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != "active" || account.LastError != "" || account.SessionRefreshFailures != 0 {
		t.Fatalf("session was not restored independently of balance: %+v", account)
	}
	if account.AccessTokenExpiresAt == nil || !account.AccessTokenExpiresAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatalf("fresh JWT was not persisted: %+v", account.AccessTokenExpiresAt)
	}
}

func TestBrowserSessionRefreshClaimsStayInsideWorkerGroup(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE session_refresh_jobs,account_reservations,task_events,tasks,api_keys,accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	groups := []string{"proxy-west", "proxy-east"}
	accounts := make(map[string]uuid.UUID, len(groups))
	for _, group := range groups {
		var accountID uuid.UUID
		if err := st.DB.QueryRow(ctx, `INSERT INTO accounts(name,cookie_ciphertext,browser_worker_group,status,last_checked_at)
			VALUES($1,'cipher',$1,'active',now()) RETURNING id`, group).Scan(&accountID); err != nil {
			t.Fatal(err)
		}
		accounts[group] = accountID
		job, err := st.EnqueueSessionRefreshJob(ctx, accountID, 0)
		if err != nil {
			t.Fatal(err)
		}
		cookieJob, ok, err := st.ClaimSessionRefreshJob(ctx, "cookie", "cookie-worker", "", time.Minute)
		if err != nil || !ok {
			t.Fatalf("cookie claim ok=%v err=%v", ok, err)
		}
		if cookieJob.ID != job.ID {
			t.Fatalf("claimed job %s, want %s", cookieJob.ID, job.ID)
		}
		if err := st.RequireBrowserSessionRefresh(ctx, cookieJob.ID, *cookieJob.LeaseToken, "browser required"); err != nil {
			t.Fatal(err)
		}
	}

	west, ok, err := st.ClaimSessionRefreshJob(ctx, "browser", "west-worker", "proxy-west", time.Minute)
	if err != nil || !ok {
		t.Fatalf("west claim ok=%v err=%v", ok, err)
	}
	if west.AccountID != accounts["proxy-west"] {
		t.Fatalf("west worker claimed account %s", west.AccountID)
	}
	east, ok, err := st.ClaimSessionRefreshJob(ctx, "browser", "east-worker", "proxy-east", time.Minute)
	if err != nil || !ok {
		t.Fatalf("east claim ok=%v err=%v", ok, err)
	}
	if east.AccountID != accounts["proxy-east"] {
		t.Fatalf("east worker claimed account %s", east.AccountID)
	}
}
