package cli

import (
	"context"
	"sync"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

type metricsSnapshot struct {
	mu      sync.Mutex
	last    time.Time
	ttl     time.Duration
	loading bool
	done    chan struct{}
	observe func(context.Context) (source.QueueObservation, []schema.Range, error)
	apply   func(source.QueueObservation, []schema.Range)
	lastErr error
}

func newMetricsSnapshot(
	ttl time.Duration,
	observe func(context.Context) (source.QueueObservation, []schema.Range, error),
	apply func(source.QueueObservation, []schema.Range),
) *metricsSnapshot {
	if ttl <= 0 {
		ttl = time.Second
	}
	return &metricsSnapshot{ttl: ttl, observe: observe, apply: apply}
}

func (snapshot *metricsSnapshot) refresh(ctx context.Context) error {
	snapshot.mu.Lock()
	if time.Since(snapshot.last) < snapshot.ttl {
		err := snapshot.lastErr
		snapshot.mu.Unlock()
		return err
	}
	if snapshot.loading {
		done := snapshot.done
		snapshot.mu.Unlock()
		select {
		case <-done:
			snapshot.mu.Lock()
			err := snapshot.lastErr
			snapshot.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	snapshot.loading, snapshot.done = true, make(chan struct{})
	done := snapshot.done
	snapshot.mu.Unlock()
	queue, ranges, err := snapshot.observe(ctx)
	snapshot.mu.Lock()
	if err == nil && snapshot.apply != nil {
		snapshot.apply(queue, ranges)
	}
	snapshot.last, snapshot.lastErr, snapshot.loading = time.Now(), err, false
	close(done)
	snapshot.mu.Unlock()
	return err
}
