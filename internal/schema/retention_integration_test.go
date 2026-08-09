//go:build integration

package schema

import (
	"log/slog"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds the fixtures every container-backed case of this step is built on. The cases are
// in retentionguards_ (the four guards, each isolated), retentionboundary_ (AC 34 and AC 35),
// retentionblastradius_ (AC 29, 36, 37 and 38 and the two near-miss decoys), retentionlive_ (AC 39),
// retentioncontention_ (AC 40's drop half) and retentionconcurrency_ (AC 23, whole);
// retentionscan_, retentionscancontrols_, retentionwords_ and retentionclassification_ are the
// container-free half.
//
// The event log here is the *migrated* one and not Step 9's hand-built fixture, which is the
// opposite of the division Step 11 drew and is deliberate. Two of this step's criteria are about
// objects only the shipped corpus creates: criterion 38's five service tables, and AC 39's
// composite foreign key from the queue into the log -- which is the whole of the ratified answer,
// since the refusal to drop a partition holding undelivered events is the server's rather than our
// Go guard's. A hand-built log carries neither, so a pass asserted against one would be asserting
// about a schema this package never ships.

// theRetainedRetention is the grid every case here runs on. The interval is an hour and keep is six
// of them, so the cutoff is a whole number of intervals behind the instant and every expectation
// below is a small, checkable multiple; precreate is two intervals, which R12 permits and which no
// case here reads, since ApplyRetention issues nothing from Plan.Create.
var theRetainedRetention = retentionOf(time.Hour, 2*time.Hour, 6*time.Hour)

// theWholeIntervalsKept is keep expressed in intervals -- the m of the derivations in
// retentionboundary_integration_test.go. It is computed rather than written, so a row cannot go on
// agreeing with an arithmetic the configuration has moved away from.
var theWholeIntervalsKept = int(theRetainedRetention.Keep / theRetainedRetention.PartitionInterval)

// aRetainedEventLog is a freshly restored database with the shipped corpus applied, holding the
// event log, its permanent DEFAULT partition, the four other service tables and no range partitions
// at all -- with the configuration a retention pass over it reads.
func aRetainedEventLog(t *testing.T) (*pgxpool.Pool, config.Config) {
	t.Helper()

	pool := migratedSchema(t)
	cfg := harnessConfig(t)
	cfg.Retention = theRetainedRetention
	return pool, cfg
}

// rangeEndingAt is the one-interval range whose upper bound is exactly the given instant, named by
// this package's own scheme so that a decoy or a target is indistinguishable from a real partition
// by its name alone -- which is the property guard 2 exists to be measured against.
func rangeEndingAt(to time.Time, interval time.Duration) Range {
	from := to.Add(-interval)
	return Range{From: from, To: to, Name: rangeName(from, to)}
}

// rangesEndingBefore is n consecutive one-interval ranges, the last of them ending exactly at the
// given instant and each earlier one an interval before it, in ascending order. Every fixture here
// is built from it, so no case plants overlapping partitions by hand.
func rangesEndingBefore(to time.Time, interval time.Duration, n int) []Range {
	ranges := make([]Range, 0, n)
	for i := n - 1; i >= 0; i-- {
		ranges = append(ranges, rangeEndingAt(to.Add(-time.Duration(i)*interval), interval))
	}
	return ranges
}

// plantMarkedPartitions creates one partition per range and writes this instance's ownership marker
// on each, which is what Step 11's pass does and therefore what a real drop target looks like.
func plantMarkedPartitions(t *testing.T, pool *pgxpool.Pool, ranges []Range) {
	t.Helper()

	plantPartitions(t, pool, harnessSchema, ranges)
	for _, planted := range ranges {
		claimPartition(t, pool, harnessSchema, planted.Name)
	}
}

// oneRetentionPass runs the shipped entry point under opts, with a counter set and a log recorder of
// its own so each case reads its own observations rather than a running total. It is deliberately
// the same passReport Step 11's cases read, so the two halves of a maintenance pass are reported in
// one vocabulary.
func oneRetentionPass(t *testing.T, on beginner, cfg config.Config, opts Options) passReport {
	t.Helper()

	stats, recorder := &MaintenanceStats{}, &logRecorder{}
	opts.Stats, opts.Logger = stats, slog.New(recorder)

	settled, refused := ApplyRetention(t.Context(), on, cfg, opts)
	return passReport{
		settled: settled, refused: refused, stats: readStats(stats),
		logged: recorder.taken(),
	}
}

// oneAppliedDropPlan applies a plan a case computed itself, which is guard 4's seam used from the
// other side: the decision is PlanMaintenance's and the execution consumes it, so a case can pin the
// cutoff at an instant of its own rather than at whichever instant the pass's own transaction
// happened to start. Every boundary row uses it for exactly that reason.
func oneAppliedDropPlan(t *testing.T, on beginner, cfg config.Config, expired []Range) passReport {
	t.Helper()

	stats, recorder := &MaintenanceStats{}, &logRecorder{}
	run := pass{on: on, cfg: cfg, opts: Options{Stats: stats, Logger: slog.New(recorder)}.normalized()}

	settled, refused := run.dropExpired(t.Context(), expired)
	return passReport{
		settled: settled, refused: refused, stats: readStats(stats),
		logged: recorder.taken(),
	}
}

// survives and isGone are the two assertions every blast-radius and boundary row is written in. They
// name the object and why it mattered, because "an object is missing" without either is a failure a
// reader cannot act on.
func survives(t *testing.T, pool *pgxpool.Pool, schema, name, why string) {
	t.Helper()

	if _, rendered := identityOf(t, pool, schema, name); rendered == "" {
		t.Errorf("%s.%s does not exist after the pass, and %s", schema, name, why)
	}
}

func isGone(t *testing.T, pool *pgxpool.Pool, schema, name, why string) {
	t.Helper()

	if _, rendered := identityOf(t, pool, schema, name); rendered != "" {
		t.Errorf("%s.%s still exists after the pass, and %s", schema, name, why)
	}
}

// refusalsOfDrops is every record naming a range the pass could not drop, one per range, read by
// message so a refusal is told from the pass's own summary at the same level.
func refusalsOfDrops(report passReport) []loggedRecord {
	var refusals []loggedRecord
	for _, record := range report.loggedAt(slog.LevelError) {
		if record.message == retentionRefusedMessage {
			refusals = append(refusals, record)
		}
	}
	return refusals
}

// theRefusalOf is the record naming one range, so an assertion binds a diagnostic to the element it
// belongs to rather than to the set.
func theRefusalOf(t *testing.T, report passReport, ranged Range) loggedRecord {
	t.Helper()

	for _, refusal := range refusalsOfDrops(report) {
		if refusal.attrs[logRange] == ranged.Name {
			return refusal
		}
	}
	t.Fatalf(
		"the pass logged no refusal naming %s; it logged %d refusals over %+v",
		ranged.Name, len(refusalsOfDrops(report)), report.logged,
	)
	return loggedRecord{}
}
