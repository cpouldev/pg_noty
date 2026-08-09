package schema

import (
	"math"
	"testing"
	"time"
)

// floorRow is one grid-index case. `truncated` is what Go's `/` answers for the same two numbers,
// and the test drives that division as well, so a row cannot claim a discrimination the language
// does not actually make. Where `truncated` differs from `wantIndex`, the row is one of the four
// that separate a true floor from Go's truncation toward zero -- and no post-epoch row can
// substitute for any of them, because the two implementations agree everywhere at or after 1970.
type floorRow struct {
	name      string
	now       time.Time
	interval  time.Duration
	wantIndex int64
	truncated int64
	wantFrom  time.Time
}

var theFloorRows = []floorRow{
	{
		// 20662 whole days separate 1970-01-01 from 2026-07-28, and 06:00 is inside that day.
		name: "a present-day instant, where the two implementations agree",
		now:  utc(2026, 7, 28, 6, 0, 0, 0), interval: 24 * time.Hour,
		wantIndex: 20662, truncated: 20662, wantFrom: utc(2026, 7, 28, 0, 0, 0, 0),
	},
	{
		// Nanosecond offset 0: 0/86400e9 is 0 with no remainder either way.
		name: "the epoch itself",
		now:  utc(1970, 1, 1, 0, 0, 0, 0), interval: 24 * time.Hour,
		wantIndex: 0, truncated: 0, wantFrom: utc(1970, 1, 1, 0, 0, 0, 0),
	},
	{
		// Nanosecond offset +1: floor(1/86400e9) is 0.
		name: "one nanosecond after the epoch",
		now:  utc(1970, 1, 1, 0, 0, 0, 1), interval: 24 * time.Hour,
		wantIndex: 0, truncated: 0, wantFrom: utc(1970, 1, 1, 0, 0, 0, 0),
	},
	{
		// Nanosecond offset -1: floor(-1/86400e9) is -1, while truncation toward zero answers 0
		// and so places this instant in the day *after* the one holding it.
		name: "one nanosecond before the epoch",
		now:  utc(1969, 12, 31, 23, 59, 59, 999999999), interval: 24 * time.Hour,
		wantIndex: -1, truncated: 0, wantFrom: utc(1969, 12, 31, 0, 0, 0, 0),
	},
	{
		// Nanosecond offset -21600e9, a quarter of a day: floor(-0.25) is -1, truncation 0.
		name: "six hours before the epoch",
		now:  utc(1969, 12, 31, 18, 0, 0, 0), interval: 24 * time.Hour,
		wantIndex: -1, truncated: 0, wantFrom: utc(1969, 12, 31, 0, 0, 0, 0),
	},
	{
		// Offset -108000e9, one and a quarter days: floor(-1.25) is -2, truncation -1.
		name: "a day and six hours before the epoch",
		now:  utc(1969, 12, 30, 18, 0, 0, 0), interval: 24 * time.Hour,
		wantIndex: -2, truncated: -1, wantFrom: utc(1969, 12, 30, 0, 0, 0, 0),
	},
	{
		// Offset exactly -86400e9. The remainder is zero, so the floor is -1 and not -2: a
		// correction applied on the sign alone, without testing the remainder, would answer -2
		// and this is the only row that says so.
		name: "exactly one interval before the epoch",
		now:  utc(1969, 12, 31, 0, 0, 0, 0), interval: 24 * time.Hour,
		wantIndex: -1, truncated: -1, wantFrom: utc(1969, 12, 31, 0, 0, 0, 0),
	},
	{
		// Offset -1800e9 against an hourly interval: floor(-0.5) is -1, truncation 0.
		name: "half an hour before the epoch at an hourly interval",
		now:  utc(1969, 12, 31, 23, 30, 0, 0), interval: time.Hour,
		wantIndex: -1, truncated: 0, wantFrom: utc(1969, 12, 31, 23, 0, 0, 0),
	},
}

// TestTheGridIndexIsATrueFloorAndNotGosTruncation is SC 2 and CK-1.
func TestTheGridIndexIsATrueFloorAndNotGosTruncation(t *testing.T) {
	for _, tc := range theFloorRows {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.now.UnixNano() / int64(tc.interval); got != tc.truncated {
				t.Fatalf("Go's / answers %d for this row's numbers, not the %d the row claims; "+
					"re-derive the row before trusting what it discriminates", got, tc.truncated)
			}
			if got := gridIndex(tc.now, tc.interval); got != tc.wantIndex {
				t.Errorf("gridIndex = %d, want %d (Go's / answers %d)",
					got, tc.wantIndex, tc.truncated)
			}

			held := rangeAt(gridIndex(tc.now, tc.interval), tc.interval)
			if !held.From.Equal(tc.wantFrom) {
				t.Errorf("the range holding %s begins at %s, want the grid line %s",
					tc.now.Format(time.RFC3339Nano), held.From.UTC().Format(time.RFC3339Nano),
					tc.wantFrom.Format(time.RFC3339Nano))
			}
			assertHolds(t, held, tc.now, "now")
		})
	}
}

// TestFloorDivRoundsTowardNegativeInfinity drives the one flooring helper directly. There is one of
// it because three call sites need the same answer and a second copy is how they would diverge.
func TestFloorDivRoundsTowardNegativeInfinity(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		numerator, divisor, wantAll int64
	}{
		{name: "an exact positive multiple", numerator: 10, divisor: 10, wantAll: 1},
		{name: "a positive remainder rounds down", numerator: 11, divisor: 10, wantAll: 1},
		{name: "below the first positive multiple", numerator: 1, divisor: 10, wantAll: 0},
		{name: "zero", numerator: 0, divisor: 10, wantAll: 0},
		{name: "a negative remainder rounds away from zero", numerator: -1, divisor: 10, wantAll: -1},
		{name: "an exact negative multiple does not over-decrement", numerator: -10, divisor: 10, wantAll: -1},
		{name: "past the first negative multiple", numerator: -11, divisor: 10, wantAll: -2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := floorDiv(tc.numerator, tc.divisor); got != tc.wantAll {
				t.Errorf("floorDiv(%d, %d) = %d, want %d",
					tc.numerator, tc.divisor, got, tc.wantAll)
			}
		})
	}
}

// TestTheGridIsNanosecondBasedAndThereforeStopsIn2262 is SC 9 and CK-11. Each clause fails under a
// seconds-based representation, which is the change the comment at gridIndex warns about: the first
// because one nanosecond would stop being a whole partition, the second and third because an int64
// count of seconds reaches the year 292277026596 rather than 2262.
func TestTheGridIsNanosecondBasedAndThereforeStopsIn2262(t *testing.T) {
	if got, want := time.Unix(0, math.MaxInt64).UTC(),
		utc(2262, 4, 11, 23, 47, 16, 854775807); !got.Equal(want) {
		t.Fatalf("the last instant an int64 of nanoseconds from the epoch expresses is %s, want %s",
			got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}

	onTheLimitsOwnDay := utc(2262, 4, 11, 0, 0, 0, 0)
	if got := gridIndex(onTheLimitsOwnDay.Add(1), time.Nanosecond) -
		gridIndex(onTheLimitsOwnDay, time.Nanosecond); got != 1 {
		t.Errorf("two instants one nanosecond apart are %d partitions apart at a 1ns interval, "+
			"want 1; the grid no longer resolves nanoseconds", got)
	}

	// An instant on 2262-04-11 is still indexed: it floors to that day's own line.
	theLimitsDay := boundary(gridIndex(utc(2262, 4, 11, 6, 0, 0, 0), 24*time.Hour), 24*time.Hour)
	if !theLimitsDay.Equal(utc(2262, 4, 11, 0, 0, 0, 0)) {
		t.Errorf("06:00 on 2262-04-11 floors to %s, want that day's own midnight line",
			theLimitsDay.UTC().Format(time.RFC3339Nano))
	}

	// The last 24h range both of whose bounds are representable begins on 2262-04-10; the range
	// holding the limit instant itself begins a day later and its upper bound is the line that
	// wraps below.
	lastWhole := rangeAt(gridIndex(utc(2262, 4, 10, 12, 0, 0, 0), 24*time.Hour), 24*time.Hour)
	if !lastWhole.From.Equal(utc(2262, 4, 10, 0, 0, 0, 0)) ||
		!lastWhole.To.Equal(utc(2262, 4, 11, 0, 0, 0, 0)) {
		t.Errorf("the last wholly representable daily range is %s, want the 2262-04-10 one",
			boundsOf([]Range{lastWhole}))
	}
	assertHolds(t, lastWhole, utc(2262, 4, 10, 12, 0, 0, 0), "noon on 2262-04-10")

	// One 24h grid line past the limit is not representable: the multiplication overflows an
	// int64 of nanoseconds and the result wraps to before the epoch. That wrap is the boundary
	// gridIndex's comment names, and it is pinned here so a change of representation fails by
	// name rather than silently widening the range this arithmetic claims to serve.
	beyond := boundary(gridIndex(time.Unix(0, math.MaxInt64), 24*time.Hour)+1, 24*time.Hour)
	if !beyond.Before(time.Unix(0, 0)) {
		t.Errorf("the grid line after the last representable one is %s, which did not wrap; the "+
			"arithmetic is no longer bounded at 2262-04-11", beyond.UTC().Format(time.RFC3339Nano))
	}
}
