package controller

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/Thinnesen/chronicle/internal/archive"
)

// maxQueueSeries limits the series of the task queue metrics. Queue names are free text in workflows.
const maxQueueSeries = 200

// QueueReader reads the backlog of the task queues.
type QueueReader interface {
	QueueDepths(ctx context.Context, limit int) ([]archive.QueueDepth, error)
}

var (
	queuePendingDesc = prometheus.NewDesc("chronicle_task_queue_pending",
		"The number of pending tasks of a task queue.", []string{labelNamespace, labelQueue}, nil)
	queueOldestDesc = prometheus.NewDesc("chronicle_task_queue_oldest_pending_seconds",
		"The age of the oldest pending task of a task queue.", []string{labelNamespace, labelQueue}, nil)
)

// QueueCollector exports the backlog of the task queues at each scrape, for example to scale task workers with KEDA.
type QueueCollector struct{ queues QueueReader }

// NewQueueCollector returns a collector of the task queues.
func NewQueueCollector(q QueueReader) *QueueCollector { return &QueueCollector{queues: q} }

// Describe implements prometheus.Collector.
func (c *QueueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queuePendingDesc
	ch <- queueOldestDesc
}

// Collect implements prometheus.Collector. When the database does not answer, it exports no queue metrics.
func (c *QueueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	depths, err := c.queues.QueueDepths(ctx, maxQueueSeries)
	if err != nil {
		ctrl.Log.WithName("task-queues").Error(err, "Cannot read the task queues for the metrics")
		return
	}
	for _, d := range depths {
		ch <- prometheus.MustNewConstMetric(queuePendingDesc, prometheus.GaugeValue, float64(d.Pending), d.Namespace, d.Queue)
		ch <- prometheus.MustNewConstMetric(queueOldestDesc, prometheus.GaugeValue, d.OldestSeconds, d.Namespace, d.Queue)
	}
}
