//go:build integration

package reconcile

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"testing"
	"time"
)

func TestRunLockPoolAcquireExpiresAtTheSuppliedBound(t *testing.T) {
	skipIfShort(t)
	assertPoolAcquireExpiry(t)
	assertPostAcquireAttemptExpiry(t)
}

func TestTakeRunLockConnectionReleasesAtTheOwningTestCleanup(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	var connection *pgxpool.Conn
	t.Run("owner", func(t *testing.T) {
		connection = takeRunLockConnection(t, pool)
	})
	acquired := pool.Stat().AcquiredConns()
	connection.Release()
	if acquired != 0 {
		t.Fatalf("owning test cleanup left %d acquired connections, want none", acquired)
	}
}

func TestAcquiredConnectionAssertionWaitsForAnAsynchronousRelease(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	connection := takeRunLockConnection(t, pool)
	released := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		connection.Release()
		close(released)
	}()
	assertAcquiredConnsEventually(t, pool, 0)
	<-released
}

func TestBlockerWatchIdentifiesABackendWhileItsStatementIsBlocked(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.blocker_watch_target (id int)")
	holder, waiter := takeRunLockConnection(t, pool), takeRunLockConnection(t, pool)
	defer holder.Release()
	defer waiter.Release()
	mustExecOn(t, holder, "BEGIN")
	mustExecOn(t, waiter, "BEGIN")
	mustExecOn(t, holder, "LOCK TABLE public.blocker_watch_target IN ACCESS EXCLUSIVE MODE")
	backend, waiting := backendIdentity(t, holder), backendIdentity(t, waiter)
	done := make(chan error, 1)
	go func() {
		_, err := waiter.Exec(t.Context(), "LOCK TABLE public.blocker_watch_target IN ACCESS SHARE MODE")
		done <- err
	}()
	waitForBackendLock(t, pool, int32From(t, waiting))
	watch := startBlockerWatch(t.Context(), pool, int32From(t, waiting))
	observed, err := watch.wait(withTimeout(t, time.Second))
	if err != nil || observed.description() != backend {
		t.Fatalf("blocker observation = (%q, %v), want backend %q", observed.description(), err, backend)
	}
	mustExecOn(t, holder, "ROLLBACK")
	if err := <-done; err != nil {
		t.Fatalf("unblocked statement: %v", err)
	}
	mustExecOn(t, waiter, "ROLLBACK")
}
func TestBlockerWatchReportsUnavailableMonitoringDifferentlyFromNoBlocker(t *testing.T) {
	skipIfShort(t)
	pool := constrainedRunLockPool(t, 1)
	occupied := takeRunLockConnection(t, pool)
	defer occupied.Release()
	watch := startBlockerWatch(t.Context(), pool, 1)
	observed, err := watch.wait(withTimeout(t, time.Second))
	if err != nil || observed.description() != "could not be identified" {
		t.Fatalf("unavailable monitor = (%q, %v)", observed.description(), err)
	}
	if got := (blockerObservation{}).description(); got != "no blocker" {
		t.Fatalf("no-blocker observation = %q", got)
	}
}
func TestPgLocksEncodesABigintAdvisoryKeyAsItsTwoUint32Parts(t *testing.T) {
	skipIfShort(t)
	on := takeRunLockConnection(t, freshDatabase(t))
	defer on.Release()
	const key int64 = 0x1020304050607080
	lockRunLockKey(t, on, key)
	defer unlockRunLockKey(t, on, key)
	var high, low uint32
	if err := on.QueryRow(t.Context(), "SELECT classid, objid FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'").Scan(&high, &low); err != nil || high != 0x10203040 || low != 0x50607080 {
		t.Fatalf("pg_locks key parts = (%#x, %#x, %v), want (0x10203040, 0x50607080)", high, low, err)
	}
}
func withTimeout(t *testing.T, wait time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	t.Cleanup(cancel)
	return ctx
}
func int32From(t *testing.T, text string) int32 {
	t.Helper()
	value, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return int32(value)
}
func takeRunLockConnection(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	on, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(on.Release)
	return on
}
func assertAcquiredConnsEventually(t *testing.T, pool *pgxpool.Pool, want int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		got := pool.Stat().AcquiredConns()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("acquired pool connections remained at %d for one second, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}
func lockRunLockKey(t *testing.T, on *pgxpool.Conn, key int64) {
	t.Helper()
	var locked bool
	if err := on.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil || !locked {
		t.Fatalf("hold run lock = (%t, %v)", locked, err)
	}
}
func unlockRunLockKey(t *testing.T, on *pgxpool.Conn, key int64) {
	t.Helper()
	var unlocked bool
	if err := on.QueryRow(t.Context(), "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil || !unlocked {
		t.Fatalf("unlock run lock = (%t, %v)", unlocked, err)
	}
}
func assertRunLockFree(t *testing.T, on *pgxpool.Conn, key int64) {
	t.Helper()
	lockRunLockKey(t, on, key)
	unlockRunLockKey(t, on, key)
}
func assertRunLockStillHeld(t *testing.T, on *pgxpool.Conn, key int64) {
	t.Helper()
	var locked bool
	if err := on.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil || locked {
		t.Fatalf("naive cancelled release left lock available = (%t, %v)", locked, err)
	}
}
func backendIdentity(t *testing.T, on *pgxpool.Conn) string {
	t.Helper()
	var identity string
	if err := on.QueryRow(t.Context(), "SELECT pg_backend_pid()::text").Scan(&identity); err != nil {
		t.Fatal(err)
	}
	return identity
}
func unlockRunLockKeyAfter(on *pgxpool.Conn, key int64, after time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() {
		time.Sleep(after)
		var unlocked bool
		err := on.QueryRow(context.Background(), "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked)
		if err == nil && !unlocked {
			err = errors.New("delayed holder did not own run lock")
		}
		on.Release()
		done <- err
	}()
	return done
}
func constrainedRunLockPool(t *testing.T, maximum int32) *pgxpool.Pool {
	t.Helper()
	restoreToSnapshot(t)
	return newRunLockPool(t, maximum)
}
func newRunLockPool(t *testing.T, maximum int32) *pgxpool.Pool {
	t.Helper()
	poolConfig, err := pgxpool.ParseConfig(harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = maximum
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
func releaseAfterRunLockError(held *runLock, workErr error) error {
	releaseErr := held.release(context.Background())
	if workErr != nil {
		return workErr
	}
	return releaseErr
}
func testRunLockKey(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, runLockTestInstance, time.Second)
	if err != nil || refusal != nil || held == nil {
		t.Fatalf("acquire fixture lock = (%v, %v, %v)", held, refusal, err)
	}
	key := held.key
	if err := held.release(context.Background()); err != nil {
		t.Fatal(err)
	}
	return key
}
func waitForBackendLock(t *testing.T, pool *pgxpool.Pool, backend int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(t.Context(), "SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1", backend).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(blockerWatchPollInterval)
	}
	t.Fatal("statement never entered a PostgreSQL lock wait")
}
