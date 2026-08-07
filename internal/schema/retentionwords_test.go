package schema

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// This file is the container-free half of guard 1: five reasons to refuse a drop, five answers, and the
// two predicates the pass reads a refusal back through. It asserts the wording by equality rather than
// by substring, because a message an operator acts on is behaviour and a substring test leaves most of
// it free to be emptied.

// theRefusedPartition is the name every row below refuses, and it is a real one from this package's
// own scheme so no row can pass by refusing a name the code could never meet.
var theRefusedPartition = rangeAt(0, 24*time.Hour).Name

// theOtherInstance is the second service sharing this database in every foreign-marker row of this
// step, container-free and tagged alike. catalogmarker_integration_test.go's foreignInstanceName is
// the same idea and cannot be read here: it is declared behind the integration tag, so a
// container-free row naming it would not compile. Like that one it is neither a prefix nor a suffix
// of ourInstance, so a comparison that only tested containment would still have to answer foreign.
const theOtherInstance = "reporting"

// theMarkerVerdicts is one row per state markerReadingOf can answer, plus the zero value no catalog
// can produce. Each names the phrase that identifies it, so a refusal rewritten to say something
// else fails the row named for it rather than passing on a neighbour's wording.
type markerVerdict struct {
	name    string
	reading markerReading
	// wantPhrase is the phrase that identifies this refusal, which the row asserts is present
	// alongside the whole-message equality: a rewrite that kept the message and moved the phrase
	// would leave readsAsAlreadyGone reading a message no refusal writes any more.
	wantPhrase string
	// want is the whole refusal, built from the constructor rather than transcribed, so a row
	// cannot ratify a message a run happened to produce.
	want error
}

var theMarkerVerdicts = []markerVerdict{
	{name: "this instance's own marker", reading: markerReading{state: markerOurs, named: ourInstance}},
	{name: "no marker at all", reading: markerReading{state: markerAbsent},
		wantPhrase: theUnmarkedPhrase, want: unmarkedPartition(theRefusedPartition)},
	{name: "a comment that is not a marker of this format",
		reading:    markerReading{state: markerUnreadable},
		wantPhrase: theUnreadablePhrase, want: unreadableMarkerOn(theRefusedPartition)},
	{name: "another instance's marker",
		reading:    markerReading{state: markerForeign, named: theOtherInstance},
		wantPhrase: theForeignMarkerPhrase,
		want:       foreignMarkerOn(theRefusedPartition, theOtherInstance, ourInstance)},
	{name: "a state no catalog can produce", reading: markerReading{},
		wantPhrase: theUnknownStatePhrase, want: unknownMarkerState(theRefusedPartition, "")},
}

// TestEachReasonToRefuseADropHasItsOwnAnswer is guard 1's three refusal states as three answers,
// alongside the fail-closed default. None is a fallthrough: an operator meeting one of these has to
// know whether the partition is unmarked, unreadable or another service's, because the three call
// for three different actions.
func TestEachReasonToRefuseADropHasItsOwnAnswer(t *testing.T) {
	for _, tc := range theMarkerVerdicts {
		t.Run(tc.name, func(t *testing.T) {
			got := refusalForMarker(tc.reading, theRefusedPartition, ourInstance)

			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("a drop was refused with %v, and this reading is this instance's own", got)
			case tc.want == nil:
				return
			case got == nil:
				t.Fatalf("a drop was permitted for a %s reading, want %v", tc.reading.state, tc.want)
			case got.Error() != tc.want.Error():
				t.Errorf("the refusal reads %q, want %q", got, tc.want)
			}
			if !strings.Contains(got.Error(), theRefusedPartition) {
				t.Errorf("the refusal %q does not name the partition it refused", got)
			}
			if !strings.Contains(got.Error(), tc.wantPhrase) {
				t.Errorf("the refusal %q does not carry %q, the phrase that identifies this "+
					"condition to the pass that met it", got, tc.wantPhrase)
			}
		})
	}
}

// TestTheMarkerVerdictsCoverEveryDeclaredStateAndTheZero keeps the grid above closed over
// ownershipmarker.go's own set, so a fifth state has to join it rather than falling silently into
// the default arm.
func TestTheMarkerVerdictsCoverEveryDeclaredStateAndTheZero(t *testing.T) {
	if want := len(markerStates) + 1; len(theMarkerVerdicts) != want {
		t.Fatalf("%d verdicts are written for %d declared marker states plus the zero one; add the "+
			"row with the state", len(theMarkerVerdicts), want)
	}
	for _, state := range append(slices.Clone(markerStates), markerState("")) {
		covered := slices.ContainsFunc(theMarkerVerdicts, func(row markerVerdict) bool {
			return row.reading.state == state
		})
		if !covered {
			t.Errorf("no verdict is written for the %q marker state", state)
		}
	}
}

// TestAForeignMarkerStillAnswersTheSentinelACallerTestsAgainst is why the foreign case wraps
// errors.go's own sentinel rather than spelling a fourth condition: a caller distinguishing "someone
// else's" from "unmarked" tests errors.Is, and the partition's name has to be in the message beside
// it because ErrForeignInstance's own wording is the schema-level claim ADR-3 makes at boot.
func TestAForeignMarkerStillAnswersTheSentinelACallerTestsAgainst(t *testing.T) {
	refused := foreignMarkerOn(theRefusedPartition, theOtherInstance, ourInstance)

	if !errors.Is(refused, ErrForeignInstance) {
		t.Errorf("the refusal %v does not answer %v", refused, ErrForeignInstance)
	}
	for _, named := range []string{theRefusedPartition, theOtherInstance, ourInstance} {
		if !strings.Contains(refused.Error(), named) {
			t.Errorf("the refusal %q does not name %s", refused, named)
		}
	}
	if errors.Is(unmarkedPartition(theRefusedPartition), ErrForeignInstance) {
		t.Errorf("an unmarked partition also answers %v, so the two conditions are one error with "+
			"two messages", ErrForeignInstance)
	}
}

// TestNoDropRefusalPhraseContainsAnother is what makes readsAsAlreadyGone a test of one refusal
// rather than of several: it reads the unmarked phrase back out of a rendered message, so a phrase
// that contained it would make an unrelated refusal read as a lost race and let the concession
// swallow a drop that was refused for a different reason.
func TestNoDropRefusalPhraseContainsAnother(t *testing.T) {
	if len(theDropRefusalPhrases) != 5 {
		t.Fatalf("%d refusal phrases are declared %v; update this count with the set",
			len(theDropRefusalPhrases), theDropRefusalPhrases)
	}
	for i, phrase := range theDropRefusalPhrases {
		for j, other := range theDropRefusalPhrases {
			if i != j && strings.Contains(other, phrase) {
				t.Errorf("%q contains %q, so a message carrying the first also reads as the second",
					other, phrase)
			}
		}
	}
}

// TestOnlyTheUnmarkedRefusalReadsAsALostDropRace is both sides of the concession's first clause. The
// accepted row is what a partition another replica dropped produces; every refused row is a condition that
// must reach the operator instead.
func TestOnlyTheUnmarkedRefusalReadsAsALostDropRace(t *testing.T) {
	if !readsAsAlreadyGone(unmarkedPartition(theRefusedPartition)) {
		t.Errorf("the unmarked refusal does not read as a lost race, so a replica that lost one "+
			"reports a failure instead of conceding: %v", unmarkedPartition(theRefusedPartition))
	}
	for _, refusal := range []error{
		unreadableMarkerOn(theRefusedPartition),
		foreignMarkerOn(theRefusedPartition, theOtherInstance, ourInstance),
		unattachedTable(theRefusedPartition),
		unknownMarkerState(theRefusedPartition, ""),
		lockTimedOut(droppingWork+theRefusedPartition, DefaultLockTimeout),
		nil,
	} {
		if readsAsAlreadyGone(refusal) {
			t.Errorf("%v reads as a lost race, and conceding it would leave the condition unreported "+
				"on every pass", refusal)
		}
	}
}
