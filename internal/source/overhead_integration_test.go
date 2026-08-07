//go:build integration && !race

package source

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// overheadRows is the ceiling's workload: one transaction writing 10,000 rows against each arm.
	// Both arms write the same count, so it is a bulk value and not a bound -- nothing is compared
	// against it, and 9 999 or 10 001 would be the same case.
	overheadRows = 10000

	// overheadWarmupRows sizes the untimed transaction each arm runs before any clock starts. It is
	// small deliberately: the warm-up measures nothing, and one transaction is enough to open the
	// pool's connection and leave this arm's own INSERT text prepared on it, which is the whole of
	// what warmBothArms exists to pay for.
	overheadWarmupRows = 200

	// overheadSamples is how many complete pairs the run tries for. One pair decides the verdict
	// by scheduling noise. The count must be even, because the arms alternate and each has to lead
	// exactly half of them; and four is the smallest even count at which the median discards a pair
	// from each tail rather than averaging every pair, which is the whole reason the median was
	// chosen. It is fixed rather than adaptive, so the test cannot sample again until it likes the
	// answer. The precedent for declaring a repeat count with the argument for its size is
	// determinism_test.go's generationRepeats.
	overheadSamples = 4

	// overheadBudget bounds the warm-up and the pairs together, and it is the timeout this test
	// previously did not have: with every database call taking t.Context(), a machine slow enough
	// to stall one INSERT stalled until `go test`'s own ten-minute default fired, and a run of this
	// test was observed at 603 s against a shape that costs about 8.5 s. 100 s admits a machine
	// twelve times slower than the one that shape was measured on -- beyond the 8.9x spread a
	// single arm recorded on the contended run that prompted this -- and the two invocations this
	// package makes of the measurement still leave more than six minutes of that default for
	// everything else in the package. Expiry abandons the measurement; it never fails it.
	overheadBudget = 100 * time.Second
)

const (
	overheadPlainTable     = "overhead_plain"
	overheadTriggeredTable = "overhead_triggered"
)

func TestTenThousandRowsStayWithinTheCommittedRegressionCeiling(t *testing.T) {
	skipIfShort(t)
	pool := overheadArms(t)

	ctx, cancel := context.WithTimeout(t.Context(), overheadBudget)
	defer cancel()

	var samples []overheadSample
	if warmBothArms(ctx, t, pool) {
		samples = overheadPairsWithin(ctx, t, pool)
	}

	verdict := judgeOverhead(samples)
	if !verdict.measured {
		skipTheUnmeasuredCeiling(t, verdict)
	}
	t.Logf("median overhead ratio=%.3f over %d complete pairs server=%s rows=%d",
		verdict.ratio, verdict.pairs, serverVersionOf(t, pool), overheadRows)
	if overheadExceedsTheCeiling(verdict.ratio) {
		t.Fatalf("trigger overhead ratio %.3f, the median over %d complete pairs, exceeds the 1.7 "+
			"ceiling", verdict.ratio, verdict.pairs)
	}
}

// overheadArms builds the two tables the measurement compares: one plain, one carrying the
// generated insert trigger, in one restored database so both arms meet the same server.
func overheadArms(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, "CREATE TABLE public."+overheadPlainTable+" (id int)")
	mustExecOn(t, pool, "CREATE TABLE public."+overheadTriggeredTable+" (id int)")
	installInsertTrigger(t, pool, "noty", "overhead", overheadTriggeredTable)
	return pool
}

// skipTheUnmeasuredCeiling ends the run without a verdict, and says so on stderr as well as in the
// test log. `go test` prints a skip only under -v, and a gate nobody can see firing is a gate that
// stops running with nobody noticing; the count it reports is the population the verdict would have
// rested on. A skipped subtest also answers true from t.Run, which is why the skip is reported here
// rather than left for a caller to infer: nothing downstream can tell a measurement that did not
// happen from one that passed.
func skipTheUnmeasuredCeiling(t *testing.T, verdict overheadVerdict) {
	t.Helper()
	reason := fmt.Sprintf("the overhead ceiling was not measured on this run: %d of %d pairs completed "+
		"inside the %s budget, fewer than the %d a verdict may rest on",
		verdict.pairs, overheadSamples, overheadBudget, overheadLeastPairs)
	reportToHarnessStderr("%s", reason)
	t.Skip(reason)
}

func serverVersionOf(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var version string
	if err := pool.QueryRow(t.Context(), "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// The recorded measurement, which the published README ceiling reads. On PostgreSQL 17.10, at
// 10,000 rows a transaction with both arms warmed and alternated, the median of the four pair ratios ranged 1.264
// to 1.674 over 71 invocations across 36 full tagged runs of the whole module. The highest observed
// is 1.674 and that is the number to read: the spread inside a run is contention and the median
// discards it, while across runs the maximum is what a ceiling has to sit above. The recorded figure
// still bounds the worst case from below and not from above, which is why the published figure asks
// for a repeat on release hardware before the ceiling is tightened.
//
// Of those 36 runs, 35 were green and none reported itself unmeasured. The one failure was the
// container's postmaster being killed mid-run -- SQLSTATE 57P01, after which every later test in
// the package failed on a container that was no longer running -- and it is a failure this test is
// right to raise rather than absorb: the budget abandons a measurement the machine was too loaded to
// take, and a database that died is a different fact, so abandonedOrFatal routes it to t.Fatal.
//
// Why the median rather than an extreme, or than a quotient of two summaries: measured over 48
// invocations of unchanged code, in two sessions of 12 full tagged runs whose load differed
// markedly. Worst value each candidate reported, and its spread in the calmer session then the
// busier one -- median of the pair ratios 1.674 (0.126, 0.410); quotient of each arm's own minimum
// 1.825 (0.490, 0.258); quotient of the arms' means 1.838 (0.140, 0.743); quotient of the arms'
// medians 1.852 (0.159, 0.595); maximum pair ratio 3.406 (0.439, 2.064). The median has the lowest
// worst case of the five and is the only one under 1.7. The minimum pair ratio is lower still at
// 1.445 and is disqualified rather than compared: it reported 0.537, and a triggered arm cannot run
// half as long as an untriggered one, so what it measures is which arm met the load.
//
// The retired maximum is the reason this was rewritten. It published 1.365 to 1.701 and then 3.482
// on a loaded machine, and over the busier of the two sessions above it exceeded the 2.0 ceiling on
// 1 of the 12 runs -- a pair ratio of 3.406 beside a median of 1.462 in the same invocation. A
// statistic that moves by 2.06 where the thing it measures moved by nothing is reporting the
// machine. The first single unwarmed sample published 1.555 and a later session of that same
// fixture measured 1.274.
