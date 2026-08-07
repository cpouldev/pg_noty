//go:build integration

package schema

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the four guards, each reached by an input that satisfies the other three, so deleting
// any one of them alone fails the case named for it and no case can be evidence for two.
//
//	guard 1  every partition below is attached, wholly expired and named by this package's own
//	         scheme, and the plan holds it -- only its marker differs, and each of the three states
//	         that is not ours is its own row.
//	guard 2  the look-alike carries a valid marker and a name the scheme produces exactly, so guard 1
//	         and an IF EXISTS would both wave it through; only attachment can refuse it.
//	guard 3  is a source scan and lives in retentionscan_test.go, because no runtime input can show
//	         that a check ran *first*.
//	guard 4  the planner is called alone and the inventory compared, which is the only evidence that
//	         the decision is genuinely side-effect-free.

// observedRanges is the bounded partitions of the event log, read through catalog.go so a case
// hands the planner exactly what the pass would have.
func observedRanges(t *testing.T, pool *pgxpool.Pool) []Range {
	t.Helper()

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions of %s: %v", TableEvents, err)
	}
	return found.Bounded
}

// removeMarker takes this instance's ownership marker off one partition, which is the state a DBA
// who re-created a partition by hand leaves, and the state the plan-versus-apply case creates
// between the two.
func removeMarker(t *testing.T, pool *pgxpool.Pool, partition string) {
	t.Helper()

	mustExecOn(t, pool, "COMMENT ON TABLE "+mustQualify(t, harnessSchema, partition)+" IS NULL")
}

// markerByHandOn writes an arbitrary comment on one partition, which is how the two states this
// package never writes itself are planted.
func markerByHandOn(t *testing.T, pool *pgxpool.Pool, partition, text string) {
	t.Helper()

	object, fault := partitionObject(harnessSchema, partition, ourInstance)
	if fault != IdentifierOK {
		t.Fatalf("build the marked object for %s: its name %s", partition, fault)
	}
	commentByHand(t, pool, object, text)
}

// TestEachMarkerStateThatIsNotOursRefusesItsOwnDropWithItsOwnReason is guard 1. The fourth partition
// is the positive control the other three are measured against: identical in every property but its
// marker, and dropped.
func TestEachMarkerStateThatIsNotOursRefusesItsOwnDropWithItsOwnReason(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	planted := rangesEndingBefore(cutoff, cfg.Retention.PartitionInterval, 4)
	plantMarkedPartitions(t, pool, planted)

	removeMarker(t, pool, planted[0].Name)
	markerByHandOn(t, pool, planted[1].Name, "created for the 2026 migration, do not drop")
	markerByHandOn(t, pool, planted[2].Name, partitionMarkerForm.text(theOtherInstance))

	report := oneAppliedDropPlan(t, pool, cfg, planted)

	for i, want := range []error{
		unmarkedPartition(planted[0].Name),
		unreadableMarkerOn(planted[1].Name),
		foreignMarkerOn(planted[2].Name, theOtherInstance, ourInstance),
	} {
		assertRefusedFor(t, report, planted[i], want)
		survives(t, pool, harnessSchema, planted[i].Name,
			"a partition this instance cannot show it created is refused rather than dropped")
	}
	isGone(t, pool, harnessSchema, planted[3].Name,
		"it is attached, wholly expired and carries this instance's own marker")

	if want := (Result{Outcome: OutcomeFailed, Dropped: 1}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
	if want := (statsReading{failures: 3}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v: three refusals none of which is a stalled "+
			"retention or a blocked range", report.stats, want)
	}
	if !errors.Is(report.refused, ErrForeignInstance) {
		t.Errorf("the pass answered %v, and one of the three partitions is marked for another "+
			"instance; a caller cannot tell that condition from the other two", report.refused)
	}
}

// assertRefusedFor pins the whole refusal one range was reported with, by equality against the
// constructor that builds it rather than against a substring -- so a message emptied or rewritten
// fails here instead of passing on the half that was left.
func assertRefusedFor(t *testing.T, report passReport, ranged Range, want error) {
	t.Helper()

	logged := theRefusalOf(t, report, ranged)
	if got := logged.attrs[logCause]; got != dropRefused(ranged, want).Error() {
		t.Errorf("%s was refused with %q, want %q", ranged.Name, got, dropRefused(ranged, want))
	}
	if logged.attrs[logExtent] != extentOf(ranged) {
		t.Errorf("%s was refused naming the extent %q, want %q",
			ranged.Name, logged.attrs[logExtent], extentOf(ranged))
	}
}

// TestAMarkerRemovedBetweenThePlanAndTheApplyRefusesTheDrop is the one case that separates a marker
// read *inside the drop's own transaction* from one performed earlier. Every other marker row above
// passes under a check made at plan time; this one passes only if the read happens after the plan.
func TestAMarkerRemovedBetweenThePlanAndTheApplyRefusesTheDrop(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	now := theClockOf(t, pool)
	planted := rangesEndingBefore(now.Add(-cfg.Retention.Keep), cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, planted)

	plan := PlanMaintenance(now, cfg, observedRanges(t, pool))
	if len(plan.Drop) != 1 || plan.Drop[0].Name != planted[0].Name {
		t.Fatalf("the planner offered %v, want the one planted expired range %s; without it this "+
			"case would remove a marker from a partition nothing was going to drop",
			namesOf(plan.Drop), planted[0].Name)
	}
	// The plan was computed while the marker was this instance's own, which is what makes the
	// removal below the *only* difference between the plan and the apply.
	assertOwnedByThisInstance(t, pool, harnessSchema, planted[0].Name)
	removeMarker(t, pool, planted[0].Name)

	report := oneAppliedDropPlan(t, pool, cfg, plan.Drop)

	assertRefusedFor(t, report, planted[0], unmarkedPartition(planted[0].Name))
	survives(t, pool, harnessSchema, planted[0].Name,
		"the marker it was planned against was gone by the time the drop ran")
	if want := (Result{Outcome: OutcomeFailed}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
}
