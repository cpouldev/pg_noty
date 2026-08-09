package config

import (
	"testing"
)

// What a header mapping decodes to: every name the author wrote, each keeping the position of its own
// key, and a shape it cannot read refused rather than returned.

// TestEveryHeaderKeepsThePositionOfItsOwnKey is what R19, R20 and R21 need. All three anchor on a header
// *name*, and R20 has to name the line the first of two spellings was written on -- so one position for
// the mapping would leave three rules pointing at the same caret.
func TestEveryHeaderKeepsThePositionOfItsOwnKey(t *testing.T) {
	decoded := decodedConfig(t, "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n"+
		"defaults:\n  headers:\n    X-Tenant: acme\n    X-Trace: on\nlisteners: []\n")

	headers := decoded.Defaults.Value.Headers
	if len(headers.values) != 2 {
		t.Fatalf("decoded %d headers, want 2", len(headers.values))
	}

	// Lines 6 and 7 are the two headers; `    ` is four runes, so each name begins at rune 5. The
	// values follow their names: `    X-Tenant: ` is fourteen runes and `    X-Trace: ` thirteen.
	written := []struct {
		name     string
		value    string
		line     int
		valueCol int
	}{
		{name: "X-Tenant", value: "acme", line: 6, valueCol: 15},
		{name: "X-Trace", value: "on", line: 7, valueCol: 14},
	}
	// Looked up by name rather than by index. rawheaders.go declines to promise document order -- after
	// stage E an inherited entry sits last while carrying an earlier line -- so a positional table would
	// assert an order the producer does not offer, and would fail on a document that merged its headers
	// rather than on any defect.
	for _, want := range written {
		held, decoded := headerNamed(headers, want.name)
		if !decoded {
			t.Errorf("no header named %q was decoded", want.name)
			continue
		}

		if held.Value.value != want.value {
			t.Errorf("header %q is %q, want %q", want.name, held.Value.value, want.value)
		}
		if held.Name.Line() != want.line || held.Name.Col() != 5 {
			t.Errorf("%q is at %d:%d, want %d:5", want.name, held.Name.Line(), held.Name.Col(), want.line)
		}
		if held.Value.Line() != want.line || held.Value.Col() != want.valueCol {
			t.Errorf("%q's value is at %d:%d, want %d:%d", want.name,
				held.Value.Line(), held.Value.Col(), want.line, want.valueCol)
		}
	}
}

// TestAHeaderNameIsReadInEverySpellingYamlPermits is what keeps the free-form level free-form: the
// contract declares no name here, so the decode must read whatever an author wrote as a key -- quoted,
// explicit, tagged -- rather than only the bare spelling. Reading them through the package's one answer
// to that question is what makes them one name rather than four.
func TestAHeaderNameIsReadInEverySpellingYamlPermits(t *testing.T) {
	spellings := map[string]string{
		"bare":          "    X-Tenant: acme\n",
		"single-quoted": "    'X-Tenant': acme\n",
		"double-quoted": `    "X-Tenant": acme` + "\n",
		"explicit":      "    ? X-Tenant\n    : acme\n",
		"tagged":        "    !!str X-Tenant: acme\n",
	}

	for spelling, written := range spellings {
		t.Run(spelling, func(t *testing.T) {
			decoded := decodedConfig(t, "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n"+
				"defaults:\n  headers:\n"+written+"listeners: []\n")

			headers := decoded.Defaults.Value.Headers
			if len(headers.values) != 1 {
				t.Fatalf("decoded %d headers, want 1", len(headers.values))
			}
			if got := headers.values[0].Name.value; got != "X-Tenant" {
				t.Errorf("read the name as %q, want %q", got, "X-Tenant")
			}
		})
	}
}

// TestAHeaderValueThatIsNotAScalarIsRefusedOnItsOwnNode is the one refusal here a document reaches. A
// header level declares no keys, so stage F asks nothing about what sits beneath one and a mapping
// written as a header value arrives intact -- where the value's own wrapper refuses it, positioned on the
// value rather than on the mapping holding it.
func TestAHeaderValueThatIsNotAScalarIsRefusedOnItsOwnNode(t *testing.T) {
	document := "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n" +
		"defaults:\n  headers:\n    X-Tenant: {a: 1}\nlisteners: []\n"

	_, diags := stageG(t, document)

	if len(diags) != 1 {
		t.Fatalf("recorded %q, want the one value it could not read", messagesOf(diags))
	}
	if diags[0].Msg != mustBeText.message {
		t.Errorf("Msg = %q, want %q", diags[0].Msg, mustBeText.message)
	}
	// Line 6 is `    X-Tenant: {a: 1}`: `    X-Tenant: ` is fourteen runes.
	if diags[0].Line != 6 || diags[0].Col != 15 {
		t.Errorf("recorded at %d:%d, want the value's own 6:15", diags[0].Line, diags[0].Col)
	}
}

// TestAnEmptyHeaderNameIsNotRefusedHere is the boundary between this decode and R19. An empty name is a
// name the decode can read, and refusing it here as well as there would be two diagnostics for one
// mistake -- so the decode keeps it and Step 9 reports it.
func TestAnEmptyHeaderNameIsNotRefusedHere(t *testing.T) {
	document := "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n" +
		"defaults:\n  headers:\n    \"\": acme\nlisteners: []\n"

	decoded, diags := stageG(t, document)

	if len(diags) != 0 {
		t.Fatalf("recorded %q; an empty header name is R19's to refuse", messagesOf(diags))
	}
	headers := decoded.Defaults.Value.Headers
	if len(headers.values) != 1 || headers.values[0].Name.value != "" {
		t.Errorf("decoded %d headers; the empty name has to survive for R19 to find", len(headers.values))
	}
}

// TestTheHeadersDecodeRefusesAShapeItCannotReadRatherThanFailing reaches the two refusals no document
// does, for the reason the operations decode's equivalents are reached directly.
func TestTheHeadersDecodeRefusesAShapeItCannotReadRatherThanFailing(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     fault
	}{
		{name: "a value that is not a mapping", document: "headers: [a]\n", want: mustBeAHeaderMapping},
		{name: "a key nothing can read", document: "headers:\n  *anchor : a\n", want: mustBeAHeaderName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.document, "$.headers")

			var held rawHeaders
			if err := held.UnmarshalYAML(node); err != nil {
				t.Fatalf("UnmarshalYAML returned %v; the headers decode must not fail", err)
			}

			pass := newDecodePass(src)
			held.resolve(pass)

			if len(pass.diags) != 1 {
				t.Fatalf("recorded %q, want exactly one refusal", messagesOf(pass.diags))
			}
			if pass.diags[0].Msg != tc.want.message {
				t.Errorf("Msg = %q, want %q", pass.diags[0].Msg, tc.want.message)
			}
		})
	}
}

// headerNamed is the decoded header carrying a name, and whether one was decoded.
//
// By name because the arrival order is the mapping's rather than the document's, and nothing outside a
// diagnostic's own position ordering depends on it.
func headerNamed(headers rawHeaders, name string) (writtenHeader, bool) {
	for _, held := range headers.values {
		if held.Name.value == name {
			return held, true
		}
	}
	return writtenHeader{}, false
}
