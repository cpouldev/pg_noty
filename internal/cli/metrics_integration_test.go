//go:build integration

package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func TestMetricsDatabaseDegradationHarnessIsPresent(t *testing.T) {
	var calls atomic.Int32
	var slow atomic.Bool
	observe := func(ctx context.Context) (source.QueueObservation, []schema.Range, error) {
		calls.Add(1)
		if slow.Load() {
			<-ctx.Done()
			return source.QueueObservation{}, nil, ctx.Err()
		}
		return source.QueueObservation{Pending: 2}, nil, nil
	}
	metrics := newMetrics(slogTestLogger(), &schema.MaintenanceStats{})
	snapshot := newMetricsSnapshot(time.Nanosecond, observe, metrics.applyQueueObservation)
	if err := snapshot.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("healthy observation calls=%d, want 1", got)
	}
	if got := scrapeMetrics(t, metrics); !containsMetric(got, `pg_noty_queue_depth{status="pending"} 2`) {
		t.Fatalf("healthy scrape lacks queue observation: %s", got)
	}

	slow.Store(true)
	start := time.Now()
	if err := serveSnapshot(t, snapshot, metrics); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow observation error=%v, want deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("slow scrape took %s, want bounded response", elapsed)
	}
	if got := scrapeMetrics(t, metrics); !containsMetric(got, `pg_noty_queue_depth{status="pending"} 2`) {
		t.Fatalf("degraded scrape lost stale in-process observation: %s", got)
	}

	calls.Store(0)
	slow.Store(false)
	observing := make(chan struct{})
	release := make(chan struct{})
	observe = func(ctx context.Context) (source.QueueObservation, []schema.Range, error) {
		if calls.Add(1) == 1 {
			close(observing)
			select {
			case <-release:
			case <-ctx.Done():
				return source.QueueObservation{}, nil, ctx.Err()
			}
		}
		return source.QueueObservation{Pending: 2}, nil, nil
	}
	snapshot = newMetricsSnapshot(time.Minute, observe, metrics.applyQueueObservation)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = snapshot.refresh(context.Background())
		}()
	}
	<-observing
	close(release)
	group.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent scrapes issued %d observations, want one", got)
	}
}

func serveSnapshot(t *testing.T, snapshot *metricsSnapshot, metrics *metrics) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	runMetricsHandler(snapshot, metrics.handler(slogTestLogger())).ServeHTTP(recorder, request)
	return ctx.Err()
}

func containsMetric(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
