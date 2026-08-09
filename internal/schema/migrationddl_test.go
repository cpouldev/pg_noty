package schema

import (
	"slices"
	"strings"
	"testing"
)

// createdObjectInventory is every object the corpus creates, as the DDL declares it, keyed by name
// and carrying the migration that creates it.
func createdObjectInventory(t *testing.T) map[string]Object {
	t.Helper()

	inventory := map[string]Object{}
	for _, found := range embeddedCorpusOrFail(t) {
		objects, unread := createdObjectsIn(found.sql)
		for _, line := range unread {
			t.Errorf("%s creates something this inventory cannot read: %q", found.file, line)
		}
		for _, object := range objects {
			inventory[object.name] = Object{
				Name: object.name, Kind: object.kind, Migration: found.version,
			}
		}
	}
	return inventory
}

// TestTheDDLAndDocGoAgreeAboutEveryObject reconciles in both directions. Step 12 asserts this
// against a real catalog one slot from now; asserting it here as well means a drift introduced in
// this step fails in this step rather than in another agent's.
func TestTheDDLAndDocGoAgreeAboutEveryObject(t *testing.T) {
	created := createdObjectInventory(t)

	for _, want := range Objects {
		got, declared := created[want.Name]
		if !declared {
			t.Errorf("doc.go names %q and neither migration creates it", want.Name)
			continue
		}
		if got != want {
			t.Errorf("the DDL creates %+v and doc.go declares %+v", got, want)
		}
	}
	for name := range created {
		if !slices.ContainsFunc(Objects, func(o Object) bool { return o.Name == name }) {
			t.Errorf("the DDL creates %q and doc.go declares no constant for it, so a caller in "+
				"a package above this one would have to write the name out", name)
		}
	}
}

// TestEveryStatementTheCorpusWritesIsReadByTheseAssertions is the closure. The readers stand in for
// a SQL parser, so a statement written in a shape they do not recognise would fall outside every
// assertion in this package while the suite stayed green.
func TestEveryStatementTheCorpusWritesIsReadByTheseAssertions(t *testing.T) {
	for _, found := range embeddedCorpusOrFail(t) {
		objects, _ := createdObjectsIn(found.sql)
		comments, unread := declaredCommentsIn(found.sql)
		for _, line := range unread {
			t.Errorf("%s writes a comment whose body this reader cannot read: %q", found.file, line)
		}

		if written := linesStartingWith(found.sql, "CREATE "); written != len(objects) {
			t.Errorf("%s writes %d CREATE statements and %d were read", found.file, written,
				len(objects))
		}
		if written := linesStartingWith(found.sql, "COMMENT ON "); written != len(comments) {
			t.Errorf("%s writes %d COMMENT statements and %d distinct targets were read",
				found.file, written, len(comments))
		}
	}
}

// declaresLine reports whether the DDL holds one line exactly as written. Whole-line equality is
// deliberate: a substring test would accept the same text inside a comment literal, which is
// exactly where a plausible-looking DDL assertion stops meaning anything.
func declaresLine(sql, line string) bool {
	return slices.Contains(strings.Split(sql, "\n"), line)
}

func hasAnyPrefix(line string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func linesStartingWith(sql, prefix string) int {
	written := 0
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(line, prefix) {
			written++
		}
	}
	return written
}

// TestThePartitionFormIsNotReadAsAPlainTable holds the two CREATE TABLE forms apart. Both begin
// with the same four words, and a reader that answered KindTable for the partition would put the
// DEFAULT partition into the inventory as a table -- where it would reconcile against doc.go's
// KindPartition entry and fail for a reason that names neither form.
func TestThePartitionFormIsNotReadAsAPlainTable(t *testing.T) {
	for _, tc := range []struct {
		line string
		want sqlObject
	}{
		{line: "CREATE TABLE events_default PARTITION OF events DEFAULT;",
			want: sqlObject{name: PartitionDefault, kind: KindPartition, on: TableEvents}},
		{line: "CREATE TABLE events (", want: sqlObject{name: TableEvents, kind: KindTable}},
		{line: "CREATE INDEX deliveries_event_idx ON deliveries (event_id);",
			want: sqlObject{name: IndexDeliveriesEvent, kind: KindIndex, on: TableDeliveries}},
	} {
		t.Run(tc.want.name, func(t *testing.T) {
			got, read := createdObjectOn(tc.line)
			if !read || got != tc.want {
				t.Errorf("createdObjectOn(%q) = (%+v, %t), want (%+v, true)",
					tc.line, got, read, tc.want)
			}
		})
	}
}

// TestTheEventLogIsKeyedAndPartitionedAsTheServerForces pins M1's measurement. PRIMARY KEY (id) and
// UNIQUE (id) are both refused on a partitioned table -- "unique constraint on partitioned table
// must include all partitioning columns" -- so the composite key is forced under every reading of
// AC 39 and its presence is evidence about nothing else.
func TestTheEventLogIsKeyedAndPartitionedAsTheServerForces(t *testing.T) {
	sql := migrationSQL(t, migrationCreating(t, TableEvents))

	for _, required := range []string{
		"    PRIMARY KEY (id, occurred_at)",
		") PARTITION BY RANGE (occurred_at);",
	} {
		if !declaresLine(sql, required) {
			t.Errorf("the event log does not declare %q", required)
		}
	}
	if declaresLine(sql, "    PRIMARY KEY (id)") {
		t.Error("the event log declares PRIMARY KEY (id), which the server refuses on a " +
			"partitioned table")
	}
}

// TestTheDefaultPartitionIsCreatedByTheMigrationItself is criterion 29's permanence at its root:
// created here, its existence depends on nothing running. Created by a maintenance pass, it is
// absent on every database where maintenance has not yet succeeded -- and its absence turns each
// write with no covering range into a failure inside the customer's own transaction.
func TestTheDefaultPartitionIsCreatedByTheMigrationItself(t *testing.T) {
	created := createdObjectInventory(t)

	partition, declared := created[PartitionDefault]
	if !declared {
		t.Fatalf("no migration creates %q", PartitionDefault)
	}
	if partition.Kind != KindPartition || partition.Migration != 2 {
		t.Errorf("%q is created as %+v, want the DEFAULT partition created by migration 2",
			PartitionDefault, partition)
	}
	if !declaresLine(migrationSQL(t, 2),
		"CREATE TABLE "+PartitionDefault+" PARTITION OF "+TableEvents+" DEFAULT;") {
		t.Errorf("%q is not created as a DEFAULT partition of %q", PartitionDefault, TableEvents)
	}
}

// TestTheLedgerMigrationCreatesTheLedgerAndNothingElse is ADR-10's split, which criterion 16 rests
// on: it needs a ledger holding versions 1 through K for some K strictly between 0 and N, and a
// one-file corpus offers no such K.
func TestTheLedgerMigrationCreatesTheLedgerAndNothingElse(t *testing.T) {
	objects, _ := createdObjectsIn(migrationSQL(t, 1))

	if len(objects) != 1 || objects[0].name != TableSchemaVersion {
		t.Fatalf("migration 1 creates %+v, want the ledger alone so the runner can read it "+
			"before applying anything else", objects)
	}
	if !declaresLine(migrationSQL(t, 1), "    PRIMARY KEY (version)") {
		t.Error("the ledger is not keyed on version, and that key is the concurrency mechanism: " +
			"it is what makes a second concurrent application of a version raise a unique " +
			"violation inside its own transaction rather than pass unnoticed")
	}
}
