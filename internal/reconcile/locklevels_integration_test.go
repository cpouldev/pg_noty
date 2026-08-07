//go:build integration

package reconcile

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dropTriggerWords = "DROP " + "TRIGGER"

func TestApplyReadsBackTheCreateAndDropTableLockLevels(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_lock_levels_target")
	created := applyCreate(target, "levels")
	assertApplyLocks(t, pool, target, created, "ShareRowExclusiveLock", "AccessExclusiveLock")
	drop := applyObject{Target: target, ServiceSchema: "public", TriggerName: "apply_levels_trigger", FunctionName: "apply_levels_fn", Drop: true}
	assertApplyLocks(t, pool, target, drop, "AccessExclusiveLock", "ShareRowExclusiveLock")
}

func TestCreateOrReplaceFunctionTakesNoTargetTableLock(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_function_lock_target")
	object := applyObject{Target: target, Creates: []string{"CREATE OR REPLACE FUNCTION public.apply_function_only() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$"}}
	_, err := applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		if err := tx.apply(t.Context(), []applyObject{object}); err != nil {
			return err
		}
		catalog := NewCatalog(tx.tx)
		locks, err := catalog.ReadLocks(t.Context(), target.OID)
		if err != nil {
			return err
		}
		if len(locks) != 0 {
			return fmt.Errorf("function-only locks = %#v, want none on target", locks)
		}
		// The empty reading is evidence only if this reader can report a lock on this OID from this
		// transaction.
		mustExecOn(t, tx.tx, "LOCK TABLE public.apply_function_lock_target IN SHARE ROW EXCLUSIVE MODE")
		held, err := catalog.ReadLocks(t.Context(), target.OID)
		if err != nil {
			return err
		}
		if !hasCatalogLock(held, "ShareRowExclusiveLock") {
			return fmt.Errorf("locks after an explicit LOCK TABLE = %#v, want ShareRowExclusiveLock; "+
				"the empty reading above measures nothing", held)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWhenBearingCreateTriggerIsAloneAtTheServer(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_when_target")
	mustExecOn(t, pool, "CREATE FUNCTION public.apply_when_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$")
	statement := "CREATE TRIGGER apply_when_trigger AFTER INSERT ON public.apply_when_target FOR EACH ROW WHEN (NEW.id > 0) EXECUTE FUNCTION public.apply_when_fn()"
	if ran, err := serverStatementCount(t, pool, statement); err != nil || ran != 1 {
		t.Fatalf("when create reached server %d times, %v; want one", ran, err)
	}
	mustExecOn(t, pool, dropTriggerWords+" apply_when_trigger ON public.apply_when_target")
	if ran, err := serverStatementCount(t, pool, statement+"; SELECT 1"); err != nil || ran != 2 {
		t.Fatalf("batched control reached server %d times, %v; want two", ran, err)
	}
	mustExecOn(t, pool, dropTriggerWords+" apply_when_trigger ON public.apply_when_target")
	var recorded []string
	_, err := applyInTransaction(t.Context(), pool, pool, func(text string) { recorded = append(recorded, text) }, func(tx *applyTx) error {
		return tx.apply(t.Context(), []applyObject{{Target: target, Creates: []string{statement}}})
	})
	if err != nil || len(recorded) != 3 || recorded[2] != statement {
		t.Fatalf("apply record = %#v, %v; want one isolated when statement", recorded, err)
	}
}

func assertApplyLocks(t *testing.T, pool *pgxpool.Pool, target TargetReading, object applyObject, want, notWant string) {
	t.Helper()
	_, err := applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		if err := tx.apply(t.Context(), []applyObject{object}); err != nil {
			return err
		}
		locks, err := NewCatalog(tx.tx).ReadLocks(t.Context(), target.OID)
		if err != nil {
			return err
		}
		if !hasCatalogLock(locks, want) || hasCatalogLock(locks, notWant) {
			return fmt.Errorf("locks = %#v, want %s and no %s", locks, want, notWant)
		}
		operation := "create"
		if object.Drop {
			operation = "drop"
		}
		t.Logf("observed %s target lock modes: %#v", operation, locks)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
