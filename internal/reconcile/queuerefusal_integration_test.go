//go:build integration

package reconcile

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Rules: derive-expected-values; the bounded aggregate is the sole event-queue observation here.
func TestLiveQueueCountRefusesRemovalAndPermissionLeavesQueueRowsInPlace(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "queue_refusal_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("queue_refusal_target", "insert")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "pending", "delivering", "dead"} {
		seedQueueStatus(t, pool, listener.Name, status)
	}
	before, err := readQueueCounts(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || before.Pending != 2 || before.Delivering != 1 || before.Live != before.Pending+before.Delivering || before.Dead != 1 {
		t.Fatalf("fixture reports pending=2 + delivering=1 = live=3 and dead=1 separately: %+v, %v", before, err)
	}
	empty := cfg
	empty.Listeners = []config.Listener{}
	plan, err := Plan(t.Context(), pool, empty, Options{})
	assertOneMatrixAction(t, plan, err, listener.Name, "insert", ActionDrop)
	var output bytes.Buffer
	options := Options{Logger: slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	start := len(statements.values())
	stopped, err := Apply(t.Context(), pool, empty, Approval{Approved: true}, options)
	queueQueries := boundedQueueQueries(statements.values()[start:])
	if err != nil || stopped.Stopped != StopPermission || stopped.Statements != 0 || len(queueQueries) != 1 || !strings.Contains(
		queueQueries[0],
		"GROUP BY status",
	) || !strings.Contains(output.String(), "live=3") || !strings.Contains(output.String(), "dead=1") {
		t.Fatalf("unpermitted removal = %+v, %v; live=%d dead=%d", stopped, err, before.Live, before.Dead)
	}
	if _, err := Apply(
		t.Context(),
		pool,
		empty,
		Approval{DestructionPermitted: true, Approved: true},
		Options{},
	); err != nil {
		t.Fatal(err)
	}
	after, err := readQueueCounts(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || after != before {
		t.Fatalf(
			"permitted removal queue counts %v -> %v, %v; this package must not delete queue rows",
			before,
			after,
			err,
		)
	}
}

func boundedQueueQueries(statements []string) []string {
	var queries []string
	for _, statement := range statements {
		if strings.Contains(statement, "event_queue") {
			queries = append(queries, statement)
		}
	}
	return queries
}
