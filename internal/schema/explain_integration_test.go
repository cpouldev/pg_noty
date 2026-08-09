//go:build integration

package schema

import (
	"reflect"
	"strings"
	"testing"
)

// Criteria 8 and 9 against a real planner: the claim index is used by phase 5's *actual* claim
// query, and it is used because it is there -- which is what the committed negative control
// establishes. The query text is testdata/phase5_claim_query.sql, held to phase 5's own bytes by
// the reverse diff in explainquery_test.go; the plan is read by explainplan_integration_test.go.

// TestTheClaimQueryPlansOnTheClaimIndexBeneathTheRowLockingNode is criterion 8 and its committed
// negative control, in that order against one queue: the same query, the same rows, the same
// statistics, differing only in whether the claim index exists. The control demands the exact
// negation of what the positive case demands, so neither can pass vacuously and neither could pass
// in the other's condition (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
//
// The two are subtests of one function rather than two top-level tests because two top-level tests
// would let `-run` select the control alone, where a dropped index is all there is to see.
func TestTheClaimQueryPlansOnTheClaimIndexBeneathTheRowLockingNode(t *testing.T) {
	skipIfShort(t)

	pool, claim := aPopulatedQueue(t), theClaimQuery(t)
	// Read before anything drops it, so the third case below can rebuild it differing in exactly one
	// property (.claude/rules/repair-the-class-not-the-reproduction.md).
	asShipped := oneStringOf(t, pool, "SELECT pg_get_indexdef(to_regclass($1))", theClaimIndex)

	t.Run("criterion 8: the claim index is used", func(t *testing.T) {
		root, rendered := planOf(t, pool, claim, theClaimArguments()...)

		assertPlanFacts(t, claimPlanFactsOf(root), claimPlanFacts{
			claimIndexBeneathTheLock: true,
			indexCondOnNextAttemptAt: true,
		}, rendered)
	})

	t.Run("the negative control: the same query and data with the claim index dropped", func(t *testing.T) {
		mustExecOn(t, pool, "DROP INDEX "+mustQualify(t, harnessSchema, theClaimIndex))
		root, rendered := planOf(t, pool, claim, theClaimArguments()...)

		assertPlanFacts(t, claimPlanFactsOf(root), claimPlanFacts{
			seqScansTheQueue: true,
			sorts:            true,
		}, rendered)
	})

	// The third control is why the absence of a Sort is asserted at all: it shows the column order is
	// load-bearing rather than saying so. The index is rebuilt under its own name, partial on the same
	// predicate, differing from the shipped one in exactly the order of its two columns.
	//
	// Measured on 17.10, the outcome is stronger than criterion 8's rationale assumed: a wrongly
	// ordered index is not merely scanned with a Sort added back, it is not chosen at all, so the plan
	// is the one with no claim index whatsoever. Either way the case fails the positive assertion.
	t.Run("the column-order control: the same index with its two columns swapped", func(t *testing.T) {
		mustExecOn(t, pool, theWronglyOrderedIndex(t, asShipped))
		root, rendered := planOf(t, pool, claim, theClaimArguments()...)

		assertPlanFacts(t, claimPlanFactsOf(root), claimPlanFacts{
			seqScansTheQueue: true,
			sorts:            true,
		}, rendered)
	})
}

// theWronglyOrderedIndex is the shipped claim index's own definition with its two columns swapped
// and nothing else touched -- taken from the catalog rather than written out here, so the control
// cannot come to differ from the shipped index in some second property nobody noticed.
func theWronglyOrderedIndex(t *testing.T, asShipped string) string {
	t.Helper()

	written := "(" + theClaimOrderingColumn + ", " + theClaimTieBreakColumn + ")"
	swapped := "(" + theClaimTieBreakColumn + ", " + theClaimOrderingColumn + ")"
	if !strings.Contains(asShipped, written) {
		t.Fatalf("the shipped %s is not over %s, so swapping its columns would build some other "+
			"index: %s", theClaimIndex, written, asShipped)
	}
	return strings.Replace(asShipped, written, swapped, 1)
}

// assertPlanFacts compares a plan against the shape a case demands, one clause at a time, so a plan
// breaking two of them says so twice rather than reporting whichever the reader looked at first
// (.claude/rules/isolate-each-clause-of-a-multi-clause-guard.md).
func assertPlanFacts(t *testing.T, got, want claimPlanFacts, rendered string) {
	t.Helper()

	clauses := []struct {
		named     string
		got, want bool
	}{
		{"the claim index scanned beneath the " + lockRowsNode + " node",
			got.claimIndexBeneathTheLock, want.claimIndexBeneathTheLock},
		{"an index condition on " + theClaimOrderingColumn,
			got.indexCondOnNextAttemptAt, want.indexCondOnNextAttemptAt},
		{"a " + seqScanNode + " on " + theQueueTable, got.seqScansTheQueue, want.seqScansTheQueue},
		{"a " + sortNode + " node", got.sorts, want.sorts},
	}

	// The clause list is closed over the facts. A fact added to claimPlanFacts and not to the list
	// would narrow the positive assertion and its control in the same stroke, and both would stay
	// green (.claude/rules/assert-a-set-wide-invariant-over-the-set.md).
	if read := reflect.TypeOf(claimPlanFacts{}).NumField(); len(clauses) != read {
		t.Fatalf("%d clauses are asserted and a plan is read for %d facts", len(clauses), read)
	}

	for _, clause := range clauses {
		if clause.got != clause.want {
			t.Errorf("the plan has %s = %t, want %t:\n%s", clause.named, clause.got, clause.want, rendered)
		}
	}
}

// TestTheInnerPredicateWithoutRowLockingPlansAsAnIndexOnlyScan is criterion 9. It pins the property
// the claim index was designed for -- it covers the predicate and the ordering -- and it is why
// criterion 8 does not demand an index-only scan: the difference between the two plans is the row
// locking and nothing else, so the covering index is defeated by the lock rather than by its shape.
func TestTheInnerPredicateWithoutRowLockingPlansAsAnIndexOnlyScan(t *testing.T) {
	skipIfShort(t)

	pool := aPopulatedQueue(t)

	t.Run("criterion 9: the index is covering where row locking does not defeat it", func(t *testing.T) {
		root, rendered := planOf(t, pool, innerPredicateOf(t, theClaimQuery(t)), int32(theClaimBatch))

		scan, found := firstNodeOfType(root, indexOnlyScanNode)
		if !found {
			t.Fatalf("the claim query's inner predicate plans with no %s, so the claim index is not "+
				"covering the predicate and the ordering it was built for:\n%s", indexOnlyScanNode, rendered)
		}
		if scan.IndexName != theClaimIndex {
			t.Errorf("the %s reads %s, want %s:\n%s",
				indexOnlyScanNode, scan.IndexName, theClaimIndex, rendered)
		}
	})
}

const (
	theSubqueryOpener   = "IN (\n"
	theRowLockingClause = " FOR UPDATE SKIP LOCKED)"
)

// innerPredicateOf is the claim query's inner SELECT with the row locking removed, derived from the
// fixture rather than written out a second time -- so a change to phase 5's predicate reaches
// criterion 9 as well, instead of leaving it planning a query that no longer exists anywhere.
//
// One further rebinding is needed here and is named rather than buried: the inner SELECT's only
// parameter is $3, and a statement referencing $3 requires $1 and $2 to be typable, which this
// derivation has just dropped. So the batch size is renumbered to $1. That belongs to the derived
// query alone; the fixture still reads `LIMIT $3`, which
// TestTheFixtureKeepsPhase5sLimitAndRowLockingClause pins.
func innerPredicateOf(t *testing.T, claim string) string {
	t.Helper()

	_, inner, opened := strings.Cut(claim, theSubqueryOpener)
	inner, _, locking := strings.Cut(inner, theRowLockingClause)
	if !opened || !locking {
		t.Fatalf("the claim query does not open a subquery with %q and close it with %q, so its "+
			"inner predicate cannot be derived from it:\n%s", theSubqueryOpener, theRowLockingClause, claim)
	}
	return strings.ReplaceAll(inner, "$3", "$1")
}
