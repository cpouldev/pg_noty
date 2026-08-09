//go:build integration

package reconcile

import (
	"testing"
)

// The second divergence a re-diff answers. The first -- a proposed create already present -- is
// TestApplyReDiffsInsteadOfTrustingAnEarlierPlan in apply_integration_test.go. This is the mirror:
// the object a plan proposes to drop is removed by another session before the apply runs. The two
// are not the same case wearing different labels. A stale create is idempotent by luck, because a
// re-diff that still proposed it would meet an object that is already there; a stale drop is not,
// because ddltext.go writes DROP TRIGGER with no IF EXISTS hedge and
// TestDropTextsCarryNoIfExistsHedge pins that it never will. The re-diff is the only
// thing standing between a stale drop and an error on an object that is already gone.
func TestApplyDropsNothingWhenThePlannedObjectIsAlreadyGone(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg := oneListenerConfig(t, pool, "rediff_drop_target")
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatalf("seeding apply: %v", err)
	}
	row, err := readRegistry(t.Context(), pool, harnessSchema, cfg.Listeners[0].Name)
	if err != nil || len(row.Triggers) != 1 {
		t.Fatalf("registry after seeding = %+v, %v; want the one recorded trigger a drop would target", row, err)
	}
	target := mustQualifiedTarget(t, "rediff_drop_target")

	// The listener leaves the configuration, so a drop is planned...
	without := cfg
	without.Listeners = nil
	planned, err := Plan(t.Context(), pool, without, Options{})
	if err != nil || len(planned.Actions) != 1 || planned.Actions[0].Kind != ActionDrop {
		t.Fatalf("plan without the listener = %+v, %v; want the one drop this case makes stale", planned, err)
	}

	// ...and another session drops both objects out from under it before the apply runs.
	mustExecOn(t, pool, "DROP TRIGGER "+quotedOwnershipIdentifier(t, row.Triggers[0].TriggerName)+" ON "+target)
	mustExecOn(t, pool, "DROP FUNCTION \""+harnessSchema+"\"."+quotedOwnershipIdentifier(t, row.Triggers[0].FunctionName)+"()")

	applied, err := Apply(t.Context(), pool, without, Approval{Approved: true, DestructionPermitted: true}, Options{})
	if err != nil {
		t.Fatalf("apply against an already-gone drop: %v", err)
	}
	if applied.Verdict != VerdictClean {
		t.Fatalf("apply = %+v; want a clean run rather than a refusal on an object that had already gone", applied)
	}
	if left := countOn(t, pool, "SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid WHERE c.relname = $1 AND NOT t.tgisinternal", "rediff_drop_target"); left != 0 {
		t.Fatalf("triggers left on the target = %d, want none", left)
	}
	if _, err := readRegistry(t.Context(), pool, harnessSchema, cfg.Listeners[0].Name); err == nil {
		t.Fatal("the registry still records the removed listener, so the applied result follows the stale plan rather than the database")
	}
}
