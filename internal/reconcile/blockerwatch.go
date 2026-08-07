package reconcile

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	blockerWatchAcquireBound = 50 * time.Millisecond
	blockerWatchPollInterval = 10 * time.Millisecond
)

// blockerObservation distinguishes no observed blocker from a monitor that could not identify one.
type blockerObservation struct {
	backend            string
	monitorUnavailable bool
}

func (observed blockerObservation) description() string {
	if observed.monitorUnavailable {
		return "could not be identified"
	}
	if observed.backend == "" {
		return "no blocker"
	}
	return observed.backend
}

// blockerWatch observes an apply backend from a second, best-effort connection while it waits.
type blockerWatch struct {
	cancel context.CancelFunc
	result <-chan blockerObservation
}

func startBlockerWatch(parent context.Context, pool *pgxpool.Pool, applyBackend int32) *blockerWatch {
	ctx, cancel := context.WithCancel(parent)
	result := make(chan blockerObservation, 1)
	go observeBlocker(ctx, pool, applyBackend, result)
	return &blockerWatch{cancel: cancel, result: result}
}

func (watch *blockerWatch) stop() blockerObservation {
	watch.cancel()
	return <-watch.result
}

func (watch *blockerWatch) wait(ctx context.Context) (blockerObservation, error) {
	select {
	case observed := <-watch.result:
		return observed, nil
	case <-ctx.Done():
		return blockerObservation{}, ctx.Err()
	}
}

func observeBlocker(ctx context.Context, pool *pgxpool.Pool, applyBackend int32, result chan<- blockerObservation) {
	acquireCtx, cancel := context.WithTimeout(ctx, blockerWatchAcquireBound)
	on, err := pool.Acquire(acquireCtx)
	cancel()
	if err != nil {
		result <- blockerObservation{monitorUnavailable: true}
		return
	}
	defer on.Release()
	ticker := time.NewTicker(blockerWatchPollInterval)
	defer ticker.Stop()
	for {
		holder, err := blockingBackend(ctx, on, applyBackend)
		if err != nil {
			result <- blockerObservation{monitorUnavailable: true}
			return
		}
		if holder != "" {
			result <- blockerObservation{backend: holder}
			return
		}
		select {
		case <-ctx.Done():
			result <- blockerObservation{}
			return
		case <-ticker.C:
		}
	}
}

func blockingBackend(ctx context.Context, on *pgxpool.Conn, applyBackend int32) (string, error) {
	holder, found, err := NewConnectionCatalog(on).BlockingBackend(ctx, applyBackend)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return holder, nil
}
