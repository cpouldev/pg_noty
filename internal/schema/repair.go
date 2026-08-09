package schema

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
)

// This file is ADR-8's other half. Step 11's pass detects a range the DEFAULT partition blocks,
// counts it and logs it, and moves no row; the DEFAULT partition cannot self-heal (M4), so
// count-and-log alone would leave the product able to detect a state it cannot fix. This is the fix,
// and it runs when an operator asks for it and at no other time.
//
// Why never automatically, in the terms it was measured in rather than as a rule -- because the
// automatic drain is the improvement a later reader reaches for, having seen the counter and the log
// line and wanted them to fix themselves. Creating a partition takes ACCESS EXCLUSIVE on the parent
// and on the DEFAULT partition, and M5 measured an insert into an *unrelated* partition blocking
// while either is held, through the parent or directly against the leaf, with no escape hatch. The
// work below is an unbounded DELETE and INSERT of the customer's own rows inside that window, and
// enqueue runs inside the customer's transaction -- so draining on the timer trades a stuck range
// for a stalled production database, the Availability NFR's central prohibition.
// TestNoProductionSourceOutsideTheDrainReachesIt holds it shut over every production source, and it
// reads *calls* and never text because maintainreport.go is required to write this function's name
// into the remedy it logs.
//
// The sequence is skill Pattern 6's measured recipe -- lift the conflicting rows out of the DEFAULT
// partition into a holding table, create the partition, put them back so the server routes them into
// it, drop the holding table -- in one transaction bounded by ddl.go, because a failure between any
// two of those steps leaves the customer's rows in a table nobody owns.
//
// Pattern 6 is written for a log nothing references, and this one is referenced: the queue's rows
// travel with their events, for the measured reason recorded on relocation in repairstatements.go.
//
// Step 15's Expected Output names two files; this step is written across nine, for the reason
// Steps 1, 3 and 11 declared the same deviation -- one file of each would breach the 200-line
// budget this package's own gate enforces.

// RepairResult is what one drain did, in the numbers an operator needs before and after it.
//
// It is not a Result: a drain is not a maintenance pass, and reporting it as one would put an
// operator-invoked data movement into the same counts a scheduled loop reports.
type RepairResult struct {
	// Partition is the partition the range now has.
	Partition string
	// Moved is how many events left the DEFAULT partition and arrived in it, and QueueMoved how many
	// delivery-queue rows travelled with them.
	Moved      int64
	QueueMoved int64
	// DefaultRowsBefore and DefaultRowsAfter are the DEFAULT partition's whole population either
	// side of the drain, read inside the drain's own transaction. Both, because the fall between
	// them is what tells an operator whether this range was all of the problem or one of several.
	DefaultRowsBefore int64
	DefaultRowsAfter  int64
}

// RepairDefaultPartition moves one range's rows out of the DEFAULT partition, gives that range a
// partition of its own and puts the rows back into it, in one transaction bounded by ddl.go's
// lock_timeout. It is invoked by an operator and never by anything on a timer -- see the head of
// this file for the measurement that makes that a requirement rather than a preference.
//
// A failure leaves the database exactly as it was: one transaction is what makes the failure mode
// "nothing happened" rather than "the rows are somewhere else". Contention is answered the same way
// and needs no lock of its own beyond ddl.go's bound. A maintenance pass, or a second drain, that
// meets this one waits and then gives up with ErrLockTimeout and an untouched schema; a second
// drain arriving after this one committed finds the range covered and is refused for saying so; and
// a customer write that lands in the DEFAULT partition for this very range between the delete and
// the create makes the server refuse the create with ErrDefaultBlocked, which rolls the whole thing
// back. Every one of those is re-runnable, and none of them half-moves a row.
func RepairDefaultPartition(
	ctx context.Context, on beginner, cfg config.Config, opts Options,
	ranged Range,
) (RepairResult, error) {

	opts = opts.normalized()

	var done RepairResult
	work, err := drainFor(cfg, ranged)
	if err == nil {
		err = boundedTx(
			ctx, on, opts,
			ddlSubject{operation: drainingWork + ranged.Name, partition: ranged.Name},
			func(ctx context.Context, tx pgx.Tx) error {
				moved, failure := work.through(ctx, tx)
				done = moved
				return failure
			},
		)
	}
	if err != nil {
		refused := finished(
			fmt.Errorf(
				"the DEFAULT partition of schema %s could not be drained for "+
					"%s: %w", cfg.Database.Schema, extentOf(ranged), err,
			),
		)
		opts.Logger.LogAttrs(
			ctx, levelOf(OutcomeFailed), drainRefusedMessage,
			slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
			slog.String(logCause, refused.Error()),
		)
		return RepairResult{}, refused
	}

	// The gauge Step 11 stores on every pass, stored here too: an operator watching it for the
	// blocked condition sees this drain in the same number, rather than having to read a log line.
	opts.Stats.StoreDefaultPartitionRows(done.DefaultRowsAfter)
	opts.Logger.LogAttrs(
		ctx, levelOf(OutcomeWorkDone), drainedMessage,
		slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
		slog.Int64(logMoved, done.Moved), slog.Int64(logQueueMoved, done.QueueMoved),
		slog.Int64(logDefaultRowsBefore, done.DefaultRowsBefore),
		slog.Int64(logDefaultRows, done.DefaultRowsAfter),
	)
	return done, nil
}

// through is the whole drain, inside the one transaction its caller opened.
//
// The world is read through catalog.go and never through a query of this file's own, so a drain and
// a pass cannot disagree about what exists. Every statement below hands its driver error back
// unwrapped rather than finishing it, and that order is load-bearing: ddl.go classifies a server
// refusal by reading the condition off it, and finishing severs the chain that read needs, so a
// timeout finished here would come back as an unrecognised error (ddlrefusal.go).
func (work drain) through(ctx context.Context, tx pgx.Tx) (RepairResult, error) {
	found, err := observePartitions(ctx, tx, work.schema, TableEvents)
	if err != nil {
		return RepairResult{}, err
	}
	if why := work.whyUnrepairable(found); why != "" {
		return RepairResult{}, finished(fmt.Errorf("%s cannot be drained: %s", extentOf(work.ranged), why))
	}
	before, err := defaultPartitionRows(ctx, tx, work.schema, PartitionDefault)
	if err != nil {
		return RepairResult{}, err
	}

	// Child first: the composite foreign key refuses to let an event go while a queue row points at
	// it, and parent first on the way back for the same reason in reverse.
	queued, err := liftedOut(ctx, tx, work.queue, work.ranged)
	if err != nil {
		return RepairResult{}, err
	}
	events, err := liftedOut(ctx, tx, work.events, work.ranged)
	if err != nil {
		return RepairResult{}, err
	}
	// The range gets its partition through the same statement and the same ownership marker a
	// maintenance pass uses, so a partition this drain created and one a pass created are the same
	// object to every later guard -- an unmarked one is one Step 14's drop guard refuses forever.
	if _, err := tx.Exec(ctx, createPartition(work.marked.target, work.parent, work.ranged)); err != nil {
		return RepairResult{}, err
	}
	if err := claimMarker(ctx, tx, work.marked); err != nil {
		return RepairResult{}, err
	}

	if err := putBack(ctx, tx, work.events, events); err != nil {
		return RepairResult{}, err
	}
	if err := putBack(ctx, tx, work.queue, queued); err != nil {
		return RepairResult{}, err
	}

	after, err := defaultPartitionRows(ctx, tx, work.schema, PartitionDefault)
	if err != nil {
		return RepairResult{}, err
	}
	return RepairResult{
		Partition: work.ranged.Name, Moved: events, QueueMoved: queued,
		DefaultRowsBefore: before, DefaultRowsAfter: after,
	}, nil
}

// liftedOut copies one relation's rows for a range into its holding table, removes them, and answers
// how many moved.
//
// The two counts are reconciled rather than assumed equal. A holding table that copied fewer rows
// than the delete removed is rows destroyed, and refusing the whole transaction over a number that
// does not add up is the only answer a drain of customer data may give.
func liftedOut(ctx context.Context, tx pgx.Tx, moved relocation, ranged Range) (int64, error) {
	held, err := tx.Exec(ctx, moved.liftStatement(), ranged.From, ranged.To)
	if err != nil {
		return 0, err
	}
	removed, err := tx.Exec(ctx, moved.removeStatement(), ranged.From, ranged.To)
	if err != nil {
		return 0, err
	}
	if removed.RowsAffected() != held.RowsAffected() {
		return 0, finished(
			fmt.Errorf(
				"%s: %d rows were copied out of %s and %d were removed from it",
				moved.named, held.RowsAffected(), moved.source, removed.RowsAffected(),
			),
		)
	}
	return held.RowsAffected(), nil
}

// putBack re-inserts one relation's rows and discards their holding table, refusing unless every
// row that was lifted out arrived.
func putBack(ctx context.Context, tx pgx.Tx, moved relocation, lifted int64) error {
	arrived, err := tx.Exec(ctx, moved.restoreStatement())
	if err != nil {
		return err
	}
	if arrived.RowsAffected() != lifted {
		return finished(
			fmt.Errorf(
				"%s: %d rows were lifted out and %d arrived in %s",
				moved.named, lifted, arrived.RowsAffected(), moved.target,
			),
		)
	}
	_, err = tx.Exec(ctx, moved.discardStatement())
	return err
}
