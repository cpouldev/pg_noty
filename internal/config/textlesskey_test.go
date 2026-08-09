package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// What a diagnostic calls a key the parser read as something other than text.
//
// `16:` is the integer sixteen and holds no name, so keyTextOf answers ("", true) -- recognised,
// and with nothing to quote. Quoting that answer renders `unknown field ""`, which names nothing
// and leaves an author with a rule, a line and no key; naming the key is the whole point of an
// unknown-key diagnostic (shapecheck.go). The rendering is a display name and reaches no
// comparison: which two keys are one key stays keyIdentity's, for the reason Note 6 records.

// aWorkerHolding is the minimal valid configuration with one entry written into its `worker` block,
// so that whatever the entry produces is the run's only diagnostic. `listeners: []` is the
// deliberate way to say this instance manages nothing (AC #31), which is what keeps the listener
// keys out of the count.
func aWorkerHolding(entry string) string {
	return "version: 1\n" +
		"database:\n  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"worker:\n" + entry +
		"listeners: []\n"
}

// textlessKeyCases is every spelling an author reaches this class through, at a level closed by R41
// and at the one closed by R28.
//
// The last row is the control that keeps the two classes apart. An empty *string* key holds text,
// and that text is empty -- so `unknown field ""` is the honest message there, and a fix that
// substituted a rendering for every empty name would report `""` as `""` and pass either way.
var textlessKeyCases = []struct {
	name     string
	document string
	wantName string
	wantRule RuleID
}{
	{
		name:     "an integer",
		document: aWorkerHolding("  16: 1\n"),
		wantName: "16",
		wantRule: R41,
	},
	{
		// The same integer in the other base. The message shows the base the author typed,
		// because it is what they have to find on the line; keyIdentity reads both as sixteen.
		name:     "an integer written in hexadecimal",
		document: aWorkerHolding("  0x10: 1\n"),
		wantName: "0x10",
		wantRule: R41,
	},
	{
		name:     "a boolean",
		document: aWorkerHolding("  true: 1\n"),
		wantName: "true",
		wantRule: R41,
	},
	{
		name:     "null written as a tilde",
		document: aWorkerHolding("  ~: 1\n"),
		wantName: "~",
		wantRule: R41,
	},
	{
		// A tag is a property of the key rather than a key of its own, so the name is read from
		// beneath it -- the tag's own token is `!!int`, which names no field at all.
		name:     "a tagged integer",
		document: aWorkerHolding("  !!int 16: 1\n"),
		wantName: "16",
		wantRule: R41,
	},
	{
		name:     "an integer at the operations level",
		document: operationsOf("    operations:\n      16: {}\n"),
		wantName: "16",
		wantRule: R28,
	},
	{
		name:     "an empty string, which holds text and whose text is empty",
		document: aWorkerHolding("  \"\": 1\n"),
		wantName: "",
		wantRule: R41,
	},
	{
		// The other side of the same partition, and the one that keeps the text branch
		// load-bearing: a block scalar keeps its content in an inner node while its own token
		// is the `>-` indicator, so a name read off the token would call this key `>-`.
		name:     "a block scalar, whose text is not its token",
		document: aWorkerHolding("  ? >-\n      blockkey\n  : 1\n"),
		wantName: "blockkey",
		wantRule: R41,
	},
}

// TestAKeyCarryingNoTextIsNamedAsItWasWritten asserts that each spelling above is reported under its
// own rule, naming the key as the author wrote it. The cases are declared beside it rather than
// inside it so that neither the table nor the assertion has to be read past to reach the other.
func TestAKeyCarryingNoTextIsNamedAsItWasWritten(t *testing.T) {
	for _, tc := range textlessKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			diags, _ := stageF(t, tc.document)

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if want := unknownField(tc.wantName); diags[0].Msg != want {
				t.Errorf("Msg = %q, want %q", diags[0].Msg, want)
			}
			if diags[0].Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", diags[0].Rule, tc.wantRule)
			}
		})
	}
}

// TestAKeyWithNoTokenIsNamedByNothingRatherThanPanicking reaches the one arm no document does.
// Every key this walk meets was parsed and so carries a token; a node built in code need not, and a
// diagnostic path that dereferenced one would panic where it was supposed to explain. The empty
// name it answers with is the message this class produced before the naming existed, so the arm
// adds no new silent output.
func TestAKeyWithNoTokenIsNamedByNothingRatherThanPanicking(t *testing.T) {
	if got := keyAsWritten(&ast.IntegerNode{}); got != "" {
		t.Errorf("keyAsWritten(a node carrying no token) = %q, want the empty name", got)
	}
}
