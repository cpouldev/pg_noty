package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// internal/schema/advisorylock.go's ReconcileLockKey is this lock's key authority. This file
// duplicates that file's acquire/release mechanics only because this wait is bounded, while
// internal/schema's must never be: "a replica that gave up would have to decide whether the schema
// it found was finished." A polling wait is not FIFO-fair; contenders are one instance's own
// replicas.
const (
	DefaultRunLockWait  = 60 * time.Second
	runLockPollInterval = 10 * time.Millisecond
	// The server's wait for the pinned backend to die -- schema's terminationWait, for its reason --
	// and a client bound above it, which must not expire while the server is still waiting.
	runLockTerminationWait = 5 * time.Second
	runLockRecoveryBound   = 2 * runLockTerminationWait
)

// runLock keeps the session lock and its pinned connection inseparable. It carries the pool and that
// connection's backend PID too: a session-scoped key outlives a connection closed under it, and is
// then reachable only from another session.
type runLock struct {
	on      *pgxpool.Conn
	pool    *pgxpool.Pool
	key     int64
	backend int32
}

func newRunLock(pool *pgxpool.Pool, on *pgxpool.Conn, key int64) *runLock {
	return &runLock{on: on, pool: pool, key: key, backend: int32(on.Conn().PgConn().PID())}
}

type runLockAttempt func(context.Context, *pgxpool.Conn, int64) (bool, error)

func acquireRunLock(
	ctx context.Context,
	pool *pgxpool.Pool,
	serviceSchema, instance string,
	wait time.Duration,
) (*runLock, *Refusal, error) {
	return acquireRunLockWith(ctx, pool, serviceSchema, instance, wait, tryRunLock)
}

func acquireRunLockWith(
	ctx context.Context,
	pool *pgxpool.Pool,
	serviceSchema, instance string,
	wait time.Duration,
	attempt runLockAttempt,
) (*runLock, *Refusal, error) {
	if wait == 0 {
		wait = DefaultRunLockWait
	}
	deadline := time.Now().Add(wait)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	on, err := pool.Acquire(waitCtx)
	if err != nil {
		return runLockWaitResult(
			waitCtx,
			instance,
			"could not be identified",
			fmt.Errorf("acquire run-lock connection: %w", err),
		)
	}
	key := schema.ReconcileLockKey(serviceSchema, instance)
	holder := "could not be identified"
	for {
		if time.Until(deadline) <= 0 {
			on.Release()
			return runLockWaitExpiry(instance, holder)
		}
		locked, err := attempt(waitCtx, on, key)
		if err != nil {
			on.Release()
			return runLockWaitResult(waitCtx, instance, holder, err)
		}
		if locked {
			return acquiredRunLockResult(waitCtx, instance, holder, newRunLock(pool, on, key))
		}
		observedHolder, err := runLockHolder(waitCtx, on, key)
		if err != nil {
			on.Release()
			return runLockWaitResult(waitCtx, instance, holder, err)
		}
		holder = observedHolder
		if err := waitCtx.Err(); err != nil {
			on.Release()
			return runLockWaitResult(waitCtx, instance, holder, err)
		}
		if err := waitForRunLockPoll(waitCtx, deadline); err != nil {
			on.Release()
			return runLockWaitResult(waitCtx, instance, holder, err)
		}
	}
}

func acquiredRunLockResult(waitCtx context.Context, instance, holder string, held *runLock) (
	*runLock,
	*Refusal,
	error,
) {
	if err := waitCtx.Err(); err == nil {
		return held, nil, nil
	}
	if err := held.release(context.Background()); err != nil {
		return nil, nil, err
	}
	return runLockWaitResult(waitCtx, instance, holder, waitCtx.Err())
}

func runLockWaitResult(waitCtx context.Context, instance, holder string, err error) (*runLock, *Refusal, error) {
	if errors.Is(waitCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return runLockWaitExpiry(instance, holder)
	}
	return nil, nil, err
}

func runLockWaitExpiry(instance, holder string) (*runLock, *Refusal, error) {
	refusal := runLockWaitExpiredRefusal(instance, holder)
	return nil, &refusal, nil
}

func tryRunLock(ctx context.Context, on *pgxpool.Conn, key int64) (bool, error) {
	var locked bool
	err := on.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("try reconcile run lock: %w", err)
	}
	return locked, nil
}

func runLockHolder(ctx context.Context, on *pgxpool.Conn, key int64) (string, error) {
	tx, err := on.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin reconcile run-lock holder read: %w", err)
	}
	defer tx.Rollback(context.Background())
	holder, found, err := NewCatalog(tx).AdvisoryLockHolder(ctx, key)
	if err != nil {
		return "", fmt.Errorf("resolve reconcile run-lock holder: %w", err)
	}
	if !found {
		return "could not be identified", nil
	}
	return holder, nil
}

func waitForRunLockPoll(ctx context.Context, deadline time.Time) error {
	wait := min(time.Until(deadline), runLockPollInterval)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// release gives the session lock back, on the connection that took it or by ending that session. Both
// routes are synchronous: nil means the server has already stopped counting this run as the holder.
func (held *runLock) release(ctx context.Context) error {
	if held == nil || held.on == nil {
		return errors.New("release reconcile run lock without a pinned connection")
	}
	on := held.on
	held.on = nil
	// A cancellation arriving mid-statement closes the pinned connection: it can no longer unlock, and
	// pgxpool destroys rather than pools it, so the handle goes back before the recovery reaches out.
	if on.Conn().IsClosed() {
		on.Release()
		return held.endTheSessionStillHoldingTheKey(ctx)
	}
	defer on.Release()
	var unlocked bool
	if err := on.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", held.key).Scan(&unlocked); err != nil {
		return fmt.Errorf("release reconcile run lock: %w", err)
	}
	if !unlocked {
		return errors.New("pinned connection did not hold reconcile run lock")
	}
	return nil
}

// endTheSessionStillHoldingTheKey frees the key the only way a closed session leaves open: it is held
// by the session, so ending the session gives it back. PostgreSQL releases a backend's advisory locks
// before pg_terminate_backend's two-argument form stops waiting -- internal/schema's
// TestATerminatedHoldersLockIsFreeToTheNextSessionWithoutWaiting asserts that. Its answer is not the
// confirmation, since false means both "already gone" and "outlived the wait"; the holder reading is,
// read for this run's own backend so a key another run has taken is not mistaken for this one's.
// Nothing reaches the refusal below: it needs a backend surviving SIGTERM for runLockTerminationWait.
func (held *runLock) endTheSessionStillHoldingTheKey(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, runLockRecoveryBound)
	defer cancel()
	on, err := held.pool.Acquire(bounded)
	if err != nil {
		return fmt.Errorf("reach the session holding the reconcile run lock: %w", err)
	}
	defer on.Release()
	if _, err := on.Exec(
		bounded,
		"SELECT pg_terminate_backend($1, $2)",
		held.backend,
		runLockTerminationWait.Milliseconds(),
	); err != nil {
		return fmt.Errorf("end the session holding the reconcile run lock: %w", err)
	}
	holder, stillHeld, err := NewConnectionCatalog(on).AdvisoryLockHolder(bounded, held.key)
	if err != nil {
		return fmt.Errorf("confirm the reconcile run lock came free: %w", err)
	}
	if stillHeld && holder == strconv.Itoa(int(held.backend)) {
		return fmt.Errorf(
			"backend %s outlived %s and still holds the reconcile run lock",
			holder,
			runLockTerminationWait,
		)
	}
	return nil
}
