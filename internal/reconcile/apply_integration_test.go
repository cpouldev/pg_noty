//go:build integration

package reconcile

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyCreatesTheReDiffedPlanAndLeavesTheNextPlanClean(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "apply_entrypoint_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := listenerForTarget("public.apply_entrypoint_target")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	planned, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(planned.Actions) != 1 || planned.Actions[0].Kind != ActionCreate {
		t.Fatalf("first plan = %+v, %v; want one create", planned, err)
	}
	applied, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || applied.Verdict != VerdictClean || applied.Statements == 0 {
		t.Fatalf("apply = %+v, %v; want a changed clean result", applied, err)
	}
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || clean.Verdict != VerdictClean || len(clean.Actions) != 0 {
		t.Fatalf("second plan = %+v, %v; want clean", clean, err)
	}
}

func TestApplyReDiffsInsteadOfTrustingAnEarlierPlan(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "apply_stale_plan_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := listenerForTarget("public.apply_stale_plan_target")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	earlier, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(earlier.Actions) != 1 || earlier.Actions[0].Kind != ActionCreate {
		t.Fatalf("earlier plan = %+v, %v; want a create that will become stale", earlier, err)
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatalf("concurrent reconciler settling earlier plan: %v", err)
	}
	var log bytes.Buffer
	options := Options{Logger: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, options)
	if err != nil || result.Statements != 0 || result.Verdict != VerdictClean || len(result.Plan.Actions) != 0 {
		t.Fatalf("re-diff after stale plan = %+v, %v; want database-clean no-op", result, err)
	}
	if workflowIndex(log.String(), "step=decision") >= 0 {
		t.Fatal("clean Apply consulted Decide; only changed plans may enter the approval grid")
	}
}

func TestApplyRecordsTheWorkflowOrder(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "apply_workflow_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := listenerForTarget("public.apply_workflow_target")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	var log bytes.Buffer
	options := Options{Logger: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	if result, err := Apply(
		t.Context(),
		pool,
		cfg,
		Approval{Approved: true},
		options,
	); err != nil || result.Verdict != VerdictClean {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	assertWorkflowOrder(t, log.String(), statements.values())
}

type recordedStatements struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordedStatements) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	r.mu.Lock()
	r.seen = append(r.seen, data.SQL)
	r.mu.Unlock()
	return ctx
}
func (*recordedStatements) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (r *recordedStatements) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}
func tracedDatabase(t *testing.T) (*pgxpool.Pool, *recordedStatements) {
	t.Helper()
	base := freshDatabase(t)
	base.Close()
	poolConfig, err := pgxpool.ParseConfig(harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorded := &recordedStatements{}
	poolConfig.ConnConfig.Tracer = recorded
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, recorded
}
func assertWorkflowOrder(t *testing.T, log string, statements []string) {
	t.Helper()
	after := 0
	for _, step := range []string{
		"step=validation", "step=registry read", "step=ownership", "step=decision", "step=readback",
		"step=registry write",
	} {
		at := workflowIndex(log[after:], step)
		if at < 0 {
			t.Fatalf("workflow log missing %q after %q: %s", step, log[:after], log)
		}
		after += at + len(step)
	}
	search, timeout := indexStatement(statements, "SET LOCAL search_path = ''"), indexStatement(
		statements,
		"SET LOCAL lock_timeout",
	)
	if search < 1 || statements[search-1] != "begin" || timeout != search+1 {
		t.Fatalf("first apply settings = %v at %d and %d, want adjacent and ordered", statements, search, timeout)
	}
	t.Logf(
		"recorded apply workflow: %s; first transaction statements: %q, %q",
		log,
		statements[search],
		statements[timeout],
	)
}
func workflowIndex(log, step string) int {
	if at := strings.Index(log, step); at >= 0 {
		return at
	}
	return strings.Index(log, strings.Replace(step, "=", "=\"", 1)+"\"")
}
func indexStatement(statements []string, prefix string) int {
	for index, statement := range statements {
		if strings.HasPrefix(statement, prefix) {
			return index
		}
	}
	return -1
}
