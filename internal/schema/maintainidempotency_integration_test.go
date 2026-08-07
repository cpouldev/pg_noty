//go:build integration

package schema

import (
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 22 -- convergence by construction -- and criterion 29's maintenance half,
// the DEFAULT partition's permanence across three distinct passes.
//
// "No DDL" is asserted by comparing the partition inventory rather than by counting the pass's own
// calls: call counting measures the bookkeeping a defective pass gets wrong, while an inventory
// carrying every partition's bound fails even when a partition was re-created over a different
// range. Criterion 29's retention half -- that a drop never takes the DEFAULT partition with it --
// is Step 14's, and no row here is evidence for it.

// stillTheSameHorizon guards a case that ran a pass earlier and is now deriving an expectation from
// the ranges that pass was given. A clock that has crossed a grid line since legitimately asks for
// one more range, and then the case rather than the pass is what was wrong.
func stillTheSameHorizon(t *testing.T, pool *pgxpool.Pool, cfg config.Config, wanted []Range) {
	t.Helper()

	now := theClockOf(t, pool)
	if !slices.EqualFunc(RequiredRanges(now, cfg.Retention), wanted, Range.sameExtentAs) {
		t.Fatalf(
			"at %s the arithmetic asks for a horizon other than the one the converging pass was "+
				"given, so a pass over it is no longer a no-op and this run proves nothing -- re-run it", now,
		)
	}
}

// TestTwoConsecutivePassesOverAConvergedSchemaIssueNoDDLAtAll is criterion 22. Two passes rather
// than one, because a pass that converged the schema *again* -- dropping and re-creating what was
// already right -- would leave the second reporting the same thing as the first.
func TestTwoConsecutivePassesOverAConvergedSchemaIssueNoDDLAtAll(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	before := partitionInventory(t, pool)

	for _, during := range []string{
		"a pass over an already-correct schema",
		"a second pass immediately after it",
	} {
		report := onePass(t, pool, cfg, Options{})
		stillTheSameHorizon(t, pool, cfg, wanted)

		if want := (Result{Outcome: OutcomeNothingNeeded}); report.settled != want ||
			report.refused != nil {
			t.Errorf(
				"%s reported %+v and %v, want %+v and no error",
				during, report.settled, report.refused, want,
			)
		}
		assertInventoryUnchanged(t, before, partitionInventory(t, pool), during)
	}
}

// TestAConvergedPassIssuesNothingBecauseItsPlanIsEmpty is criterion 22's *mechanism*, asserted from
// the plan's own side: over the world catalog.go reports after convergence, PlanMaintenance asks
// for no create at all. That is why the pass issues nothing -- there is no statement in the plan to
// issue, rather than an execution some guard declined to perform. A guard can be deleted and the
// idempotency claim then rests on nothing; an empty plan cannot issue a statement it does not hold.
func TestAConvergedPassIssuesNothingBecauseItsPlanIsEmpty(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	stillTheSameHorizon(t, pool, cfg, wanted)

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the converged event log: %v", err)
	}
	if len(found.Bounded) != len(wanted) {
		t.Fatalf(
			"the observation the plan is taken over holds %d partitions and the converging pass "+
				"created %d, so an empty plan would say nothing", len(found.Bounded), len(wanted),
		)
	}

	if plan := PlanMaintenance(theClockOf(t, pool), cfg, found.Bounded); len(plan.Create) != 0 {
		t.Errorf(
			"the plan over a converged event log asks for %v; criterion 22 is that there is "+
				"nothing to issue, not that something declined to issue it", namesOf(plan.Create),
		)
	}
}

// TestTheDefaultPartitionSurvivesEveryPassAsTheSameObject is criterion 29's maintenance half over
// the three states it names. Three, because a pass that dropped and re-created the DEFAULT
// partition as a side effect would show up in only one of them -- and identity rather than
// existence, because a re-created partition exists just as much as the original and holds none of
// its rows.
func TestTheDefaultPartitionSurvivesEveryPassAsTheSameObject(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	planted, _ := identityOf(t, pool, harnessSchema, PartitionDefault)
	if planted == 0 {
		t.Fatal(
			"the fixture carries no DEFAULT partition, so every reading below would agree that " +
				"nothing happened to it",
		)
	}

	before := theClockOf(t, pool)
	creating := onePass(t, pool, cfg, Options{})
	wanted := theHorizonBetween(t, before, theClockOf(t, pool))
	assertPassCreated(t, creating, len(wanted), "a pass that created partitions")
	assertDefaultPartitionIntact(t, pool, planted, "a pass that created partitions")

	assertPassCreated(t, onePass(t, pool, cfg, Options{}), 0, "a no-op pass")
	assertDefaultPartitionIntact(t, pool, planted, "a no-op pass")

	// One range removed by hand, so the third pass has work to do and is neither of the two above.
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, wanted[0].Name))
	stillTheSameHorizon(t, pool, cfg, wanted)
	assertPassCreated(t, onePass(t, pool, cfg, Options{}), 1, "a pass that replaced one partition")
	assertDefaultPartitionIntact(t, pool, planted, "a pass that replaced one partition")
}

// assertPassCreated is the precondition each phase above declares: a phase named for a no-op that
// created eight partitions would still find the DEFAULT partition intact, and the case named for it
// would pass without ever reaching the state it names. The expected outcome is written out here
// rather than taken from outcomeOf, so this case cannot agree with a wrong one.
func assertPassCreated(t *testing.T, report passReport, created int, during string) {
	t.Helper()

	want := Result{Outcome: OutcomeWorkDone, Created: created}
	if created == 0 {
		want.Outcome = OutcomeNothingNeeded
	}

	if report.settled != want || report.refused != nil {
		t.Fatalf(
			"%s reported %+v and %v, want %+v and no error",
			during, report.settled, report.refused, want,
		)
	}
}

// assertDefaultPartitionIntact is criterion 29's assertion: the DEFAULT partition is still there,
// and it is still the same object.
func assertDefaultPartitionIntact(t *testing.T, pool *pgxpool.Pool, planted int64, during string) {
	t.Helper()

	switch identity, _ := identityOf(t, pool, harnessSchema, PartitionDefault); {
	case identity == 0:
		t.Errorf(
			"%s left no %s at all; every write for an uncovered range now fails outright "+
				"instead of landing somewhere recoverable", during, PartitionDefault,
		)
	case identity != planted:
		t.Errorf(
			"%s replaced %s with a different object (%d, was %d), so it was dropped and "+
				"re-created as a side effect and any row it held is gone",
			during, PartitionDefault, identity, planted,
		)
	}
}
