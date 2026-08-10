package store

import (
	"context"
	"time"
)

type SchedulingHealth struct {
	QueueOldest    time.Duration
	OutboxPending  int
	OutboxOldest   time.Duration
	ExpiredLeases  int
	UncertainTasks int
}

func (s *Store) GetSchedulingHealth(ctx context.Context) (SchedulingHealth, error) {
	var health SchedulingHealth
	var queueSeconds, outboxSeconds float64
	err := s.DB.QueryRow(ctx, `SELECT
		COALESCE(EXTRACT(epoch FROM now()-(min(created_at) FILTER (WHERE status='queued'))),0),
		(SELECT count(*) FROM task_outbox WHERE delivered_at IS NULL),
		COALESCE((SELECT EXTRACT(epoch FROM now()-min(created_at)) FROM task_outbox WHERE delivered_at IS NULL),0),
		count(*) FILTER (WHERE status IN ('reserving','uploading','submitted','polling')
		  AND execution_lease_expires_at IS NOT NULL AND execution_lease_expires_at<=now()),
		(SELECT count(*) FROM tasks uncertain JOIN account_reservations reservation
		  ON reservation.task_id=uncertain.id AND reservation.state='held'
		  WHERE uncertain.status='submission_uncertain')
		FROM tasks`).Scan(&queueSeconds, &health.OutboxPending, &outboxSeconds, &health.ExpiredLeases, &health.UncertainTasks)
	if err != nil {
		return health, err
	}
	health.QueueOldest = time.Duration(queueSeconds * float64(time.Second))
	health.OutboxOldest = time.Duration(outboxSeconds * float64(time.Second))
	return health, nil
}
