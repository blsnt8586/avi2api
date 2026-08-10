package jobs

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

func TestEnqueueKindReactivatesArchivedTask(t *testing.T) {
	redisAddr := os.Getenv("LEO_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("LEO_TEST_REDIS_ADDR is not set")
	}
	opt := asynq.RedisClientOpt{Addr: redisAddr}
	client := NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()

	id := uuid.New()
	t.Cleanup(func() { _ = inspector.DeleteTask("images", id.String()) })
	if err := client.EnqueueKind(t.Context(), id, "image"); err != nil {
		t.Fatal(err)
	}
	if err := inspector.ArchiveTask("images", id.String()); err != nil {
		t.Fatal(err)
	}
	if err := client.EnqueueKind(t.Context(), id, "image"); err != nil {
		t.Fatal(err)
	}
	info, err := inspector.GetTaskInfo("images", id.String())
	if err != nil {
		t.Fatal(err)
	}
	if info.State != asynq.TaskStatePending {
		t.Fatalf("archived task state=%s, want pending", info.State)
	}
}
