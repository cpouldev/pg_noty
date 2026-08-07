//go:build integration

package reconcile

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// An aborted apply persists nothing. "Nothing persisted" is satisfied by a run that did nothing, so
// the population is asserted first: one listener's objects and registry rows are observed to exist
// *inside* the apply transaction, and that observation is part of the same assertion as the
// post-rollback absence.
var errInjectedOnTheLastListener = errors.New("injected failure on the last listener")

// rollbackObservation counts one listener's four artefacts. A partial apply persists whichever of
// them it reached, so all four are counted rather than the two catalog objects alone.
type rollbackObservation struct{ functions, triggers, listenerRows, triggerRows int }

func observeListener(t *testing.T, on reconcileCounter, name string) rollbackObservation {
	t.Helper()
	listeners, listenersErr := qualifiedRegistryTable(harnessSchema, schema.TableListeners)
	triggers, triggersErr := qualifiedRegistryTable(harnessSchema, schema.TableListenerTriggers)
	if listenersErr != nil || triggersErr != nil {
		t.Fatalf("registry table names: %v, %v", listenersErr, triggersErr)
	}
	return rollbackObservation{
		functions:    countOn(t, on, "SELECT count(*) FROM pg_proc WHERE proname = $1", "apply_"+name+"_fn"),
		triggers:     countOn(t, on, "SELECT count(*) FROM pg_trigger WHERE tgname = $1", "apply_"+name+"_trigger"),
		listenerRows: countOn(t, on, "SELECT count(*) FROM "+listeners+" WHERE name = $1", name),
		triggerRows:  countOn(t, on, "SELECT count(*) FROM "+triggers+" WHERE listener = $1", name),
	}
}

// recordListener is the registry half of one applied listener, written with the same authority the
// run itself uses.
func recordListener(ctx context.Context, tx pgx.Tx, name string, target TargetReading) error {
	qualified, fault := schema.Qualified(target.Schema, target.Table)
	if fault != schema.IdentifierOK {
		return fmt.Errorf("target %q is unusable: %s", target.Table, fault)
	}
	_, err := writeRegistry(
		ctx, tx, harnessSchema, registryListener{
			Name: name,
			Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: qualified,
			TargetOID: target.OID, Enabled: true,
		},
		[]registryTrigger{
			{
				Operation: "insert", TriggerName: "apply_" + name + "_trigger",
				FunctionName: "apply_" + name + "_fn", DDLHash: "hash",
			},
		},
	)
	return err
}

// runRollbackShape is the earlier listener that succeeds. It applies that listener's DDL and registry
// rows inside the one apply transaction, observes them there, and then fails the run the way the
// caller asks -- which is the later listener whose failure must undo all of it.
func runRollbackShape(
	t *testing.T, pool *pgxpool.Pool, name string,
	failLastListener func(context.Context, *applyTx) error,
) (before, after rollbackObservation) {
	t.Helper()
	target := applyTarget(t, pool, name)
	_, err := applyInTransaction(
		t.Context(), pool, pool, nil, func(run *applyTx) error {
			if err := run.apply(t.Context(), []applyObject{applyCreate(target, name)}); err != nil {
				return err
			}
			if err := recordListener(t.Context(), run.tx, name, target); err != nil {
				return err
			}
			before = observeListener(t, run.tx, name)
			return failLastListener(t.Context(), run)
		},
	)
	if err == nil {
		t.Fatal("the apply transaction succeeded; this fixture must fail on its last listener")
	}
	return before, observeListener(t, pool, name)
}

// rejectedByTheServer is a last listener whose own CREATE TRIGGER names a table that is not there.
func rejectedByTheServer(ctx context.Context, run *applyTx) error {
	return run.apply(
		ctx, []applyObject{
			{
				Target: TargetReading{Schema: "public", Table: "rollback_absent"},
				Creates: []string{
					"CREATE TRIGGER rollback_absent_trigger AFTER INSERT ON public.rollback_absent " +
						"FOR EACH ROW EXECUTE FUNCTION public.rollback_absent_fn()",
				},
			},
		},
	)
}

func TestAFailedApplyLeavesNothingFromTheListenersThatHadAlreadySucceeded(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	for _, tc := range []struct {
		name, listener string
		fail           func(context.Context, *applyTx) error
	}{
		// A client-side failure leaves the block committable, which is the class syncRegistry's own
		// faults fall into: an unusable catalog target, a listener that no longer compiles. It is
		// also the only class that can falsify a run committing on its error path, for the reason
		// TestACommitOfAnAbortedBlockIsARollback measures.
		{
			name: "a client-side failure, with the block still committable", listener: "rollback_committable",
			fail: func(context.Context, *applyTx) error { return errInjectedOnTheLastListener },
		},
		// The literal shape of the rule: the last listener's own DDL fails after the first
		// listener's DDL succeeded.
		{name: "a DDL statement the server rejected", listener: "rollback_rejected", fail: rejectedByTheServer},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				before, after := runRollbackShape(t, pool, tc.listener, tc.fail)
				if before != (rollbackObservation{1, 1, 1, 1}) || after != (rollbackObservation{}) {
					t.Fatalf(
						"the earlier listener was %+v inside the transaction and %+v after it; want its "+
							"function, trigger and two registry rows all present before the failure and all gone "+
							"after the rollback", before, after,
					)
				}
			},
		)
	}
}

// TestACommitOfAnAbortedBlockIsARollback pins the server behaviour that makes the two cases above two
// classes rather than two spellings of one. Once the server has rejected a statement, PostgreSQL
// executes COMMIT as ROLLBACK and pgx reports that as ErrTxCommitRollback -- so a run that committed
// on its error path would still lose the rejected-DDL case's work, and the committable case is the
// only one whose green is evidence of a rollback.
func TestACommitOfAnAbortedBlockIsARollback(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.rollback_abort_probe (id int)")
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	mustExecOn(t, tx, "INSERT INTO public.rollback_abort_probe VALUES (1)")
	if _, err := tx.Exec(t.Context(), "SELECT 1 / 0"); err == nil {
		t.Fatal("the server accepted a division by zero, so this block was never aborted")
	}
	if err := tx.Commit(t.Context()); !errors.Is(err, pgx.ErrTxCommitRollback) {
		t.Fatalf("commit of an aborted block = %v, want pgx.ErrTxCommitRollback", err)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM public.rollback_abort_probe"); got != 0 {
		t.Fatalf("the aborted block's insert survived its own commit as %d rows, want 0", got)
	}
}
