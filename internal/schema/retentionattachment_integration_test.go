//go:build integration

package schema

import "testing"

// This file is guard 2 and guard 4, the two the marker rows cannot evidence.
//
// Guard 2's subject is handed in as a plan rather than planned, and that is the point rather than a
// convenience: no planner would ever offer these tables, because observePartitions reads pg_inherits
// and neither of them has a row there. What a plan carrying one *is*, is what an over-matching
// predicate would produce -- or what a correct plan becomes when a partition is detached between the
// observation and the drop. Reaching the branch directly is how a fail-closed answer nothing else
// can reach is asserted at all.

// TestATableThatIsNotAPartitionOfTheEventLogIsRefusedRatherThanDetached is guard 2, isolated: the
// table carries a valid ownership marker, so guard 1 clears it; it exists, so IF EXISTS would drop
// it without complaint; and the plan holds it, so guard 4 has already done its part. Only attachment
// can refuse it, and the refusal is asserted by its own wording -- survival alone would also follow
// from the server's own refusal to detach a non-partition, which carries none of guard 2's promise
// that nothing was issued at all.
func TestATableThatIsNotAPartitionOfTheEventLogIsRefusedRatherThanDetached(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	lookAlike := rangeEndingAt(cutoff, cfg.Retention.PartitionInterval)

	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, lookAlike.Name)+
		" (occurred_at timestamptz NOT NULL)")
	claimPartition(t, pool, harnessSchema, lookAlike.Name)
	assertOwnedByThisInstance(t, pool, harnessSchema, lookAlike.Name)

	report := oneAppliedDropPlan(t, pool, cfg, []Range{lookAlike})

	assertRefusedFor(t, report, lookAlike, unattachedTable(lookAlike.Name))
	survives(t, pool, harnessSchema, lookAlike.Name,
		"a name matching the partition naming scheme is not evidence that a table is a partition")
	if want := (Result{Outcome: OutcomeFailed}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
	if want := (statsReading{failures: 1}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v", report.stats, want)
	}
}

// TestAPartitionDetachedBetweenThePlanAndTheApplyIsRefusedRatherThanDropped is guard 2's other half
// and ADR-7's own reason for one transaction. A crash between a DETACH and a DROP leaves exactly
// this: a table still carrying our marker and no longer a partition of anything. It is refused here,
// and on every later pass, rather than dropped -- and it is reported rather than skipped silently,
// because an orphan holding expired rows indefinitely is a condition an operator has to meet.
func TestAPartitionDetachedBetweenThePlanAndTheApplyIsRefusedRatherThanDropped(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	now := theClockOf(t, pool)
	planted := rangesEndingBefore(now.Add(-cfg.Retention.Keep), cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, planted)

	plan := PlanMaintenance(now, cfg, observedRanges(t, pool))
	if len(plan.Drop) != 1 {
		t.Fatalf("the planner offered %v, want the one planted expired range", namesOf(plan.Drop))
	}
	mustExecOn(t, pool, "ALTER TABLE "+mustQualify(t, harnessSchema, TableEvents)+
		" DETACH PARTITION "+mustQualify(t, harnessSchema, planted[0].Name))
	assertOwnedByThisInstance(t, pool, harnessSchema, planted[0].Name)

	report := oneAppliedDropPlan(t, pool, cfg, plan.Drop)

	assertRefusedFor(t, report, planted[0], unattachedTable(planted[0].Name))
	survives(t, pool, harnessSchema, planted[0].Name,
		"a table that is no longer a partition of the event log is outside what this pass may drop")
}

// TestCallingThePlannerAloneIssuesNoStatementAtAll is guard 4. The inventory carries every
// partition's bound and not only its name, so a planner that re-created one over a different range
// fails here too -- and the plan is required to be non-empty, because an unchanged inventory is also
// what a planner with nothing to do produces.
func TestCallingThePlannerAloneIssuesNoStatementAtAll(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	now := theClockOf(t, pool)
	planted := rangesEndingBefore(now.Add(-cfg.Retention.Keep), cfg.Retention.PartitionInterval, 3)
	plantMarkedPartitions(t, pool, planted)

	before := partitionInventory(t, pool)
	plan := PlanMaintenance(now, cfg, observedRanges(t, pool))

	if len(plan.Drop) != len(planted) {
		t.Fatalf("the planner offered %v over %v; with an empty plan this case would report an "+
			"unchanged inventory whether or not the planner issued anything",
			namesOf(plan.Drop), namesOf(planted))
	}
	assertInventoryUnchanged(t, before, partitionInventory(t, pool),
		"a call to PlanMaintenance with no apply after it")
}
