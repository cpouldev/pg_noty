//go:build integration

package reconcile

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Rule: count-the-population-a-vacuity-guard-guards; first and second counts share one result surface.

func TestASecondApplyIssuesZeroStatementsAndTheFirstIssuedSome(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "idempotency_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("idempotency_target", "insert")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	var output bytes.Buffer
	options := Options{Logger: slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	first, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, options)
	if err != nil || first.Statements == 0 || first.Verdict != VerdictClean {
		t.Fatalf("first apply = %+v, %v; want a non-zero changing-statement count", first, err)
	}
	second, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, options)
	if err != nil || second.Statements != 0 || second.Verdict != VerdictClean || !strings.Contains(
		output.String(),
		"statements=0",
	) {
		t.Fatalf("second apply = %+v, %v; log=%q; want zero changing statements logged", second, err, output.String())
	}
	t.Logf("changing statements: first=%d second=%d", first.Statements, second.Statements)
}
