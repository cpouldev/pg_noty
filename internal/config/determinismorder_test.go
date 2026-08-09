package config

import (
	"strings"
	"testing"
)

// This file is the diagnostic ordering key the determinism gate checks against, and the two-sided
// proof that the check is falsifiable. It is separate from determinism_test.go only because that
// file reached its mechanical line budget; the subject is the same one.

// TestTheOrderCheckRejectsATiedPairAndAcceptsOneMsgSeparates keeps the strictness above from being a
// claim no input can violate, and keeps Msg in the key load-bearing. Two diagnostics agreeing on
// every level are the state that leaves rendered order to whichever rule ran first, and the nearest
// pair that must be accepted is one Msg alone separates -- which a key stopping at Path would read
// as a tie.
func TestTheOrderCheckRejectsATiedPairAndAcceptsOneMsgSeparates(t *testing.T) {
	at := Error{File: "listeners.yaml", Line: 1, Col: 1, Path: "version"}
	tied := Errors{at, at}
	if issues := diagnosticOrderIssues(tied); len(issues) != 1 {
		t.Errorf("issues = %v, want one; a tied pair leaves the order to whichever rule ran first",
			issues)
	}

	separated := Errors{withMessage(at, "must be 1"), withMessage(at, "must be an integer")}
	if issues := diagnosticOrderIssues(separated); len(issues) != 0 {
		t.Errorf("issues = %v, want none; Msg is the level that breaks the tie the others leave",
			issues)
	}
}

func withMessage(at Error, message string) Error {
	at.Msg = message
	return at
}

// compareDiagnosticTuple is the declared key, written out here rather than borrowed from production
// so that the order the package promises is what this checks. Msg is the last level, because without
// it two diagnostics can tie and the check would prove monotonicity where totality is claimed.
func compareDiagnosticTuple(a, b Error) int {
	if a.File != b.File {
		return strings.Compare(a.File, b.File)
	}
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	if a.Col != b.Col {
		return a.Col - b.Col
	}
	if a.Path != b.Path {
		return strings.Compare(a.Path, b.Path)
	}
	return strings.Compare(a.Msg, b.Msg)
}
