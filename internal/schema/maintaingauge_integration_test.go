//go:build integration

package schema

import (
	"log/slog"
	"testing"
)

// This file is ADR-8's standing condition -- the DefaultPartitionRows gauge, stored on every pass
// and not only on the pass that was refused a range -- and the refusal a pass answers when the
// DEFAULT partition it would have read is not there at all.
//
// The gauge is what internal/cli alerts on, because it is true *before* any create is refused: by
// the time DefaultPartitionBlocked moves, the cheap window to drain the rows has usually closed.

// TestAnEventLogWithNoDefaultPartitionIsRefusedRatherThanMaintained reaches the pass's other
// refusal, the one no ordinary input takes: the observation itself could not be made. It is
// asserted rather than left to a comment, because the branch decides two things a later edit could
// quietly change -- a parent with no DEFAULT partition read as one holding no rows would report a
// healthy gauge for a schema whose write-availability net is gone.
func TestAnEventLogWithNoDefaultPartitionIsRefusedRatherThanMaintained(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, PartitionDefault))

	report := onePass(t, pool, cfg, Options{})

	if report.refused == nil {
		t.Fatalf("the pass over an event log with no %s answered no error and reported %+v",
			PartitionDefault, report.settled)
	}
	if want := (Result{Outcome: OutcomeFailed}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
	if want := (statsReading{failures: 1}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v: nothing was observed, so the gauge carries no "+
			"reading and no range was refused", report.stats, want)
	}
	if raised := report.loggedAt(slog.LevelError); len(raised) != 1 ||
		raised[0].message != unobservedMessage {
		t.Errorf("the pass logged %+v above routine operation, want one %q", raised, unobservedMessage)
	}

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil || len(found.Bounded) != 0 {
		t.Errorf("the event log holds %v after the refused pass (%v), want no partition: a pass "+
			"that could not read the world creates nothing in it", namesOf(found.Bounded), err)
	}
}

// TestTheDefaultPartitionRowsGaugeIsTheStandingConditionOnEveryPass is ADR-8's ratified refinement,
// and the three readings are the point. An empty DEFAULT reads zero, which is a reading and not an
// error; a DEFAULT holding rows reads exactly that many on a pass that attempted no create at all,
// which is what makes it the *earlier* signal; and a pass after the rows are drained reads zero
// again, which is the row a monotonic Add fails.
func TestTheDefaultPartitionRowsGaugeIsTheStandingConditionOnEveryPass(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	beyondEveryRange := wanted[len(wanted)-1].To.AddDate(1, 0, 0)
	const planted = 3

	empty := onePass(t, pool, cfg, Options{})
	assertGaugeReads(t, empty, 0, "a pass over an empty DEFAULT partition")

	for range planted {
		insertEvent(t, pool, beyondEveryRange)
	}
	holding := onePass(t, pool, cfg, Options{})
	assertGaugeReads(t, holding, planted, "a pass over a DEFAULT partition holding rows")

	// The drain is the operator's, never the pass's (ADR-8): this case performs it by hand, exactly
	// as RepairDefaultPartition will in Step 15.
	mustExecOn(t, pool, "DELETE FROM "+mustQualify(t, harnessSchema, PartitionDefault))
	drained := onePass(t, pool, cfg, Options{})
	assertGaugeReads(t, drained, 0, "a pass after the rows were drained")
}

// assertGaugeReads asserts the whole counter reading rather than the gauge alone, so "it stored the
// standing condition" is one claim with "and it refused nothing to do it": a gauge that only moved
// where a create was refused would satisfy a reading of the gauge by itself.
func assertGaugeReads(t *testing.T, report passReport, rows int64, during string) {
	t.Helper()

	if want := (statsReading{defaultPartitionRows: rows}); report.stats != want {
		t.Errorf("%s recorded %+v, want %+v", during, report.stats, want)
	}
	if want := (Result{Outcome: OutcomeNothingNeeded}); report.settled != want ||
		report.refused != nil {
		t.Fatalf("%s reported %+v and %v, want %+v and no error -- the gauge is a standing condition "+
			"and this pass had nothing to create", during, report.settled, report.refused, want)
	}
}
