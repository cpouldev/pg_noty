//go:build integration

package schema

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Criteria 5, 6 and 11's shape half: how the contract's tables are partitioned and how they are
// keyed. The uniqueness property the catalog does *not* enforce is criterion 6's third fact, next
// door in objectcontractidentity_integration_test.go.
//
// The three facts AC 39 and M1 touch are asserted separately and deliberately. The composite
// primary key on the event log is forced by the server -- M1 measured both PRIMARY KEY (id) and
// UNIQUE (id) refused on a partitioned table -- so it is evidence about the server and about
// neither the queue's occurred_at column nor the composite foreign key. Those two are the ratified
// answer (ADR-9, Accepted), and each is asserted in its own right because the column without the key
// composes nothing and a check finding only one of them would pass on a shape that composes nothing.

// partitionKeyQuery reads how a table is partitioned, from the catalog. It answers NULL for a table
// that is not partitioned at all, which coalesces to the empty string and fails the comparison
// below rather than the scan.
const partitionKeyQuery = `SELECT coalesce(pg_get_partkeydef(to_regclass($1)), '')`

// TestTheEventLogIsRangePartitionedOnOccurredAt is criterion 5's shape half. The expectation is
// derived from the contract's own clause rather than transcribed from what a run answered: the
// catalog reports the clause without its leading keywords.
func TestTheEventLogIsRangePartitionedOnOccurredAt(t *testing.T) {
	skipIfShort(t)

	want := strings.TrimPrefix(theEventLogPartitionClause, "PARTITION BY ")

	var declared string
	err := migratedSchema(t).QueryRow(t.Context(), partitionKeyQuery,
		mustQualify(t, harnessSchema, TableEvents)).Scan(&declared)
	if err != nil {
		t.Fatalf("read how %s.%s is partitioned: %v", harnessSchema, TableEvents, err)
	}
	if declared != want {
		t.Errorf("%s is partitioned by %q, want %q; without range partitioning on occurred_at "+
			"every partition decision this package makes has nothing to act on",
			TableEvents, declared, want)
	}
}

// TestEveryContractTableIsKeyedAsTheContractStates is criterion 6's key half and criterion 11's
// shape half in one closed inventory, read from the catalog rather than from the DDL text -- the
// text saying PRIMARY KEY (version) and the constraint existing are different claims, and for the
// ledger that constraint is the concurrency mechanism ADR-5 rests on.
func TestEveryContractTableIsKeyedAsTheContractStates(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	for _, table := range theContractTables {
		want, stated := theContractKeys[table]
		if !stated {
			t.Errorf("the contract states no key for %s, so its key is asserted nowhere", table)
			continue
		}
		if got := primaryKeyColumnsOf(t, pool, harnessSchema, table); !slices.Equal(got, want) {
			t.Errorf("%s is keyed on %v in the catalog, and the contract states %v", table, got, want)
		}
	}
}

// foreignKeysQuery reads a table's foreign keys structurally -- the referencing columns in key
// order, the relation referenced, and its columns in key order -- rather than through
// pg_get_constraintdef, whose text is a server rendering that would have to be transcribed from a
// run before it could be compared.
//
// conparentid = 0 is what makes this the *declared* keys. Measured on PostgreSQL 17.10: a foreign
// key onto a partitioned table leaves one further pg_constraint row per partition of the referenced
// table, each carrying the declared constraint as its parent -- so the queue reports two rows today,
// one naming events and one naming events_default, and would report one more per range partition a
// maintenance pass creates. Filtering by name instead would make this inventory's answer depend on
// how many partitions happen to exist, and a version that stopped parenting the declared row would
// fail here rather than pass.
const foreignKeysQuery = `
SELECT (SELECT array_agg(a.attname::text ORDER BY k.ord)
          FROM unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
          JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum),
       referenced.relname::text,
       (SELECT array_agg(a.attname::text ORDER BY k.ord)
          FROM unnest(c.confkey) WITH ORDINALITY AS k(attnum, ord)
          JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = k.attnum)
  FROM pg_constraint c
  JOIN pg_class referenced ON referenced.oid = c.confrelid
 WHERE c.conrelid = to_regclass($1) AND c.contype = 'f' AND c.conparentid = 0
 ORDER BY c.conname`

func foreignKeysOn(t *testing.T, pool *pgxpool.Pool, table string) []foreignKey {
	t.Helper()

	rows, err := pool.Query(t.Context(), foreignKeysQuery, mustQualify(t, harnessSchema, table))
	if err != nil {
		t.Fatalf("read the foreign keys of %s.%s: %v", harnessSchema, table, err)
	}
	defer rows.Close()

	var declared []foreignKey
	for rows.Next() {
		var found foreignKey
		if err := rows.Scan(&found.columns, &found.references, &found.referenced); err != nil {
			t.Fatalf("scan a foreign key of %s.%s: %v", harnessSchema, table, err)
		}
		declared = append(declared, found)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the foreign keys of %s.%s: %v", harnessSchema, table, err)
	}
	return declared
}

// TestTheQueueDeclaresTheRatifiedCompositeKeyAndNoOther is AC 39's key half, as a closed inventory.
// An open check that merely found the composite key would admit a stray constraint beside it, and a
// second foreign key on the queue is a second reason a partition drop can be refused -- which is
// the mechanism retention's blast-radius guards are reasoned about against.
func TestTheQueueDeclaresTheRatifiedCompositeKeyAndNoOther(t *testing.T) {
	skipIfShort(t)

	got := foreignKeysOn(t, migratedSchema(t), TableEventQueue)
	if len(got) != 1 {
		t.Fatalf("%s declares %d foreign keys %+v, want exactly the ratified composite one",
			TableEventQueue, len(got), got)
	}

	want := theRatifiedCompositeKey
	if !slices.Equal(got[0].columns, want.columns) || got[0].references != want.references ||
		!slices.Equal(got[0].referenced, want.referenced) {
		t.Errorf("%s declares %+v, want %+v; this key is what makes the refusal to drop a partition "+
			"holding an undelivered event the server's rather than our Go guard's",
			TableEventQueue, got[0], want)
	}
}

// TestTheQueueCarriesOccurredAtAsTheKeysOtherHalf is the column in its own right. It exists for no
// reason but composing the key above, so finding one without the other is finding a shape that
// composes nothing: the column alone is an unexplained denormalisation, and the key alone cannot be
// declared at all.
func TestTheQueueCarriesOccurredAtAsTheKeysOtherHalf(t *testing.T) {
	skipIfShort(t)

	observed := observedColumnsOf(t, migratedSchema(t), TableEventQueue)
	got, present := observed[theRatifiedCompositeKey.columns[1]]
	if !present {
		t.Fatalf("%s carries %v and not %s, which AC 39's ratified answer requires",
			TableEventQueue, slices.Sorted(maps.Keys(observed)), theRatifiedCompositeKey.columns[1])
	}
	if !got.notNull {
		t.Errorf("%s.%s is nullable, and a null there points the composite key at no partition at all",
			TableEventQueue, theRatifiedCompositeKey.columns[1])
	}
}
