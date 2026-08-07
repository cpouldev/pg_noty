//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: repair-the-class-not-the-reproduction; test-both-sides-of-an-exclusion-guard.
func TestDroppedTargetHasAnErrorPlanWithoutAnyDestroyAction(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, _, target := appliedTargetFixture(t, pool, "target_dropped", "insert")
	mustExecOn(t, pool, "DROP TABLE "+target)
	configured := mustQualifiedTarget(t, "target_drop_configured")
	mustExecOn(t, pool, "CREATE TABLE "+configured+" (id bigint PRIMARY KEY)")
	cfg.Listeners[0].Trigger.Table = "public.target_drop_configured"
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || plan.Verdict != VerdictError || len(plan.Refusals) != 1 || len(plan.Actions) != 0 {
		t.Fatalf("dropped target plan = %+v, %v; want one refusal and no action anywhere", plan, err)
	}
}

func TestRenamedTargetUpdatesTheRegistryWithoutDDLAndSameNameStaysClean(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "target_rename", "insert", "update")
	before := matrixIdentities(t, pool, target, listener.Name)
	renamed := mustQualifiedTarget(t, "target_renamed")
	mustExecOn(t, pool, "ALTER TABLE "+target+" RENAME TO \"target_renamed\"")
	cfg.Listeners[0].Trigger.Table = "public.target_renamed"
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(plan.Actions) != len(before) {
		t.Fatalf("renamed target plan = %+v, %v; want rename per operation", plan, err)
	}
	for _, action := range plan.Actions {
		if action.Kind != ActionRename {
			t.Fatalf("renamed target action = %+v, want rename", action)
		}
	}
	start := len(statements.values())
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || result.Statements != 0 || len(ddlStatements(statements.values()[start:])) != 0 {
		t.Fatalf(
			"renamed target apply = %+v, %v; server DDL=%q",
			result,
			err,
			ddlStatements(statements.values()[start:]),
		)
	}
	after := matrixIdentities(t, pool, renamed, listener.Name)
	if after["insert"] != before["insert"] || after["update"] != before["update"] {
		t.Fatalf("renamed target identities %v -> %v; catalog objects must stay identical", before, after)
	}
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || reading.Listener.TargetTable != renamed {
		t.Fatalf("renamed target registry = %+v, %v; want %s", reading.Listener, err, renamed)
	}
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || clean.Verdict != VerdictClean || len(clean.Actions) != 0 {
		t.Fatalf("same-name control plan = %+v, %v; want no rename", clean, err)
	}
}

func TestDroppedAndRecreatedTargetAdoptsTheNewOIDAndRecreatesItsObjects(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "target_recreated", "insert")
	old := resolvedCatalogTarget(t, pool, target).OID
	mustExecOn(t, pool, "DROP TABLE "+target)
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	newOID := resolvedCatalogTarget(t, pool, target).OID
	if old == newOID {
		t.Fatal("fixture did not allocate a new target OID")
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || reading.Listener.TargetOID != newOID || len(reading.Triggers) != 1 {
		t.Fatalf("recreated target registry = %+v, %v; want adopted OID %d", reading, err, newOID)
	}
	if _, found, err := catalogTrigger(t, pool, newOID, reading.Triggers[0].TriggerName); err != nil || !found {
		t.Fatalf("recreated target trigger found=%t err=%v", found, err)
	}
}

func appliedTargetFixture(t *testing.T, pool *pgxpool.Pool, name string, operations ...string) (
	config.Config,
	config.Listener,
	string,
) {
	t.Helper()
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, name)
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener(name, operations...)
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	return cfg, listener, target
}

func ddlStatements(statements []string) []string {
	var ddl []string
	for _, statement := range statements {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(upper, "CREATE ") || strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(
			upper,
			"ALTER ",
		) || strings.HasPrefix(upper, "COMMENT ") {
			ddl = append(ddl, statement)
		}
	}
	return ddl
}

func catalogTrigger(t *testing.T, pool *pgxpool.Pool, oid uint32, name string) (TriggerReading, bool, error) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		return TriggerReading{}, false, err
	}
	defer tx.Rollback(t.Context())
	return NewCatalog(tx).ReadTrigger(t.Context(), oid, name)
}
