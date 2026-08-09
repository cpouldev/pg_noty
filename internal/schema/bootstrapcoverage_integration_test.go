//go:build integration

package schema

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 31's refusal half, realised against a real schema. The arithmetic itself is
// Step 3's and is not re-asserted here; what is asserted is that step 10 hands that arithmetic the
// database's own instant and the catalog's own bounds, and refuses on what it answers.
//
// The decisive row is the one where coverage reaches the horizon *exactly*, because it is the only
// input that tells `>` from `>=`. A live clock cannot be made to land on a boundary on demand --
// every reading is a few milliseconds later than the one the expectation was derived from -- so the
// three horizons are paired with one real reading instead, and each is derived from that reading's
// own numbers rather than written down.

// TestCoverageIsRefusedOnlyWhenItFallsShortOfTheHorizon is criterion 31's boundary triple.
func TestCoverageIsRefusedOnlyWhenItFallsShortOfTheHorizon(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)

	began := theClockOf(t, pool)
	mustBootstrap(t, pool, cfg)

	pinned := acquiredConn(t, pool)
	seen, err := boot{on: pinned, cfg: cfg, opts: Options{}.normalized()}.readCoverage(t.Context())
	if err != nil {
		t.Fatalf("read the coverage a converged boot left behind: %v", err)
	}

	// The partitions the boot had to create, from the arithmetic and from the reading taken before
	// it. theHorizonBetween fails the run if the two readings fell either side of a grid line, in
	// which case the expectation rather than the boot was wrong.
	wanted := theHorizonBetween(t, began, seen.now)
	if len(seen.bounded) != len(wanted) {
		t.Fatalf("the boot left %d bounded partitions and the arithmetic asks for %d, so the reach "+
			"derived below is not the reach observed", len(seen.bounded), len(wanted))
	}

	// Contiguous from the reading's own instant: the ranges are the epoch grid's, consecutive from
	// the one holding now, so the coverage ends where the last one does.
	reach := wanted[len(wanted)-1].To
	toTheHorizon := reach.Sub(seen.now)

	for _, tc := range []struct {
		name      string
		precreate time.Duration
		wantShort time.Duration
	}{
		{name: "coverage reaching exactly the horizon", precreate: toTheHorizon},
		{name: "coverage extending one whole interval beyond the horizon",
			precreate: toTheHorizon - cfg.Retention.PartitionInterval},
		{name: "coverage one nanosecond short of the horizon",
			precreate: toTheHorizon + time.Nanosecond, wantShort: time.Nanosecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			short := cfg
			short.Retention.Precreate = tc.precreate

			refusal := boot{on: pinned, cfg: short, opts: Options{}.normalized()}.
				shortCoverageRefusal(seen)
			assertCoverageVerdict(t, refusal, tc.wantShort, tc.precreate)
		})
	}
}

// assertCoverageVerdict is the answer step 10 owes one horizon: nothing at all, or a refusal naming
// both the shortfall and the horizon it falls short of. The whole message is compared, so a refusal
// carrying the right sentinel and the wrong numbers fails here.
func assertCoverageVerdict(t *testing.T, refusal error, wantShort, horizon time.Duration) {
	t.Helper()

	if wantShort == 0 {
		if refusal != nil {
			t.Fatalf("coverage reaching the %s horizon was refused with %v; only coverage strictly "+
				"short of it is", horizon, refusal)
		}
		return
	}

	if !errors.Is(refusal, ErrCoverageShort) {
		t.Fatalf("coverage %s short of the %s horizon answered %v, want %v",
			wantShort, horizon, refusal, ErrCoverageShort)
	}
	if want := coverageShort(wantShort, horizon).Error(); refusal.Error() != want {
		t.Errorf("the refusal reads %q, want %q", refusal, want)
	}
	if !strings.Contains(refusal.Error(), wantShort.String()) {
		t.Errorf("the refusal %q does not name the shortfall %s an operator has to close",
			refusal, wantShort)
	}
}

// TestABootIntoASchemaCoveringNothingIsRefused is criterion 31 through the whole entry point, so the
// refusal is asserted where a service would meet it and not only where it is decided.
//
// The fixture removes every partition of the event log, DEFAULT included. That is deliberately not
// ADR-8's blocked range: rows sitting in DEFAULT are a state this boot must start over, and this one
// is a schema whose write-availability net an operator has removed.
func TestABootIntoASchemaCoveringNothingIsRefused(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)
	mustBootstrap(t, pool, cfg)

	dropEveryPartitionOfTheEventLog(t, pool)

	refusal := Bootstrap(t.Context(), pool, cfg)
	horizon := cfg.Retention.Precreate

	if !errors.Is(refusal, ErrCoverageShort) {
		t.Fatalf("a boot into a schema whose event log carries no partition answered %v, want %v",
			refusal, ErrCoverageShort)
	}
	// Coverage from now reaches now, so the shortfall is the whole horizon.
	if want := coverageShort(horizon, horizon).Error(); refusal.Error() != want {
		t.Errorf("the refusal reads %q, want %q", refusal, want)
	}
}

// dropEveryPartitionOfTheEventLog removes what the catalog reports beneath the event log, read
// through this package's own observation authority so the fixture cannot miss one the boot can see.
//
// CASCADE, because the delivery queue's composite foreign key depends on each partition it can
// reference; the fixture is an operator who has removed the write-availability net, and the queue's
// constraint going with it is collateral this case makes no claim about.
func dropEveryPartitionOfTheEventLog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions to remove: %v", err)
	}
	attached := append(namesOf(found.Bounded), found.Default)
	if len(attached) < 2 {
		t.Fatalf("the event log carries %d partitions %v, so removing them leaves a schema that was "+
			"already short of its horizon", len(attached), attached)
	}
	for _, name := range attached {
		mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, name)+" CASCADE")
	}
}
