package cli

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func TestMetricsExposeExactlyTenPinnedFamilies(t *testing.T) {
	stats := &schema.MaintenanceStats{}
	stats.StoreDefaultPartitionRows(3)
	metrics := newMetrics(slogTestLogger(), stats)
	metrics.queueDepth.WithLabelValues("pending").Set(1)
	recorder := httptest.NewRecorder()
	metrics.handler(slogTestLogger()).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	data, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{
		"pg_noty_queue_depth", "pg_noty_oldest_pending_seconds", "pg_noty_dead_events_total",
		"pg_noty_partition_coverage_seconds", "pg_noty_default_partition_rows", "pg_noty_retention_blocked_total",
		"pg_noty_delivery_duration_seconds", "pg_noty_delivery_attempts_total", "pg_noty_reconcile_duration_seconds",
		"pg_noty_reconcile_errors_total",
	}
	for _, name := range names {
		if !strings.Contains(string(data), "# TYPE "+name+" ") {
			t.Fatalf("metrics output lacks %s: %s", name, data)
		}
	}
	if got := strings.Count(string(data), "# TYPE pg_noty_"); got != len(names) {
		t.Fatalf("metric family count = %d, want %d", got, len(names))
	}
	if !strings.Contains(string(data), "# TYPE pg_noty_default_partition_rows gauge") {
		t.Fatal("default partition rows is not a gauge")
	}
	if !strings.Contains(string(data), "# TYPE pg_noty_retention_blocked_total counter") {
		t.Fatal("retention blocked is not a counter")
	}
}

func TestMetricsFeedsMoveInExpectedDirections(t *testing.T) {
	stats := &schema.MaintenanceStats{}
	metrics := newMetrics(slogTestLogger(), stats)
	stats.StoreDefaultPartitionRows(8)
	stats.StoreDefaultPartitionRows(2)
	stats.CountRetentionBlockedByLiveEvents()
	if stats.DefaultPartitionRows() != 2 || stats.RetentionBlockedByLiveEvents() != 1 {
		t.Fatal("stats accessors did not feed the expected observations")
	}
	metrics.reconcileDuration.Observe(time.Second.Seconds())
}

func TestDeadCounterPublishesTheLiveSnapshot(t *testing.T) {
	metrics := newMetrics(slogTestLogger(), &schema.MaintenanceStats{})
	metrics.applyQueueObservation(source.QueueObservation{DeadByListener: map[string]int64{"orders": 3}}, nil)
	first := scrapeMetrics(t, metrics)
	if !strings.Contains(first, `pg_noty_dead_events_total{listener="orders"} 3`) {
		t.Fatalf("first dead snapshot missing: %s", first)
	}
	metrics.applyQueueObservation(source.QueueObservation{DeadByListener: map[string]int64{"orders": 1}}, nil)
	second := scrapeMetrics(t, metrics)
	if !strings.Contains(second, `pg_noty_dead_events_total{listener="orders"} 1`) {
		t.Fatalf("dead snapshot did not decrease: %s", second)
	}
}

func TestDeadCounterDropsUnconfiguredListenerLabels(t *testing.T) {
	metrics := newMetrics(slogTestLogger(), &schema.MaintenanceStats{})
	metrics.configureListeners([]string{"orders"})
	metrics.applyQueueObservation(
		source.QueueObservation{
			DeadByListener: map[string]int64{
				"orders": 3, "foreign": 7,
			},
		}, nil,
	)
	output := scrapeMetrics(t, metrics)
	if !strings.Contains(output, `pg_noty_dead_events_total{listener="orders"} 3`) {
		t.Fatalf("configured dead listener missing: %s", output)
	}
	if strings.Contains(output, `listener="foreign"`) {
		t.Fatalf("unconfigured listener escaped the metric vocabulary: %s", output)
	}
}

func scrapeMetrics(t *testing.T, metrics *metrics) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	metrics.handler(slogTestLogger()).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	data, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
