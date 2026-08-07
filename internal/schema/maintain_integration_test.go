//go:build integration

package schema

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds the fixtures Step 11's container-backed cases are built on. The cases are in
// maintainhorizon_ (AC 25 and the ownership marker), maintainidempotency_ (AC 22 and AC 29),
// maintainrouting_ (AC 26 to 28), maintaindefault_ and maintaingauge_ (AC 30's three halves),
// maintaincontention_ (AC 33 and AC 40) and maintainconcurrency_ (AC 23's precursor and M6);
// maintainwords_test.go is the container-free half.
//
// The event log is hand-built through Step 9's eventLogFixture rather than migrated, the division
// that step drew: a pass asserted through the migration corpus is a test of Step 6's DDL as much as
// of the pass, and when it fails neither is ruled out.

// theMaintainedRetention is the grid every case here runs on: internal/config's built-in defaults,
// which its doc.go gives as partition_interval 24h, precreate 168h and keep 168h. Criterion 25
// names the first two, and its eight partitions are their consequence rather than a number written
// down anywhere -- 168h is seven whole 24h intervals ahead, and the range holding now is the eighth.
var theMaintainedRetention = retentionOf(24*time.Hour, 168*time.Hour, 168*time.Hour)

// aMaintainedEventLog is a freshly restored database holding the event log and its permanent
// DEFAULT partition and no range partitions at all, with the configuration a pass over it reads.
func aMaintainedEventLog(t *testing.T) (*pgxpool.Pool, config.Config) {
	t.Helper()

	pool := eventLogFixture(t)
	cfg := harnessConfig(t)
	cfg.Retention = theMaintainedRetention
	return pool, cfg
}

// passReport is everything one pass told its caller: what it answered, and what it recorded where a
// caller with no metrics endpoint and an operator reading logs look (criterion 33).
type passReport struct {
	settled Result
	refused error
	stats   statsReading
	logged  []loggedRecord
}

// onePass runs one maintenance pass under opts, with a counter set and a log recorder of its own so
// that each case reads its own observations rather than a running total.
func onePass(t *testing.T, on beginner, cfg config.Config, opts Options) passReport {
	t.Helper()

	stats, recorder := &MaintenanceStats{}, &logRecorder{}
	opts.Stats, opts.Logger = stats, slog.New(recorder)

	settled, refused := Maintain(t.Context(), on, cfg, opts)
	return passReport{
		settled: settled, refused: refused, stats: readStats(stats),
		logged: recorder.taken(),
	}
}

// loggedAt is every record this pass wrote at one level, which is how the cases tell a refusal from
// routine operation without reading a rendered line back.
func (report passReport) loggedAt(level slog.Level) []loggedRecord {
	var found []loggedRecord
	for _, record := range report.logged {
		if record.level == level {
			found = append(found, record)
		}
	}
	return found
}

// theClockOf is the instant the database is at, read through the clock every pass decides against
// (SC-6). Every expectation in these cases is derived from a reading of it and never from the
// partitions a run produced.
func theClockOf(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	reading, err := dbNow(t.Context(), pool)
	if err != nil {
		t.Fatalf("read the database clock: %v", err)
	}
	return reading
}

// theHorizonBetween is the ranges a pass taken between these two readings had to create, derived
// from the arithmetic and from the earlier reading. The readings are taken on either side of the
// pass and required to fall on one grid line: a pass that straddled one decided against a different
// k than this expectation was derived from -- UTC midnight alone, on the default grid -- and then
// the expectation rather than the pass is what was wrong.
func theHorizonBetween(t *testing.T, before, after time.Time) []Range {
	t.Helper()

	interval := theMaintainedRetention.PartitionInterval
	if gridIndex(before, interval) != gridIndex(after, interval) {
		t.Fatalf(
			"the pass ran across a %s grid boundary, between %s and %s; every expectation here "+
				"is derived from the instant before it, so this run proves nothing -- re-run it",
			interval, before, after,
		)
	}
	return RequiredRanges(before, theMaintainedRetention)
}

// partitionInventory is every partition of the event log with the bound the server renders for it,
// sorted, as one comparable value. Bounds and not only names, because a partition re-created over a
// different range is exactly the change a name-only inventory reports as unchanged. It is built
// from catalog.go's observation and Step 9's pg_get_expr reader rather than from a query of its
// own.
func partitionInventory(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions of %s: %v", TableEvents, err)
	}

	var inventory []string
	for _, name := range append([]string{found.Default}, namesOf(found.Bounded)...) {
		if name != "" {
			inventory = append(inventory, name+" "+renderedBoundOf(t, pool, name, nil))
		}
	}
	if len(inventory) == 0 {
		t.Fatal(
			"the event log holds no partition at all, not even the DEFAULT one, so comparing " +
				"an inventory against it would report every schema unchanged",
		)
	}

	slices.Sort(inventory)
	return inventory
}

// theObjectIdentityQuery reads one object's identity and the name the server renders for it, or
// zero and the empty string for an object that does not exist. The identity is what says whether an
// object is the *same* object across two passes, which existence cannot: a DEFAULT partition
// dropped and re-created still exists (criterion 29).
const theObjectIdentityQuery = `SELECT coalesce(to_regclass($1)::oid::bigint, 0),
       coalesce(to_regclass($1)::text, '')`

// identityOf is one object's identity and rendered name. The rendering goes through the same cast
// the routing cases read tableoid through, so the two cannot disagree about a search_path.
func identityOf(t *testing.T, pool *pgxpool.Pool, schema, name string) (int64, string) {
	t.Helper()

	var identity int64
	var rendered string
	err := pool.QueryRow(t.Context(), theObjectIdentityQuery, mustQualify(t, schema, name)).
		Scan(&identity, &rendered)
	if err != nil {
		t.Fatalf("read the identity of %s.%s: %v", schema, name, err)
	}
	return identity, rendered
}
