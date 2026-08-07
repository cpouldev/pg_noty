//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// TestAPartitionedTableIsAUsableTarget covers the relkind the resolver used to exclude. PostgreSQL
// has carried row triggers on a partitioned parent since 11, but the query admitted only relkind
// 'r', so to_regclass resolved the parent and the filter then discarded it -- and the operator was
// told the table "does not exist" for a table plainly there.
//
// The partition is created and written to as well, because a trigger on the parent that does not
// reach a partition's rows would satisfy a resolve-only assertion while delivering nothing.
func TestAPartitionedTableIsAUsableTarget(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "partitioned_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint, at timestamptz NOT NULL) PARTITION BY RANGE (at)")
	partition := mustQualifiedTarget(t, "partitioned_target_p1")
	mustExecOn(
		t, pool, "CREATE TABLE "+partition+" PARTITION OF "+target+
			" FOR VALUES FROM ('2000-01-01') TO ('2100-01-01')",
	)
	listener := listenerForTarget("public.partitioned_target")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}

	planned, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil {
		t.Fatalf("Plan on a partitioned target failed: %v", err)
	}
	if len(planned.Refusals) != 0 {
		t.Fatalf("Plan refused a partitioned target: %+v", planned.Refusals)
	}
	if len(planned.Actions) == 0 {
		t.Fatal("Plan proposed nothing for a partitioned target")
	}
	applied, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || applied.Verdict != VerdictClean {
		t.Fatalf("apply = %+v, %v; want a clean result", applied, err)
	}
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || clean.Verdict != VerdictClean || len(clean.Actions) != 0 {
		t.Fatalf("second plan = %+v, %v; want clean", clean, err)
	}
}
