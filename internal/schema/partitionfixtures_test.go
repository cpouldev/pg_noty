package schema

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file holds the fixtures and assertion helpers the boundary corpus is built from.
//
// Step 3's Expected Output names four test files; the corpus is written across nine, and that is
// this step's one deviation from it -- taken for the reason internal/goartifact declared the same
// deviation in Step 1, that a single file would breach the 200-line budget this package's own gate
// enforces. The corpus is partition_test.go (the required set), partitiondeterminism_test.go
// (AC 24's four evaluations), partitionfloor_test.go (the division and the 2262 limit),
// partitionname_test.go (the identifier), partitionretention_test.go (the cutoff and the horizon),
// partitionproperty_test.go (the generated dimension) and this file; packagesources_test.go is the
// one source reader the three source-level gates share.

// utc writes one UTC instant. Every row in this corpus writes its instants through it, so no row
// can accidentally carry a local-zone instant and read as if it were absolute.
func utc(year int, month time.Month, day, hour, minute, second, nanosecond int) time.Time {
	return time.Date(year, month, day, hour, minute, second, nanosecond, time.UTC)
}

// retentionOf is the internal/config value this package's arithmetic reads. Keep is accepted here
// even by the rows that do not use it, so a row's three durations always read in the order the YAML
// writes them.
func retentionOf(interval, precreate, keep time.Duration) config.Retention {
	return config.Retention{PartitionInterval: interval, Precreate: precreate, Keep: keep}
}

// aheadOfUTC and behindUTC are fixed zones rather than loaded ones, so the corpus needs no tzdata on
// the machine running it and cannot be made vacuous by a LoadLocation that failed. +14 and -11 are
// the extremes of the real offset range, and they straddle the date line: an implementation that
// formats or floors in local time answers a different *calendar day* under one of them, which is the
// defect these zones exist to reach.
var (
	aheadOfUTC = time.FixedZone("Kiritimati", 14*60*60)
	behindUTC  = time.FixedZone("Niue", -11*60*60)
)

// underLocalZone runs body with the ambient zone set to zone. time.Local is what an unqualified
// Format and an unqualified time.Date read, so swapping it is what reaches an implementation that
// omits .UTC(); passing an instant already carried in another location (which the corpus also does)
// reaches a different half of the same defect.
//
// The zone is swapped rather than the TZ environment variable set, because the runtime reads TZ once
// while initialising time.Local: a t.Setenv("TZ", ...) after that changes nothing, so a corpus
// written that way would report covering two zones while running one.
func underLocalZone(t *testing.T, zone *time.Location, body func()) {
	t.Helper()

	restore := time.Local
	time.Local = zone
	defer func() { time.Local = restore }()

	body()
}

// assertRanges binds each returned range to the expectation at the same position, so a set that is
// right as a multiset and wrong in its order fails.
func assertRanges(t *testing.T, got, want []Range) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("returned %d ranges %s, want the %d %s", len(got), boundsOf(got), len(want), boundsOf(want))
	}
	for i := range want {
		if !got[i].From.Equal(want[i].From) || !got[i].To.Equal(want[i].To) {
			t.Errorf("range %d is %s, want %s", i, boundsOf(got[i:i+1]), boundsOf(want[i:i+1]))
		}
	}
}

// boundsOf renders a range slice for a failure message, in UTC and at nanosecond precision, so a
// message cannot hide a difference the assertion caught.
func boundsOf(ranges []Range) string {
	rendered := ""
	for _, ranged := range ranges {
		if rendered != "" {
			rendered += " "
		}
		rendered += "[" + ranged.From.UTC().Format(time.RFC3339Nano) +
			", " + ranged.To.UTC().Format(time.RFC3339Nano) + ")"
	}
	if rendered == "" {
		return "(no ranges)"
	}
	return rendered
}

// assertGridShape asserts everything ADR-6 claims of a required set that does not depend on the
// row's own dates: the ranges tile contiguously, every bound is a whole multiple of the interval
// from 1970-01-01T00:00:00Z, and the first and last hold now and the horizon respectively -- each
// half-open, FROM inclusive and TO exclusive (M11).
func assertGridShape(t *testing.T, got []Range, now time.Time, interval, precreate time.Duration) {
	t.Helper()

	if len(got) == 0 {
		t.Fatal("no ranges returned; the range holding now is required whatever else is")
	}
	for i, ranged := range got {
		if ranged.From.UnixNano()%int64(interval) != 0 || ranged.To.UnixNano()%int64(interval) != 0 {
			t.Errorf(
				"range %d is %s, and a bound of it is not a whole multiple of %s from the epoch",
				i, boundsOf(got[i:i+1]), interval,
			)
		}
		if i > 0 && !got[i-1].To.Equal(ranged.From) {
			t.Errorf(
				"range %d begins at %s, leaving a hole after the previous range's %s",
				i, ranged.From.UTC().Format(time.RFC3339Nano),
				got[i-1].To.UTC().Format(time.RFC3339Nano),
			)
		}
	}

	assertHolds(t, got[0], now, "now")
	assertHolds(t, got[len(got)-1], now.Add(precreate), "the horizon")
}

// assertHolds asserts one range holds an instant under the half-open rule M11 measured: FROM is
// inclusive and TO is exclusive, so an instant equal to To belongs to the next range and not to
// this one.
func assertHolds(t *testing.T, ranged Range, instant time.Time, what string) {
	t.Helper()

	if ranged.From.After(instant) || !ranged.To.After(instant) {
		t.Errorf(
			"%s is %s, outside %s -- the range that must hold it",
			what, instant.UTC().Format(time.RFC3339Nano), boundsOf([]Range{ranged}),
		)
	}
}
