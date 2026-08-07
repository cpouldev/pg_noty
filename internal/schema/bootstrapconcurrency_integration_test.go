//go:build integration

package schema

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 19 and the Concurrency safety NFR: more than two replicas beginning a boot
// at the same instant against an empty database, all succeeding, and leaving what one sequential
// boot leaves.
//
// The contention is arranged rather than hoped for. Left to chance the fastest replica finishes
// before the others look, and the case passes over a race that never happened -- so the key is held
// by a session of its own until every replica is measurably queued on it, and only then released.

// theBootReplicaCount is four. The NFR requires N > 2 because a defect needing three-way contention
// is unreachable with two, and a two-replica pass reads as concurrency evidence while providing none
// for that class; a fourth costs one pool and widens the window every replica but the winner spends
// blocked.
const theBootReplicaCount = 4

// theMeasuredBootRaces are the things a replica that was *not* serialised says, each its own clause
// so a refusal names which one fired rather than only that something did. The first two are the
// task file's own measurements on 17.10, and they are why `CREATE SCHEMA IF NOT EXISTS` cannot be
// what makes this case pass: it matches on a name, and both races happen between two sessions that
// agreed on the name.
//
// The fourth was measured by this step, by releasing the key immediately after taking it and running
// the case below: four replicas then apply migration 1 at once and the loser is told a *type* name
// collides, under the same SQLSTATE as the schema path and a different index. Naming it keeps the
// message pointing at the right path rather than at the nearest one already listed.
var theMeasuredBootRaces = []struct{ path, fragment string }{
	{path: "the schema path", fragment: "pg_namespace_nspname_index"},
	{path: "the table path", fragment: "already exists"},
	{path: "the lock manager", fragment: "deadlock"},
	{path: "the relation-type path", fragment: "pg_type_typname_nsp_index"},
}

// theBootQueueDeadline bounds how long this case waits for every replica to reach the lock. It is
// generous because it bounds a setup step rather than a property: a boot that took longer to reach
// the key than this would be a machine problem, and the case says so rather than reporting a race.
// How often it looks is Step 11's theQueuePollInterval, which answers the same question.
const theBootQueueDeadline = 30 * time.Second

// theQueuedLockQuery counts the sessions waiting on an advisory lock. One container serves this
// package and its tests run one at a time, so while this case runs the only advisory locks in
// flight are its own.
const theQueuedLockQuery = `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`

// TestReplicasBootingAtOnceAllSucceedAndLeaveOneSequentialRunsResult is criterion 19 and the NFR.
func TestReplicasBootingAtOnceAllSucceedAndLeaveOneSequentialRunsResult(t *testing.T) {
	skipIfShort(t)

	began, sequential := theResultOfOneSequentialBoot(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)
	for replica, refused := range bootFromEveryReplicaAtOnce(t, pool, cfg) {
		assertReplicaWasSerialised(t, replica, refused)
	}
	theHorizonBetween(t, began, theClockOf(t, pool))

	recorded := versionsOf(recordedLedger(t, pool, harnessSchema))
	if !slices.Equal(recorded, sequential.versions) {
		t.Errorf(
			"%d replicas booting at once left the ledger recording %v, and one sequential boot "+
				"records %v; each migration is applied exactly once however many replicas boot",
			theBootReplicaCount, recorded, sequential.versions,
		)
	}
	present := objectNamesIn(t, pool, harnessSchema)
	for _, declared := range declaredObjectNames() {
		if !slices.Contains(present, declared) {
			t.Errorf(
				"%s is missing from %s after %d simultaneous boots; the schema holds %v",
				declared, harnessSchema, theBootReplicaCount, present,
			)
		}
	}
	assertInventoryUnchanged(
		t, sequential.inventory, bootInventory(t, pool, harnessSchema),
		"replicas booting at once, compared with one sequential boot,",
	)
}

// sequentialResult is what one boot on its own leaves behind, which is the answer the concurrent run
// is compared against: "they all succeeded" says nothing about whether they produced the right
// database between them.
type sequentialResult struct {
	versions  []int
	inventory []string
}

// theResultOfOneSequentialBoot boots once into its own restored database and reads the result,
// answering with the instant it began at so the caller can fail a run that straddled a grid line.
// The pool it used is dead the moment the caller restores again, and is never touched afterwards.
func theResultOfOneSequentialBoot(t *testing.T) (time.Time, sequentialResult) {
	t.Helper()

	pool := freshDatabase(t)
	began := theClockOf(t, pool)
	mustBootstrap(t, pool, aBootConfiguration(t))

	return began, sequentialResult{
		versions:  versionsOf(recordedLedger(t, pool, harnessSchema)),
		inventory: bootInventory(t, pool, harnessSchema),
	}
}

// bootFromEveryReplicaAtOnce runs one boot per replica, every one of them queued on the same key
// before any is allowed to proceed, and answers with what each replica reported.
func bootFromEveryReplicaAtOnce(t *testing.T, first *pgxpool.Pool, cfg config.Config) []error {
	t.Helper()

	replicas := []*pgxpool.Pool{first}
	for len(replicas) < theBootReplicaCount {
		replicas = append(replicas, anotherReplica(t, cfg))
	}
	watching := anotherReplica(t, cfg)

	blocking := acquiredConn(t, watching)
	held, err := acquireLock(t.Context(), blocking, lockKey(cfg.Database.Schema, cfg.Instance))
	if err != nil {
		t.Fatalf("hold the boot key while the replicas queue on it: %v", err)
	}

	refusals := make([]error, len(replicas))
	var running sync.WaitGroup
	for i, replica := range replicas {
		running.Add(1)
		go func() {
			defer running.Done()
			refusals[i] = Bootstrap(t.Context(), replica, cfg)
		}()
	}

	waitUntilReplicasAreQueuedOnTheLock(t, watching, len(replicas))
	if err := held.release(t.Context()); err != nil {
		t.Fatalf("release the key the replicas are queued on: %v", err)
	}
	running.Wait()
	return refusals
}

// waitUntilReplicasAreQueuedOnTheLock blocks until every replica is measurably waiting, so the
// release below starts a race rather than handing the key to a queue of one.
func waitUntilReplicasAreQueuedOnTheLock(t *testing.T, on *pgxpool.Pool, want int) {
	t.Helper()

	deadline := time.Now().Add(theBootQueueDeadline)
	for {
		var queued int
		if err := on.QueryRow(t.Context(), theQueuedLockQuery).Scan(&queued); err != nil {
			t.Fatalf("count the sessions queued on the boot key: %v", err)
		}
		if queued >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"%d of %d replicas were queued on the boot key after %s, so they did not all "+
					"contend and this case would pass over a race that never happened",
				queued, want, theBootQueueDeadline,
			)
		}
		time.Sleep(theQueuePollInterval)
	}
}

// assertReplicaWasSerialised is criterion 19's own assertion: this replica reported success, and it
// surfaced none of the three things an unserialised one says.
func assertReplicaWasSerialised(t *testing.T, replica int, refused error) {
	t.Helper()

	if refused == nil {
		return
	}
	for _, race := range theMeasuredBootRaces {
		if strings.Contains(refused.Error(), race.fragment) {
			t.Errorf(
				"replica %d surfaced %q, which is what %s says to a boot the advisory lock did "+
					"not serialise: %v", replica, race.fragment, race.path, refused,
			)
		}
	}
	t.Errorf(
		"replica %d answered %v, want no error: every replica booting at once must succeed",
		replica, refused,
	)
}
