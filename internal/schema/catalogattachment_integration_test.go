//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 38's predicate, asserted here at the predicate rather than only end to end
// in Step 14. A name match is the drop guard that passes every ordinary input and deletes a
// customer's table on the one that matters, so each decoy below is named by this package's own
// scheme *exactly*: a decoy whose name only resembles one would be spared by an over-matching
// predicate anyway.
//
// Every decoy also carries a valid ownership marker, so attachment is the only thing that can tell
// it from a real partition -- which is what isolates this guard from Step 14's marker guard.

// otherParentName is a second partitioned table in the same schema, so that "is a partition of
// something" and "is a partition of this parent" can be told apart.
const otherParentName = "other_events"

// TestAttachmentIsAnsweredFromTheCatalogAndNeverFromTheNameShape plants one real partition and
// three near misses, each keeping exactly one property of a real one.
func TestAttachmentIsAnsweredFromTheCatalogAndNeverFromTheNameShape(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	named := RequiredRanges(theObservedInstant, theObservedRetention)
	if len(named) < 4 {
		t.Fatalf("the arithmetic offered %d names and this fixture needs 4", len(named))
	}

	plantPartitions(t, pool, harnessSchema, named[:1])
	claimPartition(t, pool, harnessSchema, named[0].Name)
	plantLookAlikeTable(t, pool, named[1])
	plantPartitionOfAnotherParent(t, pool, named[2])
	plantPartitionInAnotherSchema(t, pool, named[3])

	for _, tc := range []struct {
		name, partition string
		// plantedIn is where the table really is, which is where its marker was written. The
		// attachment question is always asked about the configured schema, because that is the
		// question a drop guard asks.
		plantedIn string
		want      bool
	}{
		{name: "a partition of the expected parent", partition: named[0].Name,
			plantedIn: harnessSchema, want: true},
		{name: "a plain table whose name matches the scheme exactly", partition: named[1].Name,
			plantedIn: harnessSchema},
		{name: "a partition of a different partitioned parent", partition: named[2].Name,
			plantedIn: harnessSchema},
		{name: "a partition of this parent living in another schema", partition: named[3].Name,
			plantedIn: otherSchema},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertOwnedByThisInstance(t, pool, tc.plantedIn, tc.partition)

			attached, err := isPartitionOf(t.Context(), pool, harnessSchema, tc.partition, TableEvents)
			if err != nil {
				t.Fatalf("ask whether %s is attached: %v", tc.partition, err)
			}
			if attached != tc.want {
				t.Errorf("%s.%s, asked about as %s.%s, reads as attached=%t, want %t",
					tc.plantedIn, tc.partition, harnessSchema, tc.partition, attached, tc.want)
			}
		})
	}
}

// TestADetachedPartitionStillCarryingOurMarkerIsNoLongerAttached is the orphan ADR-6 keeps the
// detach and the drop in one transaction for: a crash between the two statements leaves a table
// that is ours by every other test and is no longer a partition of anything.
func TestADetachedPartitionStillCarryingOurMarkerIsNoLongerAttached(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	named := RequiredRanges(theObservedInstant, theObservedRetention)[:1]
	plantPartitions(t, pool, harnessSchema, named)
	claimPartition(t, pool, harnessSchema, named[0].Name)

	attached, err := isPartitionOf(t.Context(), pool, harnessSchema, named[0].Name, TableEvents)
	if err != nil || !attached {
		t.Fatalf("the partition reads as attached=%t (%v) before the detach; without that this "+
			"case cannot tell a detach from a fixture that never attached it", attached, err)
	}

	mustExecOn(t, pool, "ALTER TABLE "+mustQualify(t, harnessSchema, TableEvents)+
		" DETACH PARTITION "+mustQualify(t, harnessSchema, named[0].Name))

	assertOwnedByThisInstance(t, pool, harnessSchema, named[0].Name)
	if attached, err = isPartitionOf(t.Context(), pool, harnessSchema, named[0].Name, TableEvents); err != nil {
		t.Fatalf("ask whether the detached table is attached: %v", err)
	}
	if attached {
		t.Error("a detached table still reads as a partition of its former parent")
	}
}

// plantLookAlikeTable creates an ordinary table under a name the naming scheme produces.
func plantLookAlikeTable(t *testing.T, pool *pgxpool.Pool, named Range) {
	t.Helper()

	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, named.Name)+
		" (occurred_at timestamptz NOT NULL)")
	claimPartition(t, pool, harnessSchema, named.Name)
}

// plantPartitionOfAnotherParent creates a genuine partition of a different partitioned table.
func plantPartitionOfAnotherParent(t *testing.T, pool *pgxpool.Pool, named Range) {
	t.Helper()

	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, otherParentName)+
		" (occurred_at timestamptz NOT NULL) PARTITION BY RANGE (occurred_at)")
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, named.Name)+
		" PARTITION OF "+mustQualify(t, harnessSchema, otherParentName)+forValues(named))
	claimPartition(t, pool, harnessSchema, named.Name)
}

// plantPartitionInAnotherSchema creates a genuine partition of this parent, outside this schema.
func plantPartitionInAnotherSchema(t *testing.T, pool *pgxpool.Pool, named Range) {
	t.Helper()

	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, otherSchema))
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, otherSchema, named.Name)+
		" PARTITION OF "+mustQualify(t, harnessSchema, TableEvents)+forValues(named))
	claimPartition(t, pool, otherSchema, named.Name)
}

// claimPartition writes this instance's ownership marker on one table, so a decoy is ours by every
// test except the one under assertion.
func claimPartition(t *testing.T, pool *pgxpool.Pool, schema, partition string) {
	t.Helper()

	object, fault := partitionObject(schema, partition, ourInstance)
	if fault != IdentifierOK {
		t.Fatalf("build the marked object for %s.%s: its name %s", schema, partition, fault)
	}
	if err := claimMarker(t.Context(), pool, object); err != nil {
		t.Fatalf("claim %s.%s: %v", schema, partition, err)
	}
}

// assertOwnedByThisInstance is the precondition every decoy declares: the marker guard cannot be what
// answers, so only attachment can.
func assertOwnedByThisInstance(t *testing.T, pool *pgxpool.Pool, schema, partition string) {
	t.Helper()

	object, fault := partitionObject(schema, partition, ourInstance)
	if fault != IdentifierOK {
		t.Fatalf("build the marked object for %s.%s: its name %s", schema, partition, fault)
	}
	reading, err := markerOn(t.Context(), pool, object)
	if err != nil {
		t.Fatalf("read the marker on %s.%s: %v", schema, partition, err)
	}
	if reading.state != markerOurs {
		t.Fatalf("%s.%s reads as %q rather than %q, so this case is decided by the marker rather "+
			"than by attachment", schema, partition, reading.state, markerOurs)
	}
}
