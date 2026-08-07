//go:build integration

package reconcile

import (
	"maps"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The remaining half of registry truth: the registry has to describe the catalog after "any
// successful apply, over a configuration exercising create, replace, drop, disable and rename in one
// run". Its
// sibling registrytruth_integration_test.go asserts both directions over a create alone, which is the
// shape where the two tables can hardly disagree; five kinds at once is where an orphaned row or an
// unrecorded object is actually reachable. It is a file of its own because the shared 200-line budget
// does not admit both fixtures in one.

// everyActionKindOutcome is the run and what it must leave behind. listener names both the listener
// and the table it starts on; table is where it ends up, which differs only for the rename. The set is
// pinned against the action vocabulary, so a sixth kind fails here rather than becoming the kind this
// fixture never exercised.
type actionKindOutcome struct {
	kind            ActionKind
	listener, table string
	// triggers is the fixture's own operation count for the kinds that keep their objects, and zero
	// for the two that remove them.
	triggers          int
	recorded, enabled bool
}

var everyActionKindOutcome = []actionKindOutcome{
	{ActionCreate, "truth_create", "truth_create", len(actionKindOperations), true, true},
	{ActionReplace, "truth_replace", "truth_replace", len(actionKindOperations), true, true},
	{ActionRename, "truth_rename", "truth_renamed", len(actionKindOperations), true, true},
	{ActionDisable, "truth_disable", "truth_disable", 0, true, false},
	{ActionDrop, "truth_drop", "truth_drop", 0, false, false},
}

var actionKindOperations = []string{"insert", "update"}

func TestOneRunOfEveryActionKindLeavesTheRegistryDescribingTheCatalog(t *testing.T) {
	skipIfShort(t)
	if len(everyActionKindOutcome) != len(actionKinds) {
		t.Fatalf(
			"%d outcomes for %d action kinds; this run must exercise every kind",
			len(everyActionKindOutcome), len(actionKinds),
		)
	}
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	changed := oneRunOfEveryActionKind(t, pool, seededActionKindFixture(t, pool))
	plan, err := Plan(t.Context(), pool, changed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertPlanHoldsEveryActionKind(t, plan)
	if _, err := Apply(
		t.Context(),
		pool,
		changed,
		Approval{Approved: true, DestructionPermitted: true},
		Options{},
	); err != nil {
		t.Fatal(err)
	}
	assertRegistryDescribesTheCatalog(t, pool)
}

// seededActionKindFixture applies everything the run will go on to change, so that the run itself is
// the one carrying all five kinds. Every target table exists from the start; the created listener is
// the only thing the second configuration introduces.
func seededActionKindFixture(t *testing.T, pool *pgxpool.Pool) config.Config {
	t.Helper()
	cfg := harnessConfig(t)
	for _, outcome := range everyActionKindOutcome {
		mustExecOn(
			t,
			pool,
			"CREATE TABLE "+mustQualifiedTarget(t, outcome.listener)+" (id bigint PRIMARY KEY, state text)",
		)
		if outcome.kind != ActionCreate {
			cfg.Listeners = append(cfg.Listeners, actionKindListener(outcome.listener, outcome.listener))
		}
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// oneRunOfEveryActionKind is the configuration whose single apply carries all five kinds: a listener
// the fixture never applied, a trigger-affecting edit, a removal, a disable and a renamed target.
func oneRunOfEveryActionKind(t *testing.T, pool *pgxpool.Pool, applied config.Config) config.Config {
	t.Helper()
	mustExecOn(t, pool, "ALTER TABLE "+mustQualifiedTarget(t, "truth_rename")+" RENAME TO \"truth_renamed\"")
	changed := applied
	changed.Listeners = nil
	for _, outcome := range everyActionKindOutcome {
		if outcome.kind == ActionDrop {
			continue // absent from the configuration is what makes a drop a drop
		}
		listener := actionKindListener(outcome.listener, outcome.table)
		switch outcome.kind {
		case ActionReplace:
			listener.Trigger.Operations[1].When = "NEW.state != 'old'"
		case ActionDisable:
			listener.Enabled = false
		}
		changed.Listeners = append(changed.Listeners, listener)
	}
	return changed
}

func actionKindListener(name, table string) config.Listener {
	listener := matrixListener(table, actionKindOperations...)
	listener.Name = name
	return listener
}

// assertPlanHoldsEveryActionKind is what makes this one run rather than five: the kinds are counted in
// a single plan, so a fixture that quietly stopped producing one of them fails here instead of
// narrowing the registry claim below without saying so.
func assertPlanHoldsEveryActionKind(t *testing.T, plan PlanResult) {
	t.Helper()
	found := make(map[ActionKind]int, len(actionKinds))
	for _, action := range plan.Actions {
		found[action.Kind]++
	}
	for _, kind := range actionKinds {
		if found[kind] == 0 {
			t.Fatalf("one run planned %v, which holds no %s action", found, kind)
		}
	}
}

// assertRegistryDescribesTheCatalog is registry truth over the whole run, in both directions and
// for both object kinds. The function scan is schema-wide rather than per-listener, so its population
// is accumulated across the run and answered once.
func assertRegistryDescribesTheCatalog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	functions, recorded := 0, map[string]bool{}
	for _, outcome := range everyActionKindOutcome {
		reading := assertOneOutcomeDescribesTheCatalog(t, pool, outcome)
		maps.Copy(recorded, recordedFunctionNames(reading))
		functions += len(reading.Triggers)
	}
	assertAllOwnedCatalogFunctionsHaveRows(t, pool, recorded, functions)
}

// assertOneOutcomeDescribesTheCatalog answers for one listener and returns the registry rows it left,
// which the caller folds into the schema-wide function scan.
func assertOneOutcomeDescribesTheCatalog(t *testing.T, pool *pgxpool.Pool, outcome actionKindOutcome) registryReading {
	t.Helper()
	reading, err := readRegistry(t.Context(), pool, harnessSchema, outcome.listener)
	if !outcome.recorded {
		assertDroppedListenerLeftNothing(t, pool, outcome.listener, reading, err)
		return registryReading{}
	}
	if err != nil {
		t.Fatalf("read the registry for %s: %v", outcome.listener, err)
	}
	stored := mustQualifiedTarget(t, outcome.table)
	target := resolvedCatalogTarget(t, pool, stored)
	if reading.Listener.TargetTable != stored || reading.Listener.TargetOID != target.OID ||
		reading.Listener.Enabled != outcome.enabled || len(reading.Triggers) != outcome.triggers {
		t.Errorf(
			"%s after its %s = %+v with %d trigger rows; want the catalog's own %s, enabled=%t and %d rows",
			outcome.listener,
			outcome.kind,
			reading.Listener,
			len(reading.Triggers),
			stored,
			outcome.enabled,
			outcome.triggers,
		)
	}
	if outcome.triggers == 0 {
		// A disabled listener keeps its row and loses its objects, so the both-directions claim is the
		// count itself: assertAllOwnedCatalogObjectsHaveRows cannot make it, because its population is
		// the registry rows and there are none to scan against.
		if owned := ownedCatalogTriggerCount(t, pool, target.OID); owned != 0 {
			t.Errorf("%s is disabled and its target still carries %d owned triggers", outcome.listener, owned)
		}
		return reading
	}
	assertRegistryRowsMatchTheCatalog(t, pool, target.OID, reading)
	assertAllOwnedCatalogObjectsHaveRows(t, pool, target.OID, reading)
	return reading
}

func assertDroppedListenerLeftNothing(
	t *testing.T,
	pool *pgxpool.Pool,
	listener string,
	reading registryReading,
	err error,
) {
	t.Helper()
	if err == nil {
		t.Errorf("%s was dropped and still holds the registry row %+v", listener, reading.Listener)
	}
	target := resolvedCatalogTarget(t, pool, mustQualifiedTarget(t, listener))
	if owned := ownedCatalogTriggerCount(t, pool, target.OID); owned != 0 {
		t.Errorf("%s was dropped and its target still carries %d owned triggers", listener, owned)
	}
}

func ownedCatalogTriggerCount(t *testing.T, pool *pgxpool.Pool, oid uint32) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM pg_trigger WHERE tgrelid=$1 AND NOT tgisinternal",
		oid,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
