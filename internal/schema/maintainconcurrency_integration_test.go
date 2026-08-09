//go:build integration

package schema

import (
	"strings"
	"sync"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is D2's *precursor* to criterion 23, and M6's rule for reconciling a lost race.
//
// A precursor and not criterion 23: that criterion requires one pass to face a partition to create
// **and** an expired partition to drop at the same time, and it is owned by Step 14 alone. Nothing
// here drops anything, and no row here may be cited as evidence for it. What this establishes is
// the half this step owns -- three replicas creating at once all report success, and exactly one
// partition exists per range.

// theReplicaCount is three rather than two because the Concurrency safety NFR requires N > 2: a
// name disagreement surfaces under three-way contention and a two-replica test cannot reach it.
const theReplicaCount = 3

// theRaceRefusals are the two things the server says to a replica that lost a creation race:
// measured on 17.10, the first when the two agreed on a name and the second when they did not. Both
// are named, because only naming both tells "serialised correctly" from "agreed on names by luck".
var theRaceRefusals = []string{"already exists", "would overlap partition"}

// anotherReplica is another process's connection to the same database, opened through the
// production path so that three replicas are three pools rather than three goroutines sharing one.
func anotherReplica(t *testing.T, cfg config.Config) *pgxpool.Pool {
	t.Helper()

	pool, err := OpenPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open another replica's pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestThreeReplicasCreatingAtOnceIsCriterion23sPrecursor is D2's precursor.
func TestThreeReplicasCreatingAtOnceIsCriterion23sPrecursor(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	clock := theClockOf(t, pool)

	replicas := []beginner{pool}
	for len(replicas) < theReplicaCount {
		replicas = append(replicas, anotherReplica(t, cfg))
	}
	// A connection of its own to watch the lock queue from, because every replica's is busy.
	watching := anotherReplica(t, cfg)

	// The event log is held under ACCESS EXCLUSIVE while the replicas start, so that all three
	// observe an empty schema, plan the same ranges and queue on the same first create. Without it
	// the fastest replica finishes before the others look, they plan nothing, and the case passes
	// over a race that never happened. ONLY is what leaves their observations free to proceed.
	release := holdTheEventLogLocked(t, pool)

	reports := make([]passReport, len(replicas))
	var running sync.WaitGroup
	for i, replica := range replicas {
		running.Add(1)
		go func() {
			defer running.Done()
			reports[i] = onePass(t, replica, cfg, Options{})
		}()
	}
	waitUntilBackendsAreQueuedOn(t, watching, mustQualify(t, harnessSchema, TableEvents), len(replicas))
	release()
	running.Wait()

	wanted := theHorizonBetween(t, clock, theClockOf(t, pool))
	created, conceded := 0, 0
	for replica, report := range reports {
		created, conceded = created+report.settled.Created, conceded+concessionsIn(report)
		t.Logf(
			"replica %d created %d of the %d ranges and conceded %d",
			replica, report.settled.Created, len(wanted), concessionsIn(report),
		)
		assertReplicaSucceeded(t, replica, report)
	}

	if created != len(wanted) {
		t.Errorf(
			"the %d replicas created %d partitions between them over %d ranges; exactly one "+
				"partition exists per range, so the rest were conceded rather than created",
			len(replicas), created, len(wanted),
		)
	}
	if conceded < len(replicas)-1 {
		t.Errorf(
			"the replicas conceded %d ranges between them; all %d were queued on the first "+
				"range's create, so %d of them had to find that range already covered and reconcile it",
			conceded, len(replicas), len(replicas)-1,
		)
	}
	assertEveryBoundIsTheArithmetics(t, pool, wanted, cfg.Retention.PartitionInterval)
}

// concessionsIn is how many ranges one replica found already created by another, read from the
// records it wrote. It is what tells a run that met a race from one whose replicas never overlapped.
func concessionsIn(report passReport) int {
	conceded := 0
	for _, record := range report.logged {
		if record.message == concededMessage {
			conceded++
		}
	}
	return conceded
}

// assertReplicaSucceeded is the precursor's own assertion: this replica reported success, and it
// surfaced neither of the two refusals a race produces.
func assertReplicaSucceeded(t *testing.T, replica int, report passReport) {
	t.Helper()

	if report.refused == nil {
		if report.stats != (statsReading{}) {
			t.Errorf(
				"replica %d answered no error and recorded %+v; a range another replica had "+
					"already created is conceded rather than counted as a failure", replica, report.stats,
			)
		}
		return
	}

	for _, refusal := range theRaceRefusals {
		if strings.Contains(report.refused.Error(), refusal) {
			t.Errorf(
				"replica %d surfaced `%s`, which is what the server tells the loser of a "+
					"creation race: %v", replica, refusal, report.refused,
			)
		}
	}
	t.Errorf(
		"replica %d answered %v, want no error: three replicas maintaining at once must all "+
			"report success", replica, report.refused,
	)
}

// TestARangeCoveredUnderAnotherNameIsNeitherRecreatedNorOverlapped is M6 through the pass.
//
// A partition covering the required extent under a name this process would never choose is what a
// DBA, or an earlier configuration, leaves behind. A pass keyed on names would ask for its own name
// over that extent and be told `would overlap partition ...`, which is why `IF NOT EXISTS` cannot
// be what makes the concurrent case pass: it matches on the name and would not have helped here.
func TestARangeCoveredUnderAnotherNameIsNeitherRecreatedNorOverlapped(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	clock := theClockOf(t, pool)
	wanted := RequiredRanges(clock, cfg.Retention)

	planted := Range{From: wanted[0].From, To: wanted[0].To, Name: "events_planted_by_hand"}
	plantPartitions(t, pool, harnessSchema, []Range{planted})
	identity, _ := identityOf(t, pool, harnessSchema, planted.Name)

	report := onePass(t, pool, cfg, Options{})
	wanted = theHorizonBetween(t, clock, theClockOf(t, pool))

	if report.refused != nil {
		t.Fatalf(
			"the pass answered %v, want no error: every range it asks for is coverable and one "+
				"was already covered", report.refused,
		)
	}
	if want := (Result{Outcome: OutcomeWorkDone, Created: len(wanted) - 1}); report.settled != want {
		t.Errorf(
			"the pass reported %+v, want %+v -- the covered extent asked for nothing",
			report.settled, want,
		)
	}
	if _, rendered := identityOf(t, pool, harnessSchema, wanted[0].Name); rendered != "" {
		t.Errorf(
			"the pass created %s over an extent %s already covers; membership is decided by "+
				"extent and never by name", rendered, planted.Name,
		)
	}
	if now, _ := identityOf(t, pool, harnessSchema, planted.Name); now != identity {
		t.Errorf(
			"%s is no longer the object it was (%d, was %d), so the pass replaced a partition "+
				"it did not create", planted.Name, now, identity,
		)
	}
}

// TestTheReconciliationAsksWhetherTheExtentIsCovered reaches the reconciliation directly, in both
// directions, because a lost race is not reproducible on demand: the concession path runs when a
// create fails and the extent turns out to be covered, and the precursor above reaches it only when
// the replicas happen to interleave.
//
// The uncovered range is the nearest one -- a single interval past the horizon -- so a
// reconciliation that answered "covered" for anything at all fails here rather than passing on a
// range far away from every partition.
func TestTheReconciliationAsksWhetherTheExtentIsCovered(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	run := pass{on: pool, cfg: cfg, opts: Options{}.normalized()}

	if !run.covers(t.Context(), wanted[0]) {
		t.Errorf(
			"the reconciliation does not see %s, which this pass has just created; a replica "+
				"that lost the race for it would report a refusal instead of conceding",
			extentOf(wanted[0]),
		)
	}

	beyond := wanted[len(wanted)-1].To
	uncovered := Range{From: beyond, To: beyond.Add(cfg.Retention.PartitionInterval)}
	if run.covers(t.Context(), uncovered) {
		t.Errorf(
			"the reconciliation reports %s covered and no partition covers it, so a create that "+
				"genuinely failed would be discharged as a lost race", extentOf(uncovered),
		)
	}
}
