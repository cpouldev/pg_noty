package schema

import (
	"strings"
	"testing"
)

// This file is the refusing half of the bound reader: that every declared reason is reached by a
// real input, that the refusal says enough to act on, and that nothing the input supplies can turn
// the refusal off.

func TestTheBoundVocabularyIsPinnedAtFourAndEveryReasonIsReached(t *testing.T) {
	if len(boundFaults) != 4 {
		t.Fatalf("the reader declares %d bound faults %v; update this count with the set, or a "+
			"reason nothing produces reads as covered", len(boundFaults), boundFaults)
	}

	produced := map[boundFault]int{}
	for _, tc := range recordedBounds {
		_, fault := rangeFrom(tc.written, "observed_partition")
		produced[fault]++
	}
	for _, fault := range boundFaults {
		if produced[fault] == 0 {
			t.Errorf("no recorded bound produces %q, so that branch is asserted by nothing", fault)
		}
	}
}

// TestAnUnreadableBoundIsRefusedRatherThanLeftOutOfTheObservation reaches the refusal and reads its
// diagnostic, rather than only proving no current input gets there: delete the message and an
// enumeration-only test still passes.
//
// The three clauses are asserted separately because they answer different questions. The partition
// name is what an operator acts on; the fault is which rule refused; and the consequence is what
// stops a later reader "simplifying" the refusal into a skip.
func TestAnUnreadableBoundIsRefusedRatherThanLeftOutOfTheObservation(t *testing.T) {
	const written = "FOR VALUES FROM ('2027-01-02 00:00:00+00') TO (MAXVALUE)"

	refusal := unreadableBound("events_20270102T000000Z", written, boundHasNoFiniteEnd).Error()

	for _, want := range []string{
		"events_20270102T000000Z",
		string(boundHasNoFiniteEnd),
		written,
		"incomplete",
	} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal reads %q, which does not name %q", refusal, want)
		}
	}
}

// TestTheBoundRefusalIsNotGatedOnAnythingTheInputSupplies is the unconditional half. Each probe
// holds one of the reader's own literals -- the strings a guard written as "does this look like a
// bound at all?" would be switched off by -- and every one of them is still refused, because none
// of them is a bound.
func TestTheBoundRefusalIsNotGatedOnAnythingTheInputSupplies(t *testing.T) {
	for _, probe := range []struct{ name, written string }{
		{name: "nothing at all", written: ""},
		{name: "one space", written: " "},
		{name: "the DEFAULT expression with a trailing space", written: defaultBoundExpression + " "},
		{name: "the opening marker alone", written: rangeBoundOpening},
		{name: "the separator alone", written: rangeBoundMiddle},
		{name: "the closing marker alone", written: rangeBoundClosing},
		{name: "every marker in the wrong order",
			written: rangeBoundClosing + rangeBoundMiddle + rangeBoundOpening},
		{name: "the unbounded-below spelling alone", written: unboundedBelow},
		{name: "the unbounded-above spelling alone", written: unboundedAbove},
		{name: "an opening marker closed with no instants in it",
			written: rangeBoundOpening + rangeBoundMiddle + rangeBoundClosing},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if _, fault := rangeFrom(probe.written, "observed_partition"); fault == boundOK {
				t.Errorf("rangeFrom(%s) read a Range out of it; the refusal is gated on something "+
					"the input supplies, and the shapes it exists to catch are the unmeasured ones",
					probe.written)
			}
		})
	}
}
