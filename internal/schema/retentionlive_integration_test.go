//go:build integration

package schema

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 39 as ADR-9 ratified it. A partition an undelivered event still references
// is not dropped, and the refusal is the *server's*: the composite foreign key makes the detach fail,
// so the guarantee survives a wrong Go guard. That is why every row here asserts the constraint error
// text and not merely that the partition survived -- a Go guard that happened to skip the drop would
// produce the same survival and carry none of the promise.
//
// The counted-discard option was rejected. Nothing here drops a partition holding a pending or
// delivering row, however well the loss is counted.

// theLiveStatuses are the two the ratified answer narrows "live" to. Each is its own row because
// neither implies the other: internal/delivery moves a row from pending to delivering when it
// claims it, and a guard written for one status would leave the other's partition droppable.
var theLiveStatuses = []string{"pending", "delivering"}

// plantEventAwaitingDelivery writes one event at an instant and one queue row referencing it, which
// is what internal/delivery's enqueue produces. It answers the event's id so a case can read the
// queue back.
func plantEventAwaitingDelivery(t *testing.T, pool *pgxpool.Pool, at time.Time, status string) int64 {
	t.Helper()

	var id int64
	err := pool.QueryRow(t.Context(), "INSERT INTO "+mustQualify(t, harnessSchema, TableEvents)+
		" (listener, table_name, operation, payload, txid, occurred_at)"+
		" VALUES ('orders', 'public.orders', 'INSERT', '{}'::jsonb, pg_current_xact_id(), $1)"+
		" RETURNING id", at).Scan(&id)
	if err != nil {
		t.Fatalf("write an event occurring at %s: %v", at, err)
	}

	mustExecOn(t, pool, "INSERT INTO "+mustQualify(t, harnessSchema, TableEventQueue)+
		" (event_id, occurred_at, listener, status, attempts, next_attempt_at)"+
		" VALUES ($1, $2, 'orders', $3, 0, now())", id, at, status)
	return id
}

// theOrphanedQueueRowsQuery counts queue rows whose event row has vanished. It is a left join and
// not a count of either table, because the invariant is about the *pair*: the queue may hold fewer
// rows and the log fewer events, and neither number says whether a row was left pointing at nothing.
const theOrphanedQueueRowsQuery = `
SELECT count(*) FROM %[1]s AS q LEFT JOIN %[2]s AS e
    ON e.id = q.event_id AND e.occurred_at = q.occurred_at
 WHERE e.id IS NULL`

// assertNoQueueRowLostItsEvent is criterion 39's unforked invariant, asserted after every case
// whatever the case's own outcome -- it is the guarantee the policy exists to protect, and inferring
// it from a drop outcome would leave the actual property untested.
func assertNoQueueRowLostItsEvent(t *testing.T, pool *pgxpool.Pool, after string) {
	t.Helper()

	var orphaned int64
	query := strings.NewReplacer("%[1]s", mustQualify(t, harnessSchema, TableEventQueue),
		"%[2]s", mustQualify(t, harnessSchema, TableEvents)).Replace(theOrphanedQueueRowsQuery)
	if err := pool.QueryRow(t.Context(), query).Scan(&orphaned); err != nil {
		t.Fatalf("count the queue rows whose event has vanished: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("%d queue rows point at events that no longer exist after %s", orphaned, after)
	}
}

// TestAPartitionAnUndeliveredEventReferencesIsRefusedByTheServer is criterion 39's refusal half, one
// named row per live status. The second range is the control: identical in every property except
// that nothing references it, so only the queue row can be what spared the first.
func TestAPartitionAnUndeliveredEventReferencesIsRefusedByTheServer(t *testing.T) {
	skipIfShort(t)

	for _, status := range theLiveStatuses {
		t.Run("a "+status+" queue row", func(t *testing.T) {
			pool, cfg := aRetainedEventLog(t)
			cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
			held := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 2)
			plantMarkedPartitions(t, pool, held)
			plantEventAwaitingDelivery(t, pool, held[0].From, status)

			report := oneRetentionPass(t, pool, cfg, Options{})

			assertTheServerRefusedTheDetach(t, report, held[0])
			survives(t, pool, harnessSchema, held[0].Name,
				"a "+status+" queue row still points into it, and the foreign key refuses the detach")
			isGone(t, pool, harnessSchema, held[1].Name,
				"nothing references it, so only the queue row can be what spared its neighbour")
			assertTheStallIsCountedApart(t, report)
			assertNoQueueRowLostItsEvent(t, pool, "a drop refused by a "+status+" queue row")
		})
	}
}

// assertTheServerRefusedTheDetach pins the sentence the refusal carries. Both clauses, because the
// second alone is the class every foreign-key violation belongs to and only the pair identifies this
// condition at this position.
func assertTheServerRefusedTheDetach(t *testing.T, report passReport, ranged Range) {
	t.Helper()

	if report.refused == nil {
		t.Fatalf("the pass answered no error at all, and a queue row references %s", ranged.Name)
	}
	for _, clause := range []string{theRemovedPartitionPhrase, theForeignKeyPhrase} {
		if !strings.Contains(report.refused.Error(), clause) {
			t.Errorf("the refusal %q does not carry %q; survival alone would also follow from a Go "+
				"guard that skipped the drop, and that carries none of the server's guarantee",
				report.refused, clause)
		}
	}
	if logged := theRefusalOf(t, report, ranged).attrs[logCause]; !strings.Contains(logged,
		theForeignKeyPhrase) {
		t.Errorf("the line reporting %s reads %q, which does not name the constraint that refused it",
			ranged.Name, logged)
	}
}

// assertTheStallIsCountedApart is criterion 39's observability half. Without a counter of its own a
// retention stalled by a down destination is indistinguishable from a pass with nothing to do --
// which is the condition most worth alerting on, since disk grows the whole time. Both directions:
// the distinct counter moves, and the one whose remedy is a drain does not.
func assertTheStallIsCountedApart(t *testing.T, report passReport) {
	t.Helper()

	if want := (statsReading{failures: 1, retentionBlockedByLiveEvents: 1}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v: one stalled range, counted under its own name as "+
			"well as under the general failure count, and nothing on the counter whose remedy is a "+
			"drain of the DEFAULT partition", report.stats, want)
	}
	if want := (Result{Outcome: OutcomeFailed, Dropped: 1}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
}
