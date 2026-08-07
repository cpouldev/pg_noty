package schema

import (
	"testing"
	"unicode/utf8"
)

// theMembershipObserved is one partition set covering every class a pass can meet at once: two
// wholly expired, one inside the retention window but before now, and two of the three required days
// already present.
//
// Against thePlanNow the cutoff is 2026-07-25T00:00:00Z and the required set is 07-28, 07-29, 07-30,
// so the two answers below follow by arithmetic rather than by observation:
//
//	drop   07-23 (To 07-24, before the cutoff) and 07-24 (To 07-25, exactly the cutoff: TO is
//	       exclusive, so it holds nothing at or after it -- M11)
//	keep   07-25 (To 07-26, one interval past the cutoff)
//	create 07-29 alone, because 07-28 and 07-30 are already observed
var theMembershipObserved = []Range{
	observedDay(2026, 7, 23), observedDay(2026, 7, 24), observedDay(2026, 7, 25),
	observedDay(2026, 7, 28), observedDay(2026, 7, 30),
}

func membershipPlan(t *testing.T) Plan {
	t.Helper()

	return PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), theMembershipObserved)
}

// TestCreateHoldsExactlyTheRequiredRangesNotYetObserved is SC 4's create half. The expectation is the
// single day the derivation above names, written out rather than read back from RequiredRanges.
func TestCreateHoldsExactlyTheRequiredRangesNotYetObserved(t *testing.T) {
	assertRanges(t, membershipPlan(t).Create, []Range{dayRange(2026, 7, 29)})
}

// TestDropHoldsExactlyTheObservedRangesPastTheCutoff is SC 4's drop half, in observed order: the
// order retention executes, and an order derived from anything but the catalog's own would attribute
// a failure to the wrong range.
func TestDropHoldsExactlyTheObservedRangesPastTheCutoff(t *testing.T) {
	assertRanges(t, membershipPlan(t).Drop,
		[]Range{dayRange(2026, 7, 23), dayRange(2026, 7, 24)})
}

// TestDroppedRangesKeepTheNameTheCatalogReported is the drop side of M6. A partition is dropped by
// the name it exists under, so a plan that recomputed the name from the extent would issue a
// statement naming a table that does not exist -- and the fixture's names are deliberately not the
// ones this package generates, so a recomputed name cannot coincide with the expected one.
func TestDroppedRangesKeepTheNameTheCatalogReported(t *testing.T) {
	dropped := membershipPlan(t).Drop
	want := []string{"observed_20260723", "observed_20260724"}

	if len(dropped) != len(want) {
		t.Fatalf("planned %d drops %s, want %d", len(dropped), boundsOf(dropped), len(want))
	}
	for i, name := range want {
		if dropped[i].Name != name {
			t.Errorf("drop %d is named %q, want the catalog's own %q", i, dropped[i].Name, name)
		}
	}
}

// TestEveryPlanEntryCarriesItsBoundsAndItsNameTogether is CK-13: Step 11 reconciles a creation race
// by comparing the observed *bound*, because CREATE TABLE IF NOT EXISTS ... PARTITION OF matches on
// the name and a name-only plan would make that reconciliation compare the wrong thing (M6). What
// each generated name contains is partitionname_test.go's subject, not this file's; here the claim is
// that a plan entry carries all three at once and that the names are usable and distinct.
func TestEveryPlanEntryCarriesItsBoundsAndItsNameTogether(t *testing.T) {
	plan := membershipPlan(t)

	entries := append(append([]Range{}, plan.Create...), plan.Drop...)
	if len(entries) != 3 {
		t.Fatalf("the fixture planned %d entries, want the one create and two drops it derives",
			len(entries))
	}

	seen := map[string]bool{}
	for i, entry := range entries {
		switch {
		case entry.From.IsZero() || entry.To.IsZero():
			t.Errorf("entry %d (%q) carries %s, so it names a partition without saying which extent",
				i, entry.Name, boundsOf(entries[i:i+1]))
		case entry.Name == "":
			t.Errorf("entry %d carries %s with no name, so no statement can be issued for it",
				i, boundsOf(entries[i:i+1]))
		case utf8.RuneCountInString(entry.Name) > MaxIdentifierBytes:
			t.Errorf("entry %d is named %q, which PostgreSQL truncates at %d bytes (M9)",
				i, entry.Name, MaxIdentifierBytes)
		case seen[entry.Name]:
			t.Errorf("entry %d repeats the name %q already planned for another extent",
				i, entry.Name)
		}
		seen[entry.Name] = true
	}
}

// thePartialOverlapRows are the two ways an observed partition can share one bound of a required
// range without covering it. Both arise from the same real event: partition_interval is
// configuration and can change between boots, so a catalog can hold [T, T+12h) from an earlier
// setting while [T, T+24h) is now required -- the case rangeName's own comment is written for.
//
// Each row shares exactly one bound with the required 07-29 day, so a match testing only that bound
// concludes the range is covered and plans nothing, while the other bound's row still passes. Two
// rows are needed because one bound cannot fail a comparison written against the other.
var thePartialOverlapRows = []struct {
	name     string
	observed Range
}{
	{
		name: "an observed half-day sharing the required range's lower bound",
		observed: Range{From: utc(2026, 7, 29, 0, 0, 0, 0), To: utc(2026, 7, 29, 12, 0, 0, 0),
			Name: "observed_20260729_first_half"},
	},
	{
		name: "an observed two-day range sharing the required range's upper bound",
		observed: Range{From: utc(2026, 7, 28, 0, 0, 0, 0), To: utc(2026, 7, 30, 0, 0, 0, 0),
			Name: "observed_20260728_two_days"},
	},
}

// TestAPartitionSharingOneBoundDoesNotCoverTheRequiredRange keeps the extent comparison whole. A
// partition covering half of a required range leaves the other half unwritable, so agreeing on one
// bound is not coverage and the range must still be planned.
func TestAPartitionSharingOneBoundDoesNotCoverTheRequiredRange(t *testing.T) {
	for _, tc := range thePartialOverlapRows {
		t.Run(tc.name, func(t *testing.T) {
			// Both required neighbours are present, so 07-29 is the only range in question.
			observed := []Range{observedDay(2026, 7, 28), tc.observed, observedDay(2026, 7, 30)}

			plan := PlanMaintenance(thePlanNow,
				planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), observed)

			assertRanges(t, plan.Create, []Range{dayRange(2026, 7, 29)})
		})
	}
}

// TestAnObservedBoundInAnotherLocationCoversTheSameExtent is the case that decides how the two
// bounds are compared. The required ranges are built in UTC by this package's own arithmetic, while
// the observed ones are read back through a driver: pgx hands a timestamptz over in the session's
// TimeZone, so the same instant arrives carrying a different location. Go's == on a time.Time also
// compares that location, so an == comparison would call the covered range absent and ask the server
// to create a partition that already exists -- on every pass, forever.
func TestAnObservedBoundInAnotherLocationCoversTheSameExtent(t *testing.T) {
	required := dayRange(2026, 7, 29)
	elsewhere := Range{From: required.From.In(aheadOfUTC), To: required.To.In(aheadOfUTC),
		Name: "observed_20260729_read_back_in_another_zone"}

	// The precondition this row is named for: the same instants, and not the same values.
	if !required.From.Equal(elsewhere.From) || !required.To.Equal(elsewhere.To) {
		t.Fatalf("the fixture moved the instant: %s is not %s",
			boundsOf([]Range{elsewhere}), boundsOf([]Range{required}))
	}
	if required.From == elsewhere.From {
		t.Fatal("the fixture's bound compares equal under ==, so this row cannot tell the two " +
			"comparisons apart")
	}

	observed := []Range{observedDay(2026, 7, 28), elsewhere, observedDay(2026, 7, 30)}

	plan := PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), observed)

	if len(plan.Create) != 0 {
		t.Errorf("planned %s to create while the observed set covers that extent, differing only in "+
			"the location its bounds are carried in", boundsOf(plan.Create))
	}
}

// TestCreateIsDecidedByExtentRatherThanByName is what makes the reconciliation M6 requires possible.
// The observed set below covers the required day 07-29 under a name this package would never
// generate; a plan matching on the name would ask for it again and the server would answer
// `would overlap partition ...`, which AC 23 names as the failure of a name-disagreeing race.
func TestCreateIsDecidedByExtentRatherThanByName(t *testing.T) {
	covered := dayRange(2026, 7, 29)
	covered.Name = "a_name_this_package_would_never_generate"

	observed := []Range{observedDay(2026, 7, 28), covered, observedDay(2026, 7, 30)}

	plan := PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), observed)

	if len(plan.Create) != 0 {
		t.Errorf("planned %s to create while the observed set already covers that extent under "+
			"another name", boundsOf(plan.Create))
	}
}
