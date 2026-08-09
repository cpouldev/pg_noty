//go:build integration

package source

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPoolConfigIsACopySoAMaxConnsAssignmentConfiguresNothing pins the pgx v5.10.0 behaviour the
// exhaustion case is written around: Pool.Config returns config.Copy(), so assigning to the result
// limits nothing and a pool "limited" that way keeps handing out its default connections.
func TestPoolConfigIsACopySoAMaxConnsAssignmentConfiguresNothing(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)

	before := pool.Config().MaxConns
	pool.Config().MaxConns = 1
	if got := pool.Config().MaxConns; got != before {
		t.Fatalf("assigning to Pool.Config().MaxConns changed the pool from %d to %d; pgx now "+
			"returns the live config and poolLimitedToOneConnection can be simplified", before, got)
	}
}

func poolLimitedToOneConnection(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse the harness connection string: %v", err)
	}
	config.MaxConns = 1

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("open a pool of one connection: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// assertThePoolIsExhausted is what makes the case below about exhaustion. Without it the receive
// that follows passes against a pool with spare connections, which is the state it rules out.
func assertThePoolIsExhausted(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	if spare, err := pool.Acquire(ctx); err == nil {
		spare.Release()
		t.Fatal("the pool handed out a second connection, so it is not exhausted and the receive " +
			"below says nothing about the listener's independence from it")
	}
}

// drainWakes clears anything the warm-up left behind, so the receive that follows is evidence about
// the notification sent while the pool was exhausted rather than about an earlier one.
func drainWakes(t *testing.T, source *TriggerSource) {
	t.Helper()
	for {
		select {
		case <-source.Notify():
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

func TestDedicatedListenerSurvivesPoolExhaustion(t *testing.T) {
	skipIfShort(t)
	restoreToSnapshot(t)
	cfg := harnessConfig(t)
	pool := poolLimitedToOneConnection(t, cfg.Database.URL)

	notifier, err := pgx.Connect(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notifier.Close(context.Background()) })

	source, err := Open(t.Context(), pool, cfg, Options{Listen: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForDedicatedListener(t, pool, source)

	held, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	assertThePoolIsExhausted(t, pool)
	drainWakes(t, source)

	if _, err := notifier.Exec(t.Context(), `SELECT pg_notify('pg_noty_events_noty', '')`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.Notify():
	case <-time.After(time.Second):
		t.Fatal("dedicated listener stopped receiving while pool was exhausted")
	}
}
