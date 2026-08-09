package cli

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metrics struct {
	registry           *prometheus.Registry
	listeners          map[string]struct{}
	queueDepth         *prometheus.GaugeVec
	oldestPending      prometheus.Gauge
	deadEvents         *liveDeadCounter
	partitionCoverage  prometheus.Gauge
	partitionPrecreate time.Duration
	defaultRows        prometheus.GaugeFunc
	retentionBlocked   prometheus.CounterFunc
	deliveryDuration   *prometheus.HistogramVec
	deliveryAttempts   *prometheus.CounterVec
	reconcileDuration  prometheus.Histogram
	reconcileErrors    prometheus.Counter
}

func newMetrics(logger *slog.Logger, stats *schema.MaintenanceStats) *metrics {
	if stats == nil {
		stats = &schema.MaintenanceStats{}
	}
	result := &metrics{registry: prometheus.NewRegistry(), listeners: make(map[string]struct{})}
	result.queueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pg_noty_queue_depth", Help: "queue depth by status",
		}, []string{"status"},
	)
	result.oldestPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "pg_noty_oldest_pending_seconds", Help: "oldest pending age",
		},
	)
	result.deadEvents = newLiveDeadCounter()
	result.partitionCoverage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "pg_noty_partition_coverage_seconds", Help: "partition coverage",
		},
	)
	result.defaultRows = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "pg_noty_default_partition_rows", Help: "rows in DEFAULT partition",
		}, func() float64 { return float64(stats.DefaultPartitionRows()) },
	)
	result.retentionBlocked = prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "pg_noty_retention_blocked_total", Help: "retention blocks",
		}, func() float64 { return float64(stats.RetentionBlockedByLiveEvents()) },
	)
	result.deliveryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pg_noty_delivery_duration_seconds", Help: "delivery duration",
		}, []string{"listener", "outcome"},
	)
	result.deliveryAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pg_noty_delivery_attempts_total", Help: "delivery attempts",
		}, []string{"listener", "outcome"},
	)
	result.reconcileDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "pg_noty_reconcile_duration_seconds", Help: "reconcile duration",
		},
	)
	result.reconcileErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "pg_noty_reconcile_errors_total", Help: "reconcile errors",
		},
	)
	for _, collector := range []prometheus.Collector{
		result.queueDepth, result.oldestPending, result.deadEvents, result.partitionCoverage, result.defaultRows,
		result.retentionBlocked, result.deliveryDuration, result.deliveryAttempts, result.reconcileDuration,
		result.reconcileErrors,
	} {
		result.registry.MustRegister(collector)
	}
	result.deadEvents.Replace(nil)
	result.deliveryDuration.WithLabelValues("unknown", "success")
	result.deliveryAttempts.WithLabelValues("unknown", "success")
	_ = logger
	return result
}

// configureListeners closes the only live listener label vocabulary. It is called
// once from run after configuration has been resolved; an empty set keeps the
// constructor useful for isolated metric tests that supply their own snapshots.
func (m *metrics) configureListeners(names []string) {
	if m == nil {
		return
	}
	for _, name := range names {
		if name != "" {
			m.listeners[name] = struct{}{}
		}
	}
}

// liveDeadCounter deliberately emits CounterValue for a live database count. A
// retry can lower the count, so adding each snapshot to a CounterVec would
// manufacture growth and make the metric a counter reset in reverse.
type liveDeadCounter struct {
	mu     sync.RWMutex
	values map[string]float64
	desc   *prometheus.Desc
}

func newLiveDeadCounter() *liveDeadCounter {
	return &liveDeadCounter{
		values: make(map[string]float64),
		desc:   prometheus.NewDesc("pg_noty_dead_events_total", "live dead events", []string{"listener"}, nil),
	}
}

func (counter *liveDeadCounter) Describe(ch chan<- *prometheus.Desc) { ch <- counter.desc }

func (counter *liveDeadCounter) Collect(ch chan<- prometheus.Metric) {
	counter.mu.RLock()
	values := make(map[string]float64, len(counter.values))
	for listener, value := range counter.values {
		values[listener] = value
	}
	counter.mu.RUnlock()
	for listener, value := range values {
		ch <- prometheus.MustNewConstMetric(counter.desc, prometheus.CounterValue, value, listener)
	}
}

func (counter *liveDeadCounter) Replace(counts map[string]int64) {
	counter.mu.Lock()
	counter.values = make(map[string]float64, len(counts))
	for listener, count := range counts {
		counter.values[listener] = float64(count)
	}
	if len(counter.values) == 0 {
		counter.values["unknown"] = 0
	}
	counter.mu.Unlock()
}

func (m *metrics) handler(logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return promhttp.HandlerFor(
		m.registry,
		promhttp.HandlerOpts{Timeout: 2 * time.Second, MaxRequestsInFlight: 1, ErrorLog: promLogger{logger}},
	)
}

type promLogger struct{ logger *slog.Logger }

func (logger promLogger) Println(values ...any) {
	logger.logger.Error("prometheus scrape", "values", values)
}
