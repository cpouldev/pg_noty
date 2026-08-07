package schema

import (
	"fmt"
	"strings"
)

// This file is why one partition may not be dropped, and what two of those refusals mean to the
// pass that met them. It is separate from retentionreport.go for the reason ddlrefusal.go is
// separate from ddl.go: the wording of a refusal and the meaning of one change for different
// reasons, and this half changes when PostgreSQL reports a condition differently.
//
// Every reason has its own answer. A shared refusal cannot tell an operator whether a partition is
// unmarked, someone else's, or no longer attached, and those three call for three different
// actions. The foreign case reuses errors.go's ErrForeignInstance rather than inventing a fourth
// spelling of it.

// The identifying phrase of each refusal this package writes itself. Each is a constant so the
// message and the predicate that reads it back are one string rather than two that can drift, and so
// a test can pin the wording by equality rather than by substring.
const (
	// theUnmarkedPhrase is the one refusal consistent with the partition having been dropped by
	// another replica: obj_description of a relation that no longer exists reads as no marker at
	// all, which is the same reading an existing unmarked table gives. readsAsAlreadyGone is why
	// that ambiguity is safe -- it is resolved by re-observing, never by the phrase alone.
	theUnmarkedPhrase      = "carries no pg_noty ownership marker"
	theUnreadablePhrase    = "carries a comment this instance cannot read as a pg_noty ownership marker"
	theForeignMarkerPhrase = "is marked for another instance"
	theUnattachedPhrase    = "is not attached to the event log this instance maintains"
	theUnknownStatePhrase  = "was read into an ownership state this instance does not recognise"
)

// theDropRefusalPhrases is the set as a value, so a test quantifies over it and a sixth refusal has to
// join it rather than merely being written. No phrase may contain another: readsAsAlreadyGone reads one of
// them back, and a phrase that contained the unmarked one would make an unrelated refusal look like a lost
// race.
var theDropRefusalPhrases = []string{theUnmarkedPhrase, theUnreadablePhrase, theForeignMarkerPhrase,
	theUnattachedPhrase, theUnknownStatePhrase}

// unmarkedPartition refuses a partition carrying no marker. ADR-3 claims an *unmarked schema*,
// because a DBA pre-creating it is the supported minimal-privilege path; an unmarked partition is
// the opposite case, since nothing but a create this package performed writes the table form, and
// dropping one would be dropping a table this instance cannot show it made.
func unmarkedPartition(name string) error {
	return finished(fmt.Errorf("partition %s %s, so this instance cannot establish that it created "+
		"the table and will not drop it", name, theUnmarkedPhrase))
}

// unreadableMarkerOn refuses a comment that is not a marker of this format -- a note somebody wrote
// by hand, or a marker written under a later markerVersion. Defaulting it to absent or to foreign
// is a fail-open in one direction or a wrong report in the other.
func unreadableMarkerOn(name string) error {
	return finished(fmt.Errorf("partition %s %s, and a comment this reader cannot parse is not one "+
		"it may act on", name, theUnreadablePhrase))
}

// foreignMarkerOn refuses a partition another instance created. It carries errors.go's
// ErrForeignInstance rather than a fourth spelling of that condition -- so errors.Is still answers
// it -- and names the partition, which that sentinel's own message cannot: it is worded for the
// schema-level claim ADR-3 makes at boot, and an operator meeting it here needs the table.
func foreignMarkerOn(name, marked, expected string) error {
	return finished(fmt.Errorf("partition %s %s: %w", name, theForeignMarkerPhrase,
		foreignInstance(marked, expected)))
}

// unattachedTable refuses a table that is not a partition of the configured event log, whatever its
// name says. A name match is forgeable and coincidental, and it is the predicate that deletes a
// customer's table while passing every ordinary input (criterion 38, skill Pattern 7).
func unattachedTable(name string) error {
	return finished(fmt.Errorf("%s %s, and a name matching the partition naming scheme is not "+
		"evidence that it is one", name, theUnattachedPhrase))
}

// unknownMarkerState is the fail-closed answer for a reading that is none of the four declared
// states -- today only the zero markerState, which markerOn answers alongside an error a caller
// might have ignored. Nothing a catalog can hold reaches it, so it is reached directly by
// TestEachReasonToRefuseADropHasItsOwnAnswer's "a state no catalog can produce" row.
func unknownMarkerState(name string, state markerState) error {
	return finished(fmt.Errorf("partition %s %s (%s), and an unrecognised ownership state is refused "+
		"rather than treated as any of the ones this instance knows", name, theUnknownStatePhrase, state))
}

// refusalForMarker is the first drop guard's verdict on one reading, or nil for this instance's own
// marker. The default arm refuses rather than falling through to a drop, which is what makes a fifth
// markerState a loud failure instead of a silent deletion.
func refusalForMarker(reading markerReading, name, instance string) error {
	switch reading.state {
	case markerOurs:
		return nil
	case markerAbsent:
		return unmarkedPartition(name)
	case markerUnreadable:
		return unreadableMarkerOn(name)
	case markerForeign:
		return foreignMarkerOn(name, reading.named, instance)
	default:
		return unknownMarkerState(name, reading.state)
	}
}

// readsAsAlreadyGone reports whether a refusal is the one a partition another replica has already
// dropped produces: to_regclass answers NULL for a relation that is not there, obj_description of
// NULL is NULL, and the marker therefore reads as absent.
//
// It is one half of the concession and never the whole of it. An existing, genuinely unmarked
// partition gives the same reading, and dropping that is the data-loss case guard 1 exists for -- so
// the caller confirms the extent is no longer covered before conceding anything.
func readsAsAlreadyGone(err error) bool {
	return err != nil && strings.Contains(err.Error(), theUnmarkedPhrase)
}

// The server's own sentence for a detach the composite foreign key refuses. Both halves are kept:
// the second is only the *class* -- an ordinary insert violating any foreign key says it too --
// while the first is what identifies this condition and this position. Both are pinned against
// the running server, in both directions, by
// TestTheServerRefusesTheDetachInTheWordsThisPackageClassifiesOn.
const (
	theRemovedPartitionPhrase = "removing partition"
	theForeignKeyPhrase       = "violates foreign key constraint"
)

// blockedByLiveEvents reports whether the server refused a detach because pending or delivering
// queue rows still reference the partition (ADR-9).
//
// It reads a message, which is a compromise and a deliberate one. The server's own condition would
// be the better match, but errors.go's vocabulary is closed and the finishing point severs the chain
// to the driver's error before this pass sees it (ADR-11) -- so the text is what is left, and the
// two clauses together are narrow enough that no other statement in the drop transaction can raise
// them. The reap deletes from the referencing side, which cannot violate this key at all.
func blockedByLiveEvents(err error) bool {
	if err == nil {
		return false
	}
	written := err.Error()
	return strings.Contains(written, theRemovedPartitionPhrase) &&
		strings.Contains(written, theForeignKeyPhrase)
}
