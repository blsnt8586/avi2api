package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestTaskOverviewUsesTerminalCompletionTime(t *testing.T) {
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
	if _, err := st.DB.Exec(ctx, `TRUNCATE task_outbox,task_events,tasks RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	fixtures := []struct {
		status      string
		createdAt   time.Time
		completedAt *time.Time
		hash        string
	}{
		{status: "failed", createdAt: old, completedAt: &now, hash: "01"},
		{status: "succeeded", createdAt: old, completedAt: &now, hash: "02"},
		{status: "failed", createdAt: old, completedAt: &old, hash: "03"},
		{status: "queued", createdAt: old, hash: "04"},
	}
	for _, fixture := range fixtures {
		if _, err := st.DB.Exec(ctx, `INSERT INTO tasks(status,kind,model,prompt,request,request_hash,created_at,updated_at,completed_at)
			VALUES($1,'image','fixture','fixture','{}',decode($2,'hex'),$3,$3,$4)`,
			fixture.status, fixture.hash, fixture.createdAt, fixture.completedAt); err != nil {
			t.Fatal(err)
		}
	}

	overview, err := st.GetTaskOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Total != 4 || overview.FailedLastHour != 1 {
		t.Fatalf("unexpected totals: %+v", overview)
	}
	if overview.Counts["queued"] != 1 || overview.Counts["failed"] != 1 || overview.Counts["succeeded"] != 1 {
		t.Fatalf("unexpected status counts: %+v", overview.Counts)
	}
}
