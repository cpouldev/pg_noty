//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Rules: derive-expected-values; test-both-sides-of-an-exclusion-guard.
func TestPlanExaminesEveryConfiguredPairAndIssuesNoWrites(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "verdict_output")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("verdict_output", "insert", "update")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	start := len(statements.values())
	changed, err := Plan(t.Context(), pool, cfg, Options{})
	changedWrites := changingStatements(statements.values()[start:])
	wantPairs := len(cfg.Listeners[0].Trigger.Operations) // one listener × its two operation entries.
	if err != nil || changed.Examined != wantPairs || len(changedWrites) != 0 || len(ddlStatements(statements.values()[start:])) != 0 {
		t.Fatalf(
			"change plan=%+v err=%v writes=%q; want %d examined and no server writes",
			changed,
			err,
			changedWrites,
			wantPairs,
		)
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	start = len(statements.values())
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	cleanWrites := changingStatements(statements.values()[start:])
	if err != nil || clean.Verdict != VerdictClean || clean.Examined != wantPairs || len(cleanWrites) != 0 || len(ddlStatements(statements.values()[start:])) != 0 {
		t.Fatalf(
			"clean plan=%+v err=%v writes=%q; want clean %d examined and no server writes",
			clean,
			err,
			cleanWrites,
			wantPairs,
		)
	}
}
