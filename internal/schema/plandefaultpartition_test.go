package schema

import (
	"testing"
	"time"
)

// theDefaultAsTheCatalogReportsIt is the DEFAULT partition as a catalog can describe it: a name, and
// no bounds, because it has none to report. A predicate reading a zero upper bound as an instant in
// year 1 treats it as infinitely old and drops it, which is the defect AC 36 exists to close.
var theDefaultAsTheCatalogReportsIt = Range{Name: PartitionDefault}

// theUndroppableRows isolate the two clauses of the retention exemption. Each negative row satisfies
// the clause it is not named for, so a clause silently dropped fails its own row rather than hiding
// behind its neighbour.
var theUndroppableRows = []struct {
	name     string
	observed Range
	want     undroppableReason
}{
	{
		// Violates both clauses at once, which is what pins the documented order: reporting the
		// missing bound here would send a reader looking for a bounds defect rather than telling
		// them the rule that protected the partition.
		name:     "the DEFAULT partition as the catalog reports it, with neither bound",
		observed: theDefaultAsTheCatalogReportsIt,
		want:     itIsTheDefaultPartition,
	},
	{
		// Carries a real upper bound long past the cutoff, so only the name clause can exempt it.
		name:     "a partition named as the DEFAULT one that nonetheless carries an expired extent",
		observed: Range{From: utc(2026, 1, 1, 0, 0, 0, 0), To: utc(2026, 1, 2, 0, 0, 0, 0), Name: PartitionDefault},
		want:     itIsTheDefaultPartition,
	},
	{
		// Named as an ordinary partition and carrying a lower bound, so only the upper-bound clause
		// can exempt it.
		name: "an unbounded-above partition under an ordinary name",
		observed: Range{From: utc(2026, 1, 1, 0, 0, 0, 0),
			Name: "events_from_2026_onwards"},
		want: itHasNoUpperBound,
	},
	{
		// The near-miss that must survive the guard: an ordinary expired partition, differing from
		// the exempt rows only in the guarded properties.
		name:     "an ordinary bounded partition",
		observed: observedDay(2026, 7, 23),
		want:     droppable,
	},
	{
		// The other near-miss: a name beginning with the DEFAULT partition's own, which an
		// over-matching prefix test would exempt and thereby leak disk forever.
		name:     "a partition whose name merely begins with the DEFAULT partition's",
		observed: Range{From: utc(2026, 7, 23, 0, 0, 0, 0), To: utc(2026, 7, 24, 0, 0, 0, 0), Name: PartitionDefault + "_archive"},
		want:     droppable,
	},
}

func TestEachRetentionExemptionIsReportedUnderItsOwnReason(t *testing.T) {
	for _, tc := range theUndroppableRows {
		t.Run(tc.name, func(t *testing.T) {
			if got := whyUndroppable(tc.observed); got != tc.want {
				t.Errorf("whyUndroppable(%q, %s) = %q, want %q",
					tc.observed.Name, boundsOf([]Range{tc.observed}), got, tc.want)
			}
		})
	}
}

// TestTheDefaultPartitionSurvivesEvenWhenEveryRangePartitionIsExpired is AC 36's structural half, and
// the row a predicate treating an unbounded partition as infinitely old fails while passing every
// other row in this package. The DEFAULT entry is written last so a guard that only ever looked at
// the first element cannot pass either.
func TestTheDefaultPartitionSurvivesEvenWhenEveryRangePartitionIsExpired(t *testing.T) {
	// Against thePlanNow the cutoff is 2026-07-25T00:00:00Z, so every one of these three days is
	// wholly past it and all three must be dropped.
	observed := []Range{
		observedDay(2026, 7, 20), observedDay(2026, 7, 21), observedDay(2026, 7, 22),
		theDefaultAsTheCatalogReportsIt,
	}

	plan := PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), observed)

	for i, dropped := range plan.Drop {
		if dropped.Name == PartitionDefault {
			t.Fatalf("drop %d is the DEFAULT partition; it has no upper bound and therefore no age, "+
				"and dropping it makes every unrouted row unwritable", i)
		}
	}
	assertRanges(t, plan.Drop,
		[]Range{dayRange(2026, 7, 20), dayRange(2026, 7, 21), dayRange(2026, 7, 22)})
}

// TestAnExemptPartitionIsNotCountedAsCoverageEither keeps the exemption scoped to the decision it was
// reasoned about. The DEFAULT partition holds rows for every range that has no partition, so reading
// it as coverage would let a pass conclude the horizon was already covered and create nothing -- the
// state ADR-8 records as unrecoverable without an operator (M4).
func TestAnExemptPartitionIsNotCountedAsCoverageEither(t *testing.T) {
	observed := []Range{theDefaultAsTheCatalogReportsIt}

	plan := PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep), observed)

	assertRanges(t, plan.Create, theRequiredDays)
}

// TestTheExemptionDoesNotDependOnTheRetentionWindow crosses the guard with a keep long enough that
// nothing is expired: the DEFAULT partition must be absent from Drop because of what it is, not
// because the cutoff happened to spare it.
func TestTheExemptionDoesNotDependOnTheRetentionWindow(t *testing.T) {
	observed := []Range{theDefaultAsTheCatalogReportsIt, observedDay(2026, 7, 23)}

	plan := PlanMaintenance(thePlanNow,
		planConfig(thePlanInterval, thePlanPrecreate, 100*365*24*time.Hour), observed)

	if len(plan.Drop) != 0 {
		t.Errorf("planned %s to drop under a hundred-year retention window", boundsOf(plan.Drop))
	}
}
