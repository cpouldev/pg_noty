//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// TestRetargetingAListenerIsRefusedRatherThanAppliedToTheOldTable covers the one identity signal
// resolution never read. With a registry row present, resolveListenerTarget took the target from the
// row for every lookup, so the configured table was unused: the plan's replaces were generated
// against the *old* table and applied there, syncConfiguredListener then rewrote the row's spec hash
// from the new configuration while leaving its target alone, and every later plan reported clean.
// The new table never fired and the old one kept firing, with nothing left to notice.
func TestRetargetingAListenerIsRefusedRatherThanAppliedToTheOldTable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	original := mustQualifiedTarget(t, "retarget_original")
	moved := mustQualifiedTarget(t, "retarget_moved")
	mustExecOn(t, pool, "CREATE TABLE "+original+" (id bigint PRIMARY KEY)")
	mustExecOn(t, pool, "CREATE TABLE "+moved+" (id bigint PRIMARY KEY)")

	listener := listenerForTarget("public.retarget_original")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if applied, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil ||
		applied.Verdict != VerdictClean {
		t.Fatalf("first apply = %+v, %v; want clean", applied, err)
	}

	// The retarget: same listener name, a different table.
	moved2 := listenerForTarget("public.retarget_moved")
	moved2.Enabled = true
	moved2.Name = listener.Name
	cfg.Listeners = []config.Listener{moved2}

	planned, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Refusals) != 1 {
		t.Fatalf(
			"plan produced %d refusals %+v and %d actions; a retarget must stop the run rather "+
				"than propose changes against the recorded table",
			len(planned.Refusals), planned.Refusals, len(planned.Actions),
		)
	}
	message := planned.Refusals[0].Message()
	if !strings.Contains(message, "retarget_original") || !strings.Contains(message, "retarget_moved") {
		t.Errorf(
			"refusal %q does not name both the recorded and the configured table, so an "+
				"operator cannot tell which way the move went", message,
		)
	}
	if planned.Verdict == VerdictClean {
		t.Error("plan reported clean for a listener whose configured table is not its recorded one")
	}
}
