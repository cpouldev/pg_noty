//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// SC-7 and the two library measurements migrationExecOn's comment cites. A comment recording a
// measurement does not fail when the library changes; these do.
//
// This file also records where Step 6's deferred cases are now closed, because a deferral that is
// closed silently is indistinguishable from one that was forgotten. Of the three rows
// migrationdeferred_test.go marked `closedBy: 10`, and which have since been removed from it:
//
//   - "the two migrations are applied, in ascending order, one transaction each, and the ledger
//     records one row per version with that version's checksum" is closed by
//     TestAFreshDatabaseEndsWithOneLedgerRowPerEmbeddedMigration and
//     TestEveryMigrationIsAppliedExactlyOnceInAscendingOrder.
//   - "the DDL executes at all" is closed by every case that runs embeddedCorpusOrFail against the
//     container, first among them runnerroute_integration_test.go's
//     TestEveryObjectLandsInTheConfiguredSchemaAndNoneInAnother.
//   - "the closure supplied to migrationExec selects pgx.QueryExecModeSimpleProtocol" is closed by
//     TestTheRunnerNamesTheSimpleProtocolWhereItSuppliesTheSeamAndNowhereElse together with
//     TestTheDriverForcesTheSimpleProtocolWhenThereAreNoArguments below.
//
// Those three rows were left in place while Steps 8 and 9 were writing this package concurrently,
// and were removed once the slot closed. Nothing is deferred any more: Step 12 closed the object
// *inventory* -- every declared object exists with its declared columns, types and nullability,
// read back from the catalog, which is objectcontract_integration_test.go's
// TestEveryContractTableHoldsExactlyItsContractColumns -- and Step 14 closed the last case, the
// foreign key's refusal of a DETACH. migrationdeferred_test.go changed subject with them: it now
// holds theClosedDeferrals, where each of Step 6's five cases names the assertions that establish
// it and TestEveryClosedDeferralStillNamesADeclaredAssertion reconciles those names against what
// this package declares.
//
// The inventory is still not asserted *here*, which is the half of the old note that stayed true:
// the cases below read object *names* only, to say which schema they landed in.

// decoySchema holds a ledger the runner must not see, so "the applied set is empty" below cannot be
// satisfied by a reader that looked in the wrong place, or in no place at all.
const decoySchema = "somewhere_else"

// decoyVersion is what that ledger records. It is above anything any corpus here embeds, so a
// runner that read it would be refused as ahead of the binary rather than quietly agreeing.
const decoyVersion = 7

// TestAnAbsentLedgerIsAnEmptyAppliedSetRatherThanAnError is SC-7. The first boot has no ledger, so
// treating its absence as an error would break bootstrap entirely -- which is why to_regclass is
// asked rather than the table being selected from and the failure caught.
func TestAnAbsentLedgerIsAnEmptyAppliedSetRatherThanAnError(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, harnessSchema, decoySchema)
	mustExecOn(t, pool, "CREATE TABLE "+decoySchema+"."+TableSchemaVersion+
		" (version int NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL, "+
		"PRIMARY KEY (version))")
	mustExecOn(t, pool, "INSERT INTO "+decoySchema+"."+TableSchemaVersion+
		" VALUES (7, 'recorded somewhere else entirely', now())")

	for _, tc := range []struct{ name, schemaName string }{
		{name: "a schema holding no ledger", schemaName: harnessSchema},
		{name: "a schema that does not exist at all", schemaName: "never_created"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorded, err := readLedger(t.Context(), pool, pathInto(t, tc.schemaName))

			if err != nil {
				t.Fatalf("reading an absent ledger answered %v, want an empty applied set", err)
			}
			if len(recorded) != 0 {
				t.Errorf("reading an absent ledger found %v", recorded)
			}
		})
	}

	// The vacuity guard: a reader that saw nothing anywhere would satisfy both rows
	// above.
	recorded, err := readLedger(t.Context(), pool, pathInto(t, decoySchema))
	if err != nil || len(recorded) != 1 || recorded[0].version != decoyVersion {
		t.Errorf("reading the ledger that is there answered %v and %v, want the one row recording "+
			"version %d", recorded, err, decoyVersion)
	}
}

// pathInto is the search_path statement one schema's reads and writes go through.
func pathInto(t *testing.T, schemaName string) string {
	t.Helper()

	setPath, fault := searchPathStatement(schemaName)
	if fault != IdentifierOK {
		t.Fatalf("the case names the schema %s, and it %s", schemaName, fault)
	}
	return setPath
}

// TestOneExecCarriesAWholeMigrationFileThroughTheSeam is the property the seam exists for: a
// migration file holds many statements and pgx's default extended protocol accepts one per call.
func TestOneExecCarriesAWholeMigrationFileThroughTheSeam(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, harnessSchema)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("open a transaction: %v", err)
	}
	defer tx.Rollback(t.Context())

	if _, err := tx.Exec(t.Context(), pathInto(t, harnessSchema)); err != nil {
		t.Fatalf("route the transaction into %s: %v", harnessSchema, err)
	}
	if err := migrationExecOn(tx)(t.Context(), createFirstObject+createSecondObject); err != nil {
		t.Fatalf("two statements through the seam in one call: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, object := range inducedObjects {
		if !objectExists(t, pool, harnessSchema, object) {
			t.Errorf("%s is absent, so the seam carried only part of what it was given", object)
		}
	}
}

// TestTheDriverForcesTheSimpleProtocolWhenThereAreNoArguments is the measurement that makes
// selecting the mode belt and braces rather than the only thing holding the migration path on the
// simple protocol. All three rows are needed: the first alone would pass against a driver that had
// simply stopped refusing multiple commands, and the third is what shows the sanitizer
// CVE-2026-41889 lived in is reached by an argument and by nothing else.
func TestTheDriverForcesTheSimpleProtocolWhenThereAreNoArguments(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, harnessSchema)
	const twoStatements = "SELECT 1; SELECT 2"

	if _, err := pool.Exec(t.Context(), twoStatements, pgx.QueryExecModeExec); err != nil {
		t.Errorf("two statements asking for the extended protocol with no argument answered %v; pgx "+
			"is expected to override the mode, which is what the seam's no-argument signature buys", err)
	}
	if _, err := pool.Exec(t.Context(), "SELECT $1::int; SELECT 2", pgx.QueryExecModeExec, 1); err == nil {
		t.Error("two statements in one extended-protocol Exec were accepted, so the row above says " +
			"nothing about which protocol carried them")
	}
	// A parameter placeholder has no wire-level slot in the simple protocol, so this can only
	// succeed by pgx interpolating the argument into the text -- which is the sanitizer, and which
	// migrationExec has no position to supply an argument to.
	if _, err := pool.Exec(t.Context(), "SELECT $1::int", pgx.QueryExecModeSimpleProtocol, 1); err != nil {
		t.Errorf("a simple-protocol Exec carrying one argument answered %v; the sanitizer the seam's "+
			"signature keeps unreachable appears to be gone, so re-derive what the signature protects",
			err)
	}
}
