package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	TypeImage = "image:generate"
	TypeVideo = "video:generate"
	TypeAudio = "audio:generate"
)

type Payload struct {
	TaskID uuid.UUID `json:"task_id"`
}

type Client struct {
	q         *asynq.Client
	inspector *asynq.Inspector
}

func NewClient(opt asynq.RedisClientOpt) *Client {
	return &Client{q: asynq.NewClient(opt), inspector: asynq.NewInspector(opt)}
}
func (c *Client) Close() error     { return errors.Join(c.q.Close(), c.inspector.Close()) }
func WorkerQueues() map[string]int { return map[string]int{"images": 8, "videos": 2, "audio": 2} }

func (c *Client) EnqueueKind(ctx context.Context, id uuid.UUID, kind string) error {
	taskType, queue, err := queueForKind(kind)
	if err != nil {
		return err
	}
	return c.enqueue(ctx, id, taskType, queue, id.String())
}

func (c *Client) Enqueue(ctx context.Context, id uuid.UUID) error {
	return c.enqueue(ctx, id, TypeImage, "images", id.String())
}

func (c *Client) EnqueueVideo(ctx context.Context, id uuid.UUID) error {
	return c.enqueue(ctx, id, TypeVideo, "videos", id.String())
}

func (c *Client) EnqueueAudio(ctx context.Context, id uuid.UUID) error {
	return c.enqueue(ctx, id, TypeAudio, "audio", id.String())
}

func (c *Client) enqueue(ctx context.Context, id uuid.UUID, taskType, queue, taskID string) error {
	b, err := json.Marshal(Payload{TaskID: id})
	if err != nil {
		return err
	}
	enqueue := func() error {
		_, enqueueErr := c.q.EnqueueContext(ctx, asynq.NewTask(taskType, b), asynq.Queue(queue), asynq.MaxRetry(0), asynq.Timeout(35*time.Minute), asynq.TaskID(taskID))
		return enqueueErr
	}
	err = enqueue()
	if !errors.Is(err, asynq.ErrTaskIDConflict) {
		return err
	}
	info, inspectErr := c.inspector.GetTaskInfo(queue, taskID)
	if errors.Is(inspectErr, asynq.ErrTaskNotFound) {
		retryErr := enqueue()
		if errors.Is(retryErr, asynq.ErrTaskIDConflict) {
			return nil
		}
		return retryErr
	}
	if inspectErr != nil {
		return fmt.Errorf("inspect conflicting asynq task %s: %w", taskID, inspectErr)
	}
	if info.State != asynq.TaskStateArchived {
		return nil
	}
	if runErr := c.inspector.RunTask(queue, taskID); runErr != nil {
		refreshed, refreshErr := c.inspector.GetTaskInfo(queue, taskID)
		if refreshErr == nil && refreshed.State != asynq.TaskStateArchived {
			return nil
		}
		return fmt.Errorf("reactivate archived asynq task %s: %w", taskID, runErr)
	}
	return nil
}

func (c *Client) EnqueueRecovery(ctx context.Context, id uuid.UUID, kind string) error {
	taskType, queue, err := queueForKind(kind)
	if err != nil {
		return err
	}
	return c.enqueue(ctx, id, taskType, queue, "recovery-"+id.String()+"-"+uuid.NewString())
}

func queueForKind(kind string) (taskType, queue string, err error) {
	switch kind {
	case "image":
		return TypeImage, "images", nil
	case "video":
		return TypeVideo, "videos", nil
	case "audio":
		return TypeAudio, "audio", nil
	default:
		return "", "", fmt.Errorf("unsupported task kind %q", kind)
	}
}
