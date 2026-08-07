//go:build integration

package schema

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Criteria 12 and 16: applied exactly once, ascending, readable elsewhere, and never re-ordered or
// skipped when a ledger already holds a prefix.

// primaryKeyColumnsQuery reads a key from the catalog's standard view rather than from the DDL text,
// because the text saying PRIMARY KEY (version) and the constraint existing are different claims.
const primaryKeyColumnsQuery = `
SELECT k.column_name
  FROM information_schema.table_constraints c
  JOIN information_schema.key_column_usage k
    ON k.constraint_schema = c.constraint_schema AND k.constraint_name = c.constraint_name
 WHERE c.table_schema = $1 AND c.table_name = $2 AND c.constraint_type = 'PRIMARY KEY'
 ORDER BY k.ordinal_position`

// databaseClock is the server's own now(), which is the clock applied_at is written from. A
// replica's process clock would let two replicas disagree about one ledger.
func databaseClock(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	var reading time.Time
	if err := pool.QueryRow(t.Context(), "SELECT now()").Scan(&reading); err != nil {
		t.Fatalf("read the database clock: %v", err)
	}
	return reading
}

func primaryKeyColumnsOf(t *testing.T, pool *pgxpool.Pool, schemaName, table string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), primaryKeyColumnsQuery, schemaName, table)
	if err != nil {
		t.Fatalf("read the primary key of %s.%s: %v", schemaName, table, err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan a key column of %s.%s: %v", schemaName, table, err)
		}
		columns = append(columns, column)
	}
	return columns
}

// syntheticLedgerDDL is the shipped first migration's own text, reused rather than re-written so a
// synthetic corpus cannot drift from the ledger the runner actually writes into. Every synthetic
// corpus needs it: the runner records each migration inside that migration's own transaction, so
// version 1 has to leave the ledger behind before its own row can be written.
func syntheticLedgerDDL(t *testing.T) string {
	t.Helper()

	return embeddedCorpusOrFail(t)[0].sql
}

// dependentMigrations is a corpus whose every migration needs the one before it: 0003 has a foreign
// key into 0002's table. Ascending order is then *forced* rather than merely reported -- a runner
// that applied them in any other order could not complete at all, which is the observation
// criterion 12 needs and the shipped two-migration corpus cannot supply.
func dependentMigrations(t *testing.T) []migration {
	t.Helper()

	return syntheticCorpus(t, map[string]string{
		"0001_ledger.sql": syntheticLedgerDDL(t),
		"0002_second.sql": "CREATE TABLE second (id int PRIMARY KEY);",
		"0003_third.sql":  "CREATE TABLE third (id int PRIMARY KEY REFERENCES second (id));",
	})
}

// TestEveryMigrationIsAppliedExactlyOnceInAscendingOrder is criterion 12's first half.
func TestEveryMigrationIsAppliedExactlyOnceInAscendingOrder(t *testing.T) {
	skipIfShort(t)

	corpus := dependentMigrations(t)
	pool := emptySchemas(t, harnessSchema)

	run := mustMigrate(t, pool, harnessSchema, corpus)

	if want := consecutiveVersions(len(corpus)); !slices.Equal(run.applied, want) {
		t.Errorf("the run applied %v, want %v ascending", run.applied, want)
	}
	recorded := recordedLedger(t, pool, harnessSchema)
	if got := versionsOf(recorded); !slices.Equal(got, consecutiveVersions(len(corpus))) {
		t.Errorf("the ledger records %v, want every version exactly once", got)
	}
	for i := 1; i < len(recorded); i++ {
		if recorded[i].appliedAt.Before(recorded[i-1].appliedAt) {
			t.Errorf("version %d is recorded as applied at %s, before version %d's %s",
				recorded[i].version, recorded[i].appliedAt, recorded[i-1].version,
				recorded[i-1].appliedAt)
		}
	}
}

// TestTheRecordedVersionIsReadableByASecondConnectionWithoutInspectingObjects is criterion 12's
// second half, which is what internal/cli's status command depends on. The second pool is opened
// the way production opens one, which is as close to another process as one test binary gets; what
// the criterion turns on is that the answer comes from the ledger rather than from the object
// inventory, and a bare max(version) is exactly that.
func TestTheRecordedVersionIsReadableByASecondConnectionWithoutInspectingObjects(t *testing.T) {
	skipIfShort(t)

	corpus := embeddedCorpusOrFail(t)
	mustMigrate(t, emptySchemas(t, harnessSchema), harnessSchema, corpus)

	elsewhere, err := OpenPool(t.Context(), harnessConfig(t))
	if err != nil {
		t.Fatalf("open a second pool onto the migrated database: %v", err)
	}
	defer elsewhere.Close()

	name, _ := Qualified(harnessSchema, TableSchemaVersion)
	var recorded int
	if err := elsewhere.QueryRow(t.Context(), "SELECT max(version) FROM "+name).Scan(&recorded); err != nil {
		t.Fatalf("read the recorded version from a second connection: %v", err)
	}
	if want := highestEmbedded(corpus); recorded != want {
		t.Errorf("a second connection reads version %d, want %d", recorded, want)
	}
}

// TestAPartialLedgerAppliesExactlyTheVersionsItLacks is criterion 16, crossed over every K the
// criterion admits (0 < K < N) rather than over the one that came to mind.
//
// The recorded set is compared against the full range and not against its maximum, because a
// maximum cannot tell {1,2,3} from {1,2,4} or from {1,2,3,3} -- and a re-ordering or re-applying
// runner produces exactly those.
func TestAPartialLedgerAppliesExactlyTheVersionsItLacks(t *testing.T) {
	skipIfShort(t)

	corpus := dependentMigrations(t)
	for partial := firstMigrationVersion; partial < len(corpus); partial++ {
		t.Run(fmt.Sprintf("a ledger already holding %d of %d", partial, len(corpus)), func(t *testing.T) {
			pool := emptySchemas(t, harnessSchema)
			mustMigrate(t, pool, harnessSchema, corpus[:partial])
			before := recordedLedger(t, pool, harnessSchema)

			run := mustMigrate(t, pool, harnessSchema, corpus)

			assertAppliedTheRest(t, run, partial, len(corpus))
			after := recordedLedger(t, pool, harnessSchema)
			if want := consecutiveVersions(len(corpus)); !slices.Equal(versionsOf(after), want) {
				t.Errorf("the ledger records %v, want exactly %v -- no gap and no duplicate",
					versionsOf(after), want)
			}
			assertUntouched(t, before, after)
		})
	}
}

func assertAppliedTheRest(t *testing.T, run migrationRun, already, whole int) {
	t.Helper()

	want := consecutiveVersions(whole)[already:]
	if !slices.Equal(run.applied, want) {
		t.Errorf("the run applied %v, want %v ascending", run.applied, want)
	}
	if run.recorded != whole {
		t.Errorf("the run reports version %d, want %d", run.recorded, whole)
	}
}

// assertUntouched is criterion 16's "none of 1..K is re-applied", asserted against the rows
// themselves: a re-applied migration writes a new applied_at, so an unchanged timestamp is the
// evidence rather than the runner's own report of what it did.
func assertUntouched(t *testing.T, before, after []ledgerEntry) {
	t.Helper()

	if len(after) < len(before) {
		t.Fatalf("the ledger recorded %v and now records %v, so a row it had was removed",
			versionsOf(before), versionsOf(after))
	}
	for i, was := range before {
		if !after[i].appliedAt.Equal(was.appliedAt) || after[i].checksum != was.checksum {
			t.Errorf("version %d was recorded at %s with checksum %s and now reads %s with %s, so it "+
				"was applied a second time", was.version, was.appliedAt, was.checksum,
				after[i].appliedAt, after[i].checksum)
		}
	}
}
