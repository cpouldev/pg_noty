//go:build integration

package reconcile

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

const runLockTestInstance = "run-lock-test"

func TestTheRunLockIsReleasedOnTheConnectionThatTookItOnEveryPathHere(t *testing.T) {
	skipIfShort(t)
	t.Run("acquire then release", func(t *testing.T) { assertAcquiredRunLockIsReleased(t) })
	t.Run("acquire then injected error", func(t *testing.T) { assertRunLockIsReleasedAfterError(t) })
	t.Run("wait expiry", func(t *testing.T) { assertExpiredRunLockLeavesNothingPinned(t) })
}
func TestRunLockWaitUsesTheSuppliedBoundOnBothSides(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	key := testRunLockKey(t, pool)
	holder := takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	if held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, 20*time.Millisecond); err != nil || held != nil || refusal == nil {
		t.Fatalf("below-holder bound = (%v, %v, %v), want expiry", held, refusal, err)
	}
	unlockRunLockKey(t, holder, key)
	holder.Release()
	holder = takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	done := unlockRunLockKeyAfter(holder, key, 50*time.Millisecond)
	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, 300*time.Millisecond)
	if err != nil || refusal != nil || held == nil {
		t.Fatalf("above-holder bound = (%v, %v, %v), want acquired handle", held, refusal, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := held.release(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func assertPoolAcquireExpiry(t *testing.T) {
	t.Helper()
	pool := constrainedRunLockPool(t, 1)
	occupied := takeRunLockConnection(t, pool)
	defer occupied.Release()
	parent, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancel()
	const bound = 20 * time.Millisecond
	done := make(chan runLockResult, 1)
	go func() {
		held, refusal, err := acquireRunLock(parent, pool, harnessSchema, runLockTestInstance, bound)
		done <- runLockResult{held: held, refusal: refusal, err: err}
	}()
	watchdog := time.NewTimer(4 * bound)
	defer watchdog.Stop()
	select {
	case got := <-done:
		if got.err != nil || got.held != nil || got.refusal == nil || !strings.Contains(got.refusal.Message(), runLockTestInstance) || !strings.Contains(got.refusal.Message(), "could not be identified") {
			t.Fatalf("exhausted pool acquire = (%v, %v, %v), want its named expiry refusal", got.held, got.refusal, got.err)
		}
	case <-watchdog.C:
		cancel()
		<-done
		t.Fatal("pool acquisition ignored the supplied run-lock bound")
	}
}
func assertPostAcquireAttemptExpiry(t *testing.T) {
	t.Helper()
	pool := constrainedRunLockPool(t, 1)
	ready := takeRunLockConnection(t, pool)
	ready.Release()
	const bound = 20 * time.Millisecond
	attemptStarted := make(chan struct{})
	done := make(chan runLockResult, 1)
	go func() {
		held, refusal, err := acquireRunLockWith(t.Context(), pool, harnessSchema, runLockTestInstance, bound, func(ctx context.Context, _ *pgxpool.Conn, _ int64) (bool, error) {
			close(attemptStarted)
			<-ctx.Done()
			return false, ctx.Err()
		})
		done <- runLockResult{held: held, refusal: refusal, err: err}
	}()
	startWatchdog := time.NewTimer(time.Second)
	defer startWatchdog.Stop()
	select {
	case <-attemptStarted:
		if got := pool.Stat().AcquiredConns(); got != 1 {
			t.Fatalf("post-acquire attempt has %d acquired pool connections, want 1", got)
		}
	case <-startWatchdog.C:
		t.Fatal("run-lock attempt never started after acquiring its pool connection")
	}
	watchdog := time.NewTimer(4 * bound)
	defer watchdog.Stop()
	select {
	case got := <-done:
		if got.err != nil || got.held != nil || got.refusal == nil || !strings.Contains(got.refusal.Message(), runLockTestInstance) || !strings.Contains(got.refusal.Message(), "could not be identified") {
			t.Fatalf("stalled post-acquire attempt = (%v, %v, %v), want named expiry", got.held, got.refusal, got.err)
		}
	case <-watchdog.C:
		t.Fatal("post-acquire attempt ignored the supplied run-lock bound")
	}
}

func TestCancelledRunContextFailsTheNaiveReleaseButNotTheSuppliedOne(t *testing.T) {
	skipIfShort(t)
	pool, observerPool := constrainedRunLockPool(t, 1), newRunLockPool(t, 1)
	key, observer := testRunLockKey(t, pool), takeRunLockConnection(t, observerPool)
	defer observer.Release()
	naiveContext, cancelNaive := context.WithCancel(t.Context())
	naive, refusal, err := acquireRunLock(naiveContext, pool, harnessSchema, runLockTestInstance, time.Second)
	if err != nil || refusal != nil || naive == nil {
		t.Fatalf("acquire naive control = (%v, %v, %v)", naive, refusal, err)
	}
	cancelNaive()
	if err := naive.release(naiveContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("release with cancelled run context = %v, want context cancellation", err)
	}
	assertRunLockStillHeld(t, observer, key)
	cleanup := takeRunLockConnection(t, pool)
	unlockRunLockKey(t, cleanup, key)
	cleanup.Release()
	liveContext, cancelLive := context.WithCancel(t.Context())
	correct, refusal, err := acquireRunLock(liveContext, pool, harnessSchema, runLockTestInstance, time.Second)
	if err != nil || refusal != nil || correct == nil {
		t.Fatalf("acquire supplied-context case = (%v, %v, %v)", correct, refusal, err)
	}
	cancelLive()
	if err := correct.release(context.WithoutCancel(liveContext)); err != nil {
		t.Fatalf("release with supplied live context: %v", err)
	}
	assertRunLockFree(t, observer, key)
}
func assertAcquiredRunLockIsReleased(t *testing.T) {
	pool := freshDatabase(t)
	key := testRunLockKey(t, pool)
	before := pool.Stat().AcquiredConns()
	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, time.Second)
	if err != nil || refusal != nil || held == nil {
		t.Fatalf("acquire = (%v, %v, %v)", held, refusal, err)
	}
	observer := takeRunLockConnection(t, pool)
	if err := held.release(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRunLockFree(t, observer, key)
	observer.Release()
	assertAcquiredConnsEventually(t, pool, before)
}
func assertRunLockIsReleasedAfterError(t *testing.T) {
	pool := freshDatabase(t)
	key := testRunLockKey(t, pool)
	before := pool.Stat().AcquiredConns()
	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, time.Second)
	if err != nil || refusal != nil || held == nil {
		t.Fatalf("acquire before injected error = (%v, %v, %v)", held, refusal, err)
	}
	injected := errors.New("apply operation failed")
	if err := releaseAfterRunLockError(held, injected); !errors.Is(err, injected) {
		t.Fatalf("injected operation error = %v, want %v", err, injected)
	}
	observer := takeRunLockConnection(t, pool)
	assertRunLockFree(t, observer, key)
	observer.Release()
	assertAcquiredConnsEventually(t, pool, before)
}
func assertExpiredRunLockLeavesNothingPinned(t *testing.T) {
	pool := freshDatabase(t)
	key := testRunLockKey(t, pool)
	initial := pool.Stat().AcquiredConns()
	holder := takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	before, holderID := pool.Stat().AcquiredConns(), backendIdentity(t, holder)
	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, 20*time.Millisecond)
	if err != nil || held != nil || refusal == nil || !strings.Contains(refusal.Message(), runLockTestInstance) || !strings.Contains(refusal.Message(), holderID) {
		t.Fatalf("expiry = (%v, %v, %v), want instance %q and holder %q", held, refusal, err, runLockTestInstance, holderID)
	}
	assertAcquiredConnsEventually(t, pool, before)
	observer := takeRunLockConnection(t, pool)
	unlockRunLockKey(t, holder, key)
	assertRunLockFree(t, observer, key)
	holder.Release()
	observer.Release()
	assertAcquiredConnsEventually(t, pool, initial)
}
