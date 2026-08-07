package schema

import (
	"errors"
	"strings"
	"testing"
)

// This file is ADR-3's marker policy asserted where it is decided rather than where it is observed:
// the four states a schema's ownership marker can be in, and the answer step 4 gives each. Two of
// the four are named success criteria and reach a real catalog in
// bootstrapownership_integration_test.go; all four are here, because a policy asserted only through
// the states a document happens to produce leaves the others free to change.

// theClaimingInstance and theForeignInstance are the two names the foreign case needs to keep
// apart. An operator cannot tell a misconfigured instance name from two services sharing one schema
// unless the refusal names both.
const (
	theClaimingInstance = "noty"
	theForeignInstance  = "other_service"
)

// TestEveryOwnershipStateHasItsOwnAnswer ranges over the declared set rather than over a list
// copied here, so a fifth state joins this assertion by joining ownershipmarker.go. Proceeding and
// refusing are asserted per state rather than as "something happened", so a refusal for the wrong
// reason fails.
func TestEveryOwnershipStateHasItsOwnAnswer(t *testing.T) {
	if len(markerStates) != 4 {
		t.Fatalf("%d marker states %v are declared; update this expectation with the set, or a "+
			"state step 4 must answer is outside this assertion", len(markerStates), markerStates)
	}

	run := boot{cfg: configWithSchema(theClaimingInstance)}
	for _, state := range markerStates {
		t.Run(string(state), func(t *testing.T) {
			refusal := run.ownershipRefusal(markerReading{state: state, named: theForeignInstance})

			switch state {
			case markerOurs, markerAbsent:
				assertBootProceeds(t, state, refusal)
			case markerForeign:
				assertForeignRefusal(t, refusal)
			case markerUnreadable:
				assertUnreadableRefusal(t, refusal)
			}
		})
	}
}

// assertBootProceeds is ADR-3's claimable half: an unmarked schema is the supported
// minimal-privilege path a DBA pre-creating one takes, and refusing it would break the path this
// package documents.
func assertBootProceeds(t *testing.T, state markerState, refusal error) {
	t.Helper()

	if refusal != nil {
		t.Errorf("a schema whose marker reads %q was refused with %v; ADR-3 claims it rather than "+
			"refusing it, and a DBA pre-creating the schema takes exactly this path", state, refusal)
	}
}

// assertForeignRefusal is the refused half, asserted as the sentinel rather than as any error, so a
// refusal arriving for another reason fails here.
func assertForeignRefusal(t *testing.T, refusal error) {
	t.Helper()

	if !errors.Is(refusal, ErrForeignInstance) {
		t.Fatalf("a schema marked for another instance answered %v, want %v", refusal, ErrForeignInstance)
	}
	for _, named := range []string{theForeignInstance, theClaimingInstance} {
		if !strings.Contains(refusal.Error(), named) {
			t.Errorf("the refusal %q does not name %q; without both an operator cannot tell a "+
				"misconfigured instance name from two services sharing one schema", refusal, named)
		}
	}
}

// assertUnreadableRefusal is the third answer ADR-3 insists on. Defaulting it to either of the other
// two is a fail-open, so it is asserted to be a refusal *and* to be a different one from the foreign
// case.
func assertUnreadableRefusal(t *testing.T, refusal error) {
	t.Helper()

	if refusal == nil {
		t.Fatal("a schema carrying a comment this format cannot read was claimed; ADR-3 makes it a " +
			"third answer, and reading it as unmarked claims a schema on a technicality")
	}
	if errors.Is(refusal, ErrForeignInstance) {
		t.Errorf("an unreadable marker answered %v, which is the answer for a marker naming another "+
			"instance; a comment nobody can read names none", ErrForeignInstance)
	}
}

// TestAnOwnershipStateThisBootCannotNameIsRefusedRatherThanClaimed reaches step 4's fail-closed arm
// directly, because nothing markerReadingOf produces can reach it today. Without this, deleting
// that arm's refusal and returning nil instead would keep every row above green while a schema in a
// state this boot cannot name was claimed anyway.
func TestAnOwnershipStateThisBootCannotNameIsRefusedRatherThanClaimed(t *testing.T) {
	const invented = markerState("a state ownershipmarker.go does not declare")

	run := boot{cfg: configWithSchema(theClaimingInstance)}
	refusal := run.ownershipRefusal(markerReading{state: invented})

	if refusal == nil {
		t.Fatal("a marker state this boot cannot name was claimed rather than refused")
	}
	if !strings.Contains(refusal.Error(), string(invented)) {
		t.Errorf("the refusal %q does not name the state it could not act on", refusal)
	}
	unreadable := run.ownershipRefusal(markerReading{state: markerUnreadable})
	if refusal.Error() == unreadable.Error() {
		t.Errorf("an unnameable state is refused in the same words as an unreadable marker (%q), so "+
			"the two conditions are one answer", refusal)
	}
}
