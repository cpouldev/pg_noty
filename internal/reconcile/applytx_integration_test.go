//go:build integration

package reconcile

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyTransactionStartsLocallyAndCountsOnlyDatabaseChanges(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := applyTarget(t, pool, "apply_counter_target")
	seedRegistryRead(t, pool, target)
	var statements []string
	count, err := applyInTransaction(t.Context(), pool, pool, func(statement string) {
		statements = append(statements, statement)
	}, func(tx *applyTx) error {
		catalog := NewCatalog(tx.tx)
		if _, found, err := catalog.ResolveTarget(t.Context(), "public.apply_counter_target"); err != nil || !found {
			return fmt.Errorf("read catalog target: found=%t: %w", found, err)
		}
		if _, err := readRegistry(t.Context(), tx.tx, harnessSchema, "counter"); err != nil {
			return fmt.Errorf("read registry: %w", err)
		}
		return tx.apply(t.Context(), nil)
	})
	if err != nil || count != 0 {
		t.Fatalf("read-only apply = %d, %v; want zero changes", count, err)
	}
	if len(statements) != 2 || statements[0] != catalogSearchPathPin || statements[1] != "SET LOCAL lock_timeout = 3000" {
		t.Fatalf("first statements = %#v, want local search path then 3-second local timeout", statements)
	}
	count, err = applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		return tx.apply(t.Context(), []applyObject{applyCreate(target, "counter")})
	})
	if err != nil || count == 0 {
		t.Fatalf("DDL apply = %d, %v; want a non-zero change count", count, err)
	}
}

func TestApplyTransactionIsInvisibleUntilCommit(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_atomic_target")
	object := applyCreate(target, "atomic")
	count, err := applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		if err := tx.apply(t.Context(), []applyObject{object}); err != nil {
			return err
		}
		if got := countOn(t, pool, "SELECT count(*) FROM pg_proc WHERE proname = 'apply_atomic_fn'"); got != 0 {
			t.Fatalf("observer saw %d functions before commit, want none", got)
		}
		if got := countOn(t, pool, "SELECT count(*) FROM pg_trigger WHERE tgname = 'apply_atomic_trigger'"); got != 0 {
			t.Fatalf("observer saw %d triggers before commit, want none", got)
		}
		return nil
	})
	if err != nil || count == 0 {
		t.Fatalf("atomic apply = %d, %v", count, err)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM pg_proc WHERE proname = 'apply_atomic_fn'"); got != 1 {
		t.Fatalf("observer saw %d functions after commit, want all", got)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM pg_trigger WHERE tgname = 'apply_atomic_trigger'"); got != 1 {
		t.Fatalf("observer saw %d triggers after commit, want all", got)
	}
}

func TestApplyTransactionTimesOutWithTableAndBlockingBackend(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	target := applyTarget(t, pool, "apply_timeout_target")
	holder, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	mustExecOn(t, holder, "BEGIN")
	defer mustExecOn(t, holder, "ROLLBACK")
	mustExecOn(t, holder, "LOCK TABLE public.apply_timeout_target IN ACCESS EXCLUSIVE MODE")
	var pid string
	if err := holder.QueryRow(t.Context(), "SELECT pg_backend_pid()::text").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = applyInTransaction(t.Context(), pool, pool, nil, func(tx *applyTx) error {
		return tx.apply(t.Context(), []applyObject{applyCreate(target, "timeout")})
	})
	if elapsed := time.Since(started); elapsed > applyLockTimeout+250*time.Millisecond {
		t.Fatalf("lock timeout took %s, want no more than the pinned 3 seconds", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), target.Table) || !strings.Contains(err.Error(), pid) {
		t.Fatalf("timeout error = %v; want table %q and blocker %q", err, target.Table, pid)
	}
}

func applyTarget(t *testing.T, pool *pgxpool.Pool, table string) TargetReading {
	t.Helper()
	mustExecOn(t, pool, "CREATE TABLE public."+table+" (id bigint PRIMARY KEY)")
	return resolvedCatalogTarget(t, pool, "public."+table)
}

func applyCreate(target TargetReading, suffix string) applyObject {
	name := "apply_" + suffix + "_fn"
	trigger := "apply_" + suffix + "_trigger"
	return applyObject{Target: target, Creates: []string{
		fmt.Sprintf("CREATE OR REPLACE FUNCTION public.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$", name),
		fmt.Sprintf("CREATE TRIGGER %s AFTER INSERT ON public.%s FOR EACH ROW EXECUTE FUNCTION public.%s()", trigger, target.Table, name),
	}}
}

func seedRegistryRead(t *testing.T, pool *pgxpool.Pool, target TargetReading) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = writeRegistry(t.Context(), tx, harnessSchema, registryListener{Name: "counter", Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: `"public"."apply_counter_target"`, TargetOID: target.OID, Enabled: true}, nil)
	if err == nil {
		err = tx.Commit(t.Context())
	}
	if err != nil {
		t.Fatal(err)
	}
}
