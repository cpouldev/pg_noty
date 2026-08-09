package schema

import "fmt"

// This file is what a retention pass *says* and the statements it issues, split from retention.go
// for the reason maintainreport.go split from maintain.go: one file would breach the 200-line
// budget this package's own gate enforces, and the seam is the one that changes for different
// reasons -- retention.go changes when the shape of a drop changes, this file when the wording an
// operator reads does.
//
// It reaches no driver and reads no catalog. Everything here is a pure function of a Range, a schema
// name and an error, which is what lets the words and the statements be asserted container-free.

const (
	// droppingWork is the attempt an operator reads in a refusal: which maintenance to look at.
	droppingWork = "drop partition "

	retentionSettledMessage = "pg_noty retention pass finished"
	retentionRefusedMessage = "pg_noty could not drop an expired partition"
	// retentionGoneMessage is deliberately not phrased as another replica's work. Two states reach
	// it -- a partition another replica dropped between this pass's observation and its own
	// transaction, and one an operator removed by hand -- and a line naming a replica for the second
	// would send a reader looking for a race that never happened.
	retentionGoneMessage = "pg_noty found this expired range no longer present"
	// retentionReapedMessage reports the dead queue rows one drop removed. It is written after the
	// commit and never inside the transaction, because a reap that rolled back with its drop removed
	// nothing and a line saying otherwise is a false report of deleted rows.
	retentionReapedMessage = "pg_noty reaped the dead queue rows of an expired range"
	// retentionUnobservedMessage is its own line rather than retentionRefusedMessage with an empty
	// range: a pass that could not read the world refused no range, and reporting it as one would
	// send an operator looking for a partition nothing was ever attempted for.
	retentionUnobservedMessage = "pg_noty could not read the partitions it retains"
)

// The log attribute keys this pass adds to maintainreport.go's. They are constants because the tests
// asserting that an operator can act on these lines read them by name.
const (
	logDropped = "dropped"
	logReaped  = "reaped_dead_queue_rows"
)

// deadStatus is the queue status that releases an event's partition for retention. internal/delivery
// owns the transitions; this package declares the vocabulary in migration 2, and moving a row here
// is a decision to permit the event's eventual deletion rather than a report that delivery failed.
const deadStatus = "dead"

// lockTheEventLog is the drop transaction's first statement, and the reason every guard below it
// reads a state nothing can change underneath it.
//
// ONLY is deliberate: DETACH needs ACCESS EXCLUSIVE on the parent, on the partition it removes and
// on the DEFAULT partition, and taking the parent's lock up front is what serialises two replicas'
// drops from the first statement rather than from the DETACH. Without it a replica can read a
// marker, be overtaken by another replica that drops the same partition, and then meet the server's
// missing-relation error instead of the concession this pass is written to report (criterion 23).
func lockTheEventLog(parent string) string {
	return "LOCK TABLE ONLY " + parent + " IN ACCESS EXCLUSIVE MODE"
}

// detachPartition removes one partition from the event log. Plain, never the concurrent form -- the
// reason is quoted at the call site in retention.go, where the person about to reach for it will be.
func detachPartition(parent, target string) string {
	return "ALTER TABLE " + parent + " DETACH PARTITION " + target
}

// dropPartition is the drop itself. IF EXISTS supplements the marker and attachment checks and never
// substitutes for them: it suppresses the error from dropping a table that is not there, and does
// nothing whatsoever to stop dropping one that is there and is not ours (skill Pattern 7).
func dropPartition(target string) string {
	return "DROP TABLE IF EXISTS " + target
}

// reapDeadRows removes the queue rows of one range that are already dead.
//
// Dead rows alone, because the composite foreign key makes the *server* refuse to detach a partition
// a pending or delivering row still points into -- which is the guarantee ADR-9 turned on, and one a
// Go guard cannot offer. The bounds are the range's own half-open extent, so a row belonging to the
// next partition is never touched. They are bind parameters and not literals: they are values, which
// is exactly what a bind parameter protects.
func reapDeadRows(queue string) string {
	return "DELETE FROM " + queue + " WHERE status = '" + deadStatus +
		"' AND occurred_at >= $1 AND occurred_at < $2"
}

// dropTargets is every object one drop names, rendered once through the quoting authority so the
// lock, the reap, the detach and the drop cannot come to address different objects.
type dropTargets struct {
	// partition carries both the rendered name and the ownership marker form read back inside the
	// transaction, so the guard and the statements name one object.
	partition markedObject
	parent    string
	queue     string
}

// dropTargetsFor renders the three names one range's drop writes, or says which one cannot be used.
func dropTargetsFor(schema, instance string, ranged Range) (dropTargets, error) {
	marked, fault := partitionObject(schema, ranged.Name, instance)
	if fault != IdentifierOK {
		return dropTargets{}, finished(fmt.Errorf("the partition of schema %s for %s cannot be "+
			"dropped: the name %s given for it %s", schema, extentOf(ranged), ranged.Name, fault))
	}

	parent, err := qualifiedOrRefused(schema, TableEvents, "event log")
	if err != nil {
		return dropTargets{}, err
	}
	queue, err := qualifiedOrRefused(schema, TableEventQueue, "delivery queue")
	if err != nil {
		return dropTargets{}, err
	}
	return dropTargets{partition: marked, parent: parent, queue: queue}, nil
}

// qualifiedOrRefused renders one object of the configured schema, naming the role it plays so that
// three refusals with one shape still say which object could not be named. One helper rather than
// three copies of its body.
func qualifiedOrRefused(schema, name, role string) (string, error) {
	rendered, fault := Qualified(schema, name)
	if fault != IdentifierOK {
		return "", finished(fmt.Errorf("the %s of schema %s cannot be named: %s %s",
			role, schema, name, fault))
	}
	return rendered, nil
}

// dropRefused names the range a partition could not be dropped for, alongside the cause -- both,
// because a cause with no range is a line an operator cannot act on and a range with no cause says
// nothing about whether to wait for a lock or to look for undelivered events. The cause is wrapped
// rather than rendered, so errors.Is still answers which condition it was.
func dropRefused(ranged Range, err error) error {
	return finished(fmt.Errorf("the partition for %s could not be dropped: %w", extentOf(ranged), err))
}

// remedyForDrop is what an operator does about one refusal, in the line that reports it.
//
// A range live events hold does not clear on its own: it clears when the destination comes back and
// its queue rows leave pending and delivering, and until then disk grows. Every other refusal here
// clears with the lock or the marker that caused it, and the next pass retries the range unchanged.
func remedyForDrop(err error) string {
	if blockedByLiveEvents(err) {
		return "this range is retried every pass and stays until its undelivered events are " +
			"delivered or moved to " + deadStatus
	}
	return "the next pass retries this range"
}
