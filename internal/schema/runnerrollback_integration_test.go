//go:build integration

package schema

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 13, the step's gate: a migration that fails leaves no partial state. It is provable
// only by inducing a real failure through the *real* runner, because a runner that stopped wrapping
// migrations in a transaction would pass everything else in this suite. ADR-10 made the corpus a
// parameter for exactly this, so the synthetic corpora below drive the shipped runner and no fake
// migration is shipped to anyone.

// theFailingStatement parses and cannot execute, and the distinction is the whole fixture.
// PostgreSQL's simple query protocol raw-parses the entire string before executing any of it, so a
// *syntax* error would abort the migration before its first statement ran and the rollback would
// then be covering nothing. A division by zero is refused during execution instead, which is what
// puts an already-created object inside the transaction being rolled back.
const theFailingStatement = "SELECT 1 / 0;\n"

// The two creates the failing migration is built from, and the migration that must survive it.
const (
	createFirstObject  = "CREATE TABLE induced_first (id int PRIMARY KEY);\n"
	createSecondObject = "CREATE TABLE induced_second (id int PRIMARY KEY);\n"
	createSurvivor     = "CREATE TABLE survives_the_failure (id int PRIMARY KEY);\n"
)

// inducedFailureShapes differ only in where inside the migration the failure sits, which is the
// incidental property a fix aimed at the reproduction rather than at the class would test. Both
// have a create that really ran before the failure; the second has two, so it closes the case where
// the last statement is the one that fails.
var inducedFailureShapes = []struct{ name, text string }{
	{name: "a failure between two of the migration's own statements",
		text: createFirstObject + theFailingStatement + createSecondObject},
	{name: "a failure on the migration's last statement",
		text: createFirstObject + createSecondObject + theFailingStatement},
}

// inducedObjects are the tables the failing migration's own statements create, none of which may
// survive it.
var inducedObjects = []string{"induced_first", "induced_second"}

// failingCorpus is three migrations: the ledger, one that must remain applied, and one built from
// the shape given.
func failingCorpus(t *testing.T, text string) []migration {
	t.Helper()

	return syntheticCorpus(t, map[string]string{
		"0001_ledger.sql":   syntheticLedgerDDL(t),
		"0002_survivor.sql": createSurvivor,
		"0003_induced.sql":  text,
	})
}

func objectExists(t *testing.T, pool *pgxpool.Pool, schemaName, name string) bool {
	t.Helper()

	var found bool
	err := pool.QueryRow(t.Context(), "SELECT count(*) = 1 FROM information_schema.tables "+
		"WHERE table_schema = $1 AND table_name = $2", schemaName, name).Scan(&found)
	if err != nil {
		t.Fatalf("look for %s.%s: %v", schemaName, name, err)
	}
	return found
}

// TestAMigrationThatFailsLeavesNoneOfItsObjectsAndNoLedgerRow asserts all four consequences
// together. A partial assertion would pass a runner that rolled the DDL back and still committed
// the ledger row, or the reverse.
func TestAMigrationThatFailsLeavesNoneOfItsObjectsAndNoLedgerRow(t *testing.T) {
	skipIfShort(t)

	for _, shape := range inducedFailureShapes {
		t.Run(shape.name, func(t *testing.T) {
			corpus := failingCorpus(t, shape.text)
			pool := emptySchemas(t, harnessSchema)
			mustMigrate(t, pool, harnessSchema, corpus[:2])
			before := recordedLedger(t, pool, harnessSchema)

			run, err := migrate(t.Context(), pool, harnessSchema, corpus)

			assertNamesTheFailingMigration(t, err, corpus[2])
			for _, object := range inducedObjects {
				if objectExists(t, pool, harnessSchema, object) {
					t.Errorf("%s survived the failed migration, so its statements were not rolled back",
						object)
				}
			}
			assertStoppedAtTheVersionBeforeIt(t, pool, run, before)
		})
	}
}

func assertNamesTheFailingMigration(t *testing.T, err error, failing migration) {
	t.Helper()

	if err == nil {
		t.Fatalf("migration %d (%s) fails halfway and the run reported success", failing.version,
			failing.file)
	}
	for _, named := range []string{strconv.Itoa(failing.version), failing.file} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the failure reads %s, which does not name %s, so an operator cannot tell which "+
				"migration to fix", err, named)
		}
	}
}

// assertStoppedAtTheVersionBeforeIt is the ledger half of criterion 13: no row for the migration
// that failed, the recorded version is the one that was in force before the attempt, and every
// earlier migration is still applied and still carries its original timestamp.
func assertStoppedAtTheVersionBeforeIt(t *testing.T, pool *pgxpool.Pool, run migrationRun,
	before []ledgerEntry) {
	t.Helper()

	after := recordedLedger(t, pool, harnessSchema)
	if want := versionsOf(before); !slices.Equal(versionsOf(after), want) {
		t.Errorf("the ledger records %v after the failure, want the untouched %v",
			versionsOf(after), want)
	}
	if want := before[len(before)-1].version; run.recorded != want {
		t.Errorf("the run reports version %d, want the %d in force before the attempt",
			run.recorded, want)
	}
	if len(run.applied) != 0 {
		t.Errorf("the failed run reports applying %v", run.applied)
	}
	assertUntouched(t, before, after)
	if !objectExists(t, pool, harnessSchema, "survives_the_failure") {
		t.Error("the migration applied before the failing one lost its object too, so the rollback " +
			"covered more than the transaction that failed")
	}
}

// TestTheSameStatementsWithoutTheInducedFailureDoCreateBothObjects is what keeps the absences above
// from being vacuous: without it a corpus whose creates never ran at all would satisfy every
// assertion in this file.
func TestTheSameStatementsWithoutTheInducedFailureDoCreateBothObjects(t *testing.T) {
	skipIfShort(t)

	corpus := failingCorpus(t, createFirstObject+createSecondObject)
	pool := emptySchemas(t, harnessSchema)

	run := mustMigrate(t, pool, harnessSchema, corpus)

	if want := consecutiveVersions(len(corpus)); !slices.Equal(run.applied, want) {
		t.Fatalf("the run applied %v, want %v", run.applied, want)
	}
	for _, object := range inducedObjects {
		if !objectExists(t, pool, harnessSchema, object) {
			t.Errorf("%s is absent after a migration that creates it and does not fail, so its "+
				"absence above says nothing about a rollback", object)
		}
	}
}
