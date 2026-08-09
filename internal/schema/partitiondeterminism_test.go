package schema

import (
	"slices"
	"testing"
	"time"
)

// theAgreementEvaluations are AC 24's four evaluations of one required set: two instants inside one
// interval, and two ambient zones. Each carries its instant in the zone it names as well as running
// under it, because those reach two different halves of the same defect -- a formatter or a floor
// that reads the instant's own location, and one that reads the ambient time.Local.
var theAgreementEvaluations = []struct {
	name string
	now  time.Time
	zone *time.Location
}{
	{name: "early in the interval, ambient UTC", now: utc(2026, 7, 28, 6, 0, 0, 0), zone: time.UTC},
	{name: "late in the same interval, ambient UTC", now: utc(2026, 7, 28, 20, 0, 0, 0), zone: time.UTC},
	{
		name: "early, carried in and run under a +14 zone",
		now:  utc(2026, 7, 28, 6, 0, 0, 0).In(aheadOfUTC), zone: aheadOfUTC,
	},
	{
		name: "late, carried in and run under a -11 zone",
		now:  utc(2026, 7, 28, 20, 0, 0, 0).In(behindUTC), zone: behindUTC,
	},
}

// TestRequiredRangesAgreeAcrossInstantsAndZones is AC 24. Without the epoch-grid rule two boots
// minutes apart demand overlapping ranges and the server refuses the second.
//
// precreate is a whole multiple of the interval here on purpose: with a divisible precreate the
// horizon stays inside one partition as now advances through another, so the four sets are
// identical rather than merely consistent. The non-divisible case, where two instants inside one
// interval legitimately require different counts, is
// TestTheRequiredCountIsAConsequenceOfTheKRangeNotALiteral's -- and asserting identity there would
// be asserting something ADR-6 does not claim.
func TestRequiredRangesAgreeAcrossInstantsAndZones(t *testing.T) {
	cfg := retentionOf(24*time.Hour, 48*time.Hour, 72*time.Hour)

	// Derived from the grid, not from a run: 06:00 and 20:00 on 2026-07-28 both floor to the
	// 07-28 midnight line, and both horizons -- 07-30T06:00 and 07-30T20:00 -- floor to the 07-30
	// line, so every evaluation must return these three.
	want := []Range{
		{From: utc(2026, 7, 28, 0, 0, 0, 0), To: utc(2026, 7, 29, 0, 0, 0, 0)},
		{From: utc(2026, 7, 29, 0, 0, 0, 0), To: utc(2026, 7, 30, 0, 0, 0, 0)},
		{From: utc(2026, 7, 30, 0, 0, 0, 0), To: utc(2026, 7, 31, 0, 0, 0, 0)},
	}

	var namedBy [][]string
	for _, tc := range theAgreementEvaluations {
		t.Run(tc.name, func(t *testing.T) {
			underLocalZone(t, tc.zone, func() {
				got := RequiredRanges(tc.now, cfg)
				assertRanges(t, got, want)
				namedBy = append(namedBy, namesOf(got))
			})
		})
	}

	assertOneNaming(t, namedBy)
}

// assertOneNaming is AC 32's timezone half, asserted over the same four evaluations rather than in a
// corpus of its own: the names must be byte-identical, because a partition's name is how two
// replicas agree that they mean the same table (M6).
func assertOneNaming(t *testing.T, namedBy [][]string) {
	t.Helper()

	if len(namedBy) != len(theAgreementEvaluations) {
		t.Fatalf("%d of the %d evaluations produced a naming; the rest failed before naming anything",
			len(namedBy), len(theAgreementEvaluations))
	}
	for i, named := range namedBy[1:] {
		if !slices.Equal(named, namedBy[0]) {
			t.Errorf("%s named the set %v, and %s named it %v", theAgreementEvaluations[i+1].name,
				named, theAgreementEvaluations[0].name, namedBy[0])
		}
	}
}

// namesOf is the naming one required set carries, in order.
func namesOf(ranges []Range) []string {
	named := make([]string, 0, len(ranges))
	for _, ranged := range ranges {
		named = append(named, ranged.Name)
	}
	return named
}
