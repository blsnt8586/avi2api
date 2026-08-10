package store

import (
	"context"
	"os"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestSchedulingHealthQuery(t *testing.T) {
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
	health, err := st.GetSchedulingHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if health.QueueOldest < 0 || health.OutboxOldest < 0 || health.OutboxPending < 0 || health.ExpiredLeases < 0 || health.UncertainTasks < 0 {
		t.Fatalf("invalid health values: %+v", health)
	}
}
