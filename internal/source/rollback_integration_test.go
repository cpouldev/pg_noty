//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The rollback claim's own control. TestRollbackLeavesRowsAndNotificationsAbsent
// (notify_integration_test.go) asserts that an aborted transaction leaves no event row, no queue
// row and delivers no notification -- three absences, and an absence is also what a fixture that
// never installed a working trigger produces. Nothing in that test distinguishes "the rollback
// discarded the write" from "the write was never watched", so this runs the same fixture, commits,
// and requires all three to appear.
func TestTheRollbackFixtureProducesTheRowsAndNotificationWhenItCommits(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.rollback_target (id int)`)
	installInsertTrigger(t, pool, "noty", "rollback", "rollback_target")
	listener := openNotificationConnection(t, harnessConfig(t), "noty")

	transaction, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(t.Context(), `INSERT INTO public.rollback_target VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !hasNotification(t, listener, time.Second) {
		t.Error("the committed transaction delivered no notification, so the absence the rollback " +
			"case reports is not evidence that the rollback discarded anything")
	}
	for _, table := range []string{"noty.events", "noty.event_queue"} {
		if got := rowCountOf(t, pool, table); got != 1 {
			t.Errorf("the committed transaction left %d rows in %s, want exactly 1", got, table)
		}
	}
}

// rowCountOf fails on the read as well as on the value. The rollback case discards both of its scan
// errors, so an unreachable event log answers zero there and reads as the rollback having worked; a
// count this control cannot read is reported as a failed read instead.
func rowCountOf(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count the rows of %s: %v", table, err)
	}
	return count
}
