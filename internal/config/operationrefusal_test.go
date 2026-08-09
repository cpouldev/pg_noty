package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// What the operations fold declines, and what it leaves behind when it does. Each refusal is a
// reason of its own, so each has a case of its own here; what the fold accepts is
// listsugar_test.go's subject.

// TestAListEntryThatCannotBecomeAKeyIsRefusedRatherThanFolded is the fold's fail-closed branch.
// A collection cannot be written in a key position at all, so the reduction stops and says so
// rather than leaving the list for a stage that was promised one shape.
func TestAListEntryThatCannotBecomeAKeyIsRefusedRatherThanFolded(t *testing.T) {
	entries := map[string]struct {
		document string
		column   int
	}{
		// `operations: [` is thirteen runes, so the nested collection begins at rune 14.
		"a nested list":    {document: "operations: [[insert]]\n", column: 14},
		"a nested mapping": {document: "operations: [{insert: {}}]\n", column: 14},
	}

	for name, entry := range entries {
		t.Run(name, func(t *testing.T) {
			_, root, diags := stageE(t, entry.document, nil)

			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want the one refusing an entry that is not a name",
					len(diags), messagesOf(diags))
			}
			assertDiagnostic(t, diags[0], unnameableOperation, 1, entry.column)
			if _, stillAList := valueOfEntry(root, 0).(*ast.SequenceNode); !stillAList {
				t.Error("the refused list was folded anyway; a refused node keeps what its author wrote")
			}
		})
	}
}

// TestEveryUnfoldableListEntryIsNamedInOneRun is the fold's half of the stage's accumulate-then-
// stop policy: returning at the first unusable element would report one of two offending tokens.
func TestEveryUnfoldableListEntryIsNamedInOneRun(t *testing.T) {
	// `operations: [` is thirteen runes, so the first nested list begins at rune 14; `[insert], `
	// is ten more, so the second begins at rune 24.
	_, _, diags := stageE(t, "operations: [[insert], [update]]\n", nil)

	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics %q, want one per element that cannot become a name",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], unnameableOperation, 1, 14)
	assertDiagnostic(t, diags[1], unnameableOperation, 1, 24)
}

// TestAKeyThisStageIntroducesIsCheckedForAReferenceTheWayStageDChecksTheRest closes AC #6 over
// what this stage expands.
//
// A list element is a value, so stage D substituted into it -- and folding then writes those
// bytes into a key position, after the only check that looks at keys has run. The environment
// answers with a reference here, which stage D's opacity rule forbids it from having scanned.
func TestAKeyThisStageIntroducesIsCheckedForAReferenceTheWayStageDChecksTheRest(t *testing.T) {
	// A block list, because `[${OPERATION}]` is a flow sequence whose `{` opens a flow mapping.
	// Line 2 is `  - ${OPERATION}`, so the element begins at rune 5.
	_, root, diags := stageE(t, "operations:\n  - ${OPERATION}\n", map[string]string{"OPERATION": "${INSERT}"})

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a key that holds a reference",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], referenceInKey, 2, 5)
	if _, stillAList := valueOfEntry(root, 0).(*ast.SequenceNode); !stillAList {
		t.Error("the refused list was folded anyway, so the substituted bytes reached a key position")
	}
}

// TestAnIntroducedKeyNobodyCanReadIsRefusedRatherThanFolded asserts the fold's third refusal
// itself rather than only its absence.
//
// No document reaches it: an alias is the one key shape keyTextOf declines, and the fold answers
// an alias element before it asks for a key. So the branch's state is constructed directly, by
// handing operationKeyOf the shape the caller filters out -- which is what makes the refusal
// real. Without this, deleting the return leaves the suite green and a later element shape that
// keyTextOf cannot read would be folded into a key nobody can check for `${`.
func TestAnIntroducedKeyNobodyCanReadIsRefusedRatherThanFolded(t *testing.T) {
	unreadable := valueOfEntry(parsedRoot(t, "ops: &ops name\nuse: *ops\n"), 1)

	named, why, refused := operationKeyOf(unreadable)

	if !refused {
		t.Fatalf("operationKeyOf accepted %T; a key nobody can read cannot be cleared of holding a reference", unreadable)
	}
	if named != nil {
		t.Errorf("operationKeyOf answered %T with a key; a key nobody can read has no name to become", unreadable)
	}
	if why != unreadableKey {
		t.Errorf("the refusal reads %q, want %q", why.message, unreadableKey.message)
	}
}

// TestAnOperationNamedTwiceInOneListIsRefusedRatherThanFolded keeps the fold from writing a
// mapping that holds one key twice.
//
// The parser's own duplicate-key detection (R42) ran at stage B, over the keys the author wrote.
// A repeated list element is not one of those: it becomes a key here, after that detection, so a
// fold that accepted it would hand the decode a duplicate R42 never saw and turn a diagnostic
// about the author's list into one about the library's map.
func TestAnOperationNamedTwiceInOneListIsRefusedRatherThanFolded(t *testing.T) {
	// `operations: [` is thirteen runes, so the first insert begins at rune 14 and the second,
	// eight runes later, at rune 22. The refusal names the repeat rather than the first
	// occurrence, because the repeat is the token the author has to delete.
	_, root, diags := stageE(t, "operations: [insert, insert]\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a repeated operation",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], repeatedOperation(1), 1, 22)
	if _, stillAList := valueOfEntry(root, 0).(*ast.SequenceNode); !stillAList {
		t.Error("the refused list was folded anyway, so a mapping holding one key twice reached the decode")
	}
}

// TestARepeatedOperationNamesTheLineTheFirstOneIsOn is the wording half of the case above, and it
// is written as a block list because the three flow-style cases beside it put both elements on line
// 1 -- where an implementation that named line 1 whatever it found would pass every one of them.
//
// The convention is the parser's: a duplicate is reported at the repeat and names where the first
// one is, so the author is shown the token to delete and the one to keep.
func TestARepeatedOperationNamesTheLineTheFirstOneIsOn(t *testing.T) {
	// 1 `operations:`, 2 `  - insert`, 3 `  - update`, 4 `  - insert`. The repeat is on line 4 and
	// its `insert` begins at rune 5.
	_, _, diags := stageE(t, "operations:\n  - insert\n  - update\n  - insert\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a repeated operation",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], repeatedOperation(2), 4, 5)
}

// TestAnOperationNamedTwiceIsOneKeyInEitherSpelling reads the repeat through the same answer the
// merge expansion reads it through, so `[insert, !!str insert]` is the same mistake as
// `[insert, insert]` rather than a spelling that slips past.
func TestAnOperationNamedTwiceIsOneKeyInEitherSpelling(t *testing.T) {
	// The column is left to the case above: a tagged element's own token is reported one rune
	// early (TestGoccyReportsATaggedValueOneColumnBeforeItBegins), and what this row is about is
	// which mistake the two spellings are, not where the caret lands.
	_, _, diags := stageE(t, "operations: [insert, !!str insert]\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a repeated operation",
			len(diags), messagesOf(diags))
	}
	if diags[0].Msg != repeatedOperation(1).message {
		t.Errorf("Msg = %q, want %q; the two spellings name one operation", diags[0].Msg, repeatedOperation(1).message)
	}
}

// TestAnOperationListNamingOneValueInTwoRenderingsIsRefused is the same dimension at the fold's
// duplicate check, which reads the same answer. `[16, 0x10]` names one operation twice, so it
// folds to a mapping holding one key twice -- the shape Note 17 promises Step 6 never receives.
func TestAnOperationListNamingOneValueInTwoRenderingsIsRefused(t *testing.T) {
	// `operations: [` is thirteen runes, so the second element begins at rune 18.
	_, _, diags := stageE(t, "operations: [16, 0x10]\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a repeated operation",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], repeatedOperation(1), 1, 18)
}
