//go:build integration

package source

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The listening-loss case nothing reached: the reconnect loop and both of its once-only latches
// were never entered. TestReconnectBackoffGrowsAndCaps is arithmetic on nextListenBackoff and would
// pass with the loop that calls it deleted, and pg_terminate_backend appeared nowhere in the
// package. The backend is terminated for real here.
//
// Correctness never depends on the notification arriving, so the safety-net assertion below issues
// no NOTIFY at all: the event is written straight into the queue and claimed.

// listenLog counts the source's documented reporting path. Its counters are guarded because the
// listening goroutine writes them while the test reads them.
type listenLog struct {
	mu     sync.Mutex
	counts map[string]int
}

func newListenLog() *listenLog { return &listenLog{counts: map[string]int{}} }

func (l *listenLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *listenLog) Handle(_ context.Context, record slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[record.Message]++
	return nil
}

func (l *listenLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *listenLog) WithGroup(string) slog.Handler      { return l }

func (l *listenLog) count(message string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[message]
}

func (l *listenLog) await(t *testing.T, message string, want int, within time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(within); ; time.Sleep(10 * time.Millisecond) {
		if l.count(message) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"the source reported %s %d times in %s, want at least %d through its "+
					"documented path", message, l.count(message), within, want,
			)
		}
	}
}

func openListeningSource(t *testing.T, pool *pgxpool.Pool, cfg config.Config, log *listenLog) *TriggerSource {
	t.Helper()
	source, err := Open(
		t.Context(), pool, cfg, Options{
			LeasedBy: "worker", Lease: time.Minute, Listen: true, Logger: slog.New(log),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source
}

// awaitWake reports whether a wake-up arrives inside the window, provoking one each round. It
// answers with a fact rather than by failing, because these cases have to observe both outcomes:
// listen_integration_test.go's waitForDedicatedListener asks the same question by failing, and
// would ideally compose this.
func awaitWake(t *testing.T, pool *pgxpool.Pool, source *TriggerSource, within time.Duration) bool {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); {
		mustExecOn(t, pool, `SELECT pg_notify('pg_noty_events_noty', '')`)
		select {
		case _, open := <-source.Notify():
			if !open {
				t.Fatal("the wake-up channel was closed by something other than Close")
			}
			return true
		case <-time.After(25 * time.Millisecond):
		}
	}
	return false
}

// assertWakeChannelStillOpen is the clause a consumer depends on: a closed channel reads as a
// finished source, and closure is Close's signal alone. A closed channel answers immediately.
func assertWakeChannelStillOpen(t *testing.T, source *TriggerSource) {
	t.Helper()
	select {
	case _, open := <-source.Notify():
		if !open {
			t.Fatal("the wake-up channel is closed while the source is still running")
		}
	default:
	}
}

func terminateTheListeningBackend(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var terminated int
	err := pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity "+
			"WHERE datname=current_database() AND pid<>pg_backend_pid() AND query LIKE 'LISTEN %') killed",
	).
		Scan(&terminated)
	if err != nil {
		t.Fatalf("terminate the listening backend: %v", err)
	}
	if terminated != 1 {
		t.Fatalf(
			"terminated %d listening backends, want the source's 1: the case does not reach "+
				"the loss it is named for", terminated,
		)
	}
}

// assertTheOtherFourStillServe commits events with no notification of any kind and moves each
// through a different transition, which is what makes "polling is the correctness guarantee" a test.
func assertTheOtherFourStillServe(t *testing.T, pool *pgxpool.Pool, source *TriggerSource) {
	t.Helper()
	seeded := []Event{
		seedQueueEvent(t, pool, "acked"), seedQueueEvent(t, pool, "nacked"), seedQueueEvent(t, pool, "deadened"),
	}
	claimed, err := source.Claim(t.Context(), len(seeded))
	if err != nil || len(claimed) != len(seeded) {
		t.Fatalf(
			"Claim with the listener down returned %d events, %v; want the %d committed "+
				"without any wake-up", len(claimed), err, len(seeded),
		)
	}
	delivery := Delivery{HTTPStatus: 200, Duration: time.Millisecond}
	if err := source.Ack(t.Context(), claimed[0], delivery); err != nil {
		t.Fatalf("Ack with the listener down: %v", err)
	}
	if err := source.Nack(t.Context(), claimed[1], delivery, source.now().Add(theRetryDelay)); err != nil {
		t.Fatalf("Nack with the listener down: %v", err)
	}
	if err := source.Dead(t.Context(), claimed[2], delivery, theDeadReason); err != nil {
		t.Fatalf("Dead with the listener down: %v", err)
	}
}

func TestALostListeningBackendCostsLatencyAndTheSourceRecovers(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	log := newListenLog()
	source := openListeningSource(t, pool, harnessConfig(t), log)
	if !awaitWake(t, pool, source, 5*time.Second) {
		t.Fatal("the dedicated listener never received a notification, so there is no loss to cause")
	}

	terminateTheListeningBackend(t, pool)
	log.await(t, theListenLostMessage, 1, 5*time.Second)
	assertWakeChannelStillOpen(t, source)
	assertTheOtherFourStillServe(t, pool, source)

	if !awaitWake(t, pool, source, 10*time.Second) {
		t.Fatal(
			"no wake-up arrived after the drop, so listening was not re-established and one " +
				"blip costs the process its latency optimisation for the rest of its life",
		)
	}
	if got := log.count(theListenLostMessage); got != 1 {
		t.Errorf(
			"one lost connection was reported %d times, want once: an operator needs to be "+
				"told that polling is now the only path, and not once per retry", got,
		)
	}
	assertWakeChannelStillOpen(t, source)
}
