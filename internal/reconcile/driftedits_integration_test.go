//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: test-both-sides-of-an-exclusion-guard; assert-a-position-per-element-not-as-a-set.
func TestMissingAndEditedCatalogPairsRequireDifferentActions(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_edits", "insert", "update")
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := dropTriggerText(resolvedCatalogTarget(t, pool, target), reading.Triggers[0].TriggerName)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, drop)
	missing, err := Plan(t.Context(), pool, cfg, Options{})
	assertOneMatrixAction(t, missing, err, listener.Name, reading.Triggers[0].Operation, ActionCreate)
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	editFunctionForOperation(t, pool, cfg, listener, target, "update")
	edited, err := Plan(t.Context(), pool, cfg, Options{})
	assertOneMatrixAction(t, edited, err, listener.Name, "update", ActionReplace)
}

func TestOneFunctionBodyEditReplacesOnlyItsPair(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_pair", "insert", "update", "delete")
	editFunctionForOperation(t, pool, cfg, listener, target, "delete")
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	assertOneMatrixAction(t, plan, err, listener.Name, "delete", ActionReplace)
}

func TestThreeTriggerDefinitionEditsReplaceOnlyTheirOwnPair(t *testing.T) {
	skipIfShort(t)
	for _, edit := range []struct {
		name, operation string
		change          func(string) string
	}{
		{"when", "update", func(sql string) string { return strings.Replace(sql, "NEW.id > 0", "NEW.id != 2", 1) }},
		{
			"update-of", "update", func(sql string) string {
				return strings.Replace(sql, `UPDATE OF "state", "id"`, `UPDATE OF "state"`, 1)
			},
		},
		{
			"timing", "update",
			func(sql string) string { return strings.Replace(sql, "AFTER UPDATE", "BEFORE UPDATE", 1) },
		},
	} {
		t.Run(
			edit.name, func(t *testing.T) {
				skipIfShort(t)
				pool := freshDatabase(t)
				cfg, listener, target := appliedTargetFixture(t, pool, "drift_"+edit.name, "insert", "update", "delete")
				mustExecOn(t, pool, "ALTER TABLE "+target+" ADD COLUMN state text")
				listener.Trigger.Operations[1].Columns = []string{"state", "id"}
				listener.Trigger.Operations[1].When = "NEW.id > 0"
				cfg.Listeners[0] = listener
				if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
					t.Fatal(err)
				}
				replaceTriggerWith(t, pool, cfg, listener, target, edit.operation, edit.change)
				plan, err := Plan(t.Context(), pool, cfg, Options{})
				assertOneMatrixAction(t, plan, err, listener.Name, edit.operation, ActionReplace)
			},
		)
	}
}

func TestWrongRecordedHashAndThreeIndependentPairVerdicts(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_verdicts", "insert", "update", "delete")
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := dropTriggerText(resolvedCatalogTarget(t, pool, target), reading.Triggers[0].TriggerName)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, drop)
	editFunctionForOperation(t, pool, cfg, listener, target, "update")
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(plan.Actions) != 2 || plan.Actions[0].Kind != ActionCreate || plan.Actions[1].Kind != ActionReplace || plan.Actions[0].Pair.Operation != "delete" || plan.Actions[1].Pair.Operation != "update" {
		t.Fatalf("three pair verdicts=%+v err=%v; want missing create, edited replace, clean delete", plan, err)
	}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	mustExecOn(
		t,
		pool,
		"UPDATE "+harnessSchema+".listener_triggers SET ddl_hash = '1:deliberately-wrong' WHERE listener = '"+listener.Name+"' AND operation = 'delete'",
	)
	wrong, err := Plan(t.Context(), pool, cfg, Options{})
	assertOneMatrixAction(t, wrong, err, listener.Name, "delete", ActionReplace)
}

func TestFingerprintCoverageBoundaryNamesACLBothCommentsAndTargetTable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_boundary_acl", "insert")
	compiled, err := compileListener(
		cfg.Instance,
		cfg.Database.Schema,
		listener,
		resolvedCatalogTarget(t, pool, target),
	)
	if err != nil {
		t.Fatal(err)
	}
	function := "\"" + harnessSchema + "\".\"" + compiled.Sets[0].FunctionName + "\"()"
	mustExecOn(t, pool, "GRANT EXECUTE ON FUNCTION "+function+" TO PUBLIC")
	mustExecOn(t, pool, "ALTER TABLE "+target+" ADD COLUMN untouched text")
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || plan.Verdict != VerdictClean {
		t.Fatalf("ACL and target-table changes are outside the fingerprint: plan=%+v err=%v", plan, err)
	}
	// A comment is ownership provenance, not a definition: changing it must refuse rather than claim
	// drift coverage. Each of the two markers is its own case, because they are read by different
	// catalog queries and a boundary asserted on one says nothing about the other.
	mustExecOn(t, pool, "COMMENT ON TRIGGER \""+compiled.Sets[0].TriggerName+"\" ON "+target+" IS 'foreign'")
	assertACommentIsRefusedRatherThanCalledDrift(t, pool, cfg, "trigger")
	mustExecOn(t, pool, compiled.Sets[0].CommentTrigger)
	mustExecOn(t, pool, "COMMENT ON FUNCTION "+function+" IS 'foreign'")
	assertACommentIsRefusedRatherThanCalledDrift(t, pool, cfg, "function")
}

func assertACommentIsRefusedRatherThanCalledDrift(t *testing.T, pool *pgxpool.Pool, cfg config.Config, marker string) {
	t.Helper()
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || plan.Verdict != VerdictError || len(plan.Actions) != 0 {
		t.Fatalf(
			"%s comment boundary plan=%+v err=%v; want ownership refusal, not a fingerprint replacement",
			marker, plan, err,
		)
	}
}

func replaceTriggerWith(
	t *testing.T,
	pool *pgxpool.Pool,
	cfg config.Config,
	listener config.Listener,
	target, operation string,
	change func(string) string,
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
	for _, set := range compiled.Sets {
		if set.Operation != operation {
			continue
		}
		drop, err := dropTriggerText(resolvedCatalogTarget(t, pool, target), set.TriggerName)
		if err != nil {
			t.Fatal(err)
		}
		changed := change(set.CreateTrigger)
		if changed == set.CreateTrigger {
			t.Fatalf("%s edit did not alter trigger", operation)
		}
		mustExecOn(t, pool, drop)
		mustExecOn(t, pool, changed)
		mustExecOn(t, pool, set.CommentTrigger)
		return
	}
	t.Fatalf("operation %q missing", operation)
}

func editFunctionForOperation(
	t *testing.T,
	pool *pgxpool.Pool,
	cfg config.Config,
	listener config.Listener,
	target, operation string,
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
	for _, set := range compiled.Sets {
		if set.Operation == operation {
			edited := strings.Replace(set.CreateFunction, "RETURN NULL;", "RETURN NEW;", 1)
			if edited == set.CreateFunction {
				t.Fatal("function fixture lacks editable body")
			}
			mustExecOn(t, pool, edited)
			return
		}
	}
	t.Fatalf("operation %q missing from generated set", operation)
}
