//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This discharges the deferred Apply-level half of TestAPlantedUnmarkedTriggerIsRefusedAndSurvivesByteIdentical.
func TestAnUnmarkedCollisionLeavesBothListenersUnchanged(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	firstTarget, secondTarget := mustQualifiedTarget(t, "collision_first"), mustQualifiedTarget(t, "collision_second")
	mustExecOn(t, pool, "CREATE TABLE "+firstTarget+" (id bigint PRIMARY KEY)")
	mustExecOn(t, pool, "CREATE TABLE "+secondTarget+" (id bigint PRIMARY KEY)")
	first, second := matrixListener("collision_first", "insert"), matrixListener("collision_second", "insert")
	first.Name, second.Name = "collision", "sibling"
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{first, second}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	firstRow, err := readRegistry(t.Context(), pool, harnessSchema, first.Name)
	if err != nil {
		t.Fatal(err)
	}
	secondRow, err := readRegistry(t.Context(), pool, harnessSchema, second.Name)
	if err != nil {
		t.Fatal(err)
	}
	firstBefore := readCatalogPair(
		t,
		pool,
		resolvedCatalogTarget(t, pool, firstTarget).OID,
		firstRow.Triggers[0].TriggerName,
		0,
		false,
	)
	secondBefore := readCatalogPair(
		t,
		pool,
		resolvedCatalogTarget(t, pool, secondTarget).OID,
		secondRow.Triggers[0].TriggerName,
		0,
		false,
	)
	mustExecOn(t, pool, "COMMENT ON TRIGGER \""+firstRow.Triggers[0].TriggerName+"\" ON "+firstTarget+" IS NULL")
	cfg.Listeners = []config.Listener{first}
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true, DestructionPermitted: true}, Options{})
	if err != nil || result.Stopped != StopOwnership || len(result.Plan.Actions) != 0 {
		t.Fatalf("collision apply=%+v err=%v; want ownership refusal before permitted sibling drop", result, err)
	}
	firstAfter := readCatalogPair(
		t,
		pool,
		resolvedCatalogTarget(t, pool, firstTarget).OID,
		firstRow.Triggers[0].TriggerName,
		0,
		false,
	)
	secondAfter := readCatalogPair(
		t,
		pool,
		resolvedCatalogTarget(t, pool, secondTarget).OID,
		secondRow.Triggers[0].TriggerName,
		0,
		false,
	)
	if firstBefore != firstAfter || secondBefore != secondAfter {
		t.Fatalf("collision=%#v sibling=%#v; want no object definition changed", firstAfter, secondAfter)
	}
	if marker := readCatalogTrigger(
		t,
		pool,
		resolvedCatalogTarget(t, pool, firstTarget).OID,
		firstRow.Triggers[0].TriggerName,
	).Marker; marker != nil {
		t.Fatalf("collision trigger marker=%q; want the planted absence preserved", *marker)
	}
	if _, err := readRegistry(t.Context(), pool, harnessSchema, second.Name); err != nil {
		t.Fatalf("sibling registry was removed despite the ownership refusal: %v", err)
	}
}

// TestASecondInstancesObjectsMakeTheRunsVerdictNonZero is the verdict clause of the foreign-instance
// rule. Its other clauses are asserted before Plan and Apply exist, so
// ownershipsurvival_integration_test.go asserts the refusal and the untouched sibling at
// DetermineOwnership level and nothing drove a foreign instance's marker through an actual run. That
// left "the run's verdict is non-zero" unowned. It is asserted here because this is where the
// rule's Apply-level twin already lives, and because it is the case object naming makes inevitable:
// the marker carries the instance and the derived object name does not, so two deployments sharing
// one table collide by construction rather than by accident.
func TestASecondInstancesObjectsMakeTheRunsVerdictNonZero(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	sharedName, siblingName := "foreign_shared_target", "foreign_sibling_target"
	shared, sibling := mustQualifiedTarget(t, sharedName), mustQualifiedTarget(t, siblingName)
	mustExecOn(t, pool, "CREATE TABLE "+shared+" (id bigint PRIMARY KEY)")
	mustExecOn(t, pool, "CREATE TABLE "+sibling+" (id bigint PRIMARY KEY)")
	ours, elsewhere := matrixListener(sharedName, "insert"), matrixListener(siblingName, "insert")
	ours.Name, elsewhere.Name = "orders", "receipts"
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{ours, elsewhere}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatalf("seeding this instance's two listeners: %v", err)
	}
	row, err := readRegistry(t.Context(), pool, harnessSchema, ours.Name)
	if err != nil || len(row.Triggers) != 1 {
		t.Fatalf("registry after seeding = %+v, %v; want the one trigger the collision lands on", row, err)
	}

	// A second deployment takes over the shared table under its own instance. Its marker is derived
	// rather than transcribed, and the name equality below asserts that premise rather than assuming it.
	foreign := generatedOwnershipCatalogFixture(t, "bravo", ours.Name, sharedName)
	if foreign.set.TriggerName != row.Triggers[0].TriggerName {
		t.Fatalf(
			"instances generated %q and %q; the derived name must omit the instance",
			foreign.set.TriggerName, row.Triggers[0].TriggerName,
		)
	}
	mustExecOn(
		t, pool, "COMMENT ON TRIGGER "+quotedOwnershipIdentifier(t, row.Triggers[0].TriggerName)+
			" ON "+shared+" IS "+harnessLiteral(foreign.set.Marker),
	)

	// This instance's configuration would otherwise drop that pair -- one of the three the rule names,
	// and the destructive one.
	without := cfg
	without.Listeners = []config.Listener{elsewhere}
	planned, err := Plan(t.Context(), pool, without, Options{})
	if err != nil {
		t.Fatalf("plan against a second instance's objects: %v", err)
	}
	if planned.Verdict.ExitCode() == 0 || planned.Verdict != VerdictError || len(planned.Refusals) != 1 {
		t.Fatalf(
			"plan = %+v; want a single refusal and a non-zero verdict, since a zero one tells an "+
				"operator the run was clean while another deployment's objects went unreconciled", planned,
		)
	}
	if !strings.Contains(planned.Refusals[0].Message(), foreign.set.Marker) {
		t.Fatalf(
			"refusal %q does not carry the conflicting marker %q, so it names no different instance",
			planned.Refusals[0].Message(), foreign.set.Marker,
		)
	}
	applied, err := Apply(t.Context(), pool, without, Approval{Approved: true, DestructionPermitted: true}, Options{})
	if err != nil || applied.Verdict.ExitCode() == 0 || applied.Stopped != StopOwnership {
		t.Fatalf("apply = %+v, %v; want the ownership stop and a non-zero verdict", applied, err)
	}

	// The refusal's presence in the diff must not disturb this instance's own objects elsewhere.
	if _, err := readRegistry(t.Context(), pool, harnessSchema, elsewhere.Name); err != nil {
		t.Fatalf("this instance's other-table listener was removed by the collision: %v", err)
	}
	if left := countOn(
		t, pool, "SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid "+
			"WHERE c.relname = $1 AND NOT t.tgisinternal", siblingName,
	); left != 1 {
		t.Fatalf("triggers on this instance's other table = %d, want the one it owns", left)
	}
}
