//go:build integration

package reconcile

import (
	"errors"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: test-both-sides-of-an-exclusion-guard; the same listener differs only in retention.

func TestRetainedWithNoOperationsAndRemovedListenerHaveDifferentRegistryOutcomes(t *testing.T) {
	skipIfShort(t)
	for _, testCase := range []struct {
		name     string
		retained bool
	}{
		// The configuration keeps this listener but explicitly gives it no operation.
		{name: "criterion_16_retained", retained: true},
		// The only changed property is listener presence in the configuration.
		{name: "criterion_17_removed"},
	} {
		t.Run(
			testCase.name, func(t *testing.T) {
				pool, cfg, listener := appliedRemovalFixture(t, "matrix_lifecycle")
				if testCase.retained {
					listener.Trigger.Operations = config.Operations{}
					cfg.Listeners[0] = listener
				} else {
					cfg.Listeners = []config.Listener{}
				}
				plan, err := Plan(t.Context(), pool, cfg, Options{})
				assertOneMatrixAction(t, plan, err, listener.Name, "insert", ActionDrop)
				if _, err := Apply(
					t.Context(),
					pool,
					cfg,
					Approval{DestructionPermitted: true, Approved: true},
					Options{},
				); err != nil {
					t.Fatal(err)
				}
				assertRemovalRegistryOutcome(t, pool, listener.Name, testCase.retained)
			},
		)
	}
}

func TestEmptyListenersPlanDestructiveAndAbsentConfigurationCannotPlan(t *testing.T) {
	skipIfShort(t)
	pool, cfg, listener := appliedRemovalFixture(t, "matrix_empty_list")
	empty := cfg
	empty.Listeners = []config.Listener{}
	plan, err := Plan(t.Context(), pool, empty, Options{})
	if err != nil || !plan.Destructive() || len(plan.Actions) != 1 {
		t.Fatalf("explicit empty listeners plan = %+v, %v; want one destructive removal", plan, err)
	}
	stopped, err := Apply(t.Context(), pool, empty, Approval{Approved: true}, Options{})
	if err != nil || stopped.Stopped != StopPermission || stopped.Statements != 0 {
		t.Fatalf("empty listeners apply = %+v, %v; want permission refusal without changes", stopped, err)
	}
	absent, _, diagnostics := config.Parse(
		sourceBytes(t, absentListenersFixture),
		"absent-listeners.yaml",
		config.MapEnv(nil),
	)
	assertEmptyAndAbsentTakeDifferentPlanningPaths(t, plan, absent, diagnostics, listener.Name)
}

func appliedRemovalFixture(t *testing.T, name string) (*pgxpool.Pool, config.Config, config.Listener) {
	t.Helper()
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, name+"_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener(name+"_target", "insert")
	listener.Name = name
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	return pool, cfg, listener
}

func assertRemovalRegistryOutcome(t *testing.T, pool *pgxpool.Pool, listener string, retained bool) {
	t.Helper()
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if retained {
		if err != nil || reading.Listener.Name != listener || len(reading.Triggers) != 0 {
			t.Fatalf("retained listener registry = %+v, %v; want listener and no triggers", reading, err)
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) || registryTriggerCount(t, pool, listener) != 0 {
		t.Fatalf("removed listener registry = %+v, %v; want no listener or trigger row", reading, err)
	}
}

func assertEmptyAndAbsentTakeDifferentPlanningPaths(
	t *testing.T,
	plan PlanResult,
	absent *config.Config,
	diagnostics config.Errors,
	listener string,
) {
	t.Helper()
	if plan.Actions[0].Pair.Listener != listener || absent != nil || len(diagnostics) == 0 {
		t.Fatalf(
			"empty plan=%+v absent=%#v diagnostics=%#v; want Plan only for the explicit empty list",
			plan,
			absent,
			diagnostics,
		)
	}
}

func registryTriggerCount(t *testing.T, pool *pgxpool.Pool, listener string) int {
	t.Helper()
	return countOn(t, pool, "SELECT count(*) FROM "+harnessSchema+".listener_triggers WHERE listener = $1", listener)
}
