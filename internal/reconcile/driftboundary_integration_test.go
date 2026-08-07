//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: test-both-sides-of-an-exclusion-guard; name-the-test-a-comment-claims-exists.
func TestPinnedDecisionReadingsDifferFromUnpinnedAndRefusePretty(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_boundary", "insert")
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	oid := resolvedCatalogTarget(t, pool, target).OID
	pinned := readCatalogPair(t, pool, oid, reading.Triggers[0].TriggerName, 0, false)
	unpinned := readOutsideCatalog(t, pool, oid, reading.Triggers[0].TriggerName)
	if pinned == unpinned {
		t.Fatalf("pinned=%#v unpinned=%#v; want the negative search_path control to differ", pinned, unpinned)
	}
	if plan, err := Plan(t.Context(), pool, cfg, Options{}); err != nil || plan.Verdict != VerdictClean {
		t.Fatalf("decision consumes pinned readings: plan=%+v err=%v", plan, err)
	}
	assertPrettyFormIsNotDecisionReadback(t, pool, oid, reading.Triggers[0].TriggerName, pinned[0])
}

func assertPrettyFormIsNotDecisionReadback(t *testing.T, pool *pgxpool.Pool, oid uint32, trigger, pinned string) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	plain, pretty := outsideTriggerForms(t, tx, oid, trigger)
	if pretty == pinned || !strings.Contains(plain, "ON public.") || strings.Contains(pretty, "ON public.") {
		t.Fatalf("plain=%q pretty=%q pinned=%q; pretty is refused as a decision input", plain, pretty, pinned)
	}
}
