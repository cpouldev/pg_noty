//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the other half of criterion 39: the narrowing that keeps a single permanently-failed
// webhook from turning retention into unbounded growth. A row moved to dead is a decision to permit
// its event's eventual deletion, so those rows are reaped -- inside the drop's own transaction,
// immediately before the detach, because a reap in a transaction of its own would delete queue rows
// and then fail to drop, losing them for nothing.

// queueRowsInStatus is how many queue rows the schema holds in one status.
func queueRowsInStatus(t *testing.T, pool *pgxpool.Pool, status string) int64 {
	t.Helper()

	var held int64
	err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+
		mustQualify(t, harnessSchema, TableEventQueue)+" WHERE status = $1", status).Scan(&held)
	if err != nil {
		t.Fatalf("count the %s queue rows: %v", status, err)
	}
	return held
}

// theReapsLogged is every record reporting dead queue rows one drop removed.
func theReapsLogged(report passReport) []loggedRecord {
	var reaps []loggedRecord
	for _, record := range report.logged {
		if record.message == retentionReapedMessage {
			reaps = append(reaps, record)
		}
	}
	return reaps
}

// TestAPartitionWhoseOnlyReferencesAreDeadIsDroppedAndThoseRowsReaped is criterion 39's third case.
func TestAPartitionWhoseOnlyReferencesAreDeadIsDroppedAndThoseRowsReaped(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	expired := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, expired)
	plantEventAwaitingDelivery(t, pool, expired[0].From, deadStatus)
	plantEventAwaitingDelivery(t, pool, expired[0].From.Add(time.Minute), deadStatus)

	report := oneRetentionPass(t, pool, cfg, Options{})

	if want := (Result{Outcome: OutcomeWorkDone, Dropped: 1}); report.settled != want ||
		report.refused != nil {
		t.Fatalf("the pass reported %+v and %v, want %+v and no error: nothing but dead rows "+
			"references this range", report.settled, report.refused, want)
	}
	isGone(t, pool, harnessSchema, expired[0].Name,
		"its only referencing queue rows were dead, which releases it for retention")
	if held := queueRowsInStatus(t, pool, deadStatus); held != 0 {
		t.Errorf("%d dead queue rows survive the drop of the partition their events lived in; they "+
			"are reaped inside that transaction, immediately before the detach", held)
	}
	assertTheReapWasCountedAndLogged(t, report, expired[0], "2")
	assertNoQueueRowLostItsEvent(t, pool, "a drop that reaped the dead rows referencing it")
}

// assertTheReapWasCountedAndLogged is the count an operator reads. It is asserted against the number
// of rows the case planted rather than against whatever the pass reported, and it names the range,
// so a reap attributed to the wrong partition fails here.
func assertTheReapWasCountedAndLogged(t *testing.T, report passReport, ranged Range, want string) {
	t.Helper()

	reaps := theReapsLogged(report)
	if len(reaps) != 1 {
		t.Fatalf("the pass logged %d reaps, want exactly one for %s: %+v",
			len(reaps), ranged.Name, report.logged)
	}
	if got := reaps[0].attrs[logReaped]; got != want {
		t.Errorf("the pass reports %s dead queue rows reaped, and the case planted %s", got, want)
	}
	if got := reaps[0].attrs[logRange]; got != ranged.Name {
		t.Errorf("the reap is reported against range %s, want %s", got, ranged.Name)
	}
}

// TestAReapRollsBackWithTheDropThatFailedAfterIt is ADR-7's atomicity shown rather than argued, on
// the one statement whose rollback matters most. The range holds a dead row *and* a pending one: the
// reap removes the first, the foreign key then refuses the detach over the second, and the whole
// transaction rolls back -- so the dead row is still there, rather than having been lost for a drop
// that never happened. The absence of the reap line is part of it: that line is written after the
// commit, so a rolled-back reap reports nothing.
func TestAReapRollsBackWithTheDropThatFailedAfterIt(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	expired := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, expired)
	plantEventAwaitingDelivery(t, pool, expired[0].From, deadStatus)
	plantEventAwaitingDelivery(t, pool, expired[0].From.Add(time.Minute), theLiveStatuses[0])

	report := oneRetentionPass(t, pool, cfg, Options{})

	assertTheServerRefusedTheDetach(t, report, expired[0])
	survives(t, pool, harnessSchema, expired[0].Name, "a pending queue row still references it")
	if held := queueRowsInStatus(t, pool, deadStatus); held != 1 {
		t.Errorf("%d dead queue rows survive a drop that was refused after they were reaped, want 1; "+
			"a reap in a transaction of its own would have deleted them for nothing", held)
	}
	if reaps := theReapsLogged(report); len(reaps) != 0 {
		t.Errorf("the pass reported %d reaps for a transaction that rolled back: %+v", len(reaps), reaps)
	}
	assertNoQueueRowLostItsEvent(t, pool, "a drop refused after its reap")
}

// TestARefusedRangeIsRetriedOnTheFollowingPassRatherThanDroppedFromThePlan is criterion 39's last
// clause. A stalled retention has to clear by itself once the destination comes back, so the range
// must still be planned -- and still refused, and still counted -- on the pass after the one that
// met it.
func TestARefusedRangeIsRetriedOnTheFollowingPassRatherThanDroppedFromThePlan(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	held := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, held)
	plantEventAwaitingDelivery(t, pool, held[0].From, theLiveStatuses[0])

	first := oneRetentionPass(t, pool, cfg, Options{})
	planned := namesOf(PlanMaintenance(theClockOf(t, pool), cfg, observedRanges(t, pool)).Drop)
	second := oneRetentionPass(t, pool, cfg, Options{})

	if !slices.Contains(planned, held[0].Name) {
		t.Fatalf("after a refused drop the planner offers %v, which does not name %s; a range "+
			"dropped from the plan is one that never clears when the destination comes back",
			planned, held[0].Name)
	}
	for i, report := range []passReport{first, second} {
		if want := (statsReading{failures: 1, retentionBlockedByLiveEvents: 1}); report.stats != want {
			t.Errorf("pass %d recorded %+v, want %+v on each of the two", i+1, report.stats, want)
		}
		if !blockedByLiveEvents(report.refused) {
			t.Errorf("pass %d answered %v, and the queue row holding the range is untouched; a range "+
				"that stops being refused is one that was dropped", i+1, report.refused)
		}
	}
	survives(t, pool, harnessSchema, held[0].Name, "the queue row holding it is still pending")
}

// TestTheServerRefusesTheDetachInTheWordsThisPackageClassifiesOn pins retentionrefusal.go's two
// clauses against the running server, in both directions: the detach refusal carries both, and an
// ordinary foreign-key violation carries only the class -- so a classification narrowed to that
// clause would count a row some application inserted wrongly as a stalled retention.
func TestTheServerRefusesTheDetachInTheWordsThisPackageClassifiesOn(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	held := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, held)
	plantEventAwaitingDelivery(t, pool, held[0].From, theLiveStatuses[0])

	refused := oneRetentionPass(t, pool, cfg, Options{}).refused
	if refused == nil || !blockedByLiveEvents(refused) {
		t.Fatalf("the server refused the detach with %v, which this package does not classify as a "+
			"stalled retention; the counter it moves is the one an operator alerts on", refused)
	}

	_, ordinary := pool.Exec(t.Context(), "INSERT INTO "+
		mustQualify(t, harnessSchema, TableEventQueue)+
		" (event_id, occurred_at, listener, status, attempts, next_attempt_at)"+
		" VALUES (-1, now(), 'orders', 'pending', 0, now())")
	if ordinary == nil || !strings.Contains(ordinary.Error(), theForeignKeyPhrase) {
		t.Fatalf("an insert referencing no event answered %v, and this case needs it to carry %q so "+
			"the acceptance side below is not vacuous", ordinary, theForeignKeyPhrase)
	}
	if blockedByLiveEvents(ordinary) {
		t.Errorf("an ordinary foreign-key violation (%v) reads as a stalled retention, so an "+
			"application inserting a bad row moves the counter an operator alerts on", ordinary)
	}
}
