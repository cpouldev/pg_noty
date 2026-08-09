//go:build integration

package schema

import (
	"maps"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 30's recovery half asserted end to end, and the complementary half beside
// it. The two subtests below are built from one fixture constructor and differ in one thing only --
// whether the drain ran -- which is what isolates the drain as the cause rather than leaving "the
// repair fixed it" resting on a pass that might have reported success all along.
//
// Every case restores the shared database when it is done. M4 measured that the blocked state does
// not self-heal, so a leftover blocked range makes unrelated later cases in this container fail.

// TestTheRecoveryChainRunsEndToEndAndOnlyWhenTheDrainDoes is the whole of criterion 30's recovery
// half: rows in DEFAULT for range R, a pass reporting ErrDefaultBlocked and leaving R uncovered,
// the drain, R's partition holding those rows, and a subsequent pass reporting coverage restored.
func TestTheRecoveryChainRunsEndToEndAndOnlyWhenTheDrainDoes(t *testing.T) {
	skipIfShort(t)

	t.Run("without the drain the range stays uncovered and no pass reports success", func(t *testing.T) {
		blocked := aBlockedEventLog(t)
		defer restoreToSnapshot(t)

		// Two further passes, because one repetition cannot tell "still blocked" from "blocked once".
		for _, during := range []string{"the pass after the refusal", "a third pass"} {
			report := onePass(t, blocked.pool, blocked.cfg, Options{})
			stillTheSameHorizon(t, blocked.pool, blocked.cfg, blocked.wanted)
			assertRangeBlocked(t, blocked.pool, report, blocked.blocked, during)
		}
	})

	t.Run("with the drain the rows are routed into the new partition and coverage is restored",
		func(t *testing.T) {
			blocked := aBlockedEventLog(t)
			defer restoreToSnapshot(t)
			was := eventInventory(t, blocked.pool)

			done, err := RepairDefaultPartition(t.Context(), blocked.pool, blocked.cfg, Options{},
				blocked.blocked)
			if err != nil {
				t.Fatalf("drain %s out of the DEFAULT partition: %v", extentOf(blocked.blocked), err)
			}
			assertDrained(t, blocked, was, done)
			assertCoverageRestored(t, blocked)
		})
}

// assertDrained is everything the drain has to have preserved and the one thing it has to have
// changed, over the rows it was given.
func assertDrained(t *testing.T, blocked blockedLog, was map[int64]eventRow, done RepairResult) {
	t.Helper()

	now := eventInventory(t, blocked.pool)
	if before, after := slices.Sorted(maps.Keys(was)), slices.Sorted(maps.Keys(now)); !slices.Equal(before, after) {
		t.Fatalf("the log held events %v before the drain and %v after it; events.id is unique only by "+
			"construction (M1), and internal/reconcile and internal/delivery resolve "+
			"event_queue.event_id through exactly these values", before, after)
	}

	_, landed := identityOf(t, blocked.pool, harnessSchema, blocked.blocked.Name)
	for _, id := range blocked.drained {
		if now[id].columns != was[id].columns {
			t.Errorf("event %d reads %s after the drain and read %s before it; the re-insert has to "+
				"carry every column and not only the identity", id, now[id].columns, was[id].columns)
		}
		if now[id].partition != landed {
			t.Errorf("event %d is held by %s after the drain, want %s -- a drain that put every row "+
				"back where it came from produces an identical row count and restores nothing",
				id, now[id].partition, landed)
		}
	}

	assertMovedCountIsThePlantedCount(t, blocked, done)
	assertQueueStillResolvesToItsEvents(t, blocked.pool)
	// Criterion 29: the drain empties a range *out of* the DEFAULT partition and never drops it.
	assertDefaultPartitionIntact(t, blocked.pool, blocked.defaultPartition, "the drain")
}

// assertMovedCountIsThePlantedCount is SC-5 against the count this suite *planted* rather than
// against a number the drain reported about itself. The third clause ties the two gauge readings
// to it, so the blast radius an operator reads before and after the drain has to agree with the
// move it reports.
func assertMovedCountIsThePlantedCount(t *testing.T, blocked blockedLog, done RepairResult) {
	t.Helper()

	planted := int64(len(blocked.drained))
	if done.Moved != planted {
		t.Errorf("the drain reports %d rows moved and %d were planted in %s",
			done.Moved, planted, extentOf(blocked.blocked))
	}
	if done.QueueMoved != planted {
		t.Errorf("the drain reports %d queue rows moved and each of the %d planted events was "+
			"enqueued", done.QueueMoved, planted)
	}
	if done.Partition != blocked.blocked.Name {
		t.Errorf("the drain reports partition %s and was asked for %s", done.Partition, blocked.blocked.Name)
	}
	if fell := done.DefaultRowsBefore - done.DefaultRowsAfter; fell != done.Moved {
		t.Errorf("the DEFAULT partition read %d rows before the drain and %d after it, a fall of %d, "+
			"and the drain reports %d moved", done.DefaultRowsBefore, done.DefaultRowsAfter, fell, done.Moved)
	}
}

// assertQueueStillResolvesToItsEvents closes the mapping internal/reconcile and internal/delivery
// depend on, which the id set alone does not: a drain that renumbered the events would leave the
// queue pointing at nothing, and the left join is what says so directly.
func assertQueueStillResolvesToItsEvents(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	queue, events := mustQualify(t, harnessSchema, TableEventQueue), mustQualify(t, harnessSchema, TableEvents)
	statement := countRowsPrefix + queue + " q WHERE NOT EXISTS (SELECT 1 FROM " + events +
		" e WHERE e.id = q.event_id AND e.occurred_at = q.occurred_at)"

	var orphaned int
	if err := pool.QueryRow(t.Context(), statement).Scan(&orphaned); err != nil {
		t.Fatalf("count the queue rows whose event has vanished: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("%d queue rows no longer resolve to an event; internal/reconcile and "+
			"internal/delivery read every payload through event_queue.event_id, so an orphan there "+
			"is an undeliverable event", orphaned)
	}
}

// assertCoverageRestored is the chain's last link, and it is a *pass* reporting restoration rather
// than the partition merely existing: a pass that still reported a shortfall over a partition that
// exists is exactly the failure this link is here to catch.
func assertCoverageRestored(t *testing.T, blocked blockedLog) {
	t.Helper()

	report := onePass(t, blocked.pool, blocked.cfg, Options{})
	stillTheSameHorizon(t, blocked.pool, blocked.cfg, blocked.wanted)

	if want := (Result{Outcome: OutcomeNothingNeeded}); report.settled != want || report.refused != nil {
		t.Errorf("the pass after the drain reported %+v and %v, want %+v and no error -- the drain "+
			"created the one range that was missing, so there is nothing left to do",
			report.settled, report.refused, want)
	}
	if !coversRange(t, blocked.pool, blocked.blocked) {
		t.Errorf("%s is still uncovered after a drain that reported success", extentOf(blocked.blocked))
	}
	if short := CoverageShortfall(observedBounds(t, blocked.pool), theClockOf(t, blocked.pool),
		blocked.cfg.Retention.Precreate); short != 0 {
		t.Errorf("the partitions in place fall %s short of the horizon after the drain; coverage "+
			"counted from now stops at the first hole, and the drained range was that hole", short)
	}
}
