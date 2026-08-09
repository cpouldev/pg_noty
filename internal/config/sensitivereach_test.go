package config

import (
	"strings"
	"testing"
)

// dedentedSecret is planted in a value whose continuation line is indented *less* than the line the
// value is anchored on and still more than the block enclosing it, which is what YAML permits a
// multi-line flow scalar and what an indentation-only walk cannot follow.
const (
	dedentedSecret = "PGNOTY-DEDENTED-CONTINUATION"
	dedentedKept   = "PGNOTY-DEDENTED-PUBLIC-SIBLING"
)

// TestAMultilineQuotedSecretIsRedactedWhereItsContinuationDedents is the leak the path-aware branch
// carried until blankIndentedContinuation was given the fallback's lexical answer as well as its
// own indentation one. Every document here is valid YAML that parses, so every one of them takes
// the branch that runs for every well-formed configuration file.
//
// The anchor line is indented six columns and each continuation five, which is legal because a
// multi-line flow scalar need only be indented past the block that encloses it -- `signing:` at four
// -- rather than past its own first line. The old walk stopped at the first line indented no further
// than the anchor and rendered everything after it verbatim.
func TestAMultilineQuotedSecretIsRedactedWhereItsContinuationDedents(t *testing.T) {
	for _, tc := range []struct {
		name     string
		document string
	}{
		{
			name:     "a double-quoted signing secret",
			document: signingSecrets("      secrets: \"PART-A\n     " + dedentedSecret + "\"\n"),
		},
		{
			name:     "a single-quoted signing secret",
			document: signingSecrets("      secrets: 'PART-A\n     " + dedentedSecret + "'\n"),
		},
		{
			name:     "a quoted key before the value",
			document: signingSecrets("      \"secrets\": \"PART-A\n     " + dedentedSecret + "\"\n"),
		},
		{
			// Three lines, so a fix that blanked exactly one continuation line still fails.
			name: "a secret spanning three lines",
			document: signingSecrets("      secrets: \"PART-A\n     PART-B\n" +
				"     " + dedentedSecret + "\"\n"),
		},
		{
			name: "a connection string password split across lines",
			document: "database:\n  url: \"postgres://noty:PART-A\n " +
				dedentedSecret + "@db.internal:5432/noty\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertParsesAndHides(t, tc.document, dedentedSecret)
		})
	}
}

// TestRedactionOfADedentedContinuationStopsWhereTheValueCloses is the other side of the guard above.
// The lexical state must reopen nothing once the quote closes, or every key below a multi-line
// secret would be blanked and a diagnostic would lose the context it exists to show.
func TestRedactionOfADedentedContinuationStopsWhereTheValueCloses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		document string
	}{
		{
			name: "a sibling key at the root after the quote closes",
			document: signingSecrets("      secrets: \"PART-A\n     "+dedentedSecret+"\"\n") +
				dedentedKept + ": kept\n",
		},
		{
			name: "a public sibling holding an apostrophe of its own",
			document: signingSecrets("      secrets: \"PART-A\n     "+dedentedSecret+"\"\n") +
				dedentedKept + ": it's kept\n",
		},
		{
			// A block scalar has no closing token, so indentation remains its only extent. This row
			// is the control for that shape and nothing more: its payload holds no byte that can
			// open lexical state, so it cannot distinguish the guard that keeps state from being
			// advanced inside a block. That partition is
			// TestABlockScalarsExtentIsItsIndentationWhateverItsBytesOpen's.
			name: "a block scalar's own indented extent",
			document: signingSecrets("      secrets:\n      - |\n        "+dedentedSecret+"\n") +
				dedentedKept + ": kept\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := assertParsesAndHides(t, tc.document, dedentedSecret)
			if !strings.Contains(rendered, dedentedKept) {
				t.Errorf("redaction ran past the value and took the public sibling with it:\n%s", rendered)
			}
		})
	}
}

// lexicalOpenersInsideABlockScalar are the bytes sensitiveContinuation.afterFrom has an arm for that
// *opens* state. Inside a block scalar every one of them is content: a block's extent is its
// indentation and nothing else, so none may carry the blanking past where the block ends.
var lexicalOpenersInsideABlockScalar = []struct {
	name string
	text string
}{
	{name: "an apostrophe", text: "'"},
	{name: "a double quote", text: `"`},
	{name: "a flow sequence opener", text: "["},
	{name: "a flow mapping opener", text: "{"},
}

// openingArmsOfTheStateMachine is how many arms of sensitiveContinuation.afterFrom open state: the
// two quote spellings and the two flow openers. Its closers and its comment indicator close or end
// state and cannot re-open one, so they are not this list's subject.
const openingArmsOfTheStateMachine = 4

// TestABlockScalarsExtentIsItsIndentationWhateverItsBytesOpen is the falsifiable form of
// blankIndentedContinuation's "the state is advanced only while it is open" guard.
//
// Each row plants, inside a block scalar, a byte that opens lexical state. With the guard the state
// stays closed, the walk stops where the block's indentation ends, and the public sibling below it
// survives; without the guard that byte re-opens a quote or a container, the walk never stops, and
// the sibling is blanked -- every diagnostic below a multi-line secret losing the context it exists
// to show. The row previously written for this guard carried no such byte, so removing the guard
// failed nothing.
func TestABlockScalarsExtentIsItsIndentationWhateverItsBytesOpen(t *testing.T) {
	for _, opener := range lexicalOpenersInsideABlockScalar {
		t.Run(opener.name, func(t *testing.T) {
			document := signingSecrets("      secrets:\n      - |\n        "+
				dedentedSecret+opener.text+"\n") + dedentedKept + ": kept\n"

			rendered := assertParsesAndHides(t, document, dedentedSecret)
			if !strings.Contains(rendered, dedentedKept) {
				t.Errorf("%s inside the block scalar re-opened lexical state, so the blanking ran past "+
					"the block and took the public sibling with it:\n%s", opener.name, rendered)
			}
		})
	}
}

// TestEveryOpenerTheStateMachineRecognisesIsPlantedInABlockScalar closes the row list above over the
// production arms it is quantified against, so a byte that stops opening state -- or a fifth one
// that starts -- fails here by name rather than leaving a row that cannot falsify what it is written
// for.
func TestEveryOpenerTheStateMachineRecognisesIsPlantedInABlockScalar(t *testing.T) {
	if got := len(lexicalOpenersInsideABlockScalar); got != openingArmsOfTheStateMachine {
		t.Fatalf("%d block-scalar openers for %d opening arms of sensitiveContinuation.afterFrom; add "+
			"the row with the arm", got, openingArmsOfTheStateMachine)
	}
	for _, opener := range lexicalOpenersInsideABlockScalar {
		if !noSensitiveContinuation.after(opener.text).open() {
			t.Errorf("%q opens no lexical state, so the row written for it cannot distinguish the guard "+
				"that stops the state being advanced inside a block", opener.text)
		}
	}
}

// assertParsesAndHides fails unless the document reaches the path-aware branch -- a document that
// stopped parsing would be answered by the fallback and prove nothing about this one -- and unless
// the rendered text hides the secret.
func assertParsesAndHides(t *testing.T, document, secret string) string {
	t.Helper()

	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("the document no longer parses, so it cannot exercise the path-aware branch: %+v\n%s",
			diags, document)
	}

	rendered := renderEveryLineOf(document)
	if strings.Contains(rendered, secret) {
		t.Fatalf("the path-aware branch quoted %s:\n%s", secret, rendered)
	}
	return rendered
}
