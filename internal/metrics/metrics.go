package metrics

import (
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	TasksTotal          = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "aiv2api_tasks_total", Help: "Terminal tasks by provider, media kind, and status."}, []string{"provider", "kind", "status"})
	TasksActive         = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "aiv2api_tasks_active", Help: "Claimed tasks currently processed by provider and media kind."}, []string{"provider", "kind"})
	TaskDuration        = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "aiv2api_task_duration_seconds", Help: "Successful generation task duration by provider and media kind.", Buckets: prometheus.DefBuckets}, []string{"provider", "kind"})
	RequestLogDropped   = prometheus.NewCounter(prometheus.CounterOpts{Name: "aiv2api_request_log_dropped_total", Help: "API request log records dropped because the bounded buffer was full."})
	AdmissionRejected   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "aiv2api_admission_rejected_total", Help: "Generation requests rejected before task creation."}, []string{"reason"})
	QueueOldestSeconds  = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_queue_oldest_seconds", Help: "Age of the oldest queued task."})
	OutboxPending       = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_outbox_pending", Help: "Task Outbox rows waiting for dispatch."})
	OutboxOldestSeconds = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_outbox_oldest_seconds", Help: "Age of the oldest pending Outbox row."})
	ExpiredTaskLeases   = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_expired_task_leases", Help: "Non-terminal tasks with expired worker leases."})
	SubmissionUncertain = prometheus.NewGauge(prometheus.GaugeOpts{Name: "aiv2api_submission_uncertain", Help: "Tasks requiring submission reconciliation."})
)

func Register(configured []providers.Descriptor) {
	prometheus.MustRegister(TasksTotal, TasksActive, TaskDuration, RequestLogDropped, AdmissionRejected,
		QueueOldestSeconds, OutboxPending, OutboxOldestSeconds, ExpiredTaskLeases, SubmissionUncertain)
	for _, provider := range configured {
		for _, kind := range provider.Capabilities {
			TasksActive.WithLabelValues(provider.ID, kind).Set(0)
			TaskDuration.WithLabelValues(provider.ID, kind)
			for _, status := range []string{"succeeded", "failed", "submission_uncertain"} {
				TasksTotal.WithLabelValues(provider.ID, kind, status).Add(0)
			}
		}
	}
}
