//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criteria 37 and 38: everything the drop set does not name is proven untouched, in
// three concentric rings. Outside the configured schema, by comparing a full catalog inventory for
// *equality* -- the Blast radius NFR is explicit that this is not to be established by inspecting
// the statements issued, because a defect misreports exactly there. Inside it, by every object the
// object contract declares plus the two near-miss decoys. The DEFAULT partition is the third ring
// and has retentiondefault_integration_test.go to itself.
//
// Each decoy differs from a real, droppable partition in the guarded property and in nothing else:
// both carry a valid ownership marker, both are named by this package's own scheme *exactly*, and
// both are long expired. A decoy with a near-miss name or a foreign marker would be spared by an
// over-matching predicate anyway, which means it would test nothing.

// theOutsideInventoryQuery is every schema, relation, constraint and index the database holds
// outside one schema, with its owner, its partition bound, its definition and its comment, folded
// into one ordered value so two readings compare directly. Relations alone would let an altered
// constraint or a re-owned table pass.
//
// Constraints are filtered on conparentid = 0. A foreign key onto a partitioned table leaves one
// further pg_constraint row per partition of the referenced table, parented to the declared one, so
// without the filter the inventory would move whenever a partition anywhere was created or dropped
// -- which is a count of partitions and not a change outside the schema (Step 12's measurement).
//
// PostgreSQL's own schemas are excluded rather than compared: pg_toast gains and loses a schema-local
// relation for every table created or dropped anywhere, so including it would make the comparison a
// restatement of what this pass did rather than a claim about what it did not.
const theOutsideInventoryQuery = `
SELECT coalesce(string_agg(entry, E'\n' ORDER BY entry), '') FROM (
  SELECT 'schema ' || n.nspname || ' owner=' || pg_get_userbyid(n.nspowner) ||
         ' comment=' || coalesce(obj_description(n.oid, 'pg_namespace'), '-') AS entry
    FROM pg_catalog.pg_namespace n
   WHERE n.nspname <> $1 AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
  UNION ALL
  SELECT 'relation ' || n.nspname || '.' || c.relname || ' ' || c.relkind::text ||
         ' owner=' || pg_get_userbyid(c.relowner) ||
         ' bound=' || coalesce(pg_get_expr(c.relpartbound, c.oid), '-') ||
         ' comment=' || coalesce(obj_description(c.oid, 'pg_class'), '-')
    FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname <> $1 AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
  UNION ALL
  SELECT 'constraint ' || n.nspname || '.' || co.conname || ' ' || pg_get_constraintdef(co.oid)
    FROM pg_catalog.pg_constraint co JOIN pg_catalog.pg_namespace n ON n.oid = co.connamespace
   WHERE n.nspname <> $1 AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
     AND co.conparentid = 0
  UNION ALL
  SELECT 'index ' || i.schemaname || '.' || i.indexname || ' ' || i.indexdef
    FROM pg_catalog.pg_indexes i
   WHERE i.schemaname <> $1 AND i.schemaname NOT LIKE 'pg\_%' AND i.schemaname <> 'information_schema'
) inventory`

// theInventoryOutside reads that whole world as one comparable value, and fails when it is empty:
// comparing nothing against nothing reports every schema unchanged.
func theInventoryOutside(t *testing.T, pool *pgxpool.Pool, schema string) string {
	t.Helper()

	var inventory string
	if err := pool.QueryRow(t.Context(), theOutsideInventoryQuery, schema).Scan(&inventory); err != nil {
		t.Fatalf("read the catalog outside %s: %v", schema, err)
	}
	if inventory == "" {
		t.Fatalf("nothing at all exists outside %s, so comparing an inventory against it would "+
			"report every database untouched", schema)
	}
	return inventory
}

// plantUnattachedLookAlike is criterion 38's decoy: an ordinary table in the configured schema, of
// a name the partition naming scheme produces exactly, carrying this instance's own valid marker.
// It differs from a real target in one property -- it is attached to nothing -- so only guard 2 can
// spare it, and a predicate reading the shape of a name destroys it.
func plantUnattachedLookAlike(t *testing.T, pool *pgxpool.Pool, named Range) {
	t.Helper()

	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, named.Name)+
		" (occurred_at timestamptz NOT NULL)")
	claimPartition(t, pool, harnessSchema, named.Name)
	assertOwnedByThisInstance(t, pool, harnessSchema, named.Name)
}

// plantPartitionOfAnotherParentElsewhere is criterion 37's decoy: a genuine partition, long expired,
// of a *different* partitioned parent in a *different* schema, again named exactly by this scheme
// and again carrying a valid marker. It defeats "is a partition of something", which the look-alike
// does not test, and neither decoy implies the other.
func plantPartitionOfAnotherParentElsewhere(t *testing.T, pool *pgxpool.Pool, named Range) {
	t.Helper()

	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, otherSchema))
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, otherSchema, otherParentName)+
		" (occurred_at timestamptz NOT NULL) PARTITION BY RANGE (occurred_at)")
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, otherSchema, named.Name)+
		" PARTITION OF "+mustQualify(t, otherSchema, otherParentName)+forValues(named))
	claimPartition(t, pool, otherSchema, named.Name)
	assertOwnedByThisInstance(t, pool, otherSchema, named.Name)
}

// TestNothingOutsideTheDropSetIsTouchedAndEveryDecoySurvives is criteria 37 and 38 together, over a
// pass that really did drop something -- the first range is the positive control the two decoys are
// measured against, identical to them in every property but the guarded one.
func TestNothingOutsideTheDropSetIsTouchedAndEveryDecoySurvives(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	named := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 3)

	plantMarkedPartitions(t, pool, named[:1])
	plantUnattachedLookAlike(t, pool, named[1])
	plantPartitionOfAnotherParentElsewhere(t, pool, named[2])

	before := theInventoryOutside(t, pool, harnessSchema)
	if !strings.Contains(before, named[2].Name) {
		t.Fatalf("the outside-the-schema inventory does not name %s, which was just planted in %s, "+
			"so an identical reading afterwards would say nothing about that decoy",
			named[2].Name, otherSchema)
	}

	report := oneRetentionPass(t, pool, cfg, Options{})

	if want := (Result{Outcome: OutcomeWorkDone, Dropped: 1}); report.settled != want ||
		report.refused != nil {
		t.Fatalf("the pass reported %+v and %v, want %+v and no error: one attached, marked, wholly "+
			"expired partition and two decoys the plan never names",
			report.settled, report.refused, want)
	}
	isGone(t, pool, harnessSchema, named[0].Name,
		"it is the attached, marked, wholly expired partition the decoys are measured against")
	survives(t, pool, harnessSchema, named[1].Name,
		"it is a plain table, and a name matching the scheme is not evidence of a partition")
	survives(t, pool, otherSchema, named[2].Name,
		"it is a partition of another parent in another schema, outside this package's remit")

	if after := theInventoryOutside(t, pool, harnessSchema); after != before {
		t.Errorf("the catalog outside %s changed across a retention pass.\nbefore:\n%s\nafter:\n%s",
			harnessSchema, before, after)
	}
	assertEveryContractObjectSurvives(t, pool)
}

// theServiceTables are the five criterion 38 names, read out of the object contract rather than
// transcribed, so a table added there joins this assertion instead of quietly escaping it.
var theServiceTables = []string{TableListeners, TableListenerTriggers, TableEventQueue,
	TableDeliveries, TableSchemaVersion}

// assertEveryContractObjectSurvives is criterion 38, widened to the whole declared contract: the
// five service tables the criterion names, the event log itself, the DEFAULT partition and the four
// indexes. The drop predicate has to distinguish partitions of the event log from every other
// relation in the schema, and naming only the five would leave the rest untested.
func assertEveryContractObjectSurvives(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	declared := make([]string, 0, len(Objects))
	for _, object := range Objects {
		declared = append(declared, object.Name)
		survives(t, pool, harnessSchema, object.Name,
			"every object the contract declares outlives a retention pass")
	}
	for _, table := range theServiceTables {
		if !slices.Contains(declared, table) {
			t.Errorf("criterion 38 names %s and the object contract does not declare it, so this "+
				"assertion no longer covers the table the criterion is about", table)
		}
	}
}
