//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// Rules: pin-documented-precedence; the inputs below violate both rules whose order matters.
func TestEqualSpecificationHashStillRecreatesOneDroppedCatalogTrigger(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "precedence_drift", "insert")
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || clean.Verdict != VerdictClean || len(clean.Actions) != 0 {
		t.Fatalf("intact equal-hash plan = %+v, %v; want clean neighbour", clean, err)
	}
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || len(reading.Triggers) != 1 {
		t.Fatalf("registry = %+v, %v", reading, err)
	}
	drop, err := dropTriggerText(resolvedCatalogTarget(t, pool, target), reading.Triggers[0].TriggerName)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, drop)
	drift, err := Plan(t.Context(), pool, cfg, Options{})
	assertOneMatrixAction(t, drift, err, listener.Name, "insert", ActionCreate)
	if drift.Verdict != VerdictChangesPending {
		t.Fatalf("dropped trigger plan verdict = %q, want changes pending", drift.Verdict)
	}
}

func TestInvalidChangeBearingConfigurationWritesNothingAndItsCorrectionApplies(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "precedence_validation")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("precedence_validation", "insert")
	listener.Trigger.Operations[0].When = "NEW.missing > 0"
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	start := len(statements.values())
	failed, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || failed.Verdict != VerdictError || failed.Stopped != StopValidation || len(failed.Plan.Diagnostics) == 0 || len(changingStatements(statements.values()[start:])) != 0 {
		t.Fatalf(
			"invalid changed apply = %+v, %v; server changes=%q",
			failed,
			err,
			changingStatements(statements.values()[start:]),
		)
	}
	listener.Trigger.Operations[0].When = "NEW.id > 0"
	cfg.Listeners[0] = listener
	corrected, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || corrected.Verdict != VerdictClean || corrected.Statements == 0 {
		t.Fatalf("corrected apply = %+v, %v; valid input must issue DDL", corrected, err)
	}
}

func TestBootstrapAbsenceReturnsItsErrorVerdict(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	plan, err := Plan(t.Context(), pool, harnessConfig(t), Options{})
	if err != nil || plan.Verdict != VerdictError || len(plan.Refusals) != 1 || len(plan.Actions) != 0 {
		t.Fatalf("absent bootstrap plan = %+v, %v; want structural error verdict", plan, err)
	}
}

func TestOwnershipRefusalPrecedesTheUnpermittedDestructiveDecision(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "precedence_ownership", "insert")
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || len(reading.Triggers) != 1 {
		t.Fatalf("registry = %+v, %v", reading, err)
	}
	trigger, fault := schema.Quoted(reading.Triggers[0].TriggerName)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	mustExecOn(t, pool, "COMMENT ON TRIGGER "+trigger+" ON "+target+" IS NULL")
	empty := cfg
	empty.Listeners = []config.Listener{}
	result, err := Apply(t.Context(), pool, empty, Approval{Approved: true}, Options{})
	if err != nil || result.Stopped != StopOwnership || len(result.Plan.Refusals) != 1 {
		t.Fatalf("ownership plus permission apply = %+v, %v; want ownership first", result, err)
	}
	if strings.Contains(
		result.Plan.Refusals[0].Message(),
		"allow-delete",
	) || strings.Contains(result.Plan.Refusals[0].Remediation(), "allow-delete") {
		t.Fatalf("ownership refusal suggested permission flag: %+v", result.Plan.Refusals[0])
	}
}
