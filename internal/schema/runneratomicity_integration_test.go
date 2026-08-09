//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The migration and its ledger row are one transaction, which is a stronger claim than the one
// PostgreSQL gives away and is the claim ADR-5's concurrency mechanism rests on.

// refusesItsOwnLedgerRow is a migration whose statements all succeed and whose ledger row then
// cannot be written. It is the case that separates "the migration text is atomic" -- which
// PostgreSQL gives for free, since the simple query protocol wraps a multi-statement string in an
// implicit transaction -- from "the migration and its ledger row are atomic", which is this
// runner's own doing and is what ADR-5 rests on: a version whose row the key refuses must leave the
// database exactly as it was, not half-migrated with nothing recording it.
const refusesItsOwnLedgerRow = "CREATE TABLE created_before_the_ledger_refuses (id int PRIMARY KEY);\n" +
	"ALTER TABLE " + TableSchemaVersion + " ADD CONSTRAINT no_version_three CHECK (version <> 3);\n"

// TestAMigrationWhoseLedgerRowCannotBeWrittenRollsBackItsObjectsToo is the concurrency mechanism
// demonstrated rather than reasoned about. A second replica applying version 3 concurrently makes
// this exact thing happen through the primary key instead of through a CHECK, and the consequence
// has to be the same: the migration rolls back and is cleanly retryable, rather than leaving its
// objects behind with no row recording them and the next run meeting "relation already exists".
func TestAMigrationWhoseLedgerRowCannotBeWrittenRollsBackItsObjectsToo(t *testing.T) {
	skipIfShort(t)

	corpus := failingCorpus(t, refusesItsOwnLedgerRow)
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, corpus[:2])
	before := recordedLedger(t, pool, harnessSchema)

	run, err := migrate(t.Context(), pool, harnessSchema, corpus)

	assertNamesTheFailingMigration(t, err, corpus[2])
	if objectExists(t, pool, harnessSchema, "created_before_the_ledger_refuses") {
		t.Error("the table the migration created survived a refused ledger row, so the two are in " +
			"different transactions and a concurrent replica leaves this database half-migrated")
	}
	if constraintExists(t, pool, harnessSchema, "no_version_three") {
		t.Error("the constraint the migration added survived its own refused ledger row")
	}
	assertStoppedAtTheVersionBeforeIt(t, pool, run, before)
}

func constraintExists(t *testing.T, pool *pgxpool.Pool, schemaName, name string) bool {
	t.Helper()

	var found bool
	err := pool.QueryRow(t.Context(), "SELECT count(*) = 1 FROM information_schema.table_constraints "+
		"WHERE table_schema = $1 AND constraint_name = $2", schemaName, name).Scan(&found)
	if err != nil {
		t.Fatalf("look for the constraint %s in %s: %v", name, schemaName, err)
	}
	return found
}
