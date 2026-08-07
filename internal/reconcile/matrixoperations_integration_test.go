//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type matrixIdentity struct{ trigger, function uint32 }

// Rules: assert-a-position-per-element-not-as-a-set; falsify-the-assertion-not-a-copy-of-it.

func TestOneChangedOperationReplacesOnlyItsCatalogPair(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "matrix_change_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, state text)")
	listener := matrixListener("matrix_change_target", "insert", "update", "delete")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	before := matrixIdentities(t, pool, target, listener.Name)
	beforeHash := registryDDLHash(t, pool, listener.Name, "update")
	listener.Trigger.Operations[1].When = "NEW.state != 'old'"
	cfg.Listeners[0] = listener
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0] != (Action{
		Pair: Pair{
			Listener: listener.Name, Operation: "update",
		}, Kind: ActionReplace,
	}) {
		t.Fatalf("one edited operation plan = %+v, %v", plan, err)
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	after := matrixIdentities(t, pool, target, listener.Name)
	if before["update"] == after["update"] || before["insert"] != after["insert"] || before["delete"] != after["delete"] {
		t.Fatalf("identities before=%v after=%v; only update may change", before, after)
	}
	if afterHash := registryDDLHash(t, pool, listener.Name, "update"); afterHash == beforeHash {
		t.Fatalf("update ddl_hash = %q after a changed configuration", afterHash)
	}
	assertIdentityControlCanFail(t, pool, cfg, listener, target, before["insert"])
}

func TestAddedAndRemovedOperationsAreBoundToTheirOwnPairs(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "matrix_operations_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("matrix_operations_target", "insert", "update")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	before := matrixIdentities(t, pool, target, listener.Name)
	t.Run(
		"criterion_14_added", func(t *testing.T) {
			listener.Trigger.Operations = append(listener.Trigger.Operations, config.Operation{Kind: "delete"})
			cfg.Listeners[0] = listener
			plan, err := Plan(t.Context(), pool, cfg, Options{})
			assertOneMatrixAction(t, plan, err, listener.Name, "delete", ActionCreate)
			if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
				t.Fatal(err)
			}
			withDelete := matrixIdentities(t, pool, target, listener.Name)
			if withDelete["delete"].trigger == 0 || withDelete["insert"] != before["insert"] || withDelete["update"] != before["update"] {
				t.Fatalf("added pair identities=%v, want only delete added", withDelete)
			}
		},
	)
	t.Run(
		"criterion_15_removed", func(t *testing.T) {
			listener.Trigger.Operations = listener.Trigger.Operations[:2]
			cfg.Listeners[0] = listener
			plan, err := Plan(t.Context(), pool, cfg, Options{})
			assertOneMatrixAction(t, plan, err, listener.Name, "delete", ActionDrop)
			if _, err := Apply(
				t.Context(),
				pool,
				cfg,
				Approval{DestructionPermitted: true, Approved: true},
				Options{},
			); err != nil {
				t.Fatal(err)
			}
			after := matrixIdentities(t, pool, target, listener.Name)
			if _, exists := after["delete"]; exists || after["insert"] != before["insert"] || after["update"] != before["update"] {
				t.Fatalf("removed operation identities=%v, want only delete absent", after)
			}
		},
	)
}

func matrixIdentities(t *testing.T, pool *pgxpool.Pool, target, listener string) map[string]matrixIdentity {
	t.Helper()
	recorded, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if err != nil {
		t.Fatal(err)
	}
	targetReading := resolvedCatalogTarget(t, pool, target)
	identities := make(map[string]matrixIdentity, len(recorded.Triggers))
	for _, trigger := range recorded.Triggers {
		reading := readCatalogTrigger(t, pool, targetReading.OID, trigger.TriggerName)
		identities[trigger.Operation] = matrixIdentity{trigger: reading.OID, function: reading.FunctionOID}
	}
	return identities
}

func assertIdentityControlCanFail(
	t *testing.T,
	pool *pgxpool.Pool,
	cfg config.Config,
	listener config.Listener,
	target string,
	before matrixIdentity,
) {
	t.Helper()
	compiled, err := compileListener(
		cfg.Instance,
		cfg.Database.Schema,
		listener,
		resolvedCatalogTarget(t, pool, target),
	)
	if err != nil {
		t.Fatal(err)
	}
	set := compiled.Sets[0]
	drop, err := dropTriggerText(resolvedCatalogTarget(t, pool, target), set.TriggerName)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, drop)
	mustExecOn(t, pool, set.CreateTrigger)
	after := readCatalogTrigger(t, pool, resolvedCatalogTarget(t, pool, target).OID, set.TriggerName)
	if before.trigger == after.OID {
		t.Fatal("identity control did not fail after a deliberate trigger recreation")
	}
}

func registryDDLHash(t *testing.T, pool *pgxpool.Pool, listener, operation string) string {
	t.Helper()
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range reading.Triggers {
		if trigger.Operation == operation {
			return trigger.DDLHash
		}
	}
	t.Fatalf("registry has no %s operation", operation)
	return ""
}

func assertOneMatrixAction(t *testing.T, plan PlanResult, err error, listener, operation string, kind ActionKind) {
	t.Helper()
	want := Action{Pair: Pair{Listener: listener, Operation: operation}, Kind: kind}
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0] != want {
		t.Fatalf("plan = %+v, %v; want only %+v", plan, err, want)
	}
}
