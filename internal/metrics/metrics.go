package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	TasksTotal          = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "leonardo_tasks_total", Help: "Completed tasks by status."}, []string{"status"})
	TasksActive         = prometheus.NewGauge(prometheus.GaugeOpts{Name: "leonardo_tasks_active", Help: "Tasks currently processed."})
	TaskDuration        = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "leonardo_task_duration_seconds", Help: "Generation task duration.", Buckets: prometheus.DefBuckets})
	RequestLogDropped   = prometheus.NewCounter(prometheus.CounterOpts{Name: "aiv2api_request_log_dropped_total", Help: "API request log records dropped because the bounded buffer was full."})
	AdmissionRejected   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "aiv2api_admission_rejected_total", Help: "Generation requests rejected before task creation."}, []string{"reason"})
	QueueOldestSeconds  = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_queue_oldest_seconds", Help: "Age of the oldest queued task."})
	OutboxPending       = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_outbox_pending", Help: "Task Outbox rows waiting for dispatch."})
	OutboxOldestSeconds = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_outbox_oldest_seconds", Help: "Age of the oldest pending Outbox row."})
	ExpiredTaskLeases   = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_expired_task_leases", Help: "Non-terminal tasks with expired worker leases."})
	SubmissionUncertain = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_submission_uncertain", Help: "Tasks requiring submission reconciliation."})
)

func Register() {
	prometheus.MustRegister(TasksTotal, TasksActive, TaskDuration, RequestLogDropped, AdmissionRejected,
		QueueOldestSeconds, OutboxPending, OutboxOldestSeconds, ExpiredTaskLeases, SubmissionUncertain)
}
