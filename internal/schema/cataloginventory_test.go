package schema

import (
	"slices"
	"testing"
	"time"
)

// This file is the container-free half of cataloginventory.go: which answer each pair of catalog
// columns resolves to, and what the inventory does about each. The race that produces the vanished
// pair is reached against the running server in catalogvanished_integration_test.go; here the pair
// is handed over directly, so every reading has a case whether or not a container is running. The
// refusals each reading can earn are cataloginventoryrefusal_test.go.

// The two bounds below are the server's own rendering, and the instants beside them are counted off
// that text with the offset applied -- the same derivation partitionboundcorpus_test.go's rows are
// written from, never a reading taken from this reader.
const (
	januaryBound = "FOR VALUES FROM ('2026-01-01 00:00:00+00') TO ('2026-01-02 00:00:00+00')"
	juneBound    = "FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-06-02 00:00:00+00')"
)

var (
	juneFirst  = utcInstant(2026, time.June, 1, 0, 0, 0, 0)
	juneSecond = utcInstant(2026, time.June, 2, 0, 0, 0, 0)
)

// rendered is one bound the server wrote out, as the observation reads it.
func rendered(written string) *string { return &written }

// TestEveryPairOfBoundColumnsResolvesToADeclaredReading crosses both axes the two columns range over
// -- whether the row declares a bound, and whether the server rendered one -- so no cell is left to
// prose. It is also what makes add's
// fail-closed default affordable, which
// TestABoundReadingNoObservationCanProduceIsRefusedRatherThanSkipped reaches by hand: no pair a
// catalog row can hold gets there.
func TestEveryPairOfBoundColumnsResolvesToADeclaredReading(t *testing.T) {
	if len(boundReadings) != 3 {
		t.Fatalf("the observation declares %d bound readings %v; update this count with the set, or "+
			"a reading nothing produces reads as covered", len(boundReadings), boundReadings)
	}

	produced := map[boundReading]int{}
	for _, tc := range []struct {
		name           string
		declaresABound bool
		rendered       *string
		want           boundReading
	}{
		{name: "a live partition", declaresABound: true, rendered: rendered(januaryBound),
			want: boundRendered},
		{name: "a partition dropped since the scan listed it", declaresABound: true,
			want: boundPartitionVanished},
		{name: "a child that declares no bound of its own", want: boundUndeclared},

		// The fourth cell of the grid. No catalog row holds it -- a row rendering a bound declares
		// one -- and it is written down so the crossing is complete rather than three-quarters of a
		// claim: whatever it answers must still be a declared reading.
		{name: "a bound rendered for a row declaring none", rendered: rendered(januaryBound),
			want: boundRendered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundReadFrom(tc.declaresABound, tc.rendered)
			produced[got.reading]++

			if got.reading != tc.want {
				t.Errorf("the pair (declares=%t, rendered=%v) reads as %s, want %s",
					tc.declaresABound, tc.rendered, got.reading, tc.want)
			}
			if !slices.Contains(boundReadings, got.reading) {
				t.Errorf("the pair reads as %s, which is none of the declared readings %v; the "+
					"refusal in add is reachable from a catalog row after all", got.reading, boundReadings)
			}
		})
	}

	for _, reading := range boundReadings {
		if produced[reading] == 0 {
			t.Errorf("no pair of columns produces %s, so that branch is asserted by nothing", reading)
		}
	}
}

// TestOnlyTheVanishedPartitionIsLeftOutOfTheInventory is both sides of the exclusion guard. The two
// rows differ in one property only -- whether the server rendered the bound -- so a skip that had
// widened to any row it could not read would drop the first as well.
func TestOnlyTheVanishedPartitionIsLeftOutOfTheInventory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		bound       observedBound
		wantBounded int
	}{
		{name: "the same row a moment before it was dropped", wantBounded: 1,
			bound: boundReadFrom(true, rendered(januaryBound))},
		{name: "the row of a partition dropped since the scan listed it", wantBounded: 0,
			bound: boundReadFrom(true, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var found observedPartitions

			if err := found.add(harnessSchema, "events_january", harnessSchema, tc.bound); err != nil {
				t.Fatalf("add a row reading as %s: %v", tc.bound.reading, err)
			}
			if len(found.Bounded) != tc.wantBounded {
				t.Errorf("the inventory holds %d bounded partitions %v, want %d",
					len(found.Bounded), found.Bounded, tc.wantBounded)
			}
		})
	}
}

// TestTheInventoryAroundAVanishedPartitionIsReturnedIntact is the half a per-row case cannot state:
// the observation carries on. Each survivor is compared against its own counterpart in order, so two
// entries sharing one extent could not satisfy the expectation written for a third.
func TestTheInventoryAroundAVanishedPartitionIsReturnedIntact(t *testing.T) {
	var found observedPartitions

	for _, row := range []struct {
		name  string
		bound observedBound
	}{
		{name: "events_january", bound: boundReadFrom(true, rendered(januaryBound))},
		{name: "events_vanished", bound: boundReadFrom(true, nil)},
		{name: PartitionDefault, bound: boundReadFrom(true, rendered(defaultBoundExpression))},
		{name: "events_june", bound: boundReadFrom(true, rendered(juneBound))},
	} {
		if err := found.add(harnessSchema, row.name, harnessSchema, row.bound); err != nil {
			t.Fatalf("add %s, reading as %s: %v", row.name, row.bound.reading, err)
		}
	}

	wanted := []Range{
		{Name: "events_january", From: januaryFirst, To: januarySecond},
		{Name: "events_june", From: juneFirst, To: juneSecond},
	}
	if len(found.Bounded) != len(wanted) {
		t.Fatalf("the inventory holds %d bounded partitions %v, want the %d that did not vanish",
			len(found.Bounded), found.Bounded, len(wanted))
	}
	for i, want := range wanted {
		if got := found.Bounded[i]; got.Name != want.Name || !got.From.Equal(want.From) ||
			!got.To.Equal(want.To) {
			t.Errorf("bounded partition %d is %s [%s, %s), want %s [%s, %s)",
				i, got.Name, got.From, got.To, want.Name, want.From, want.To)
		}
	}
	if found.Default != PartitionDefault {
		t.Errorf("the inventory tagged %q as the DEFAULT partition, want %q; a vanished sibling must "+
			"not cost the observation its write-availability net", found.Default, PartitionDefault)
	}
}
