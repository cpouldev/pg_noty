package config

import (
	"strings"
	"testing"
)

// What each wrapper reads: the value its own type admits, in every spelling YAML and the environment can
// deliver it in, and the unit set the one duration function decides.
//
// The never-fail guarantee is scalar_test.go's subject and the positions are scalarposition_test.go's;
// this file is about the answers.

// TestEveryWrapperReadsTheValueTheContractDeclares is the success half: each wrapper converts the
// value an author writes for its type, in both the plain and the quoted spelling.
//
// The quoted spelling is not decoration. After stage D an interpolated value is a *string* node
// whatever it now holds, so "16" and 16 must reach an int alike or AC #5 holds for a literal and
// silently fails for the reference it exists for.
func TestEveryWrapperReadsTheValueTheContractDeclares(t *testing.T) {
	tests := []struct {
		kind    string
		written string
		want    string
	}{
		{kind: "Str", written: "noty", want: "noty"},
		{kind: "Str", written: `"noty"`, want: "noty"},
		{kind: "Int", written: "16", want: "16"},
		{kind: "Int", written: `"16"`, want: "16"},
		// 0x10 is the integer sixteen written another way, and the parser has already read it as
		// such: a wrapper reading the source text instead of the parsed value would answer 0.
		{kind: "Int", written: "0x10", want: "16"},
		{kind: "Bool", written: "false", want: "false"},
		{kind: "Bool", written: "TRUE", want: "true"},
		{kind: "Bool", written: `"true"`, want: "true"},
		{kind: "Dur", written: "10s", want: "10s"},
		{kind: "Dur", written: `"1h30m"`, want: "1h30m0s"},
		{kind: "StrList", written: "[id, status]", want: "id,status"},
		{kind: "StrList", written: "[]", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.kind+" reading "+tc.written, func(t *testing.T) {
			got := kindNamed(t, tc.kind).read(nodeReadBy(t, "value: "+tc.written+"\n", "$.value"))

			if len(got.diags) != 0 {
				t.Fatalf("recorded %q for a value of its own type", messagesOf(got.diags))
			}
			if !got.valid || !got.set {
				t.Errorf("Set = %t and Valid() = %t, want both true for a value it read", got.set, got.valid)
			}
			if got.value != tc.want {
				t.Errorf("read %q, want %q", got.value, tc.want)
			}
		})
	}
}

// kindNamed is the row of wrapperKinds a case names, failing rather than skipping when the name is
// not one of the five so a typo cannot silently drop a case.
func kindNamed(t *testing.T, name string) wrapperKind {
	t.Helper()

	for _, kind := range wrapperKinds {
		if kind.name == name {
			return kind
		}
	}

	t.Fatalf("no wrapper called %s", name)
	return wrapperKind{}
}

// TestAnInterpolatedValueReachesTheFieldsDeclaredType is AC #5's and SC-3's mechanism at the
// wrapper: the reference is substituted by stage D into the string node the author wrote, and the
// wrapper still answers a typed value.
//
// It runs the two stages the pipeline runs, so what is asserted is the node decode actually meets
// rather than a hand-built one. The `7d` row is the failure half: captured with a position, and the
// wrapper still answers nil -- the wording R40 will carry is Step 8's.
func TestAnInterpolatedValueReachesTheFieldsDeclaredType(t *testing.T) {
	tests := []struct {
		kind      string
		variable  string
		set       string
		want      string
		wantFault bool
	}{
		{kind: "Int", variable: "N", set: "16", want: "16"},
		{kind: "Dur", variable: "P", set: "10s", want: "10s"},
		{kind: "Bool", variable: "J", set: "true", want: "true"},
		{kind: "Dur", variable: "P", set: "7d", want: "0s", wantFault: true},
	}

	for _, tc := range tests {
		t.Run(tc.kind+" from "+tc.variable+"="+tc.set, func(t *testing.T) {
			document := "value: ${" + tc.variable + "}\n"
			src, root, diags := stageE(t, document, map[string]string{tc.variable: tc.set})
			if len(diags) != 0 {
				t.Fatalf("the document does not reach decode: %q", messagesOf(diags))
			}

			got := kindNamed(t, tc.kind).read(t, src, nodeIn(t, root, "$.value"))

			if got.value != tc.want {
				t.Errorf("read %q, want %q", got.value, tc.want)
			}
			if tc.wantFault {
				assertOneFaultAtTheReference(t, got, src)
				return
			}
			if len(got.diags) != 0 {
				t.Errorf("recorded %q for a reference that resolved to a value of its type", messagesOf(got.diags))
			}
		})
	}
}

// assertOneFaultAtTheReference is the `7d` half: one diagnostic, positioned on the value the author
// wrote rather than on the text the environment supplied, which is what keeps a resolved secret out
// of rendered output (ADR-5, ADR-7).
func assertOneFaultAtTheReference(t *testing.T, got reading, src *source) {
	t.Helper()

	if len(got.diags) != 1 {
		t.Fatalf("recorded %d diagnostics %q, want exactly one", len(got.diags), messagesOf(got.diags))
	}
	// `value: ` is seven runes, so the reference begins at rune 8 of line 1.
	if got.diags[0].Line != 1 || got.diags[0].Col != 8 {
		t.Errorf("recorded at %d:%d, want the value's own position 1:8", got.diags[0].Line, got.diags[0].Col)
	}
	if quoted := got.diags[0].Msg; strings.Contains(quoted, "7d") {
		t.Errorf("the message quotes the resolved value %q; a message is rendered as it is built", quoted)
	}
	if rendered := (Errors{got.diags[0]}).Render(src.CloneBytes()); !strings.Contains(rendered, "${P}") {
		t.Errorf("the rendered block does not quote the reference the author wrote:\n%s", rendered)
	}
}
