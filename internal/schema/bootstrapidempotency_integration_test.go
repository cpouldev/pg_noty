//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"
)

// This file is criterion 18 and the blast-radius NFR: the two negative claims a boot makes about
// what it did *not* change, both established by comparing inventories rather than by inspecting the
// statements issued -- because the statements are what a defective boot would misreport.
//
// The inside comparison reads through Step 10's shared schema inventory, extended with the
// definitions that inventory does not carry: a constraint's definition, a relation's partition
// bound, a column's default. An existence-only comparison passes an index or a check constraint
// silently redefined, which is precisely the class a second boot could introduce.

// TestASecondBootstrapLeavesTheSchemaByteIdenticalAndTheLedgerRowForRow is criterion 18.
func TestASecondBootstrapLeavesTheSchemaByteIdenticalAndTheLedgerRowForRow(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)

	began := theClockOf(t, pool)
	mustBootstrap(t, pool, cfg)

	inside := bootInventory(t, pool, harnessSchema)
	outside := catalogInventory(t, pool, harnessSchema)
	ledger := recordedLedger(t, pool, harnessSchema)
	if len(ledger) == 0 {
		t.Fatal("the first boot recorded no ledger row, so comparing the ledger below would report " +
			"it unchanged whatever the second boot did")
	}

	mustBootstrap(t, pool, cfg)

	// Two boots straddling a grid boundary decide against different k, so the second legitimately
	// creates a partition the first did not and the comparison below would be about the clock
	// rather than about idempotency. That is a reason to re-run, not a failure of the boot.
	theHorizonBetween(t, began, theClockOf(t, pool))

	assertInventoryUnchanged(t, inside, bootInventory(t, pool, harnessSchema), "a second bootstrap")
	assertInventoryUnchanged(t, outside, catalogInventory(t, pool, harnessSchema),
		"a second bootstrap, seen from outside the schema it owns,")
	assertLedgerUnchangedRowForRow(t, ledger, recordedLedger(t, pool, harnessSchema))
}

// assertLedgerUnchangedRowForRow compares the ledger entry by entry rather than by count, because a
// second boot that reapplied one migration and dropped another's row leaves the count where it was.
// The timestamp is compared too: a row rewritten in place keeps its version and its checksum.
func assertLedgerUnchangedRowForRow(t *testing.T, before, after []ledgerEntry) {
	t.Helper()

	if len(after) != len(before) {
		t.Fatalf("the ledger records %d versions %v after a second boot and %d %v before it",
			len(after), versionsOf(after), len(before), versionsOf(before))
	}
	for i, was := range before {
		switch {
		case after[i].version != was.version:
			t.Errorf("ledger row %d records version %d, was %d", i, after[i].version, was.version)
		case after[i].checksum != was.checksum:
			t.Errorf("ledger row %d records checksum %s for version %d, was %s",
				i, after[i].checksum, was.version, was.checksum)
		case !after[i].appliedAt.Equal(was.appliedAt):
			t.Errorf("version %d was recorded at %s and now reads %s, so the row was rewritten and "+
				"the migration applied again", was.version, was.appliedAt, after[i].appliedAt)
		}
	}
}

// TestABootTouchesNothingOutsideItsOwnSchema is the blast-radius NFR.
//
// The customer's own objects are planted first, so the comparison has a subject: two readings of an
// empty database outside the configured schema compare equal whatever a boot did to relations, and
// the run would be green over nothing.
func TestABootTouchesNothingOutsideItsOwnSchema(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)

	mustExecOn(t, pool, "CREATE TABLE public.customer_orders (id bigint PRIMARY KEY, "+
		"placed_at timestamptz NOT NULL, note text DEFAULT 'none')")
	mustExecOn(t, pool, "CREATE INDEX customer_orders_placed_idx ON public.customer_orders (placed_at)")

	before := catalogInventory(t, pool, harnessSchema)
	if !holdsAnEntryNaming(before, "customer_orders") {
		t.Fatalf("the inventory outside %s does not see the customer's own table, so comparing two "+
			"readings of it says nothing about what a boot leaves alone", harnessSchema)
	}

	mustBootstrap(t, pool, cfg)

	assertInventoryUnchanged(t, before, catalogInventory(t, pool, harnessSchema),
		"a bootstrap, seen from outside the schema it owns,")
}

// holdsAnEntryNaming reports whether an inventory carries an entry mentioning one object.
func holdsAnEntryNaming(inventory []string, object string) bool {
	return slices.ContainsFunc(inventory, func(entry string) bool {
		return strings.Contains(entry, object)
	})
}
