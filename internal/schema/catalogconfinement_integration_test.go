//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the observation's blast radius and its one session dependency: what it must not see,
// what it must refuse, and the DateStyle it reads bounds under.

// TestAnIdenticallyNamedParentInAnotherSchemaIsNeverObserved is SC-8 demonstrated behaviourally
// rather than only read off the query text. Both schemas hold an event log with partitions under
// identical names, so an observation that dropped its schema qualification would answer with twice
// as many partitions as either schema has.
func TestAnIdenticallyNamedParentInAnotherSchemaIsNeverObserved(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	plantEventLog(t, pool, otherSchema)

	ours := RequiredRanges(theObservedInstant, theObservedRetention)
	theirs := ours[:2]
	plantPartitions(t, pool, harnessSchema, ours)
	plantPartitions(t, pool, otherSchema, theirs)

	for _, tc := range []struct {
		schema string
		want   []Range
	}{
		{schema: harnessSchema, want: ours},
		{schema: otherSchema, want: theirs},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			found, err := observePartitions(t.Context(), pool, tc.schema, TableEvents)
			if err != nil {
				t.Fatalf("observe schema %s: %v", tc.schema, err)
			}

			names := make([]string, 0, len(found.Bounded))
			for _, observed := range found.Bounded {
				names = append(names, observed.Name)
			}
			wanted := make([]string, 0, len(tc.want))
			for _, wantedRange := range tc.want {
				wanted = append(wanted, wantedRange.Name)
			}
			slices.Sort(names)
			slices.Sort(wanted)

			if !slices.Equal(names, wanted) {
				t.Errorf("observing %s found %v, want %v; the other schema holds the same names",
					tc.schema, names, wanted)
			}
		})
	}
}

// TestAPartitionOfThisParentOutsideTheSchemaIsRefusedRatherThanObserved covers the shape the
// observation cannot represent: measured, a partition of our parent can be created in another
// schema, and the catalog reports it under our parent. Reporting it would offer retention a table
// outside this package's remit to drop; leaving it out would leave the same incomplete world an
// unreadable bound would.
func TestAPartitionOfThisParentOutsideTheSchemaIsRefusedRatherThanObserved(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	named := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, named[:1])

	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, otherSchema))
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, otherSchema, named[1].Name)+
		" PARTITION OF "+mustQualify(t, harnessSchema, TableEvents)+forValues(named[1]))

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err == nil {
		t.Fatalf("the observation answered with %d partitions and the DEFAULT %q, and one of this "+
			"parent's partitions lives in %s", len(found.Bounded), found.Default, otherSchema)
	}
	for _, want := range []string{named[1].Name, otherSchema, harnessSchema} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q, which does not name %q", err, want)
		}
	}
}

// TestAFreshConnectionRendersBoundsUnderTheDateStyleTheReaderExpects pins the one session setting the bound reader
// depends on. Under ISO the offset is carried, so any TimeZone renders an instant the reader recovers exactly; under
// any other DateStyle the rendering changes shape entirely, and this is where a server configured otherwise announces
// itself.
func TestAFreshConnectionRendersBoundsUnderTheDateStyleTheReaderExpects(t *testing.T) {
	skipIfShort(t)

	var style string
	if err := eventLogFixture(t).QueryRow(t.Context(), "SHOW DateStyle").Scan(&style); err != nil {
		t.Fatalf("read DateStyle: %v", err)
	}
	if !strings.HasPrefix(style, "ISO") {
		t.Errorf("a fresh connection renders under DateStyle %q; the bound reader recovers ISO "+
			"renderings and refuses every other one, so this server's partitions are unobservable",
			style)
	}
}

// TestObservationRefusesABoundRenderedUnderAnotherDateStyle reaches the reader's refusal through
// the server rather than by handing it a string, and asserts both sides of the guard on one
// connection: the same partitions are read under ISO and refused under SQL, DMY, so a reader that
// had quietly started guessing would fail the second half.
func TestObservationRefusesABoundRenderedUnderAnotherDateStyle(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	named := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, named)

	pinned, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("pin a connection to change its DateStyle on: %v", err)
	}
	defer pinned.Release()

	setDateStyle(t, pinned, "ISO, MDY")
	if found, err := observePartitions(t.Context(), pinned, harnessSchema, TableEvents); err != nil {
		t.Fatalf("observe under ISO, which is what the reader reads: %v", err)
	} else if len(found.Bounded) != len(named) {
		t.Fatalf("observing under ISO found %d partitions, want %d", len(found.Bounded), len(named))
	}

	setDateStyle(t, pinned, "SQL, DMY")
	_, err = observePartitions(t.Context(), pinned, harnessSchema, TableEvents)
	if err == nil {
		t.Fatal("the observation read bounds the server rendered under SQL, DMY; a reading it " +
			"cannot make is a refusal and never a guess")
	}
	if !strings.Contains(err.Error(), string(boundInstantUnreadable)) {
		t.Errorf("the refusal reads %q, want it to name %q", err, boundInstantUnreadable)
	}
}

// setDateStyle changes one pinned connection's rendering of a timestamp.
func setDateStyle(t *testing.T, pinned *pgxpool.Conn, style string) {
	t.Helper()

	if _, err := pinned.Exec(t.Context(), "SET DateStyle TO '"+style+"'"); err != nil {
		t.Fatalf("set DateStyle to %s: %v", style, err)
	}
}
