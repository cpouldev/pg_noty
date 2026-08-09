package config

import (
	"strings"
	"testing"
)

// The pinned `${…}` grammar as one table: how a reference is written, what it stands for once
// the environment has been consulted, and which spellings are refused (envreference.go,
// envexpansion.go).
//
// Every row runs through the stage rather than through the scanner alone, so it also proves
// the recursion reached the value it was written in. The readers it runs through -- stageD,
// textIn and messagesOf -- are declared in interpolate_test.go.

// grammarSecret is what a resolved reference stands for in the rows below: a value a
// configuration would really hold, so a row that substituted the wrong thing is unmistakable.
const grammarSecret = "postgres://noty:pw@db.internal/noty"

// grammarCases is AC #3's whole grammar, so a future change to it is one row rather than a new
// function. It is declared here rather than inside the test that walks it so that the walk
// stays short enough to read at a glance.
//
// Every want is derived from the pinned grammar and the row's own environment, never from a
// run: `${NAME}` stands for the variable's value, `${NAME:-default}` for the default
// whenever the variable holds no text, `$${` for the two characters `${`, and every other
// unescaped `${…}` for a diagnostic that leaves the value exactly as it was written.
//
// A row omitting vars runs in the empty environment, where every variable is unset, which is
// what MapEnv documents a nil map to mean.
var grammarCases = []struct {
	name    string
	written string
	vars    map[string]string
	want    string
	faults  []string
}{
	// The six accepting forms, and the three environment states each of the two resolving
	// forms has to keep apart.
	{name: "a set variable substitutes its value",
		written: "${NAME}", vars: map[string]string{"NAME": grammarSecret}, want: grammarSecret},
	{name: "a variable set to the empty string substitutes the empty string",
		written: "${NAME}", vars: map[string]string{"NAME": ""}, want: ""},
	{name: "an unset variable with no default is a diagnostic and never an empty value",
		written: "${NAME}", want: "${NAME}", faults: []string{`environment variable "NAME" is not set`}},
	{name: "a default substitutes when the variable is unset",
		written: "${NAME:-fallback}", want: "fallback"},
	{name: "a default substitutes when the variable is set to the empty string",
		written: "${NAME:-fallback}", vars: map[string]string{"NAME": ""}, want: "fallback"},
	{name: "a default yields to a variable set to a value",
		written: "${NAME:-fallback}", vars: map[string]string{"NAME": "chosen"}, want: "chosen"},
	{name: "an explicitly empty default is legal and yields the empty string",
		written: "${NAME:-}", want: ""},
	// The escape consumes `$${` and writes `${`; `NAME}` after it is ordinary text, so the
	// whole value is the seven characters the author wrote minus one `$`.
	{name: "the escape yields a literal reference even when the variable is set",
		written: "$${NAME}", vars: map[string]string{"NAME": "unused"}, want: "${NAME}"},
	{name: "a lone dollar passes through unchanged", written: "$", want: "$"},
	{name: "a bare double dollar passes through unchanged", written: "$$", want: "$$"},

	// The four rejecting forms.
	{name: "a name starting with a digit is refused",
		written: "${1VAR}", vars: map[string]string{"1VAR": "unreachable"}, want: "${1VAR}",
		faults: []string{"a variable name must match [A-Za-z_][A-Za-z0-9_]*"}},
	{name: "an empty name is refused",
		written: "${}", want: "${}", faults: []string{"a variable name is required"}},
	{name: "a reference with no closing brace is refused",
		written: "${NAME", vars: map[string]string{"NAME": "unreachable"}, want: "${NAME",
		faults: []string{"no closing brace"}},
	{name: "the opening delimiter alone is refused",
		written: "${", want: "${", faults: []string{"no closing brace"}},

	// The grammar is closed: every other unescaped `${…}` shape is refused rather than passed
	// through, which is what stops a malformed reference reaching a webhook URL.
	{name: "a space in the name is refused",
		written: "${A B}", want: "${A B}", faults: []string{"a variable name must match"}},
	{name: "a hyphen in the name is refused",
		written: "${A-B}", want: "${A-B}", faults: []string{"a variable name must match"}},
	// `:-` is the whole default marker, so a bare colon is part of the name and the name is
	// refused. A row that accepted this would make `${A:B}` an alias for `${A}` with the rest
	// silently dropped.
	{name: "a bare colon is not the default marker",
		written: "${A:B}", vars: map[string]string{"A": "unreachable"}, want: "${A:B}",
		faults: []string{"a variable name must match"}},
	// References do not nest: the first `}` ends the reference, so the name reads `${A` and is
	// refused, and the trailing `}` is ordinary text.
	{name: "a nested reference is refused rather than resolved inside out",
		written: "${${A}}", vars: map[string]string{"A": "NAME", "NAME": "unreachable"},
		want: "${${A}}", faults: []string{"a variable name must match"}},
	// The default text may not contain `}`: the reference ends at the first one, so `${A:-x}`
	// resolves to `x` and `y}` is the ordinary text that follows it.
	{name: "a default ends at the first closing brace",
		written: "${A:-x}y}", want: "xy}"},

	// The grammar is closed over what it *emits* as well as over what it reads, and the answer
	// differs by source. A default is text the author wrote in this document, so it is owed the
	// grammar; an environment value is bytes from outside it, so it is opaque and is never
	// scanned again.
	//
	// The reference ends at the first `}`, so this default is the three characters `${B` -- an
	// unterminated reference. Emitting it verbatim would put the literal `${B}` into a value
	// with nothing reported, which is the one shape the grammar exists to refuse.
	{name: "a reference written inside a default is refused rather than emitted",
		written: "${A:-${B}}", want: "${A:-${B}}", faults: []string{"no closing brace"}},
	// The escape is part of the grammar the default is owed, so an author who wants a literal
	// `${` in a default has the same one way to write it.
	{name: "an escape inside a default yields a literal reference",
		written: "${A:-$${B}", want: "${B"},
	// Only the default that is actually used is read: a variable set to a value means the
	// author's default text is never emitted, so there is nothing to re-enter.
	{name: "a default holding a reference is not read when the variable is set",
		written: "${A:-${B}}", vars: map[string]string{"A": "resolved", "B": "unreachable"},
		want: "resolved}"},
	// The other side of the same distinction, and the one the opacity exemption is for: bytes
	// the environment supplied are never scanned again, so a value holding a reference stays
	// the text it was given (ADR-5). This row is what stops the row above being satisfied by
	// re-scanning everything.
	{name: "an environment value holding a reference is not scanned again",
		written: "${A}", vars: map[string]string{"A": "${B}", "B": "leaked"}, want: "${B}"},

	// Compositions, because the scan is left to right and one occurrence must not disturb the
	// next.
	{name: "an escape and a real reference on the same line",
		written: "$${LITERAL} and ${NAME}", vars: map[string]string{"NAME": "resolved"},
		want: "${LITERAL} and resolved"},
	// The first `$` is a lone dollar, because `$$$` is not the sentinel; the escape begins at
	// the second.
	{name: "a lone dollar immediately before an escape",
		written: "$$${LITERAL}", vars: map[string]string{"LITERAL": "unused"}, want: "$${LITERAL}"},
	{name: "two references in one value",
		written: "${LEFT}-${RIGHT}", vars: map[string]string{"LEFT": "l", "RIGHT": "r"}, want: "l-r"},
	{name: "two unset variables in one value are both reported",
		written: "${LEFT}-${RIGHT}", want: "${LEFT}-${RIGHT}", faults: []string{
			`environment variable "LEFT" is not set`,
			`environment variable "RIGHT" is not set`,
		}},
	{name: "a dollar at the end of the text", written: "trailing$", want: "trailing$"},
}

// TestTheInterpolationGrammarBehavesAsPinned walks grammarCases: each row is one spelling, the
// value it becomes, and the diagnostics it earns.
func TestTheInterpolationGrammarBehavesAsPinned(t *testing.T) {
	for _, tc := range grammarCases {
		t.Run(tc.name, func(t *testing.T) {
			_, root, _, faults := stageD(t, "value: "+tc.written+"\n", tc.vars)

			if got := textIn(t, root, "$.value"); got != tc.want {
				t.Errorf("value = %q, want %q", got, tc.want)
			}
			assertMessages(t, faults, tc.faults)
			assertGrammarIsClosed(t, tc.written, tc.vars, textIn(t, root, "$.value"), faults)
		})
	}
}

// assertMessages checks that each diagnostic's message holds the wanted substring, in
// order, and that there are exactly as many diagnostics as wanted.
func assertMessages(t *testing.T, diags Errors, want []string) {
	t.Helper()

	if len(diags) != len(want) {
		t.Fatalf("got %d diagnostics %q, want %d matching %q", len(diags), messagesOf(diags), len(want), want)
	}
	for i, fragment := range want {
		if !strings.Contains(diags[i].Msg, fragment) {
			t.Errorf("diagnostic %d is %q, want it to hold %q", i, diags[i].Msg, fragment)
		}
	}
}

// assertGrammarIsClosed is the table-wide invariant behind "any other unescaped `${…}`
// sequence is a positioned diagnostic": a row that reported nothing, wrote no escape and
// drew on no environment value holding `${` must leave no `${` behind. Those two
// exclusions are the grammar's own two ways of producing a literal `${` deliberately.
func assertGrammarIsClosed(t *testing.T, written string, vars map[string]string, got string, faults Errors) {
	t.Helper()

	if len(faults) != 0 || strings.Contains(written, "$${") {
		return
	}
	for _, value := range vars {
		if strings.Contains(value, referenceOpen) {
			return
		}
	}
	if strings.Contains(got, referenceOpen) {
		t.Errorf("%q became %q, which still holds %q and reported nothing", written, got, referenceOpen)
	}
}
