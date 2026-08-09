package config

import (
	"strings"
	"testing"
)

// The carriage-return marking: that it is aligned with the lines it describes, that it is computed
// correctly for every line-ending convention, and that both redaction branches consult it.
//
// It exists because the parser and a human disagree about what a line is, and two secret leaks came out of
// that gap -- one per redaction branch. The mechanism is one boolean per line, so a one-position shift or a
// missed CRLF normalisation silently misaligns it and a secret is rendered. Those are the mutations pinned
// here, because both survived the whole suite when the mechanism was first written.

// TestTheLineMarkingIsAlignedWithTheLines is the invariant the marking's one bounds guard rests on. Without
// it the guard is a hedge absorbing a disagreement nothing detects.
func TestTheLineMarkingIsAlignedWithTheLines(t *testing.T) {
	documents := map[string]string{
		"line feeds only":              "a: 1\nb: 2\nc: 3\n",
		"carriage returns only":        "a: 1\rb: 2\rc: 3\r",
		"windows line endings":         "a: 1\r\nb: 2\r\nc: 3\r\n",
		"mixed endings":                "a: 1\r\nb: 2\rc: 3\nd: 4\n",
		"consecutive carriage returns": "a: 1\r\r\rb: 2\n",
		"a carriage return at the end": "a: 1\r",
		"no trailing break":            "a: 1",
		"empty":                        "",
	}

	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte(document))

			if len(src.continuesPhysicalLine) != len(src.lines) {
				t.Fatalf("%d marks for %d lines (%q); the marking is misaligned, so every line after the "+
					"first disagreement is asked about the wrong break",
					len(src.continuesPhysicalLine), len(src.lines), src.lines)
			}
			if len(src.lines) > 0 && src.continuesTheLineAbove(1) {
				t.Error("the first line is marked as continuing one above it")
			}
		})
	}
}

// TestTheMarkingNamesExactlyTheCarriageReturnSplits is the marking's own correctness, per line rather than
// per document, and it is what the two surviving mutations of the first implementation fail.
//
// Dropping the CRLF normalisation makes a Windows file produce more marks than lines; returning the marking
// one entry short shifts every answer by a line. Both left the whole suite green, which is what an
// unasserted mechanism looks like from the outside.
func TestTheMarkingNamesExactlyTheCarriageReturnSplits(t *testing.T) {
	tests := []struct {
		name     string
		document string
		// want is one entry per line: true where that line was split off by a lone carriage return.
		want []bool
	}{
		{name: "line feeds break, and continue nothing", document: "a\nb\nc\n", want: []bool{false, false, false, false}},
		{name: "a lone return continues the line above", document: "a\rb\n", want: []bool{false, true, false}},
		{name: "CRLF is one break, not a continuation", document: "a\r\nb\r\n", want: []bool{false, false, false}},
		{name: "a return after a feed still continues", document: "a\n\rb\n", want: []bool{false, false, true, false}},
		{name: "two returns give two continuations", document: "a\r\rb\n", want: []bool{false, true, true, false}},
		{name: "mixed, so CRLF cannot be read as CR", document: "a\r\nb\rc\n", want: []bool{false, false, true, false}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte(tc.document))

			if len(src.lines) != len(tc.want) {
				t.Fatalf("%q split into %d lines %q, want %d", tc.document, len(src.lines), src.lines, len(tc.want))
			}
			for number, want := range tc.want {
				if got := src.continuesTheLineAbove(number + 1); got != want {
					t.Errorf("line %d (%q) continues the line above = %t, want %t; the marking is shifted "+
						"against the lines", number+1, src.lines[number], got, want)
				}
			}
		})
	}
}

// TestTheTwoLineViewsDisagreeWhereTheMechanismSaysTheyDo is what the whole mechanism rests on, asserted
// rather than assumed: the parser's view of a document and a human's differ exactly at a lone carriage
// return. Nothing watched that divergence, so a change collapsing the two views would remove the mechanism's
// reason to exist with every test still green.
func TestTheTwoLineViewsDisagreeWhereTheMechanismSaysTheyDo(t *testing.T) {
	const document = "a: 1\rb: 2\n"
	src := newSource("listeners.yaml", []byte(document))

	// The parser's view: two lines, because it counts the return as a break.
	if len(src.lines) != 3 {
		t.Fatalf("the document split into %d lines %q, want 3", len(src.lines), src.lines)
	}
	if !src.continuesTheLineAbove(2) {
		t.Fatal("line 2 is not marked as a split tail, so the two views no longer disagree here and the " +
			"marking has nothing to say")
	}

	// A human's view: one physical line, which is what splitting on line feeds alone answers.
	if physical := strings.Split(document, "\n"); len(physical) != 2 || physical[0] != "a: 1\rb: 2" {
		t.Errorf("the physical lines are %q; the divergence this mechanism exists for is gone", physical)
	}
}

// TestBothRedactionBranchesCoverAValueEndingAtACarriageReturn is the second leak's reproduction, and it is
// written across both branches because a class covered by one and not the other is exactly how it hid: the
// key-scoped fallback blanked the tail and the path-aware branch replaced the value and left it.
//
// `secrets: PART1AAA` ends exactly at the return, so the value matches what its parser line holds and reads
// as contained -- while its tail sits on the next parser line, on the same line the author wrote.
func TestBothRedactionBranchesCoverAValueEndingAtACarriageReturn(t *testing.T) {
	const secret = "PART1AAA"
	const tail = "other: BBB"
	document := "version: 1\nlisteners:\n- destination:\n    signing:\n      secrets: " + secret + "\r      " + tail + "\n"

	src := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(src)
	if !pathsAreResolvable(root, diags) {
		t.Fatal("the document does not reach the path-aware branch, so this covers one branch only")
	}

	branches := map[string][]string{
		"path-aware":          redactDeclaredPaths(src, root).text,
		"key-scoped fallback": redactBySensitiveKeyName(src.lines, src.continuesPhysicalLine),
	}

	for name, redacted := range branches {
		t.Run(name, func(t *testing.T) {
			rendered := strings.Join(redacted, "\n")

			if strings.Contains(rendered, secret) {
				t.Errorf("the value itself was rendered:\n%s", rendered)
			}
			if strings.Contains(rendered, tail) {
				t.Errorf("the tail past the carriage return was rendered; a value ending at a return is "+
					"contained on its parser line and not on the line its author wrote:\n%s", rendered)
			}
		})
	}
}
