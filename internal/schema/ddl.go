package schema

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file is the package's one lock-bounded execution path: every maintenance statement Steps 11,
// 13, 14 and 15 issue runs through boundedTx, in its own transaction, under SET LOCAL lock_timeout.
// TestDDLIsTheOnlyFileIssuingALockBoundedStatement holds that shut, because a second path is a
// statement that escapes the bound.
//
// The bound is an availability control on a database this program does not own, not a tuning value.
// M5 measured a partition create *and* a partition drop each taking ACCESS EXCLUSIVE on the parent
// and on the DEFAULT partition, and measured an INSERT into an *unrelated* partition blocking while
// either is held -- issued through the parent or directly against the leaf, with no escape hatch.
// Since enqueue runs inside the customer's own transaction, an unbounded wait here is a stalled
// production application (criterion 40).
//
// The obvious improvement a later reader will reach for is the concurrent form of DETACH PARTITION,
// which takes no lock on the parent at all. It is unusable here at every version, and the server's
// own words for why are these:
//
//	cannot detach partitions concurrently when a default partition exists
//
// This design mandates a permanent DEFAULT partition as its write-availability net, so that
// restriction is unconditional rather than a workload or a version matter -- and what it rules out
// is not a slower path but a hard runtime error the first time it is tried (M3, skill Pattern 5).
// Nothing in this file spells the keyword, and
// TestConcurrentlyReachesNoStatementInTheBoundedExecutionAuthority keeps it that way.
//
// What would have to become true for it to be usable, stated so the prohibition is explained rather
// than merely asserted: this schema would have to stop carrying a permanent DEFAULT partition. That
// partition is created by migration 2 and never dropped, never detached and never re-created
// (criteria 29 and 36), because it is what turns a maintenance-loop failure into "rows land in
// DEFAULT" instead of "the customer's writes break". So the answer is: nothing this package
// permits.

// beginner is what a bounded statement opens its transaction on: the pool a maintenance pass holds,
// or the single pinned connection bootstrap keeps for its whole run. Both satisfy it, so neither
// caller has to unwrap the other's handle and no second execution path is needed for the pinned
// case.
type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ddlSubject is what a bounded statement is doing and what it is doing it to. Both are carried
// because the two refusals report different things: a timeout names the attempt that gave up, so an
// operator knows which maintenance to look at, and a DEFAULT-partition refusal names the partition
// that stays uncreatable until those rows are drained.
type ddlSubject struct {
	// operation is the attempt in the words an operator reads: `create partition events_...`.
	operation string
	// partition is the unqualified name of the partition the statement acts on, which is the range
	// RepairDefaultPartition is invoked for.
	partition string
}

// oneStatement is the work a bounded transaction runs when the whole of it is a single statement,
// which is what precreate needs. Retention needs several inside one transaction (ADR-7), so the
// transaction rather than the statement is the primitive here and this is the thin case of it.
func oneStatement(statement string) func(context.Context, pgx.Tx) error {
	return func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, statement)
		return err
	}
}

// boundedTx runs work inside its own transaction, bounded by SET LOCAL lock_timeout, and answers
// with the refusal in this package's own vocabulary.
//
// Its own transaction rather than the caller's, and SET LOCAL rather than SET: a pooled connection
// outlives every statement that ran on it, so a session-level bound would silently govern the next
// caller's unrelated work until that connection was reaped
// (TestTheBoundIsLocalToItsTransactionAndDoesNotOutliveIt).
func boundedTx(ctx context.Context, on beginner, opts Options, about ddlSubject,
	work func(context.Context, pgx.Tx) error) error {

	opts = opts.normalized()

	tx, err := on.Begin(ctx)
	if err != nil {
		return finished(classified(err, about, opts.LockTimeout))
	}
	// Rolling back after a successful Commit answers pgx.ErrTxClosed and changes nothing, so one
	// deferred call covers every failure below rather than a branch per statement.
	//
	// It shares the caller's context, which a cancellation would already have poisoned -- and that
	// is safe rather than merely conventional: measured on pgxpool v5.10.0, Conn.Release destroys a
	// connection whose TxStatus is not 'I' instead of returning it, so a rollback that could not be
	// sent cannot hand the next caller a connection with this transaction still open.
	defer func() { _ = tx.Rollback(ctx) }()

	// The bound, the work and the commit share one exit because they share one answer: whichever
	// refused first is what the caller is told about. They are not lifted into a helper, because a
	// helper returning an error from this file would have to finish that error itself -- and
	// finishing before classifying is the one order that loses the server's condition.
	if _, err = tx.Exec(ctx, lockTimeoutStatement(opts.LockTimeout)); err == nil {
		err = work(ctx, tx)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return finished(classified(err, about, opts.LockTimeout))
	}
	return nil
}

// lockTimeoutStatement is the bound as the server takes it.
//
// The value is written into the statement rather than bound as a parameter because SET accepts no
// parameters at all -- and it is injection-free for a reason that does not depend on remembering
// that: it is an int64 rendered in base ten, which has no other spelling. Every *name* this package
// puts into a statement goes through identifier.go instead.
func lockTimeoutStatement(bound time.Duration) string {
	return "SET LOCAL lock_timeout = " + strconv.FormatInt(millisecondsOfBound(bound), 10)
}

// millisecondsOfBound is a bound in the unit PostgreSQL counts lock_timeout in, never rounding down
// to zero.
//
// Zero is not the smallest bound, it is the absence of one: the server reads it as "wait forever",
// which is the single outcome this file exists to prevent. So a bound below the unit's resolution
// becomes the shortest bound the unit can express rather than none, and so does a negative one --
// the server refuses a negative setting outright, and refusing to run at all is worse than running
// under the tightest bound there is.
func millisecondsOfBound(bound time.Duration) int64 {
	if milliseconds := bound.Milliseconds(); milliseconds > 0 {
		return milliseconds
	}
	return 1
}

// serverRefusal is what the server said about a refusal: the condition it reported it under, and
// the sentence it reported. It is the driver's error reduced to the two things ddlrefusal.go reads,
// which is what lets that file classify without importing the driver -- and therefore without
// having to finish the errors it builds.
type serverRefusal struct {
	code    string
	message string
}

// serverRefusalIn is what the server said, and whether the server said anything at all. A cancelled
// context, a dropped socket and a pool with no capacity left all arrive here carrying no condition,
// and none of them may be read as one.
//
// errors.As rather than a type assertion: pgx wraps its *pgconn.PgError on several paths, and an
// assertion would leave those unclassified while every direct case still passed
// (TestAWrappedServerRefusalIsStillClassified).
func serverRefusalIn(err error) (serverRefusal, bool) {
	var refusal *pgconn.PgError
	if !errors.As(err, &refusal) {
		return serverRefusal{}, false
	}
	return serverRefusal{code: refusal.Code, message: refusal.Message}, true
}
