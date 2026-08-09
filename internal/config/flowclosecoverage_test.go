package config

import (
	"fmt"
	"strings"
	"testing"
)

// This file asserts, at document level, what a flow close leaves covered on the line below it.
//
// Three different mechanisms can keep such a line covered, and only one of them is the tainted-close
// provenance sensitiveContinuation carries: the fallback blanks a shape it cannot read as a key
// whatever the lexical state says, and a line the parser split off by a lone carriage return is
// covered by continuesTheLineAbove for the same reason. Naming all sixteen rows of the cross for the
// tainted close therefore claimed a partition twelve of them cannot reach -- measured: with
// taintedClose forced false, exactly the four rows below fail and the other twelve pass. So each
// dimension records whether the tainted close is what decides it, and only the rows where it is are
// named for it.

const (
	hiddenAfterAFlowClose = "PGNOTYTAINTEDCLOSEHIDDENTEXT"
	publicAfterAFlowClose = "PGNOTYTAINTEDCLOSEPUBLICTEXT"
)

var lineShapesBelowAnUntrustedClose = []struct {
	name string
	line string
	// readableAsAKey reports whether the fallback can read this shape as a key of its own. Only a
	// shape it can read would be kept without the tainted close, so only those rows distinguish it;
	// the others it blanks for having no readable key at all.
	readableAsAKey bool
}{
	{name: "compact key", line: hiddenAfterAFlowClose + ":" + hiddenAfterAFlowClose},
	{name: "readable key", line: hiddenAfterAFlowClose + ": " + hiddenAfterAFlowClose,
		readableAsAKey: true},
	{name: "quoted key", line: `"` + hiddenAfterAFlowClose + `": ` + hiddenAfterAFlowClose,
		readableAsAKey: true},
	{name: "plain content", line: hiddenAfterAFlowClose},
}

var endingsBelowAnUntrustedClose = []struct {
	name string
	text string
	// leavesCoverageToTheTaintedClose reports whether the tainted close is the only mechanism left.
	// A lone carriage return makes the line below a physical continuation of this one, which
	// continuesTheLineAbove covers whatever the lexical state holds.
	leavesCoverageToTheTaintedClose bool
}{
	{name: "line feed", text: "\n", leavesCoverageToTheTaintedClose: true},
	{name: "carriage return", text: "\r"},
}

// taintedCloseDecidedRows is how many rows of the cross the tainted close alone decides: two
// container kinds, times the one ending that leaves the coverage to it, times the two shapes the
// fallback could otherwise read.
const taintedCloseDecidedRows = 2 * 1 * 2

func documentBelowAFlowClose(kind byte, closer, ending, next string) string {
	return signingSecrets("      secrets: " + string(kind) + "first\n" +
		"      other: continued" + closer + "tainted" + ending +
		next + "\n" + publicAfterAFlowClose + ": public\n")
}

// assertCoveredBelowAFlowClose is the one claim both tests below make, so the partition a row
// belongs to changes which rows run it and never what it asserts.
func assertCoveredBelowAFlowClose(t *testing.T, kind byte, ending, next, shape string) {
	t.Helper()

	rendered := renderEveryLineOf(documentBelowAFlowClose(kind, flowClosersOfDepth(kind, 1), ending, next))
	if strings.Contains(rendered, hiddenAfterAFlowClose) {
		t.Errorf("a %s below an untrusted %c close was rendered", shape, kind)
	}
	if !strings.Contains(rendered, publicAfterAFlowClose) {
		t.Errorf("the readable control below a %s was removed for %c", shape, kind)
	}
}

// TestAnUntrustedFlowCloseCoversAKeyTheFallbackCouldOtherwiseRead is the tainted close's own
// partition: the rows where nothing else keeps the line below covered. Every one of them fails with
// taintedClose forced false.
func TestAnUntrustedFlowCloseCoversAKeyTheFallbackCouldOtherwiseRead(t *testing.T) {
	rows := 0
	for _, kind := range []byte{'[', '{'} {
		for _, ending := range endingsBelowAnUntrustedClose {
			for _, next := range lineShapesBelowAnUntrustedClose {
				if !ending.leavesCoverageToTheTaintedClose || !next.readableAsAKey {
					continue
				}
				rows++
				t.Run(fmt.Sprintf("%c/%s/%s", kind, ending.name, next.name), func(t *testing.T) {
					assertCoveredBelowAFlowClose(t, kind, ending.text, next.line, next.name)
				})
			}
		}
	}
	// Without this the partition could be emptied by clearing a flag and the test would still pass,
	// reporting coverage of the mechanism it is named for from no rows at all.
	if rows != taintedCloseDecidedRows {
		t.Fatalf("the tainted close decides %d rows, want the %d its two dimensions declare",
			rows, taintedCloseDecidedRows)
	}
}

// TestEveryLineShapeBelowAnUntrustedFlowCloseStaysCovered is the containment claim over the whole
// cross, named for containment rather than for the tainted close. Twelve of its sixteen rows are
// answered by the fallback's unreadable-key handling or by continuesTheLineAbove and pass with
// taintedClose removed; they are kept because what they assert is worth asserting, and they are not
// evidence about the tainted close.
func TestEveryLineShapeBelowAnUntrustedFlowCloseStaysCovered(t *testing.T) {
	for _, kind := range []byte{'[', '{'} {
		for _, ending := range endingsBelowAnUntrustedClose {
			for _, next := range lineShapesBelowAnUntrustedClose {
				t.Run(fmt.Sprintf("%c/%s/%s", kind, ending.name, next.name), func(t *testing.T) {
					assertCoveredBelowAFlowClose(t, kind, ending.text, next.line, next.name)
				})
			}
		}
	}
}

// TestATrustedFlowCloseLeavesTheLineBelowItReadable is the other side of the guard: a close whose
// suffix is trusted ends the value, and the key below it keeps the context a diagnostic exists to
// show.
func TestATrustedFlowCloseLeavesTheLineBelowItReadable(t *testing.T) {
	for _, kind := range []byte{'[', '{'} {
		document := signingSecrets("      secrets: " + string(kind) + "first\n" +
			"      other: continued" + flowClosersOfDepth(kind, 1) + "\n" +
			publicAfterAFlowClose + ": public\n")
		if rendered := renderEveryLineOf(document); !strings.Contains(rendered, publicAfterAFlowClose) {
			t.Errorf("a trusted %c close removed the readable control below it", kind)
		}
	}
}
