//go:build integration && !race

package source

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// warmBothArms pays every one-off cost before any clock starts, and pays it for both arms. pgxpool
// opens its connections lazily, so on a fresh pool the first Begin carries a TCP connect,
// authentication and the pool's first-connection setup; pgx then caches one prepared statement per
// connection per statement text, so the first INSERT of a given text carries a Parse/Describe round
// trip of its own. Left unpaid, all of it lands in whichever arm is timed first -- the untriggered
// baseline, in the ordering this file used to have -- inflating the denominator and biasing the
// ratio toward passing. The warm-up runs through timeWrites so the text it prepares is byte
// identical to the one the measurement issues, and it runs inside the budget so that a machine too
// slow to warm up is reported as unmeasured rather than left to the binary's own timeout.
func warmBothArms(ctx context.Context, t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	for _, table := range []string{overheadPlainTable, overheadTriggeredTable} {
		if _, completed := timeWrites(ctx, t, pool, table, overheadWarmupRows); !completed {
			return false
		}
	}
	if open := pool.Stat().TotalConns(); open != 1 {
		t.Fatalf("the pool holds %d connections after the warm-up; every use here is sequential, so "+
			"a second connection means a timed transaction can land on one whose statement cache is "+
			"cold and pay a setup cost the other arm did not", open)
	}
	return true
}

// overheadPairsWithin times complete pairs until it has overheadSamples of them or ctx expires. A
// pair is kept only when both of its arms finished, because the verdict is a median over the pairs'
// own quotients and a half-timed pair has no quotient to contribute.
func overheadPairsWithin(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []overheadSample {
	t.Helper()
	samples := make([]overheadSample, 0, overheadSamples)
	for sample := range overheadSamples {
		pair, completed := timeBothArms(ctx, t, pool, sample%2 == 0)
		if !completed {
			t.Logf("pair %d of %d abandoned: the %s measurement budget expired inside a transaction",
				sample+1, overheadSamples, overheadBudget)
			return samples
		}
		t.Logf("pair %d of %d: baseline=%s triggered=%s ratio=%.3f",
			sample+1, overheadSamples, pair.baseline, pair.triggered, pair.ratio())
		samples = append(samples, pair)
	}
	return samples
}

// timeBothArms times one pair. baselineFirst alternates with the pair number, so each arm leads
// exactly half the pairs and whatever advantage attaches to going first -- a page cache still
// filling, a checkpoint that has just finished, the other arm's rows still being written back -- is
// shared between the arms instead of settling on one of them. The two timings of a pair are taken
// next to each other, in one process against one server in one run, which is what makes their
// quotient a ratio rather than a comparison of two machines.
func timeBothArms(ctx context.Context, t *testing.T, pool *pgxpool.Pool, baselineFirst bool) (overheadSample, bool) {
	t.Helper()
	leadingTable, trailingTable := overheadTriggeredTable, overheadPlainTable
	if baselineFirst {
		leadingTable, trailingTable = overheadPlainTable, overheadTriggeredTable
	}

	leading, completed := timeWrites(ctx, t, pool, leadingTable, overheadRows)
	if !completed {
		return overheadSample{}, false
	}
	trailing, completed := timeWrites(ctx, t, pool, trailingTable, overheadRows)
	if !completed {
		return overheadSample{}, false
	}
	if baselineFirst {
		return overheadSample{baseline: leading, triggered: trailing}, true
	}
	return overheadSample{baseline: trailing, triggered: leading}, true
}

// timeWrites writes rows single-row inserts into public.<table> in one transaction and answers how
// long the whole transaction took and whether it finished inside ctx. Begin and Commit are inside
// the clock for both arms because part of the trigger's cost is the commit's: the notification its
// guard raises is delivered there. The deferred Rollback returns the pooled connection: after a
// Commit it is a no-op, and after an abandonment it is handed the expired ctx deliberately, so it
// answers at once instead of issuing a round trip that could block on the very connection the
// budget just gave up on. It runs after the return value is evaluated, so it is outside the clock.
func timeWrites(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string, rows int) (time.Duration, bool) {
	t.Helper()
	started := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return abandonedOrFatal(ctx, t, err)
	}
	defer tx.Rollback(ctx)

	for id := 1; id <= rows; id++ {
		if _, err := tx.Exec(ctx, "INSERT INTO public."+table+" VALUES ($1)", id); err != nil {
			return abandonedOrFatal(ctx, t, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return abandonedOrFatal(ctx, t, err)
	}
	return time.Since(started), true
}

// abandonedOrFatal separates the two reasons a timed transaction can stop. A transaction the
// measurement budget cut short says the machine could not be measured and abandons the pair;
// anything else says the generated trigger or the fixture is wrong and fails the test.
//
// The question is put to ctx rather than to err because pgx cancels a query by closing its
// connection, so the error it hands back after a deadline is as often a connection error as it is
// context.DeadlineExceeded -- and asking the error's identity would route a budget overrun into
// t.Fatal, which is the flake this budget exists to remove. Asking ctx errs the other way: a
// genuine SQL fault raised in the same instant the budget expired is reported as an unmeasured run
// rather than as a failure, and an unmeasured run is loud (skipTheUnmeasuredCeiling writes to
// stderr) where a silent pass would not be.
func abandonedOrFatal(ctx context.Context, t *testing.T, err error) (time.Duration, bool) {
	t.Helper()
	if ctx.Err() != nil {
		return 0, false
	}
	t.Fatal(err)
	return 0, false
}

// TestTheMeasurementBudgetAbandonsAWorkloadOnlyWhenItExpires reaches the abandonment arm directly,
// because no run on healthy hardware reaches it through the live measurement -- the 603 s run that
// prompted the budget had no budget to expire. The last row is the nearest input that must survive the
// guard: a budget that fits the work must let it finish, or the measurement is abandoned on every run and
// the ceiling stops being measured at all.
func TestTheMeasurementBudgetAbandonsAWorkloadOnlyWhenItExpires(t *testing.T) {
	skipIfShort(t)
	pool := overheadArms(t)
	for _, testCase := range []struct {
		name          string
		budget        time.Duration
		rows          int
		wantCompleted bool
	}{
		{name: "no budget at all, so the transaction never opens", rows: overheadRows},
		// A 10,000-row transaction takes about 900 ms warm, so 100 ms opens one and cannot finish it.
		{name: "a budget that opens the transaction and cannot finish it",
			budget: 100 * time.Millisecond, rows: overheadRows},
		{name: "the measurement's own budget over a workload that fits it",
			budget: overheadBudget, rows: overheadWarmupRows, wantCompleted: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), testCase.budget)
			defer cancel()

			elapsed, completed := timeWrites(ctx, t, pool, overheadPlainTable, testCase.rows)
			if completed != testCase.wantCompleted {
				t.Fatalf("timeWrites over %d rows reported completed=%t after %s, want %t; the budget "+
					"abandons a measurement and never fails one", testCase.rows, completed, elapsed,
					testCase.wantCompleted)
			}
		})
	}
}
