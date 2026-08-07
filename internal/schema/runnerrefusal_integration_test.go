//go:build integration

package schema

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Criteria 14 and 15 and SC-10: the three ways a run ends without applying anything. Both negative
// claims are established by comparing a full inventory before and after rather than by counting the
// runner's own calls, because call counting measures the bookkeeping a defective runner gets wrong.

// schemaInventoryQuery is every column, constraint and index a schema holds, with its definition,
// as one sorted list. Columns alone would let an altered constraint pass. It reads
// information_schema so that a compared inventory is stable text rather than an oid, and Step 13's
// byte-identical comparison should read through this same helper so the two steps cannot come to
// disagree about what "unchanged" means.
const schemaInventoryQuery = `
SELECT 'column ' || table_name || '.' || column_name || ' ' || data_type || ' null=' || is_nullable
  FROM information_schema.columns WHERE table_schema = $1
UNION ALL
SELECT 'constraint ' || table_name || ' ' || constraint_type || ' ' || constraint_name
  FROM information_schema.table_constraints WHERE table_schema = $1
UNION ALL
SELECT 'index ' || indexdef FROM pg_indexes WHERE schemaname = $1
ORDER BY 1`

func schemaInventory(t *testing.T, pool *pgxpool.Pool, schemaName string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), schemaInventoryQuery, schemaName)
	if err != nil {
		t.Fatalf("read the inventory of %s: %v", schemaName, err)
	}
	defer rows.Close()

	var inventory []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatalf("scan an inventory entry of %s: %v", schemaName, err)
		}
		inventory = append(inventory, entry)
	}
	if len(inventory) == 0 {
		t.Fatalf("%s holds nothing at all, so comparing an inventory against it proves nothing",
			schemaName)
	}
	return inventory
}

// assertInventoryUnchanged is the independent evidence behind "applied nothing, altered nothing".
// It names the first entry that differs rather than only the counts, because an inventory that
// swapped one constraint for another is the same length.
func assertInventoryUnchanged(t *testing.T, before, after []string, during string) {
	t.Helper()

	if slices.Equal(before, after) {
		return
	}
	for i, was := range before {
		if i >= len(after) {
			t.Fatalf("%s dropped %s from %s", during, was, harnessSchema)
		}
		if after[i] != was {
			t.Fatalf("%s changed %s into %s in %s", during, was, after[i], harnessSchema)
		}
	}
	t.Fatalf("%s added %v to %s", during, after[len(before):], harnessSchema)
}

// TestADatabaseAheadOfTheBinaryIsRefusedWithoutAlteringAnything is criterion 14. A rolled-back
// deployment silently operating on a newer schema is a data-corruption path, which is why the error
// names both versions -- an operator needs the gap in both directions to decide whether to roll
// forward or redeploy.
func TestADatabaseAheadOfTheBinaryIsRefusedWithoutAlteringAnything(t *testing.T) {
	skipIfShort(t)

	whole := embeddedCorpusOrFail(t)
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, whole)
	before := schemaInventory(t, pool, harnessSchema)

	older := whole[:len(whole)-1]
	run, err := migrate(t.Context(), pool, harnessSchema, older)

	if !errors.Is(err, ErrSchemaAhead) {
		t.Fatalf("a binary embedding up to version %d met a database recording %d and answered %v",
			highestEmbedded(older), highestEmbedded(whole), err)
	}
	for _, named := range []int{highestEmbedded(whole), highestEmbedded(older)} {
		if !strings.Contains(err.Error(), strconv.Itoa(named)) {
			t.Errorf("the refusal reads %s, which does not name version %d", err, named)
		}
	}
	if len(run.applied) != 0 {
		t.Errorf("the refused run reports applying %v", run.applied)
	}
	assertInventoryUnchanged(t, before, schemaInventory(t, pool, harnessSchema), "the refusal")

	recorded := versionsOf(recordedLedger(t, pool, harnessSchema))
	if want := consecutiveVersions(len(whole)); !slices.Equal(recorded, want) {
		t.Errorf("the ledger records %v after the refusal, want the untouched %v", recorded, want)
	}
}

// TestAnAlreadyCurrentDatabaseAppliesNothingAndIssuesNoDDL is criterion 15, and it is also the
// equality case of the ahead-of-the-binary guard: the recorded version equals the highest embedded
// one, which is the only input `>` and `>=` answer differently for.
func TestAnAlreadyCurrentDatabaseAppliesNothingAndIssuesNoDDL(t *testing.T) {
	skipIfShort(t)

	corpus := embeddedCorpusOrFail(t)
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, corpus)
	before := schemaInventory(t, pool, harnessSchema)
	recordedBefore := recordedLedger(t, pool, harnessSchema)

	run := mustMigrate(t, pool, harnessSchema, corpus)

	if len(run.applied) != 0 {
		t.Errorf("a second run against a current database applied %v", run.applied)
	}
	if run.recorded != highestEmbedded(corpus) {
		t.Errorf("the second run reports version %d, want %d", run.recorded, highestEmbedded(corpus))
	}
	assertInventoryUnchanged(t, before, schemaInventory(t, pool, harnessSchema), "the second run")
	assertUntouched(t, recordedBefore, recordedLedger(t, pool, harnessSchema))
}

// TestAFileEditedAfterItWasAppliedIsReportedRatherThanIgnored is SC-10, and it is the reason the
// ledger carries a checksum at all (Flyway's prior art). The edit is a comment, so the objects the
// edited file creates are byte-for-byte the ones already there and no other check in this package
// could find it.
func TestAFileEditedAfterItWasAppliedIsReportedRatherThanIgnored(t *testing.T) {
	skipIfShort(t)

	written := map[string]string{
		"0001_ledger.sql": syntheticLedgerDDL(t),
		"0002_second.sql": "CREATE TABLE second (id int PRIMARY KEY);",
	}
	applied := syntheticCorpus(t, written)
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, applied)
	before := schemaInventory(t, pool, harnessSchema)

	written["0002_second.sql"] += "\n-- one comment added after this migration was applied\n"
	edited := syntheticCorpus(t, written)
	_, err := migrate(t.Context(), pool, harnessSchema, edited)

	if err == nil {
		t.Fatal("a file edited after it was applied was carried forward silently, which makes the " +
			"ledger's checksum column decoration")
	}
	for _, named := range []string{"0002_second.sql", applied[1].checksum, edited[1].checksum} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal reads %s, which does not name %s", err, named)
		}
	}
	assertInventoryUnchanged(t, before, schemaInventory(t, pool, harnessSchema), "the refusal")
}

// TestADatabaseAheadOfTheBinaryIsReportedAsThatRatherThanAsAnEditedFile drives the documented
// precedence through the real runner with an input violating both rules at once, without which both
// orders answer identically and reversing them would change what an operator is told while the
// suite stayed green.
func TestADatabaseAheadOfTheBinaryIsReportedAsThatRatherThanAsAnEditedFile(t *testing.T) {
	skipIfShort(t)

	written := map[string]string{
		"0001_ledger.sql": syntheticLedgerDDL(t),
		"0002_second.sql": "CREATE TABLE second (id int PRIMARY KEY);",
	}
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, syntheticCorpus(t, written))

	// Shorter than the ledger *and* carrying a different version 1 than the one recorded.
	written["0001_ledger.sql"] += "\n-- edited after it was applied\n"
	delete(written, "0002_second.sql")
	_, err := migrate(t.Context(), pool, harnessSchema, syntheticCorpus(t, written))

	if !errors.Is(err, ErrSchemaAhead) {
		t.Errorf("a database ahead of the binary whose first file was also edited answered %v, want "+
			"the ahead refusal: the binary is simply the wrong one, and the edit is a consequence", err)
	}
}
