//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: test-both-sides-of-an-exclusion-guard; the second clean plan resolves the disable.
// Deferred to internal/delivery: this proves queue rows and statuses survive disabling, not that
// they deliver.
func TestASecondPlanAfterADisableReportsClean(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "disable_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("disable_target", "insert", "update")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "pending", "delivering", "dead"} {
		seedQueueStatus(t, pool, listener.Name, status)
	}
	beforeQueue, err := readQueueCounts(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	objects := disableObjects(t, pool, target, listener.Name)
	listener.Enabled = false
	cfg.Listeners[0] = listener
	result, err := Apply(t.Context(), pool, cfg, Approval{DestructionPermitted: true, Approved: true}, Options{})
	if err != nil || result.Statements == 0 {
		t.Fatalf("disable apply = %+v, %v; want object removals", result, err)
	}
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || reading.Listener.Enabled || len(reading.Triggers) != 0 {
		t.Fatalf("disabled registry = %+v, %v; want retained disabled listener only", reading, err)
	}
	assertDisabledObjectsDropped(t, pool, target, objects)
	afterQueue, err := readQueueCounts(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || afterQueue != beforeQueue {
		t.Fatalf("queue counts %v -> %v, %v; want unchanged statuses", beforeQueue, afterQueue, err)
	}
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || plan.Verdict != VerdictClean || len(plan.Actions) != 0 {
		t.Fatalf("second plan after disable = %+v, %v; retained trigger rows would resurrect by drift", plan, err)
	}
}

func disableObjects(t *testing.T, pool *pgxpool.Pool, target, listener string) map[string]uint32 {
	t.Helper()
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if err != nil {
		t.Fatal(err)
	}
	oid := resolvedCatalogTarget(t, pool, target).OID
	objects := make(map[string]uint32, len(reading.Triggers))
	for _, trigger := range reading.Triggers {
		objects[trigger.TriggerName] = readCatalogTrigger(t, pool, oid, trigger.TriggerName).FunctionOID
	}
	return objects
}

func assertDisabledObjectsDropped(t *testing.T, pool *pgxpool.Pool, target string, objects map[string]uint32) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	catalog, oid := NewCatalog(tx), resolvedCatalogTarget(t, pool, target).OID
	for triggerName, functionOID := range objects {
		if _, found, err := catalog.ReadTrigger(t.Context(), oid, triggerName); err != nil || found {
			t.Fatalf("disabled trigger %q found=%t err=%v", triggerName, found, err)
		}
		if _, found, err := catalog.ReadFunction(t.Context(), functionOID); err != nil || found {
			t.Fatalf("disabled function %d found=%t err=%v", functionOID, found, err)
		}
	}
}
