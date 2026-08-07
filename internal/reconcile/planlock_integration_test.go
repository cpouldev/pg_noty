//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// TestPlanCompletesWhileAnotherSessionHoldsTheRunLock is the behavioural half of "plan takes no lock
// at all", the No-command-hangs clause that nothing asserted. Its structural half is
// TestPlanReachesNoRunLockAcquisition; neither replaces the other, because a call graph cannot see a
// lock taken through a name it does not recognise and a passing plan cannot see a lock never
// contended for. Both are needed, and this one is the side an operator would feel.
func TestPlanCompletesWhileAnotherSessionHoldsTheRunLock(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg := oneListenerConfig(t, pool, "plan_no_lock_target")
	key := schema.ReconcileLockKey(harnessSchema, harnessSchema)
	holder := takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	defer holder.Release()

	observer := takeRunLockConnection(t, pool)
	defer observer.Release()
	if held, err := runLockHolder(t.Context(), observer, key); err != nil || held == "could not be identified" {
		t.Fatalf(
			"run-lock holder = %q, %v; this case has to plan against a key that is genuinely "+
				"held, or its success measures nothing", held, err,
		)
	}

	// The bound is supplied as an input and set far below any plausible plan runtime. A Plan that
	// waited on the run lock at all could then only answer by expiring, so the create below is the
	// measurement: it is the plan this configuration yields when the key is free.
	planned, err := Plan(t.Context(), pool, cfg, Options{RunLockWait: belowTheHoldersRuntime})
	if err != nil || len(planned.Actions) != 1 || planned.Actions[0].Kind != ActionCreate {
		t.Fatalf("plan while the run lock is held = %+v, %v; want the create it plans with the key free", planned, err)
	}
	if len(planned.Refusals) != 0 {
		t.Fatalf("plan reported %+v; a plan that took the run lock would refuse here instead", planned.Refusals)
	}

	// Apply, on the same key and the same bound, is refused. Without this the plan's success is also
	// what a run against an already-free key looks like.
	refused, err := Apply(
		t.Context(),
		pool,
		cfg,
		Approval{Approved: true},
		Options{RunLockWait: belowTheHoldersRuntime},
	)
	if err != nil || refused.Stopped != StopRunLockWaitExpired {
		t.Fatalf(
			"apply against the same held key = %+v, %v; want the wait-expired refusal that shows "+
				"the key does block a run", refused, err,
		)
	}
	unlockRunLockKey(t, holder, key)
}
