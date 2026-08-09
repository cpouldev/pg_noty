//go:build integration

package source

import (
	"context"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func installInsertTrigger(t *testing.T, pool *pgxpool.Pool, instance, listener, table string) {
	t.Helper()
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Instance, request.Listener.Name, request.Target.Table = instance, listener, table
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
}

func openNotificationConnection(t *testing.T, cfg config.Config, instance string) *pgx.Conn {
	t.Helper()
	connection, err := pgx.Connect(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	channel, fault := schema.Quoted("pg_noty_events_" + instance)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	if _, err := connection.Exec(t.Context(), "LISTEN "+channel); err != nil {
		connection.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	return connection
}

// nextNotification waits for one notification and returns it, or nil once the timeout elapses. It
// returns the notification rather than a verdict because the literal-grammar claim reads the
// channel name the server delivered on, and a second waiter written for that would be this one with a wider return
// type.
func nextNotification(t *testing.T, connection *pgx.Conn, timeout time.Duration) *pgconn.Notification {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	delivered, err := connection.WaitForNotification(ctx)
	if err != nil {
		return nil
	}
	return delivered
}

func hasNotification(t *testing.T, connection *pgx.Conn, timeout time.Duration) bool {
	t.Helper()
	return nextNotification(t, connection, timeout) != nil
}

func TestRollbackLeavesRowsAndNotificationsAbsent(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.rollback_target (id int)`)
	installInsertTrigger(t, pool, "noty", "rollback", "rollback_target")
	listener := openNotificationConnection(t, harnessConfig(t), "noty")
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO public.rollback_target VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if hasNotification(t, listener, 250*time.Millisecond) {
		t.Fatal("rolled-back transaction delivered a notification")
	}
	events := countOn(t, pool, "SELECT count(*) FROM noty.events")
	queue := countOn(t, pool, "SELECT count(*) FROM noty.event_queue")
	if events != 0 || queue != 0 {
		t.Fatalf("rollback left events=%d queue=%d", events, queue)
	}
}

func TestThousandRowTransactionDeliversOneNotification(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.thousand_target (id int)`)
	installInsertTrigger(t, pool, "noty", "thousand", "thousand_target")
	listener := openNotificationConnection(t, harnessConfig(t), "noty")
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		t.Context(),
		`INSERT INTO public.thousand_target SELECT generate_series(1, 1000)`,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !hasNotification(t, listener, time.Second) || hasNotification(t, listener, 250*time.Millisecond) {
		t.Fatal("1000-row transaction did not produce exactly one notification")
	}
	if count := countOn(t, pool, "SELECT count(*) FROM noty.events"); count != 1000 {
		t.Fatalf("event count=%d, want one row per inserted row", count)
	}
}

func TestSameConnectionTransactionsNotifyTwice(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.twice_target (id int)`)
	installInsertTrigger(t, pool, "noty", "twice", "twice_target")
	listener := openNotificationConnection(t, harnessConfig(t), "noty")
	writer, err := pgx.Connect(t.Context(), harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(context.Background())
	for id := 1; id <= 2; id++ {
		if _, err := writer.Exec(t.Context(), "INSERT INTO public.twice_target VALUES ($1)", id); err != nil {
			t.Fatal(err)
		}
		if !hasNotification(t, listener, time.Second) {
			t.Fatalf("same connection transaction %d produced no notification", id)
		}
	}
}

func TestTwoListenersSameInstanceCoalesceGlobally(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.same_a (id int)`)
	mustExecOn(t, pool, `CREATE TABLE public.same_b (id int)`)
	installInsertTrigger(t, pool, "noty", "same_a", "same_a")
	installInsertTrigger(t, pool, "noty", "same_b", "same_b")
	listener := openNotificationConnection(t, harnessConfig(t), "noty")
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tx.Exec(t.Context(), `INSERT INTO public.same_a VALUES (1)`)
	_, _ = tx.Exec(t.Context(), `INSERT INTO public.same_b VALUES (1)`)
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !hasNotification(t, listener, time.Second) || hasNotification(t, listener, 250*time.Millisecond) {
		t.Fatal("same-instance listeners did not coalesce to one notification")
	}
}
