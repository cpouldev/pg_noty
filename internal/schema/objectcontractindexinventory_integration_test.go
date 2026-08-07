//go:build integration

package schema

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 10: the event log and every partition of it, including DEFAULT, carry exactly the index
// their primary key implies and nothing else. It is a closed inventory with its size pinned because
// its earlier formulation -- forbidding indexes on columns criterion 5 already proves absent --
// could not fail, and a stray index on the partitioned table is what it exists to catch.
//
// The planned case "the per-relation inventory is closed with its size pinned, so a stray index
// fails it" was listed as container-free, and it is here instead: the subject is what the catalog
// holds per relation, and there is nothing container-free to close such an inventory over. It is
// TestAStrayIndexOnTheEventLogFailsTheClosedInventory below, which plants the stray index rather
// than describing one.

// eventLogRelations is the event log and every partition attached to it, read through the package's
// own observation authority rather than assembled from names -- so a partition created by anything
// at all joins the inventory below.
func eventLogRelations(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions of %s: %v", TableEvents, err)
	}

	// Named rather than left to the count below, which an empty name would satisfy: criterion 10
	// says "including DEFAULT", so a parent carrying no DEFAULT partition has to fail as that
	// rather than as a relation holding no index.
	if found.Default == "" {
		t.Fatalf("%s has no DEFAULT partition, so the inventory below would range over a relation "+
			"with no name at all", TableEvents)
	}

	relations := []string{TableEvents, found.Default}
	for _, bounded := range found.Bounded {
		relations = append(relations, bounded.Name)
	}
	return relations
}

// relationIndexesQuery is every index on one relation. pg_indexes lists a partition's own indexes
// as well as the parent's, so a stray index on one partition is visible here where a query against
// the parent alone would miss it.
const relationIndexesQuery = `
SELECT indexname FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 ORDER BY indexname`

func indexNamesOn(t *testing.T, pool *pgxpool.Pool, relation string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), relationIndexesQuery, harnessSchema, relation)
	if err != nil {
		t.Fatalf("read the indexes on %s.%s: %v", harnessSchema, relation, err)
	}
	defer rows.Close()

	carried, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect the indexes on %s.%s: %v", harnessSchema, relation, err)
	}
	return carried
}

// indexInventoryIssues reports how the indexes one relation carries diverge from criterion 10's
// closed inventory: exactly one index, the one the primary key implies, over the key's own columns.
// It answers issues rather than failing, so the stray-index case below drives this very function.
func indexInventoryIssues(t *testing.T, pool *pgxpool.Pool, relation string) []string {
	t.Helper()

	carried := indexNamesOn(t, pool, relation)
	if len(carried) != 1 {
		return []string{fmt.Sprintf("%s carries %d indexes %v, want only the one its primary key "+
			"implies", relation, len(carried), carried)}
	}

	var issues []string
	want := theContractKeys[TableEvents]
	if got := indexColumnsOf(t, pool, carried[0]); !slices.Equal(got, want) {
		issues = append(issues, fmt.Sprintf("%s's only index %s is over %v, want the key's own %v",
			relation, carried[0], got, want))
	}
	if primary := oneStringOf(t, pool, isPrimaryKeyIndexQuery, carried[0]); primary != "true" {
		issues = append(issues, carried[0]+" is not the primary key's own index, so this relation "+
			"carries one of its own and its key's index is somewhere else entirely")
	}
	return issues
}

const isPrimaryKeyIndexQuery = `
SELECT i.indisprimary::text FROM pg_index i WHERE i.indexrelid = to_regclass($1)`

// TestTheEventLogAndEveryPartitionCarryTheKeyIndexAndNothingElse is criterion 10, as the closed
// inventory its own text demands. The earlier formulation forbade indexing columns criterion 5
// already proves absent and therefore could not fail; a count that is closed can.
func TestTheEventLogAndEveryPartitionCarryTheKeyIndexAndNothingElse(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	planted := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, planted)

	relations := eventLogRelations(t, pool)
	if want := len(planted) + 2; len(relations) != want {
		t.Fatalf("the inventory ranges over %d relations %v, want %d -- the parent, the permanent "+
			"DEFAULT partition and the %d planted ranges", len(relations), relations, want, len(planted))
	}
	for _, relation := range relations {
		for _, issue := range indexInventoryIssues(t, pool, relation) {
			t.Error(issue)
		}
	}
}

// TestAStrayIndexOnTheEventLogFailsTheClosedInventory proves the inventory can bite, which is what
// a pin nothing ever fails cannot claim for itself.
func TestAStrayIndexOnTheEventLogFailsTheClosedInventory(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	if issues := indexInventoryIssues(t, pool, TableEvents); len(issues) != 0 {
		t.Fatalf("the shipped event log already diverges (%v), so the stray index below would "+
			"prove nothing", issues)
	}

	mustExecOn(t, pool, "CREATE INDEX events_stray_idx ON "+
		mustQualify(t, harnessSchema, TableEvents)+" (listener)")

	issues := indexInventoryIssues(t, pool, TableEvents)
	if len(issues) != 1 || !strings.Contains(issues[0], "2 indexes") {
		t.Errorf("a stray index on %s produced %v, want one issue reporting two", TableEvents, issues)
	}
}
