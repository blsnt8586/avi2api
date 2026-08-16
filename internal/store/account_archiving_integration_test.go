package store

import (
	"errors"
	"testing"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func TestArchiveAccountRejectsHeldWork(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	account, err := st.ListAccounts(ctx)
	if err != nil || len(account) != 1 {
		t.Fatalf("list accounts: count=%d err=%v", len(account), err)
	}
	_, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "archive-held", domain.ImageRequest{
		Model: "gpt-image-2", Prompt: "archive-held",
	}, "archive-held", 8, ruleID, time.Minute)
	if err != nil || !created {
		t.Fatalf("create task: created=%v err=%v", created, err)
	}

	err = st.ArchiveAccount(ctx, account[0].ID)
	var inUse *AccountInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("archive error=%v, want AccountInUseError", err)
	}
	if inUse.ActiveTasks != 1 || inUse.HeldReservations != 1 {
		t.Fatalf("archive details=%+v", inUse)
	}
	var archived bool
	if err := st.DB.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM accounts WHERE id=$1`, account[0].ID).Scan(&archived); err != nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
}

func TestArchiveAccountRemovesItFromOperationsAndRefresh(t *testing.T) {
	ctx, st, keyID, ruleID := newRoutingFixture(t)
	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("list accounts: count=%d err=%v", len(accounts), err)
	}
	accountID := accounts[0].ID
	job, err := st.EnqueueSessionRefreshJob(ctx, accountID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveAccount(ctx, accountID); err != nil {
		t.Fatal(err)
	}

	var archived bool
	var status string
	var refreshEnabled bool
	if err := st.DB.QueryRow(ctx, `SELECT archived_at IS NOT NULL,status,session_refresh_enabled FROM accounts WHERE id=$1`, accountID).Scan(&archived, &status, &refreshEnabled); err != nil {
		t.Fatal(err)
	}
	if !archived || status != "disabled" || refreshEnabled {
		t.Fatalf("archived=%v status=%s refresh_enabled=%v", archived, status, refreshEnabled)
	}
	storedJob, err := st.GetSessionRefreshJob(ctx, job.ID)
	if err != nil || storedJob.Status != "cancelled" {
		t.Fatalf("refresh job=%+v err=%v", storedJob, err)
	}
	page, err := st.ListAccountsPage(ctx, 1, 20, "")
	if err != nil || page.Total != 0 || len(page.Data) != 0 {
		t.Fatalf("account page=%+v err=%v", page, err)
	}
	overview, err := st.GetAccountOverview(ctx)
	if err != nil || overview.TotalAccounts != 0 {
		t.Fatalf("overview=%+v err=%v", overview, err)
	}
	if _, err := st.EnqueueSessionRefreshJob(ctx, accountID, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enqueue archived account error=%v", err)
	}
	if _, created, err := st.CreateReservedTask(ctx, keyID, "image", "gpt-image-2", "archive-route", domain.ImageRequest{
		Model: "gpt-image-2", Prompt: "archive-route",
	}, "archive-route", 8, ruleID, time.Minute); created || !errors.Is(err, ErrNoHealthyAccount) {
		t.Fatalf("create after archive: created=%v err=%v", created, err)
	}
}
