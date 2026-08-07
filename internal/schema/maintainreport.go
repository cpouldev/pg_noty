package schema

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// This file is what a maintenance pass *says*: the statement it writes for one range, and the words
// it reports a range in. It is separate from maintain.go for the reason internal/goartifact and
// partition.go's corpus split before it -- one file would breach the 200-line budget this package's
// own gate enforces -- and the seam is the one that changes for different reasons: maintain.go
// changes when the shape of a pass changes, this file when the wording an operator reads does.
//
// It reaches no driver and no catalog. Everything here is a pure function of a Range and an error,
// which is what lets criterion 33's wording and M6's statement be asserted container-free.

const (
	// repairCommand is the drain a blocked range's log line points at. It is a constant so that the
	// pointer and the test asserting it cannot drift apart, and it is a word here rather than a
	// call: ADR-8 forbids this pass from draining anything.
	repairCommand = "RepairDefaultPartition"
	// observingWork and creatingWork are what an operator reads in a refusal -- the attempt that
	// gave up, so they know which maintenance to look at.
	observingWork = "observe the partitions of " + TableEvents
	creatingWork  = "create partition "

	settledMessage  = "pg_noty maintenance pass finished"
	refusedMessage  = "pg_noty could not create a partition"
	concededMessage = "pg_noty found this range already created by another replica"
	// unobservedMessage is its own line rather than refusedMessage with an empty range: a pass that
	// could not read the world refused no range, and reporting it as one would send an operator
	// looking for a partition nothing was ever attempted for.
	unobservedMessage = "pg_noty could not read the partitions it maintains"
	// unservableMessage is its own line for the same reason and one more: an unservable horizon
	// refused no range either, and unlike every other refusal here it does not clear on its own --
	// the next pass over the same configuration refuses identically until that configuration changes.
	unservableMessage = "pg_noty was asked for more partitions than a database can hold"
)

// The log attribute keys. They are constants because the tests asserting that an operator can act
// on these lines read them by name, and a key spelled twice stops being greppable the first time
// one copy is edited.
const (
	logRange       = "range"
	logExtent      = "extent"
	logRemedy      = "remedy"
	logCause       = "cause"
	logOutcome     = "outcome"
	logCreated     = "created"
	logDefaultRows = "default_partition_rows"
)

// createPartition is the statement one range is realised by.
//
// It carries no IF NOT EXISTS, deliberately. That form matches on the *name*, and a name is not an
// extent: measured on 17.10, the loser of a true race is told `relation ... already exists`
// outright, and a replica that agreed on a name while disagreeing on an extent would carry on with
// the wrong one. The race is reconciled against the observed bound instead, by pass.covers (M6).
//
// The bounds are written into the statement as literals because FOR VALUES takes a constant
// expression and no bind parameter can carry one. They are injection-free for a reason that does
// not depend on remembering it: an instant rendered in RFC 3339 has exactly one spelling and no
// quote anywhere in it. Every *name* here has already been through identifier.go.
func createPartition(target, parent string, ranged Range) string {
	return "CREATE TABLE " + target + " PARTITION OF " + parent +
		" FOR VALUES FROM ('" + boundLiteral(ranged.From) + "') TO ('" + boundLiteral(ranged.To) + "')"
}

// boundLiteral writes one bound as the absolute instant it is.
//
// In UTC, so that the statement does not depend on the zone of the process that computed it: the
// server refuses overlapping ranges, so a replica elsewhere writing the same range in its own zone
// would either agree by luck or make the range permanently uncreatable. At nanosecond precision,
// because internal/config requires only that partition_interval be greater than zero (R10) and a
// second-resolution literal would name a different range on a sub-second grid.
func boundLiteral(bound time.Time) string {
	return bound.UTC().Format(time.RFC3339Nano)
}

// extentOf writes one range's half-open extent the way an operator reads it, with the square
// bracket and the parenthesis that say which end is included. It is one renderer rather than two so
// that the error and the log line cannot come to describe the same range differently.
func extentOf(ranged Range) string {
	return "[" + boundLiteral(ranged.From) + ", " + boundLiteral(ranged.To) + ")"
}

// creationRefused names the range a partition could not be created for, alongside the cause.
//
// Both, because criterion 33 asks for both: a cause with no range is a line an operator cannot act
// on, and a range with no cause says nothing about whether to wait for a lock or to drain the
// DEFAULT partition. The cause is wrapped rather than rendered, so errors.Is still answers which
// condition it was.
//
// It passes through the finishing point although this file names no driver, and that is worth
// saying rather than leaving to be rediscovered: the gate enforcing ADR-11 is an *import* test, so
// a file that only ever receives driver errors -- through boundedTx, already elided -- is outside
// it. This file is compliant by hand there, and the hand is here.
func creationRefused(ranged Range, err error) error {
	return finished(fmt.Errorf("the partition for %s could not be created: %w", extentOf(ranged), err))
}

// remedyFor is what an operator does about one refusal, in the line that reports it.
//
// A range the DEFAULT partition blocked does not clear on its own (M4) -- retrying it forever
// changes nothing -- so its line names the one command that clears it. Every other refusal does
// clear on its own, and naming the drain there would send an operator to move rows over a lock that
// has already been released.
func remedyFor(err error) string {
	if errors.Is(err, ErrDefaultBlocked) {
		return repairCommand + " moves the rows in the DEFAULT partition that block this range"
	}
	return "the next pass retries this range"
}

// outcomeOf is what a pass amounted to, in criterion 33's vocabulary.
//
// A refusal outranks the work that succeeded beside it: a pass that created six ranges and was
// refused the seventh has left a day uncovered, and a caller reading that as work done would never
// look. The zero counts answer OutcomeNothingNeeded, which is the pass criterion 22 describes --
// nothing was needed, so nothing was issued.
func outcomeOf(created, refused int) Outcome {
	switch {
	case refused > 0:
		return OutcomeFailed
	case created > 0:
		return OutcomeWorkDone
	default:
		return OutcomeNothingNeeded
	}
}

// levelOf is the level one outcome is reported at.
//
// A failed pass is reported strictly above routine operation, which is criterion 33's third clause:
// without the gap, a maintenance loop that has been failing for a week reads exactly like one with
// nothing to do, and no alerting rule can tell them apart.
func levelOf(outcome Outcome) slog.Level {
	if outcome == OutcomeFailed {
		return slog.LevelError
	}
	return slog.LevelInfo
}
