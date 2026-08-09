//go:build integration

package reconcile

import (
	"strings"
	"testing"
)

func TestTargetTablesAreTakenInAscendingQualifiedNameOrder(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	first := applyTarget(t, pool, "apply_order_a")
	last := applyTarget(t, pool, "apply_order_z")
	var recorded []string
	_, err := applyInTransaction(t.Context(), pool, pool, func(statement string) { recorded = append(recorded, statement) }, func(tx *applyTx) error {
		// The reverse input is deliberate: preserving configuration order would fail at index zero.
		return tx.apply(t.Context(), []applyObject{applyCreate(last, "order_z"), applyCreate(first, "order_a")})
	})
	if err != nil {
		t.Fatal(err)
	}
	got := triggerTargetSequence(recorded, first.Table, last.Table)
	want := []string{first.Table, last.Table}
	if len(got) != len(want) {
		t.Fatalf("target sequence = %#v, want %#v", got, want)
	}
	for index, table := range want {
		if got[index] != table {
			t.Fatalf("target sequence at %d = %q, want %q; got %#v", index, got[index], table, got)
		}
	}
}

func TestDropsPrecedeEveryCreateForOneTarget(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_drop_first_target")
	seed := applyCreate(target, "drop_first")
	if _, err := applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		return tx.apply(t.Context(), []applyObject{seed})
	}); err != nil {
		t.Fatal(err)
	}
	replace := seed
	replace.ServiceSchema, replace.TriggerName, replace.FunctionName, replace.Drop = "public", "apply_drop_first_trigger", "apply_drop_first_fn", true
	var recorded []string
	if _, err := applyInTransaction(t.Context(), pool, pool, func(statement string) { recorded = append(recorded, statement) }, func(tx *applyTx) error {
		return tx.apply(t.Context(), []applyObject{replace})
	}); err != nil {
		t.Fatal(err)
	}
	drops, creates := statementPositions(recorded)
	if len(drops) != 2 || len(creates) != 2 {
		t.Fatalf("recorded = %#v; want two drops and two creates", recorded)
	}
	// Creating first upgrades SHARE ROW EXCLUSIVE to ACCESS EXCLUSIVE; two sessions doing that can deadlock even with identical table order.
	for _, drop := range drops {
		for _, create := range creates {
			if drop > create {
				t.Fatalf("drop at %d followed create at %d: %#v", drop, create, recorded)
			}
		}
	}
}

func triggerTargetSequence(statements []string, names ...string) []string {
	var targets []string
	for _, statement := range statements {
		if !strings.HasPrefix(statement, "CREATE TRIGGER ") {
			continue
		}
		for _, name := range names {
			if strings.Contains(statement, " ON public."+name+" ") {
				targets = append(targets, name)
			}
		}
	}
	return targets
}

func statementPositions(statements []string) (drops, creates []int) {
	for index, statement := range statements {
		if strings.HasPrefix(statement, "DROP ") {
			drops = append(drops, index)
		}
		if strings.HasPrefix(statement, "CREATE ") {
			creates = append(creates, index)
		}
	}
	return drops, creates
}
