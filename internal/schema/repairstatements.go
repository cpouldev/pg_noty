package schema

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is what a drain *addresses* and what it *says*: the names one repair of one range
// names, the four statements a relation's rows travel by, and the words an operator reads it in. It
// is separate from repair.go for the reason maintainreport.go split from maintain.go before it --
// one file would breach the 200-line budget this package's own gate enforces -- and the seam is the
// one that changes for different reasons: repair.go changes when the shape of a drain changes, this
// file when the SQL it moves rows by, or the wording it reports in, does.
//
// It reaches no driver and no catalog. Everything here is a pure function of a Range, a
// configuration and an observation, which is what lets the statements and the two refusals be
// asserted container-free.

const (
	// drainingWork is what an operator reads in a refusal: the attempt that gave up.
	drainingWork = "drain the DEFAULT partition for range "

	drainedMessage      = "pg_noty drained a range out of the DEFAULT partition"
	drainRefusedMessage = "pg_noty could not drain a range out of the DEFAULT partition"

	// The blast radius an operator reads either side of the drain, as log attributes. logDefaultRows
	// is maintainreport.go's, read by name so the gauge a pass writes and the gauge a drain writes
	// are one key.
	logMoved             = "moved"
	logQueueMoved        = "queue_rows_moved"
	logDefaultRowsBefore = "default_partition_rows_before"
)

const (
	// theDrainedRows selects exactly one half-open range (M11): FROM is inclusive and TO is
	// exclusive, so a row written at the upper bound belongs to the range that *starts* there and is
	// left exactly where it is. Both bounds are bind parameters because they are values; every name
	// a statement below carries has been through identifier.go instead, which is the one thing a
	// bind parameter cannot do (skill Pattern 7).
	theDrainedRows = " WHERE occurred_at >= $1 AND occurred_at < $2"

	// overridingSystemValue is what carries the original ids across the move, and it is load-bearing
	// in both directions. Measured on 17.10, events.id is GENERATED ALWAYS, so the whole-row
	// re-insert without this clause is refused outright rather than renumbering -- and a re-insert
	// written to dodge that by naming the other columns would be accepted and *would* renumber,
	// orphaning every event_queue row pointing at those events. events.id is unique only by
	// construction (M1), so nothing downstream would notice.
	// TestTheReInsertIsRefusedWithoutOverridingSystemValue is the control.
	overridingSystemValue = "OVERRIDING SYSTEM VALUE "

	// holdingSuffix opens the name of the table one relation's rows wait in while the partition is
	// created. It carries the relation's own name so that a leftover -- which only a crash between
	// this transaction and its rollback could leave -- tells an operator what is in it.
	holdingSuffix = "_repair"
)

// holdingName is where one relation's rows wait, as an identifier PostgreSQL will not truncate. It
// goes through partition.go's own generator rather than concatenating, because a schema-qualified
// range name plus a prefix is already past the 63-byte limit and a silent truncation there would
// collide the event log's holding table with the queue's (M9, skill Pattern 9).
func holdingName(relation string, ranged Range) string {
	return ObjectName(relation+holdingSuffix, ranged.Name)
}

// relocation is one relation's rows for a range: lifted out into a holding table, and put back.
//
// Two of them make a drain. The event log's rows leave the DEFAULT partition and come back through
// the *parent*, so the server routes them into the partition created between those two steps. The
// queue's leave and return to itself.
//
// The queue travels at all because it has to. Skill Pattern 6's recipe is written for a log nothing
// references, and measured on 17.10 against migration 2's own DDL, removing an event from the
// DEFAULT partition while an event_queue row still points at it is refused outright:
//
//	update or delete on table "events_default" violates foreign key constraint
//	"event_queue_event_id_occurred_at_fkey1" on table "event_queue"
//
// A dead queue row never goes away, so a drain that refused to move the queue would leave such a
// range stuck forever -- the very state ADR-8 exists to clear. Both relations move inside one
// transaction with every id preserved, so the mapping internal/reconcile and internal/delivery
// resolve through survives (M1) and the key is satisfied at every statement boundary.
type relocation struct {
	// named is the relation in the words a refusal reports it in.
	named string
	// source is the qualified relation rows are removed from and target the one they are put back
	// into. They differ for the event log alone.
	source, target string
	// holding is the qualified name of the table those rows wait in.
	holding string
	// override is overridingSystemValue where the target carries an identity column, and empty
	// otherwise.
	override string
}

func (moved relocation) liftStatement() string {
	return "CREATE TABLE " + moved.holding + " AS SELECT * FROM " + moved.source + theDrainedRows
}

func (moved relocation) removeStatement() string {
	return "DELETE FROM " + moved.source + theDrainedRows
}

func (moved relocation) restoreStatement() string {
	return "INSERT INTO " + moved.target + " " + moved.override + "SELECT * FROM " + moved.holding
}

func (moved relocation) discardStatement() string {
	return "DROP TABLE " + moved.holding
}

// drain is one range's whole repair with every name it addresses rendered once, so no two
// statements of it can come to name different objects.
type drain struct {
	ranged Range
	schema string
	// marked is the partition the range gets, and the ownership marker written on it inside the same
	// transaction. The marker is not decoration: an unmarked partition is one Step 14's drop guard
	// refuses to drop for as long as it exists, so a drain that skipped it would leave a partition
	// growing forever (ADR-3).
	marked markedObject
	parent string
	// queue and events are the two relocations, held by name rather than in a slice because the
	// order they run in is the point: out child-first, back parent-first.
	queue, events relocation
}

// drainFor renders every name one drain addresses, or says which one it cannot use.
func drainFor(cfg config.Config, ranged Range) (drain, error) {
	if !ranged.To.After(ranged.From) {
		return drain{}, finished(
			fmt.Errorf(
				"the range %s given to drain is %s, which selects no row "+
					"and describes no partition", ranged.Name, extentOf(ranged),
			),
		)
	}

	schemaName := cfg.Database.Schema
	marked, fault := partitionObject(schemaName, ranged.Name, cfg.Instance)
	if fault != IdentifierOK {
		return drain{}, finished(
			fmt.Errorf(
				"the partition of schema %s for %s cannot be created: the "+
					"name %s given for it %s", schemaName, extentOf(ranged), ranged.Name, fault,
			),
		)
	}

	rendered := map[string]string{}
	for _, name := range []string{
		TableEvents, PartitionDefault, TableEventQueue,
		holdingName(TableEvents, ranged), holdingName(TableEventQueue, ranged),
	} {
		target, nameFault := Qualified(schemaName, name)
		if nameFault != IdentifierOK {
			return drain{}, finished(
				fmt.Errorf(
					"the drain of schema %s cannot name %s: it %s",
					schemaName, name, nameFault,
				),
			)
		}
		rendered[name] = target
	}
	return assembled(ranged, schemaName, marked, rendered), nil
}

// assembled is the drain those rendered names make. It is separate from the rendering above so that
// neither function needs the other's error handling to be read.
func assembled(ranged Range, schemaName string, marked markedObject, rendered map[string]string) drain {
	return drain{
		ranged: ranged, schema: schemaName, marked: marked, parent: rendered[TableEvents],
		queue: relocation{
			named:   TableEventQueue,
			source:  rendered[TableEventQueue],
			target:  rendered[TableEventQueue],
			holding: rendered[holdingName(TableEventQueue, ranged)],
		},
		events: relocation{
			named:    TableEvents,
			source:   rendered[PartitionDefault],
			target:   rendered[TableEvents],
			holding:  rendered[holdingName(TableEvents, ranged)],
			override: overridingSystemValue,
		},
	}
}

// whyUnrepairable is why this range cannot be drained against the world just observed, or the empty
// string. It is a reason rather than a bool so a caller can say which rule refused, and the empty
// string is a spelling no reason below uses.
//
// Both are fail-closed. A parent carrying no DEFAULT partition, or one whose DEFAULT partition is
// not the one migration 2 created, has nothing this drain knows how to empty -- and deleting from
// the wrong table would be the data-loss defect the whole step exists to avoid. A range some
// partition already covers holds no row in the DEFAULT partition at all, so a drain of it would ask
// the server to create a partition that exists.
//
// The missing net is tested first, and the order is behaviour rather than description: a log with
// no DEFAULT partition has nothing to drain whatever range it is asked about, and reporting such a
// log's covered range as merely covered would send an operator to look at the range instead of at
// the write-availability net that is gone. The row violating both is in
// repairrefusal_integration_test.go.
func (work drain) whyUnrepairable(found observedPartitions) string {
	switch {
	case found.Default == "":
		return "the event log carries no DEFAULT partition, so there is nothing to drain out of"
	case found.Default != PartitionDefault:
		return "the event log's DEFAULT partition is " + found.Default + " and this package drains " +
			PartitionDefault
	case len(rangesNotYetObserved([]Range{work.ranged}, found.Bounded)) == 0:
		return "a partition already covers " + extentOf(work.ranged) + ", so none of its rows is in " +
			"the DEFAULT partition"
	}
	return ""
}
