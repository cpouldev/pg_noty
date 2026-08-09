package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// What the committed corpus proves about the diagnostics an anchor produces, as opposed to the
// values it carries: that a fault found while an anchored mapping is being reduced keeps a real
// position and renders a snippet, and that one broken anchor read by two listeners reaches its
// author once (AC #32's last clause and ADR-3's).
//
// It is a file of its own because the two subjects fail for different reasons: a value clause
// fails when the expansion drops content, a diagnostic clause when it drops a token.

// anchoredError is AC #32's last clause on one fixture, loaded once for the two behaviours the
// clause names. Both are asserted, and asserted separately, so a failure says which one broke.
//
// The error is stage E's own, because stages F to I are unwired: it is raised while the anchored
// mapping is being reduced, which is the moment a position could be lost. Step 8 should re-assert
// both halves against a semantic rule once one exists.
func anchoredError(t *testing.T) ([]byte, Error) {
	t.Helper()

	path := filepath.Join(invalidCorpus, "error_inside_an_anchored_mapping.yaml")
	document := readFixtureBytes(t, path)

	_, _, errs := Parse(document, filepath.Base(path), MapEnv(nil))
	if len(errs) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one inside the anchored mapping", len(errs), messagesOf(errs))
	}
	return document, errs[0]
}

// TestAnErrorInsideAnAnchoredMappingKeepsARealPosition is the first half: expansion is the
// operation most likely to lose a token, and a lost token reports as a zero position.
//
// The exact rune is asserted rather than a range. A caret that drifted one line up still lands in
// the document and still points at a line the renderer would quote, so a range check passes for a
// diagnostic sending its reader to the wrong place -- which leaves the position held by the
// golden alone.
func TestAnErrorInsideAnAnchoredMappingKeepsARealPosition(t *testing.T) {
	_, raised := anchoredError(t)

	// Line 9 of the fixture is `        <<: *missing_defaults`: eight spaces, `<<` at rune 9,
	// the colon at 11, the separating space at 12, so the alias the refusal is about begins at
	// rune 13.
	const wantLine, wantColumn = 9, 13

	if raised.Line != wantLine || raised.Col != wantColumn {
		t.Errorf("the diagnostic is at %d:%d, want %d:%d -- the alias inside the anchored mapping",
			raised.Line, raised.Col, wantLine, wantColumn)
	}
}

// TestAnErrorInsideAnAnchoredMappingRendersANonEmptySnippet is the second half, and it is not
// implied by the first: a position can be in range and still point at a line holding no part of
// what the diagnostic is about, which renders as a snippet quoting the wrong text.
// It asserts the *marked* line rather than the presence of the text, because the renderer quotes
// context lines around the caret: a snippet whose caret had drifted to line 7 or 8 would still
// contain the offending line, as one of the lines it quotes for context.
func TestAnErrorInsideAnAnchoredMappingRendersANonEmptySnippet(t *testing.T) {
	document, raised := anchoredError(t)

	rendered := Errors{raised}.Render(document)
	if !strings.Contains(rendered, ">  9 |         <<: *missing_defaults") {
		t.Errorf("the rendered snippet does not mark the offending line:\n%s", rendered)
	}
}

// TestTwoListenersAliasingOneBrokenAnchorYieldOneDiagnostic is ADR-3's consequence, and the
// second half is what makes it a claim about the mechanism rather than about the count: stage E
// raises one diagnostic per listener that merged the broken anchor, and de-duplication on
// (File, Line, Col, Msg) is what collapses them.
//
// Anchoring on the anchor's own value is what puts the two on one position. An alias-specific
// suppression would give the same count here and would fail this test, because the stage's own
// output would then hold one diagnostic rather than two.
func TestTwoListenersAliasingOneBrokenAnchorYieldOneDiagnostic(t *testing.T) {
	path := filepath.Join(invalidCorpus, "broken_anchor_aliased_twice.yaml")
	document := readFixtureBytes(t, path)

	_, _, raw := stageE(t, string(document), nil)
	if len(raw) != 2 {
		t.Fatalf("stage E raised %d diagnostics %q, want one per listener that merged the anchor",
			len(raw), messagesOf(raw))
	}

	_, _, errs := Parse(document, filepath.Base(path), MapEnv(nil))
	if len(errs) != 1 {
		t.Fatalf("Parse returned %d diagnostics %q, want the two collapsed to one", len(errs), messagesOf(errs))
	}
	if errs[0].Line != raw[0].Line || errs[0].Col != raw[0].Col {
		t.Errorf("the surviving diagnostic is at %d:%d, want the shared %d:%d the collapse rests on",
			errs[0].Line, errs[0].Col, raw[0].Line, raw[0].Col)
	}
}
