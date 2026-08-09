package schema

import (
	"errors"
	"strings"
	"testing"
)

// This file is the container-free half of ADR-9's classification: which sentence from the server
// means "undelivered events still hold this range", and which does not. Its twin against the running
// server is TestTheServerRefusesTheDetachInTheWordsThisPackageClassifiesOn -- a constant naming a
// sentence and the server saying it are different claims, and only the second one is the property.

// theServersDetachRefusal is the sentence PostgreSQL 17.10 writes when the composite foreign key
// refuses a detach, transcribed from a measured run rather than invented -- the trailing digit of the
// constraint name is the per-partition child row a key onto a partitioned table leaves, so it varies
// with how many partitions exist and nothing here reads it. It is the row the classification must
// accept; every row after it differs from it in exactly one property. Its twin against the running
// server is TestTheServerRefusesTheDetachInTheWordsThisPackageClassifiesOn, which is what fails if a
// release rewords this and leaves the classifier quietly inert.
const theServersDetachRefusal = `ERROR: removing partition "events_20260731T085721.121274000Z_` +
	`20260731T095721.121274000Z" violates foreign key constraint ` +
	`"event_queue_event_id_occurred_at_fkey2" (SQLSTATE 23503)`

// theClassifiedRefusals is one row per sentence the pass can meet on the drop path. Each refused row
// carries one of the two clauses and not the other, so a classification narrowed to a single clause
// fails the row named for the clause it dropped.
var theClassifiedRefusals = []struct {
	name, written string
	want          bool
}{
	{name: "the server's own detach refusal", written: theServersDetachRefusal, want: true},

	{name: "an ordinary insert violating some other foreign key",
		written: `ERROR: insert or update on table "event_queue" violates foreign key constraint ` +
			`"event_queue_event_id_occurred_at_fkey" (SQLSTATE 23503)`},
	{name: "a message naming a removed partition and no key at all",
		written: `ERROR: removing partition "events_x" from a table that has none (SQLSTATE 42P01)`},
	{name: "the lock timeout this same path can meet",
		written: "canceling statement due to lock timeout (SQLSTATE 55P03)"},
	{name: "the DEFAULT-partition refusal the create path classifies",
		written: `ERROR: updated partition constraint for default partition "events_default" would ` +
			`be violated by some row (SQLSTATE 23514)`},
	{name: "this package's own unmarked refusal",
		written: unmarkedPartition("events_x").Error()},
}

// TestTheStalledRetentionClassificationNeedsBothClausesOfTheServersSentence is both sides of the
// predicate the distinct counter is moved by. Counting an ordinary foreign-key violation as a
// stalled retention would send an operator to look for a down destination over a row some
// application inserted wrongly, and missing the real one leaves disk growing with nothing to alert on.
func TestTheStalledRetentionClassificationNeedsBothClausesOfTheServersSentence(t *testing.T) {
	for _, tc := range theClassifiedRefusals {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockedByLiveEvents(errors.New(tc.written)); got != tc.want {
				t.Errorf("blockedByLiveEvents(%q) = %t, want %t", tc.written, got, tc.want)
			}
		})
	}
	if blockedByLiveEvents(nil) {
		t.Error("a pass that met no refusal at all reads as blocked by live events")
	}
}

// TestTheRemedyTellsAStalledRetentionFromEveryOtherRefusal is criterion 39's operator half. A range
// live events hold clears when the destination comes back and its queue rows leave pending and
// delivering; every other refusal here clears with the lock or the marker that caused it, and a line
// telling an operator to wait for a delivery over a released lock sends them to the wrong place.
func TestTheRemedyTellsAStalledRetentionFromEveryOtherRefusal(t *testing.T) {
	stalled := remedyForDrop(errors.New(theServersDetachRefusal))
	retried := remedyForDrop(lockTimedOut(droppingWork+"events_x", DefaultLockTimeout))

	if stalled == retried {
		t.Fatalf("both refusals are reported with the remedy %q, so a retention stalled by a down "+
			"destination reads exactly like one waiting for a lock", stalled)
	}
	if !containsAll(stalled, deadStatus, "delivered") {
		t.Errorf("the stalled remedy %q does not name what has to happen to the queue rows holding "+
			"the range", stalled)
	}
	if !containsAll(retried, "next pass") {
		t.Errorf("the ordinary remedy %q does not say the range is simply retried", retried)
	}
}

// containsAll reports whether one line carries every phrase, which is how a remedy is asserted to
// say each of the things an operator needs rather than merely to be non-empty.
func containsAll(line string, phrases ...string) bool {
	for _, phrase := range phrases {
		if !strings.Contains(line, phrase) {
			return false
		}
	}
	return true
}
