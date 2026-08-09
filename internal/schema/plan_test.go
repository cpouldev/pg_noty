package schema

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// The plan corpus is written across three files -- this one (totality, convergence and membership),
// plandefaultpartition_test.go (AC 36's guard, one clause per row) and plancardinality_test.go (the
// range count internal/config permits but no process can hold). Step 4's Expected Output names
// plan_test.go alone; the split is this step's second deviation from it, taken for the reason Step
// 3 declared the same one, that a single file would breach the 200-line budget this package's own
// gate enforces.

// The fixture, derived once so every row below can be checked without running anything.
//
// 2026-07-28T00:00:00Z is 1785196800s after the epoch, which is 20662 whole days -- the constant
// Step 3's own corpus pins as theDayLineSeconds. On a 24h grid that instant is therefore exactly
// grid line k=20662, and the horizon now+48h is k=20664, so the required set is k = 20662, 20663,
// 20664: the days 07-28, 07-29 and 07-30. The retention cutoff is now-72h = 2026-07-25T00:00:00Z.
var (
	thePlanNow       = utc(2026, 7, 28, 0, 0, 0, 0)
	thePlanInterval  = 24 * time.Hour
	thePlanPrecreate = 48 * time.Hour
	thePlanKeep      = 72 * time.Hour
)

// planConfig is the internal/config value PlanMaintenance reads, wrapped in the Config its signature
// takes.
func planConfig(interval, precreate, keep time.Duration) config.Config {
	return config.Config{Retention: retentionOf(interval, precreate, keep)}
}

// theRequiredDays is the required set written out from the derivation above rather than read back
// from RequiredRanges, so a row comparing against it cannot pass by agreeing with a wrong answer.
var theRequiredDays = []Range{
	dayRange(2026, 7, 28), dayRange(2026, 7, 29), dayRange(2026, 7, 30),
}

// observedDay is one partition as a catalog would report it: the extent, and the name the catalog
// read off pg_class. The name is a fixture string rather than this package's generated one, so a
// row asserting membership cannot be satisfied by the naming algorithm agreeing with itself.
func observedDay(year int, month time.Month, day int) Range {
	extent := dayRange(year, month, day)
	extent.Name = fmt.Sprintf("observed_%04d%02d%02d", year, month, day)
	return extent
}

// serialisedPlan renders a whole plan, every field of every entry, so a totality row fails on any
// difference rather than on the fields someone thought to spot-check.
//
// The instants are rendered through %v and deliberately not through boundsOf, which normalises to
// UTC: normalising here would erase exactly the difference the two-timezone rows exist to catch, so
// this is the one place in the package's corpus that must not reuse it.
func serialisedPlan(plan Plan) string {
	var written strings.Builder
	for _, section := range []struct {
		label  string
		ranges []Range
	}{{label: "create", ranges: plan.Create}, {label: "drop", ranges: plan.Drop}} {
		fmt.Fprintf(&written, "%s(%d):\n", section.label, len(section.ranges))
		for _, ranged := range section.ranges {
			fmt.Fprintf(&written, "  %s|%v|%v\n", ranged.Name, ranged.From, ranged.To)
		}
	}
	return written.String()
}

// theTotalityObserved is a set holding one range to drop and one already covered, so a plan built
// from it is non-empty in both slices -- a totality claim over two empty slices would hold for an
// implementation that returned nothing at all.
var theTotalityObserved = []Range{observedDay(2026, 7, 23), observedDay(2026, 7, 28)}

// TestPlanMaintenanceIsATotalFunctionOfItsArguments is SC 1, over the three dimensions the plan must
// not vary with: how many times it is called, the process's ambient timezone, and where inside one
// interval `now` falls.
//
// The second-instant row needs its own derivation, because a plan is not trivially stable across it:
// the retention cutoff moves with now even when the grid does not. now+6h moves the cutoff from
// 2026-07-25T00:00:00Z to 06:00 on the same day, and every observed bound in this corpus is a UTC
// midnight, so no observed range's upper bound lies between the two cutoffs and the drop set is
// stable by construction rather than by luck. The create set is stable because floor(now/24h) and
// floor((now+48h)/24h) are unchanged at 20662 and 20664.
func TestPlanMaintenanceIsATotalFunctionOfItsArguments(t *testing.T) {
	cfg := planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep)
	subject := PlanMaintenance(thePlanNow, cfg, theTotalityObserved)
	first := serialisedPlan(subject)

	// The guard counts the entries the claim is about rather than the size of the rendering they
	// were folded into: a serialisation of two empty slices is not empty either, and a totality
	// claim over one would hold for an implementation that planned nothing at all.
	if len(subject.Create) == 0 || len(subject.Drop) == 0 {
		t.Fatalf(
			"the fixture plans %d creates and %d drops; both must be non-empty or every row "+
				"below compares one empty plan against another",
			len(subject.Create), len(subject.Drop),
		)
	}

	for _, tc := range []struct {
		name string
		call func() Plan
	}{
		{
			name: "the same call repeated", call: func() Plan {
				return PlanMaintenance(thePlanNow, cfg, theTotalityObserved)
			},
		},
		{
			name: "the process running fourteen hours ahead of UTC", call: func() Plan {
				return planUnderZone(t, aheadOfUTC, cfg)
			},
		},
		{
			name: "the process running eleven hours behind UTC", call: func() Plan {
				return planUnderZone(t, behindUTC, cfg)
			},
		},
		{
			name: "a second instant six hours later, inside the same 24h interval", call: func() Plan {
				return PlanMaintenance(thePlanNow.Add(6*time.Hour), cfg, theTotalityObserved)
			},
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				if got := serialisedPlan(tc.call()); got != first {
					t.Errorf("the plan serialised to\n%s\nwant the byte-identical\n%s", got, first)
				}
			},
		)
	}
}

// planUnderZone builds the fixture's plan with the ambient zone swapped, which is what reaches an
// implementation that formats or floors through an unqualified time call.
func planUnderZone(t *testing.T, zone *time.Location, cfg config.Config) Plan {
	t.Helper()

	var built Plan
	underLocalZone(
		t, zone, func() {
			built = PlanMaintenance(thePlanNow, cfg, theTotalityObserved)
		},
	)
	return built
}

// TestAnAlreadyConvergedSetPlansNothingAtAll is AC 22's structural half. The idempotency guarantee is
// that there is nothing to issue, so emptiness of both slices is the property -- not that some caller
// checked a flag and skipped a call.
func TestAnAlreadyConvergedSetPlansNothingAtAll(t *testing.T) {
	// Everything required and nothing expired: the three required days, each already present.
	observed := []Range{observedDay(2026, 7, 28), observedDay(2026, 7, 29), observedDay(2026, 7, 30)}

	plan := PlanMaintenance(
		thePlanNow, planConfig(thePlanInterval, thePlanPrecreate, thePlanKeep),
		observed,
	)

	if len(plan.Create) != 0 {
		t.Errorf("a converged set planned %s to create", boundsOf(plan.Create))
	}
	if len(plan.Drop) != 0 {
		t.Errorf("a converged set planned %s to drop", boundsOf(plan.Drop))
	}
}
