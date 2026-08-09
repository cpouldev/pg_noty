//go:build integration

package source

import (
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// theFilteredUpdate is the operation both halves of the column-filter claim read, declared once so
// the catalog half and the counting half cannot disagree about what was configured.
var theFilteredUpdate = config.Operation{Kind: "update", Columns: []string{"status", "total"}}

// TestOnlyTheUpdateTriggerCarriesAColumnFilter is the catalog half. internal/config permits
// `columns` only under `update`, and a filter leaking onto the other two silently narrows what they
// watch -- which the event counts below cannot see, because the statements they write touch every
// column anyway. tgattr is the server's own record of which columns a filter names, so an emitted
// OF list that never attached reads as an empty one here.
func TestOnlyTheUpdateTriggerCarriesAColumnFilter(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, theShapeTargetTable)
	sets := installedShapeSets(
		t, pool, config.Operation{Kind: "insert"}, theFilteredUpdate,
		config.Operation{Kind: "delete"},
	)

	installed := triggerInventoryOf(t, pool, "public", theShapeTarget)
	for _, set := range sets {
		want := []string{}
		if set.Operation == "update" {
			want = theFilteredUpdate.Columns
		}
		if got := installed[set.TriggerName].filtered; !slices.Equal(got, want) {
			t.Errorf("the %s trigger's tgattr names %v, want %v", set.Operation, got, want)
		}
	}
}

// TestUpdateOfFilterIsAppliedByTheServer is the counting half, and it is the half the
// catalog read above cannot make: tgattr says which columns the filter names, not whether the server
// honours it when a row is written. Both statements below update one column of one row, so the only
// thing separating them is whether that column is in the filter.
func TestUpdateOfFilterIsAppliedByTheServer(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, theShapeTargetTable)
	installedShapeSets(t, pool, theFilteredUpdate)
	mustExecOn(t, pool, `INSERT INTO public.shape_target VALUES (1, 'new', 1, 1)`)

	mustExecOn(t, pool, `UPDATE public.shape_target SET other=2 WHERE id=1`)
	if got := rowCountOf(t, pool, "noty.events"); got != 0 {
		t.Fatalf("updating a column outside the filter produced %d events, want none", got)
	}

	// The near-miss: without it, a trigger the server never installed at all also produces no event
	// and reads as a filter that worked.
	mustExecOn(t, pool, `UPDATE public.shape_target SET status='changed' WHERE id=1`)
	if got := rowCountOf(t, pool, "noty.events"); got != 1 {
		t.Fatalf(
			"updating a filtered column produced %d events, want one; the negative above is "+
				"not evidence that the filter is what suppressed it", got,
		)
	}
}
