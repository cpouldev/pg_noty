package schema

import "fmt"

// This file is what one look at the catalog amounts to: the inventory observePartitions builds, and
// the decision each row it read joins that inventory by. catalog.go is the other half -- the
// handles and the queries -- and the seam between them is the one that changes for different
// reasons: that file changes when what this package asks the catalog changes, this one when what an
// answer means does. The queries stay there and may not be copied here, because catalog.go is the
// package's single observation authority and
// TestCatalogIsTheOnlyProductionSourceThatNamesTheSystemCatalogs is what holds it to that.
//
// The subject is one asymmetry. A partition can be dropped by another replica between the scan that
// lists it and the call that renders its bound, and under the three-replica retention this package
// supports that is the expected case rather than an anomaly. Such a row is *skipped*, and every
// other row this file cannot read still fails the whole observation -- because a partition left out
// of the inventory is one a drop decision was computed without, and that argument does not hold for
// a partition which is already gone: nothing can be decided without it that is not already true.
//
// The skip is safe in the destructive direction at every consumer, which is why it is a skip rather
// than a wider refusal. A range no longer in the inventory is a range retention is never offered
// and therefore never drops; it is one PlanMaintenance may ask to be created, which is correct
// because it really is gone, and which the server refuses with `would overlap partition..` in the
// one case it is not; and it is one repair.go's drain reads as uncovered, which is the reading that
// lets a lost drop race reconcile through noLongerCovered instead of aborting the pass that met it.

// boundReading is which of the three answers one catalog row gives about a child's partition bound.
// Each is its own answer rather than a shared negative, because a caller does three different things
// with them and a shared one cannot say which happened.
type boundReading string

const (
	// boundRendered is the ordinary answer: the server wrote the bound out and partitionbound.go
	// reads it back.
	boundRendered boundReading = "rendered"
	// boundPartitionVanished is the row of a partition dropped between the scan that listed it and
	// the expression call that would have rendered its bound. The row declares a bound -- so it was
	// a partition when this transaction's snapshot was taken -- and none was rendered, because the
	// relation resolves afresh and is no longer there.
	boundPartitionVanished boundReading = "vanished"
	// boundUndeclared is a child whose own row carries no partition bound at all, which is what a
	// table attached by classic inheritance reads as. Measured on PostgreSQL 17.10: a partitioned
	// parent cannot have one -- `cannot inherit from partitioned table` (SQLSTATE 42809), pinned by
	// TestAPartitionedParentAdmitsNoChildThatDeclaresNoBound -- so a row reading this way says the
	// parent is not partitioned, which is a world this package must not decide against.
	boundUndeclared boundReading = "undeclared"
)

// boundReadings is the vocabulary as a value, so a test quantifies over it rather than over a list
// copied into one assertion.
var boundReadings = []boundReading{boundRendered, boundPartitionVanished, boundUndeclared}

// observedBound is what one catalog row said about one child's bound: which of the three answers it
// gives, and the expression the server rendered where there is one.
type observedBound struct {
	reading boundReading
	// written is meaningful only under boundRendered. It is the empty string otherwise, which is not
	// a reading either of the other two can be confused with, because neither carries text at all.
	written string
}

// boundReadFrom is the two columns the observation reads a bound through, resolved into one answer.
//
// declaresABound comes from the scanned row and rendered from a fresh resolution of the same
// relation, and that is why the pair separates a dropped partition from a child that never was one:
// only a row which was a bounded partition in this snapshot and is not a relation now can answer
// true and NULL. It is a parameter rather than a test on rendered because the two questions differ,
// the same reason markerReadingOf takes presence separately from text.
func boundReadFrom(declaresABound bool, rendered *string) observedBound {
	switch {
	case rendered != nil:
		return observedBound{reading: boundRendered, written: *rendered}
	case declaresABound:
		return observedBound{reading: boundPartitionVanished}
	default:
		return observedBound{reading: boundUndeclared}
	}
}

// observedPartitions is what one look at the catalog found beneath one partitioned parent.
type observedPartitions struct {
	// Bounded is every partition whose bound this observation read, in catalog name order.
	Bounded []Range
	// Default is the DEFAULT partition's unqualified name, or empty when the parent has none. A
	// name and never a Range, deliberately: a DEFAULT partition has no upper bound, and giving it a
	// comparable one -- a sentinel, or a zero instant -- is exactly what would make ExpiredRanges
	// read it as older than every cutoff and retention drop the safety net every unrouted row lands
	// in (criterion 36).
	Default string
}

// add places one partition the catalog reported into the observation, refuses it, or -- for the one
// reading this file's header argues the case for -- leaves it out.
//
// A child living outside the configured schema is refused rather than kept or quietly dropped.
// Measured: a partition of our parent can be created in another schema, and the catalog reports it
// under our parent. Keeping it would offer retention a table outside this package's remit to drop,
// which the blast-radius doctrine forbids; dropping it from the set would leave the same incomplete
// world an unreadable bound would.
//
// The schema is tested before the reading, and the order is behaviour rather than description: a
// child of another schema is one this package may not act on whatever its bound says, and a
// vanished one there must still be reported as out of remit rather than skipped as somebody else's
// race. The case violating both is
// TestAVanishedChildOfAnotherSchemaIsStillRefusedForBeingOutOfRemit.
func (found *observedPartitions) add(schema, name, within string, bound observedBound) error {
	if within != schema {
		return finished(fmt.Errorf("partition %s of this parent lives in schema %s and not in the "+
			"configured %s; this package may neither drop it nor decide without it",
			name, within, schema))
	}

	switch bound.reading {
	case boundRendered:
		return found.addRendered(name, bound.written)
	case boundPartitionVanished:
		return nil
	case boundUndeclared:
		return finished(undeclaredBound(name))
	default:
		return finished(unrecognisedBoundReading(name, bound.reading))
	}
}

// addRendered places one partition whose bound the server wrote out, or refuses it for the reason
// partitionbound.go names.
func (found *observedPartitions) addRendered(name, written string) error {
	if isDefaultBound(written) {
		found.Default = name
		return nil
	}

	extent, fault := rangeFrom(written, name)
	if fault != boundOK {
		return finished(unreadableBound(name, written, fault))
	}
	found.Bounded = append(found.Bounded, extent)
	return nil
}

// undeclaredBound refuses a child that declares no partition bound of its own. It names the parent
// rather than the bound, because that is what is actually wrong: a child reading this way is one
// classic inheritance attached, which a partitioned parent cannot have, so the parent under
// observation is not the partitioned event log this package maintains.
func undeclaredBound(name string) error {
	return fmt.Errorf("the child %s of this parent declares no partition bound of its own, so this "+
		"parent is not partitioned and its children are not partitions; deciding against it would "+
		"be deciding against a table this package did not build", name)
}

// unrecognisedBoundReading is the fail-closed answer for a reading that is none of the three
// declared ones -- today only the zero boundReading, which no catalog row can produce, since
// boundReadFrom's every arm answers a declared one. Nothing an observation can hold reaches it, so
// it is reached directly by TestABoundReadingNoObservationCanProduceIsRefusedRatherThanSkipped.
func unrecognisedBoundReading(name string, reading boundReading) error {
	return fmt.Errorf("the bound of partition %s was read into a state this instance does not "+
		"recognise (%s), and an unrecognised reading is refused rather than treated as any of the "+
		"ones it knows", name, reading)
}
