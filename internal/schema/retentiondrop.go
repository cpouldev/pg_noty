package schema

import "context"

// This file is the drop's one transaction and the questions asked inside it, split from retention.go
// so that the transaction reads as the five steps it is. It is where the DETACH is issued, so it is
// where both of ADR-7's mandatory comments live -- a reader about to reach for the concurrent form
// arrives here and not at the entry point.
//
// It names no driver, and that is load-bearing rather than tidy. Every handle is catalog.go's
// catalogWriter, which a pgx.Tx satisfies, so this transaction body cannot commit or roll anything
// back: ddl.go owns that, and this file owns only what happens between.
//
// Nothing here finishes an error the *server* raised, and that order is the whole of ddlrefusal.go's
// warning. finished severs the chain to *pgconn.PgError deliberately (ADR-11), so an error finished
// here would reach boundedTx carrying no SQLSTATE at all -- a lock timeout would stop answering
// ErrLockTimeout and the foreign-key refusal would stop moving the counter ADR-9 gives it. Every
// statement below therefore returns its error exactly as it arrived, and boundedTx classifies it
// first and finishes it second. This package's own refusals have already been through the finishing
// point where they were built, and carry no driver error to sever.

// dropWithinOneTransaction is ADR-7: the lock, both guards, the reap, the DETACH and the DROP all
// commit together or none of them does. It answers with the dead queue rows the reap removed.
//
// Splitting the DETACH and the DROP across two transactions is the failure this shape exists to
// prevent. A crash between them leaves a table that is still marked as ours and is no longer a
// partition of anything -- which guard 2 refuses on every later pass, so it is never reclaimed and
// holds its expired rows indefinitely.
//
// The obvious improvement a later reader will reach for is the concurrent form of DETACH PARTITION,
// which takes no lock on the parent at all. It is unusable here at every version, and the server's
// own words for why are these:
//
//	cannot detach partitions concurrently when a default partition exists
//
// This design mandates a permanent DEFAULT partition as its write-availability net (criteria 29 and
// 36), so that restriction is unconditional rather than a workload or a version matter -- and what
// it rules out is not a slower path but a hard runtime error the first time it is tried. It also
// cannot run inside a transaction block, which is the same thing ADR-7 requires (M3, Pattern 5).
//
// ADR-7's cost, recorded so it is not mistaken for an oversight: the official partition-maintenance
// advice is to detach first and inspect or archive the standalone table before dropping it. That
// window is given up here deliberately. Keeping it would mean committing the detach on its own,
// which is exactly the crash-between-two-transactions orphan above -- and an orphan holding expired
// data forever is worse than losing the chance to inspect a partition whose whole extent has already
// aged out of the retention window.
func (run pass) dropWithinOneTransaction(ctx context.Context, tx catalogWriter, names dropTargets,
	ranged Range) (int64, error) {

	if _, err := tx.Exec(ctx, lockTheEventLog(names.parent)); err != nil {
		return 0, err
	}
	if err := run.confirmOwnMarker(ctx, tx, names.partition, ranged); err != nil {
		return 0, err
	}
	if err := run.confirmAttachedToTheEventLog(ctx, tx, ranged); err != nil {
		return 0, err
	}

	reaped, err := tx.Exec(ctx, reapDeadRows(names.queue), ranged.From, ranged.To)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, detachPartition(names.parent, names.partition.target)); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, dropPartition(names.partition.target))
	return reaped.RowsAffected(), err
}

// confirmOwnMarker is guard 1. It runs on the transaction that performs the drop rather than on the
// pool: the marker a plan was computed against is not the marker the drop acts under, and only a
// read inside this transaction -- behind the lock its first statement took -- is a claim about the
// object that is about to be removed. A read performed at plan time passes every marker case and
// misses exactly the one that matters, which is why the marker is removed between the plan and the
// apply in TestAMarkerRemovedBetweenThePlanAndTheApplyRefusesTheDrop.
func (run pass) confirmOwnMarker(ctx context.Context, tx catalogReader, marked markedObject,
	ranged Range) error {

	reading, err := markerOn(ctx, tx, marked)
	if err != nil {
		return err
	}
	return refusalForMarker(reading, ranged.Name, run.cfg.Instance)
}

// confirmAttachedToTheEventLog is guard 2, asked of pg_inherits through catalog.go and never of the
// shape of a name. A detached table still carrying our marker has no pg_inherits row, so an orphan
// left by a crash between a detach and a drop answers false here -- and is refused rather than
// removed, on this pass and on every later one.
func (run pass) confirmAttachedToTheEventLog(ctx context.Context, tx catalogReader,
	ranged Range) error {

	attached, err := isPartitionOf(ctx, tx, run.cfg.Database.Schema, ranged.Name, TableEvents)
	if err != nil {
		return err
	}
	if !attached {
		return unattachedTable(ranged.Name)
	}
	return nil
}

// noLongerCovered reports whether the extent this drop was planned for has stopped being covered by
// any partition, which is how a lost drop race is reconciled -- by the *observed extent*, through
// plan.go's own membership rule, never by the name the statement wrote (M6).
//
// It is the second half of the concession and never the whole of it: readsAsAlreadyGone is what
// restricts the reconciliation to the one refusal a dropped relation produces, so an existing
// unmarked partition -- which reads identically -- is still refused rather than waved through.
//
// An observation that itself failed answers false, so the drop's own refusal is what the caller is
// told about: this reconciliation may discharge an error, never introduce one.
func (run pass) noLongerCovered(ctx context.Context, ranged Range) bool {
	seen, err := run.observe(ctx)
	return err == nil && len(rangesNotYetObserved([]Range{ranged}, seen.partitions)) == 1
}

// countDropRefusal moves the counters one refused range calls for: the general failure count, which
// criterion 33 requires a caller to read with no metrics endpoint, and -- where undelivered events
// are what refused -- the distinct count ADR-9 gives that condition. A retention stalled by a down
// destination grows disk the whole time and is otherwise indistinguishable from a pass that found
// nothing to do, which is the condition most worth alerting on.
func (run pass) countDropRefusal(err error) {
	run.opts.Stats.CountFailure()
	if blockedByLiveEvents(err) {
		run.opts.Stats.CountRetentionBlockedByLiveEvents()
	}
}
