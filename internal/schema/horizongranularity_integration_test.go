//go:build integration

package schema

import (
	"testing"
	"time"
)

// This file is the measurement horizon.go's storedInstantGranularity rests on, taken against the
// running server rather than transcribed from a document.
//
// The bound is an argument in two steps, and each has its own test below, because either one failing
// on its own would leave the guard wrong in a different way:
//
//  1. A timestamptz keeps microseconds, so two instants closer together than one are one value once
//     stored. A release that widened the type would leave the guard refusing grids the server had
//     grown able to keep -- the safe direction, and still a bound that had stopped being derived.
//  2. An extent written on a grid that is not a whole multiple of that unit does not come back as
//     itself. This is the half that makes the guard worth having, and it is why the condition tests
//     divisibility rather than size: this package decides what exists by comparing observed extents
//     and never by name (M6), so a partition whose extent changed in the writing is one it created
//     once and can never recognise again.
//
// TestTheStoredInstantGranularityIsAMicrosecond is the container-free twin, pinning the constant
// against the literal these two produce.

// theStoredInstantQuery casts two decimal literals to timestamptz and answers whether the server
// reads them as one instant. Both are cast from text, and that is load-bearing: pgx encodes a Go
// time.Time to microseconds on the *client*, so a query taking them as instants would measure the
// driver's truncation and report it as the server's.
const theStoredInstantQuery = `SELECT $1::text::timestamptz = $2::text::timestamptz`

// TestTheServerStoresAnInstantToTheMicrosecond is step 1, with the equality case of the type's own
// resolution: a nanosecond apart is the finest difference the server must lose, and a microsecond
// apart the coarsest it must keep, so only a type of exactly that resolution answers both.
func TestTheServerStoresAnInstantToTheMicrosecond(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name     string
		apart    time.Duration
		wantSame bool
	}{
		{name: "one nanosecond apart, under what a timestamptz stores",
			apart: time.Nanosecond, wantSame: true},
		{name: "exactly the microsecond a timestamptz stores",
			apart: storedInstantGranularity, wantSame: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var same bool
			err := pool.QueryRow(t.Context(), theStoredInstantQuery,
				base.Format(time.RFC3339Nano), base.Add(tc.apart).Format(time.RFC3339Nano)).Scan(&same)
			if err != nil {
				t.Fatalf("compare two instants %s apart through timestamptz: %v", tc.apart, err)
			}

			if same != tc.wantSame {
				t.Errorf("two instants %s apart are stored as one value = %t, want %t; the guard "+
					"refuses every grid this unit cannot keep whole, and %s is where it stops "+
					"keeping them", tc.apart, same, tc.wantSame, storedInstantGranularity)
			}
		})
	}
}

// TestOnlyAWholeMultipleOfAStoredInstantSurvivesBeingWrittenAndReadBack is step 2, and it is the
// class the guard refuses rather than the reproduction that found it. The rows either side of the
// boundary differ by a single nanosecond of interval; the row *above* it is the one a size test
// would serve and this one refuses, and without it the divisibility condition could be rewritten as
// `< 1µs` with nothing failing.
//
// The extent is compared through plan.go's own membership rule rather than by inspecting the bound
// text, because that rule is what the consequence is about: a required range the observation cannot
// match is one every later pass asks for again, and the server answers `would overlap partition`.
func TestOnlyAWholeMultipleOfAStoredInstantSurvivesBeingWrittenAndReadBack(t *testing.T) {
	skipIfShort(t)

	for _, tc := range []struct {
		name         string
		interval     time.Duration
		wantSurvives bool
	}{
		{name: "a grid of exactly one stored instant",
			interval: storedInstantGranularity, wantSurvives: true},
		{name: "a grid of two whole stored instants",
			interval: 2 * storedInstantGranularity, wantSurvives: true},
		{name: "a grid one nanosecond finer than a stored instant",
			interval: storedInstantGranularity - time.Nanosecond},
		{name: "a grid coarser than a stored instant and not a whole multiple of one",
			interval: storedInstantGranularity + storedInstantGranularity/2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertExtentSurvives(t, tc.interval, tc.wantSurvives)
		})
	}
}

// assertExtentSurvives plants one range of the given grid and asks the package's own observation
// whether what came back is the extent the arithmetic asked for.
//
// A range the server refuses outright counts as not surviving, and the two outcomes are reported
// apart: an interval whose adjacent bounds round to one instant earns `empty range bound specified
// for partition`, and one whose bounds merely round elsewhere is accepted and comes back changed.
// Both are fatal and only the second is silent, which is why the guard is written for the wider of
// them.
func assertExtentSurvives(t *testing.T, interval time.Duration, wantSurvives bool) {
	t.Helper()

	pool := eventLogFixture(t)
	asked := rangeAt(gridIndex(theObservedInstant, interval), interval)

	_, err := pool.Exec(t.Context(), "CREATE TABLE "+mustQualify(t, harnessSchema, asked.Name)+
		" PARTITION OF "+mustQualify(t, harnessSchema, TableEvents)+forValues(asked))
	if err != nil {
		if wantSurvives {
			t.Fatalf("a %s grid's range %s was refused as %v; a grid the guard serves has to be "+
				"creatable", interval, extentOf(asked), err)
		}
		t.Logf("a %s grid's range %s is refused outright: %v", interval, extentOf(asked), err)
		return
	}

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partition just planted on a %s grid: %v", interval, err)
	}
	if len(found.Bounded) != 1 {
		t.Fatalf("the observation read %d bounded partitions, want the one just planted", len(found.Bounded))
	}

	if survived := found.Bounded[0].sameExtentAs(asked); survived != wantSurvives {
		t.Errorf("a %s grid asked for %s and the catalog reports %s, so the extent survived = %t, "+
			"want %t; membership is decided by extent (M6), and one that changed in the writing is a "+
			"partition this package created once and asks for on every later pass",
			interval, extentOf(asked), extentOf(found.Bounded[0]), survived, wantSurvives)
	}
}
