//go:build integration

package schema

import (
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 23, whole, which this step owns alone. Step 11's three-replica case is D2's
// *precursor* -- it creates and drops nothing -- and no row of it is cited here: the criterion
// requires one pass facing a partition to create **and** an expired partition to drop at the same
// moment, and a create-only race says nothing about what three replicas do to one doomed partition.
//
// Both refusals a race can produce are named absent, because they are produced by different races:
// `relation ... already exists` is what the loser of a name-agreeing race is told, and `would overlap
// partition ...` what the loser of a name-disagreeing one is. Naming only the first would leave
// "agreed on names by luck" indistinguishable from "serialised correctly".
//
// This file is the only one in the package whose replicas observe while another replica drops, and
// until cataloginventory.go it was passing partly by luck: a replica whose observation listed the
// doomed partition and then failed to render its bound refused the whole inventory, and
// assertEveryReplicaSucceeded reported it as a replica that answered an error. It was measured at
// roughly 2 failures in 17 full tagged runs -- often enough to be seen, rare enough to be read as
// harness flake. The class is not tested here, because a race cannot be made to happen on demand and
// a case that fails one run in eight is worth less than no case at all; it is constructed
// deterministically instead, by
// TestAPartitionDroppedBetweenTheScanAndTheBoundReadIsSkippedRatherThanAbortingTheInventory. What
// this file still contributes to that class is the only thing it can: that three replicas doing the
// real work no longer meet it.

// aCombinedPass is one replica's whole maintenance pass: the create half and the drop half, sharing
// one counter set and one recorder, because a replica reports as one process and a caller reading
// two sets could not say what that process did.
type aCombinedPass struct {
	created, dropped Result
	refused          []error
	stats            statsReading
	logged           []loggedRecord
}

func oneCombinedPass(t *testing.T, on beginner, cfg config.Config) aCombinedPass {
	t.Helper()

	stats, recorder := &MaintenanceStats{}, &logRecorder{}
	opts := Options{Stats: stats, Logger: slog.New(recorder)}

	created, creating := Maintain(t.Context(), on, cfg, opts)
	dropped, dropping := ApplyRetention(t.Context(), on, cfg, opts)
	return aCombinedPass{
		created: created, dropped: dropped,
		refused: []error{creating, dropping}, stats: readStats(stats), logged: recorder.taken(),
	}
}

// aScheduleWithBothHalvesOfWork is a schema holding one wholly-expired partition anchored at a given
// instant and none of the ranges the horizon asks for, so every pass over it has a partition to
// create *and* one to drop.
//
// The instant is a parameter and not a fresh reading, because both runs below have to plant the same
// *absolute* range: a partition's name encodes its bounds, so two schedules anchored microseconds
// apart hold differently-named partitions and their inventories could not be compared at all.
func aScheduleWithBothHalvesOfWork(t *testing.T, at time.Time) (*pgxpool.Pool, config.Config, Range) {
	t.Helper()

	pool, cfg := aRetainedEventLog(t)
	expired := rangesEndingBefore(at.Add(-cfg.Retention.Keep), cfg.Retention.PartitionInterval, 1)
	plantMarkedPartitions(t, pool, expired)
	return pool, cfg, expired[0]
}

// TestThreeReplicasCreatingAndDroppingAtOnceConvergeOnOnePassesInventory is criterion 23.
func TestThreeReplicasCreatingAndDroppingAtOnceConvergeOnOnePassesInventory(t *testing.T) {
	skipIfShort(t)

	empty, _ := aRetainedEventLog(t)
	began := theClockOf(t, empty)

	alone, cfg, _ := aScheduleWithBothHalvesOfWork(t, began)
	if single := oneCombinedPass(t, alone, cfg); single.dropped.Dropped != 1 {
		t.Fatalf(
			"the single pass this run is compared against dropped %d partitions, want 1; "+
				"without it the comparison below would be against a run that did nothing",
			single.dropped.Dropped,
		)
	}
	wanted := partitionInventory(t, alone)

	// The same schedule again: the harness restores the database, so this is a fresh world planted
	// with the same absolute ranges as the one above.
	pool, cfg, expired := aScheduleWithBothHalvesOfWork(t, began)
	reports := theReplicasThatRanTogether(t, pool, cfg)
	stillOnTheSameGridLine(t, began, theClockOf(t, pool), cfg.Retention.PartitionInterval)

	assertEveryReplicaSucceeded(t, reports)
	if dropped := totalDropped(reports); dropped != 1 {
		t.Errorf(
			"the %d replicas dropped %d partitions between them over one expired range; it is "+
				"dropped exactly once and the rest of them find it already gone", len(reports), dropped,
		)
	}
	isGone(t, pool, harnessSchema, expired.Name, "it is the one wholly expired range in the schedule")
	assertEveryBoundIsTheArithmetics(
		t, pool, RequiredRanges(began, cfg.Retention),
		cfg.Retention.PartitionInterval,
	)

	if got := partitionInventory(t, pool); !slices.Equal(got, wanted) {
		t.Errorf("three replicas left\n%v\nand one pass over the same schedule leaves\n%v", got, wanted)
	}
}

// theReplicasThatRanTogether starts theReplicaCount passes against one schema and holds them at the
// same starting line, so all of them observe the same world, plan the same ranges and race. Without
// the lock the fastest replica finishes before the others look, they plan nothing, and the case
// passes over a race that never happened.
func theReplicasThatRanTogether(t *testing.T, pool *pgxpool.Pool, cfg config.Config) []aCombinedPass {
	t.Helper()

	replicas := []beginner{pool}
	for len(replicas) < theReplicaCount {
		replicas = append(replicas, anotherReplica(t, cfg))
	}
	watching := anotherReplica(t, cfg)
	release := holdTheEventLogLocked(t, pool)

	reports := make([]aCombinedPass, len(replicas))
	var running sync.WaitGroup
	for i, replica := range replicas {
		running.Add(1)
		go func() {
			defer running.Done()
			reports[i] = oneCombinedPass(t, replica, cfg)
		}()
	}
	waitUntilBackendsAreQueuedOn(t, watching, mustQualify(t, harnessSchema, TableEvents), len(replicas))
	release()
	running.Wait()
	return reports
}

// assertEveryReplicaSucceeded is the criterion's first clause, and the two named refusals with it.
func assertEveryReplicaSucceeded(t *testing.T, reports []aCombinedPass) {
	t.Helper()

	for replica, report := range reports {
		t.Logf(
			"replica %d created %d and dropped %d", replica, report.created.Created,
			report.dropped.Dropped,
		)
		for _, refused := range report.refused {
			assertNoRaceRefusal(t, replica, refused)
		}
	}
}

// assertNoRaceRefusal names both refusals a race produces before reporting the refusal itself, so a
// run that met one is told which race it lost rather than only that it failed.
func assertNoRaceRefusal(t *testing.T, replica int, refused error) {
	t.Helper()

	if refused == nil {
		return
	}
	for _, race := range theRaceRefusals {
		if strings.Contains(refused.Error(), race) {
			t.Errorf(
				"replica %d surfaced `%s`, which is what the server tells the loser of a race: "+
					"%v", replica, race, refused,
			)
		}
	}
	t.Errorf(
		"replica %d answered %v, want no error: three replicas maintaining and retaining at "+
			"once must all report success", replica, refused,
	)
}

// totalDropped is how many partitions the replicas removed between them, which is the clause that
// separates "dropped exactly once" from "dropped by each of them in turn".
func totalDropped(reports []aCombinedPass) int {
	dropped := 0
	for _, report := range reports {
		dropped += report.dropped.Dropped
	}
	return dropped
}
