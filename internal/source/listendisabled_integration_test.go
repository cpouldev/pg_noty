//go:build integration

package source

import (
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The third listening-loss case: listening disabled entirely. The other three take listening away
// from a source that asked for it; this one never asks, which is what a deployment behind a transaction-mode
// pooler runs as permanently rather than as an incident. The design promise is that correctness never
// depends on the notification arriving, so every part of it has to hold with the optimisation off --
// which is why the safety-net clause here issues no NOTIFY at all and claims what was committed.
//
// Two clauses are needed to say "disabled", not one. A source that ignored Options.Listen would open a
// dedicated backend; one that piggy-backed the handed-in pool would deliver wake-ups without opening
// anything. The backend count and the notification probe therefore rule out different implementations
// and neither substitutes for the other.

const (
	// theDisabledSettlingWindow is how long the server is watched for a backend that must not appear.
	// listen.go's loop reaches pgx.Connect on its first iteration with no delay before it, so a
	// listening source is visible at the server within milliseconds and this covers that many times over.
	theDisabledSettlingWindow = 500 * time.Millisecond
	// theDisabledProbeWindow is how long a wake-up is provoked for. awaitWake issues one pg_notify per
	// round, so this is tens of chances for a notification that must not be delivered.
	theDisabledProbeWindow = 250 * time.Millisecond
	// theCloseDeadline turns a Close that never returns into a named failure rather than a hang that
	// would take the rest of the package's cases down with it.
	theCloseDeadline = 5 * time.Second
)

// assertNoDedicatedBackendOpens is the strongest available statement of "disabled entirely": the count
// is the server's own, so it answers about connections rather than about the flag that was handed in.
// The other side of the guard is listen_integration_test.go's
// TestDedicatedListenerUsesConfiguredURLAndOneBackend, which measures this same count rising by one
// when Listen is true -- without that twin, a counter that never moved would pass here.
func assertNoDedicatedBackendOpens(t *testing.T, pool *pgxpool.Pool, before int) {
	t.Helper()
	if before < 1 {
		t.Fatal("the server reports no client backend even for the pool's own connection, so this " +
			"counter sees nothing and \"no backend was opened\" is not a measurement")
	}
	for deadline := time.Now().Add(theDisabledSettlingWindow); time.Now().Before(deadline); {
		if got := clientBackendCount(t, pool); got != before {
			t.Fatalf("client backends went from %d to %d with Options.Listen false: a dedicated "+
				"connection was opened, so listening is not disabled at all", before, got)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// assertListeningWasNeverReported ranges over the whole set listen.go can report rather than over a
// copy of the two this case was written for, so a message added there is covered on the day it is
// added. Nothing was attempted, so there is no loss to surface: an operator told about one would be
// told something untrue.
func assertListeningWasNeverReported(t *testing.T, log *listenLog) {
	t.Helper()
	if len(reportedListenMessages) != 3 {
		t.Fatalf("the reported-message set holds %d entries; this clause is only silence about "+
			"listening if it ranges over all of listen.go's", len(reportedListenMessages))
	}
	for _, message := range reportedListenMessages {
		if got := log.count(message); got != 0 {
			t.Errorf("the source reported %q %d times with Options.Listen false, having attempted no "+
				"connection: there is no loss to surface, so the report names one that never happened",
				message, got)
		}
	}
}

// assertCloseReturnsAndClosesTheChannel drives Close off the test goroutine on purpose. Open closes
// listenerDone before returning on this path, so a Close that waited on it wrongly would hang rather
// than fail, and the hang would be charged to whichever case the package timeout happened to kill.
func assertCloseReturnsAndClosesTheChannel(t *testing.T, source *TriggerSource) {
	t.Helper()
	returned := make(chan error, 1)
	go func() { returned <- source.Close() }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Close returned %v with listening disabled, want nil", err)
		}
	case <-time.After(theCloseDeadline):
		t.Fatalf("Close did not return within %s: Open closes listenerDone on the non-listening path, "+
			"so the wait Close makes is satisfied before it is made", theCloseDeadline)
	}
	select {
	case _, open := <-source.Notify():
		if open {
			t.Error("Close left a value in the wake-up channel; with listening disabled nothing can " +
				"have signalled one")
		}
	case <-time.After(time.Second):
		t.Error("the wake-up channel is still open after Close returned, so a consumer selecting on " +
			"it never learns the source has finished")
	}
}

func TestListeningDisabledEntirelyStillServesThroughPolling(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	log := newListenLog()
	before := clientBackendCount(t, pool)

	// openListeningSource fixes Listen: true, and this case may write no file but its own, so the
	// source is opened here rather than by widening that helper. Everything else below is that
	// file's.
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{
		LeasedBy: "worker", Lease: time.Minute, Listen: false, Logger: slog.New(log),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	assertNoDedicatedBackendOpens(t, pool, before)
	assertWakeChannelStillOpen(t, source)
	// The near miss. A source that read Options.Listen but connected through the pool anyway opens no
	// backend of its own, so the clause above passes for it and only a delivered notification tells
	// the two apart. assertNoWakeUpArrives states the same fact for the refusing-endpoint case and
	// names that case's reason, which is not this one's.
	if awaitWake(t, pool, source, theDisabledProbeWindow) {
		t.Error("a wake-up arrived with Options.Listen false, so the flag decides nothing and the " +
			"source listens whatever it is told")
	}

	// Committed with no notification of any kind, then claimed: this is the polling safety net, and
	// it is the whole reason the clauses above are allowed to be true.
	assertTheOtherFourStillServe(t, pool, source)
	assertWakeChannelStillOpen(t, source)
	if got := clientBackendCount(t, pool); got != before {
		t.Errorf("client backends were %d when the source opened and %d after all four transitions "+
			"had run, want %d for its whole running life", before, got, before)
	}
	assertListeningWasNeverReported(t, log)
	assertCloseReturnsAndClosesTheChannel(t, source)
}
