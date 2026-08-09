package config

import "testing"

// This file pins the separation writtenstart.go is built on: where a value's text begins and how
// far back its redactable extent reaches are two answers, they coincide for every shape but one,
// and only the second one ever moves.
//
// Without it the separation is only a design. A later edit that widened `line` instead of
// `reachesBackTo` would close the same class -- the gap would be blanked either way -- and would be
// caught by nothing here: the golden corpus would report a moved caret, which reads as a rendering
// change rather than as the two questions being merged back into one. The rows below fail in the
// terms the design is stated in instead.
//
// Every wanted line is counted from the document theValueDeclaredSecretIn builds, whose body begins
// at line 6, and the count is written beside the row so a reader can check it without running
// anything.

// extentSeparationShape is one shape a value declared sensitive can be written in, and what the two
// answers must say about it. holdsAGap is the row's own claim about which side of the partition it
// falls on, asserted rather than assumed: a row that stopped writing a gap would otherwise keep
// reporting coverage of the class it was named for.
type extentSeparationShape struct {
	name              string
	body              string
	holdsAGap         bool
	wantReachesBackTo int
}

// extentSeparationShapes partitions the shapes a `secrets` value can be written in by whether an
// author can write anything between its key's colon and its own first token.
//
// The no-gap classes are here for two different reasons, and both sit on a boundary of the guard
// that decides whether to widen (`opener >= written.line`). A value written beside its key is that
// guard's equality case: its key's colon and its own first token are on one line, so `opener` and
// `line` are both 6 and only `>=` leaves the answer alone -- under `>` the extent would reach back
// to line 7, past the end of the value. A container written directly below its key is the case one
// further along, where the guard does widen and the widened answer lands back on the value's own
// line; a row that only wrote the two far-apart classes would leave both boundaries free.
var extentSeparationShapes = []extentSeparationShape{
	// Beside its key: the value's own text begins on line 6, and there is nothing above it.
	{name: "a scalar written beside its key", body: "      secrets: one\n", wantReachesBackTo: 6},
	{name: "a flow sequence written beside its key", body: "      secrets: [one, two]\n",
		wantReachesBackTo: 6},

	// Directly below its key: the key is line 6, the value line 7, and the block the colon opened
	// begins on line 7 too -- so the widened answer is the value's own line and nothing moves.
	{name: "a block sequence directly below its key",
		body: "      secrets:\n        - one\n", wantReachesBackTo: 7},
	{name: "a block mapping directly below its key",
		body: "      secrets:\n        a: one\n", wantReachesBackTo: 7},
	{name: "a flow sequence directly below its key",
		body: "      secrets:\n        [one, two]\n", wantReachesBackTo: 7},

	// A gap: the key is line 6, so the block it opened begins on line 7 whatever is written there
	// and however far below it the value's own first token ends up.
	{name: "a comment above a block sequence",
		body:      "      secrets:\n        # note\n        - one\n",
		holdsAGap: true, wantReachesBackTo: 7},
	{name: "a comment above a block mapping",
		body:      "      secrets:\n        # note\n        a: one\n",
		holdsAGap: true, wantReachesBackTo: 7},
	{name: "a comment above a flow mapping written below its key",
		body:      "      secrets:\n        # note\n        {a: one}\n",
		holdsAGap: true, wantReachesBackTo: 7},
	{name: "an anchor property above a block sequence",
		body:      "      secrets:\n        &name\n        - one\n",
		holdsAGap: true, wantReachesBackTo: 7},
	// Two lines of gap, so a widening that reached back one line rather than to the opener answers
	// 8 here and passes every row above.
	{name: "two comments above a block sequence",
		body:      "      secrets:\n        # one\n        # two\n        - x\n",
		holdsAGap: true, wantReachesBackTo: 7},
	// The key, the note and the value are all indented alike, which is why the opener is read from
	// the parser's token stream rather than from indentation (writtenstart.go).
	{name: "a comment above a block sequence at its key's own indentation",
		body:      "      secrets:\n      # note\n      - one\n",
		holdsAGap: true, wantReachesBackTo: 7},
}

// TestOnlyTheRedactableExtentMovesWhenAValueReachesBackToItsBlockOpener is the separation itself:
// per shape, the replacement position is the same from both answers, the unwidened answer's extent
// is its own line, and the widened one differs exactly where a gap is written.
func TestOnlyTheRedactableExtentMovesWhenAValueReachesBackToItsBlockOpener(t *testing.T) {
	gaps, coincidences := 0, 0

	for _, shape := range extentSeparationShapes {
		if shape.holdsAGap {
			gaps++
		} else {
			coincidences++
		}
		t.Run(shape.name, func(t *testing.T) {
			text, value := theValueDeclaredSecretIn(t, shape.body)
			plain := writtenStartOf(text, value)
			widened := writtenStartReachingItsBlockOpener(text, value)

			assertOnlyTheExtentMoved(t, plain, widened)
			assertTheExtentReachesBackTo(t, shape, plain, widened)
		})
	}

	if gaps == 0 || coincidences == 0 {
		t.Fatalf("the table holds %d shapes writing a gap and %d writing none; the claim is that the "+
			"two answers differ on one partition and coincide on the other, so an empty side makes "+
			"half of it vacuous", gaps, coincidences)
	}
}

// assertOnlyTheExtentMoved is the half of the separation the golden corpus would report as a moved
// caret rather than as a merged question. Every field but the extent is what redactValue replaces
// at and what a diagnostic inherits, so a widening applied to any of them fails here by name.
func assertOnlyTheExtentMoved(t *testing.T, plain, widened writtenStart) {
	t.Helper()

	if plain.line != widened.line {
		t.Errorf("the value's text begins on line %d unwidened and line %d widened; reaching back to "+
			"the block opener must move the extent alone, and this is the line the redactor replaces "+
			"at", plain.line, widened.line)
	}
	if plain.column != widened.column {
		t.Errorf("the value's text begins at column %d unwidened and column %d widened; the two "+
			"answers must agree about the replacement column", plain.column, widened.column)
	}
	if plain.beginsOnALineOfItsOwn != widened.beginsOnALineOfItsOwn {
		t.Errorf("the anchor line is the value's own = %t unwidened and %t widened; the reach walk "+
			"reads this to decide which lines are inside the value and must get one answer",
			plain.beginsOnALineOfItsOwn, widened.beginsOnALineOfItsOwn)
	}
}

// assertTheExtentReachesBackTo is the other half: what each answer says about the extent, and which
// side of the partition the shape falls on.
func assertTheExtentReachesBackTo(
	t *testing.T,
	shape extentSeparationShape,
	plain, widened writtenStart,
) {
	t.Helper()

	if plain.reachesBackTo != plain.line {
		t.Errorf("writtenStartOf reports an extent reaching back to line %d from line %d; the "+
			"unwidened answer is what an element of a container gets, and reaching back from one "+
			"would erase the elements written above it (sensitiveextent.go)",
			plain.reachesBackTo, plain.line)
	}
	if widened.reachesBackTo != shape.wantReachesBackTo {
		t.Errorf("the extent reaches back to line %d, want line %d counted from the document",
			widened.reachesBackTo, shape.wantReachesBackTo)
	}
	if reachesBack := widened.reachesBackTo < widened.line; reachesBack != shape.holdsAGap {
		t.Errorf("the widened extent reaches past the value's own line = %t, want %t: this shape "+
			"writes %s between its key's colon and its own first token",
			reachesBack, shape.holdsAGap, gapDescription(shape.holdsAGap))
	}
}

func gapDescription(holdsAGap bool) string {
	if holdsAGap {
		return "author-written runes"
	}
	return "nothing"
}
