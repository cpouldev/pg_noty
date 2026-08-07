//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// expireTheLease puts one claimed row's lease in the past, which is what a worker killed mid-delivery
// leaves behind. Done with a statement rather than by waiting so the case is deterministic.
func expireTheLease(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	mustExecOn(
		t, pool, "UPDATE "+mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)+
			" SET leased_until = now() - interval '1 second' WHERE event_id=$1", id,
	)
}

// reclaimExpiredLeases keeps the helper used by the source integration corpus while routing the
// sweep through the production sibling capability. The helper has no SQL of its own: the adapter
// owns the qualified identifier and PostgreSQL-clock predicate, while callers own when to sweep.
func reclaimExpiredLeases(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	source := claimableSource(t, pool, "reclaimer")
	tag, err := source.ReclaimExpired(t.Context())
	if err != nil {
		t.Fatalf("run the lease-reclaim sweep: %v", err)
	}
	return tag
}

// claimableSource opens one source against the harness with a live lease.
func claimableSource(t *testing.T, pool *pgxpool.Pool, worker string) *TriggerSource {
	t.Helper()
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: worker, Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source
}

func mustClaim(t *testing.T, source *TriggerSource, n int) []Event {
	t.Helper()
	claimed, err := source.Claim(t.Context(), n)
	if err != nil {
		t.Fatalf("claim %d: %v", n, err)
	}
	return claimed
}

// TestAnExpiredLeaseIsRecoverableOnlyAfterTheReclaimSweep is the recoverability clause, and it is
// written the way the code actually behaves rather than the way the clause reads at a glance. The
// event *is* recoverable -- the last two steps re-claim it -- but not by this package alone: the
// stranded-claim check below is the finding, and it is asserted rather than described, so a later
// change that made the claim query offer expired leases would fail here by name.
func TestAnExpiredLeaseIsRecoverableOnlyAfterTheReclaimSweep(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	seed := seedQueueEvent(t, pool, "listener")
	worker := claimableSource(t, pool, "worker-a")

	claimed := mustClaim(t, worker, 1)
	if len(claimed) != 1 || claimed[0].Attempt != 1 {
		t.Fatalf(
			"first claim returned %d events, first attempt %d; want one at attempt 1",
			len(claimed), attemptOf(claimed),
		)
	}
	if held := mustClaim(t, worker, 1); len(held) != 0 {
		t.Fatalf("a live lease was re-offered: %d events", len(held))
	}
	if swept, err := worker.ReclaimExpired(t.Context()); err != nil {
		t.Fatalf("reclaim with a live lease: %v", err)
	} else if swept != 0 {
		t.Fatalf("reclaim reset %d live leases, want none", swept)
	}

	expireTheLease(t, pool, seed.ID)
	if stranded := mustClaim(t, worker, 1); len(stranded) != 0 {
		t.Fatalf(
			"Claim offered %d events whose lease had expired; the committed claim query "+
				"selects status='pending' only, so this package cannot be what recovered it -- "+
				"re-derive the reclaim sweep's contract in the delivery worker before changing the query",
			len(stranded),
		)
	}

	if swept := reclaimExpiredLeases(t, pool); swept != 1 {
		t.Fatalf("the reclaim sweep reset %d rows, want the one stranded row", swept)
	}
	if row := readQueueRow(t, pool, seed.ID); !row.present || row.status != "pending" || row.attempts != 1 {
		t.Fatalf("reclaim left queue row %#v, want pending with attempts unchanged at 1", row)
	}
	recovered := mustClaim(t, worker, 1)
	if len(recovered) != 1 || recovered[0].ID != seed.ID {
		t.Fatalf(
			"after the sweep the queue offered %d events, want the stranded %d back",
			len(recovered), seed.ID,
		)
	}
	if recovered[0].Attempt != 2 {
		t.Errorf(
			"the recovered event is at attempt %d, want 2; a second delivery of the same "+
				"event has to be counted as one", recovered[0].Attempt,
		)
	}
}

func attemptOf(claimed []Event) int {
	if len(claimed) == 0 {
		return 0
	}
	return claimed[0].Attempt
}
