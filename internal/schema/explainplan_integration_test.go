//go:build integration

package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The plan reader both of Step 16's plan assertions run through. Plans are read from
// `EXPLAIN (FORMAT JSON)`'s own `Node Type`/`Plans` tree and never string-matched against the text
// format, which is skill Pattern 10's measured approach: the text format is brittle across releases
// and, more to the point here, cannot express "beneath the row-locking node", which is the whole
// shape criterion 8 is about.
//
// One reader, and one set of facts read out of it, serves the positive assertion and its negative
// control. That is not only de-duplication: it is what makes the control a control. The two cases
// demand the exact negation of each other from the same measurement, so a plan satisfying both is
// unrepresentable and a reader that answered the same thing whatever it was given fails one of
// them.

// The plan node types these assertions name. `LockRows` is what `FOR UPDATE SKIP LOCKED` becomes,
// and it is the node the claim index's scan has to sit beneath.
const (
	lockRowsNode      = "LockRows"
	indexScanNode     = "Index Scan"
	indexOnlyScanNode = "Index Only Scan"
	seqScanNode       = "Seq Scan"
	sortNode          = "Sort"
)

// planNode is as much of a plan as these assertions read. The field names are EXPLAIN's own, so a
// release renaming one leaves the field zero and fails an assertion rather than silently reading a
// different key.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	IndexName    string     `json:"Index Name"`
	IndexCond    string     `json:"Index Cond"`
	Plans        []planNode `json:"Plans"`
}

// planOf explains one statement and answers with the root of its plan tree and the JSON it was read
// from, which every failure message carries so a reader is shown the plan rather than told about it.
//
// The parameters are bound *here*, at EXPLAIN time, and are never substituted into the statement:
// what is planned is the fixture's bytes and nothing else, which is what makes criterion 8's
// "verbatim" survive contact with a planner. EXPLAIN is not ANALYZEd on purpose -- the claim query
// locks rows, and executing it while the fixture is being asserted about would interact with it.
func planOf(t *testing.T, pool *pgxpool.Pool, statement string, arguments ...any) (planNode, string) {
	t.Helper()

	var rendered string
	if err := pool.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+statement, arguments...).
		Scan(&rendered); err != nil {
		t.Fatalf("EXPLAIN the statement below with %d bound parameters: %v\n%s",
			len(arguments), err, statement)
	}

	var explained []struct{ Plan planNode }
	if err := json.Unmarshal([]byte(rendered), &explained); err != nil {
		t.Fatalf("parse the plan JSON: %v\n%s", err, rendered)
	}
	if len(explained) != 1 {
		t.Fatalf("EXPLAIN answered %d plans, want exactly 1:\n%s", len(explained), rendered)
	}
	return explained[0].Plan, rendered
}

// planNodesUnder is one node and everything beneath it, in pre-order.
func planNodesUnder(node planNode) []planNode {
	found := []planNode{node}
	for _, child := range node.Plans {
		found = append(found, planNodesUnder(child)...)
	}
	return found
}

// firstNodeOfType is the first node of a type at or beneath the node given. Passing a node other
// than the root is how a *relationship* is asserted rather than the mere presence of two node types:
// a plan holding a `LockRows` and an `Index Scan` in unrelated branches answers false here.
func firstNodeOfType(node planNode, nodeType string) (planNode, bool) {
	for _, found := range planNodesUnder(node) {
		if found.NodeType == nodeType {
			return found, true
		}
	}
	return planNode{}, false
}

// claimPlanFacts is what the positive assertion and its negative control both read out of a plan.
//
// What it deliberately does not carry is measured, not forgotten (M10, on PostgreSQL 17.10):
//
//   - whether the scan is an *Index Only* Scan. `FOR UPDATE SKIP LOCKED` forecloses one, because row
//     locking needs the heap tuple that an index-only scan exists to avoid reading. Criterion 9
//     asserts the covering property where it is observable -- on the non-locking inner predicate --
//     and criterion 8 must not demand it here.
//   - whether the index's partial predicate appears as an index condition. Measured, `status =
//     'pending'` stays a *filter* on the scan even though the index is partial on it.
//
// Both would be pinning an identity the server never promised, and an assertion that can only pass
// by explaining some other query is worse than no assertion.
type claimPlanFacts struct {
	// claimIndexBeneathTheLock is criterion 8's shape: Limit -> LockRows -> Index Scan on the claim
	// index.
	claimIndexBeneathTheLock bool
	indexCondOnNextAttemptAt bool
	seqScansTheQueue         bool
	// sorts is what catches a column order mistake. An index on (event_id, next_attempt_at) would
	// still be scanned and would reintroduce the sort the claim ordering is meant to come free.
	sorts bool
}

func claimPlanFactsOf(root planNode) claimPlanFacts {
	var facts claimPlanFacts

	if locking, found := firstNodeOfType(root, lockRowsNode); found {
		scan, scanned := firstNodeOfType(locking, indexScanNode)
		facts.claimIndexBeneathTheLock = scanned && scan.IndexName == theClaimIndex
		facts.indexCondOnNextAttemptAt = facts.claimIndexBeneathTheLock &&
			strings.Contains(scan.IndexCond, theClaimOrderingColumn)
	}

	for _, node := range planNodesUnder(root) {
		if node.NodeType == seqScanNode && node.RelationName == theQueueTable {
			facts.seqScansTheQueue = true
		}
		if node.NodeType == sortNode {
			facts.sorts = true
		}
	}
	return facts
}
