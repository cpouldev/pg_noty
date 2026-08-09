package schema

import (
	"slices"
	"strings"
	"testing"
)

// theDeclaredIndexes is the `**Indexes**` block of the Requirements, pinned rather than derived.
// The two partial ones carry their predicate as its own field, so a partial index that lost its
// WHERE clause fails on the predicate rather than passing on merely existing -- which is the
// difference criterion 7 asks for.
var theDeclaredIndexes = []sqlIndex{
	{name: IndexQueueClaim, table: TableEventQueue, columns: "next_attempt_at, event_id",
		predicate: "status = 'pending'"},
	{name: IndexQueueLeaseReclaim, table: TableEventQueue, columns: "leased_until",
		predicate: "status = 'delivering'"},
	{name: IndexQueueListenerStatus, table: TableEventQueue, columns: "listener, status"},
	{name: IndexDeliveriesEvent, table: TableDeliveries, columns: "event_id"},
}

func TestTheFourDeclaredIndexesAreExactlyWhatTheContractNames(t *testing.T) {
	if len(theDeclaredIndexes) != 4 {
		t.Fatalf("%d indexes are enumerated; update this count with the set",
			len(theDeclaredIndexes))
	}

	got := declaredIndexesIn(migrationSQL(t, 2))
	if len(got) != len(theDeclaredIndexes) {
		t.Fatalf("migration 2 declares %d indexes %+v, want the four the contract names",
			len(got), got)
	}
	for i, want := range theDeclaredIndexes {
		if got[i] != want {
			t.Errorf("index %d is declared %+v, want %+v", i, got[i], want)
		}
	}
}

// TestNeitherPartialIndexIsDeclaredWithoutItsPredicate is the same claim from the other side. A
// partial index written without its WHERE clause is still an index on the same columns, so the
// reconciliation above would fail on one field and could be repaired by widening the expectation;
// this row says why that repair is wrong.
func TestNeitherPartialIndexIsDeclaredWithoutItsPredicate(t *testing.T) {
	for _, want := range theDeclaredIndexes {
		if want.predicate == "" {
			continue
		}
		full := "CREATE INDEX " + want.name + " ON " + want.table + " (" + want.columns + ");"
		if declaresLine(migrationSQL(t, 2), full) {
			t.Errorf("%s is declared without its predicate %q, so rows outside it would enter "+
				"the index and the scan it exists to serve would widen", want.name, want.predicate)
		}
	}
}

// TestEveryIndexCarriesAComment is AC 7's second half. The comments are what Step 12 reads back
// from obj_description, so an index without one cannot satisfy that criterion there.
func TestEveryIndexCarriesAComment(t *testing.T) {
	comments, _ := declaredCommentsIn(migrationSQL(t, 2))

	for _, want := range theDeclaredIndexes {
		explained, present := comments["INDEX "+want.name]
		if !present {
			t.Errorf("%s carries no comment explaining the choice", want.name)
			continue
		}
		if len(strings.Fields(explained)) < 8 {
			t.Errorf("%s is explained by %q, which states no choice", want.name, explained)
		}
	}
}

// TestTheClaimIndexCommentRecordsTheCorrectedReasoning pins the correction rather than the
// Requirements text it replaces. The original comment claimed the second column "makes the scan
// index-only"; measured, the claim query takes FOR UPDATE SKIP LOCKED, row locking needs the heap
// tuple, and the plan is a row-locking node over an index scan. What the second column actually
// buys is that the claim ordering is satisfied from the index, so no sort node appears -- and a
// comment repeating the refuted claim would send Step 12 looking for a plan that cannot occur.
func TestTheClaimIndexCommentRecordsTheCorrectedReasoning(t *testing.T) {
	comments, _ := declaredCommentsIn(migrationSQL(t, 2))
	explained := comments["INDEX "+IndexQueueClaim]

	for _, required := range []string{"covering", "row locking", "sort"} {
		if !strings.Contains(explained, required) {
			t.Errorf("the claim index comment does not record %q; it reads: %q",
				required, explained)
		}
	}
}

// TestTheEventLogCarriesNoIndexOfItsOwn is the point of the split storage design, asserted where
// the DDL declares it: the claim path never touches the partitioned table, so the event log needs
// no queue index at all. Criterion 10 asserts the same closed inventory against a real catalog.
func TestTheEventLogCarriesNoIndexOfItsOwn(t *testing.T) {
	for _, found := range embeddedCorpusOrFail(t) {
		indexed := slices.ContainsFunc(declaredIndexesIn(found.sql), func(index sqlIndex) bool {
			return index.table == TableEvents || index.table == PartitionDefault
		})
		if indexed {
			t.Errorf("%s declares an index on the event log, and the primary key's own index is "+
				"the only one its key implies", found.file)
		}
	}
}
