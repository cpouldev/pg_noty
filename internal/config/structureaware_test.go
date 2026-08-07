package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// V2 through the running stage: the recursion writes values and only values.
//
// Its three clauses are the three claims here. A mapping key is seen and reported but never
// rewritten (AC #6); an anchor name and an alias name are never rewritten, and an anchored
// value is substituted once however many aliases reach it; and an *ast.LiteralNode is
// descended, because a block scalar's content lives one level below the node a diagnostic
// about it points at.
//
// All three shapes are *ast.StringNode, which is why a type switch cannot tell them apart and
// why valueposition_test.go asserts the same rule structurally as well.

// keyReferenceCases is every spelling a key holding a reference can be written in, with the
// position the diagnostic about it belongs at.
var keyReferenceCases = []struct {
	name       string
	document   string
	wantLine   int
	wantColumn int
}{
	// `        ` is eight spaces, so the key begins at rune 9.
	{name: "plain key", document: "a:\n  b:\n    c:\n      d:\n        ${HEADER_NAME}: v\n",
		wantLine: 5, wantColumn: 9},
	// A quoted key is anchored on its opening quote, at rune 1 of its own line.
	{name: "double-quoted key", document: "\"${HEADER_NAME}\": v\n", wantLine: 1, wantColumn: 1},
	{name: "single-quoted key", document: "'${HEADER_NAME}': v\n", wantLine: 1, wantColumn: 1},
	// An explicit key wraps the scalar; the `?` is what the key is anchored on.
	{name: "explicit key", document: "? ${HEADER_NAME}\n: v\n", wantLine: 1, wantColumn: 1},
	// A key written with a reference in a nested mapping is still a key.
	{name: "key inside a sequence item", document: "l:\n  - ${HEADER_NAME}: v\n", wantLine: 2, wantColumn: 5},
	// The escape is refused in a key too, which is the decision refuseReferenceInKey's
	// comment records: a key is never interpolated, so `$${` is never unescaped in one
	// either, and accepting it would leave a key silently carrying both of its dollars.
	{name: "escaped reference in a key", document: "$${HEADER_NAME}: v\n", wantLine: 1, wantColumn: 1},
	// The two wrappers a key can carry besides `?`. Each is anchored on its own
	// introducer -- the `&` and the `!!str` -- because that is where the key begins on
	// the page, and each is read through to the scalar it wraps rather than through the
	// wrapper's rendering.
	{name: "anchored key", document: "&shared ${HEADER_NAME}: v\n", wantLine: 1, wantColumn: 1},
	{name: "tagged key", document: "!!str ${HEADER_NAME}: v\n", wantLine: 1, wantColumn: 1},
}

// TestAReferenceInAMappingKeyIsRejectedAtTheKeysPosition is AC #6. The recursion already
// visits every key in order to reach the values beneath it, so a key that reads like a
// reference is this stage's to diagnose rather than something to walk silently past.
func TestAReferenceInAMappingKeyIsRejectedAtTheKeysPosition(t *testing.T) {
	for _, tc := range keyReferenceCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, faults := stageD(t, tc.document, map[string]string{"HEADER_NAME": "X-Tenant"})

			if len(faults) != 1 {
				t.Fatalf("got %d diagnostics %q, want exactly one about the key", len(faults), messagesOf(faults))
			}
			if !strings.Contains(faults[0].Msg, "mapping keys") {
				t.Errorf("Msg = %q, want it to state that keys are not interpolated", faults[0].Msg)
			}
			if faults[0].Line != tc.wantLine || faults[0].Col != tc.wantColumn {
				t.Errorf("diagnostic at %d:%d, want the key's position %d:%d",
					faults[0].Line, faults[0].Col, tc.wantLine, tc.wantColumn)
			}
		})
	}
}

// TestAKeyIsNeverRewrittenByInterpolation is the other half of AC #6: the key is diagnosed
// and left exactly as written, so nothing downstream can observe a substituted key.
func TestAKeyIsNeverRewrittenByInterpolation(t *testing.T) {
	const document = "${HEADER_NAME}: v\n"

	_, root, originals, faults := stageD(t, document, map[string]string{"HEADER_NAME": "X-Tenant"})

	mapping, isMapping := root.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("root is %T, want *ast.MappingNode", root)
	}
	if got := mapping.Values[0].Key.GetToken().Value; got != "${HEADER_NAME}" {
		t.Errorf("the key became %q, want it left as written", got)
	}
	if len(originals) != 0 {
		t.Errorf("originals holds %d entries, want none: a key is not an interpolated value", len(originals))
	}
	if len(faults) != 1 {
		t.Errorf("got %d diagnostics, want exactly one", len(faults))
	}
}

// TestAnAnchorSurvivesAndItsValueIsUnescapedExactlyOnce is CK-8 and CK-9 together.
//
// The anchor's name matches the variable charset and the environment sets a variable of
// that name, so an implementation that treated a name as content would resolve it. The
// anchored value is written with the escape and reached through two aliases: because an
// alias site holds a name rather than the content, the anchored node is visited once and
// `$${` is unescaped once. A recursion that descended alias sites would unescape twice and
// turn `${LITERAL}` into an unresolved reference.
func TestAnAnchorSurvivesAndItsValueIsUnescapedExactlyOnce(t *testing.T) {
	const document = "base: &SHARED $${LITERAL}\nfirst: *SHARED\nsecond: *SHARED\n"

	_, root, _, faults := stageD(t, document, map[string]string{"SHARED": "leaked", "LITERAL": "leaked"})

	if len(faults) != 0 {
		t.Fatalf("got %q, want nothing: an escaped reference resolves nothing", messagesOf(faults))
	}

	anchor, isAnchor := valueOfFirstEntry(root).(*ast.AnchorNode)
	if !isAnchor {
		t.Fatalf("the anchored value is %T, want *ast.AnchorNode", valueOfFirstEntry(root))
	}
	if got := anchor.Name.String(); got != "SHARED" {
		t.Errorf("the anchor name became %q, want it left as written", got)
	}
	if got := anchor.Value.(*ast.StringNode).Value; got != "${LITERAL}" {
		t.Errorf("the anchored value is %q, want %q unescaped exactly once", got, "${LITERAL}")
	}
	for index, entry := range []int{1, 2} {
		alias := valueOfEntry(root, entry).(*ast.AliasNode)
		if got := alias.Value.String(); got != "SHARED" {
			t.Errorf("alias %d names %q, want it left as written", index, got)
		}
	}
}

// TestABlockScalarsContentIsInterpolatedAndAnchoredOnItsIndicator is V2's literal descent.
// The content is substituted, and the diagnostic about it points at the block's own `|`
// rather than at the inner node, whose token the parser reports on the block's last line
// (Implementation Note 1).
func TestABlockScalarsContentIsInterpolatedAndAnchoredOnItsIndicator(t *testing.T) {
	const document = "when: |\n  OLD.status <> ${OLD_STATUS}\n"

	_, root, _, resolved := stageD(t, document, map[string]string{"OLD_STATUS": "'paid'"})
	if len(resolved) != 0 {
		t.Fatalf("got %q, want nothing", messagesOf(resolved))
	}
	if got := textIn(t, root, "$.when"); got != "OLD.status <> 'paid'\n" {
		t.Errorf("block content = %q, want the substituted text", got)
	}

	_, _, _, faults := stageD(t, document, nil)
	if len(faults) != 1 {
		t.Fatalf("got %d diagnostics %q, want one", len(faults), messagesOf(faults))
	}
	// `when: ` is six runes, so the `|` indicator is at rune 7 of line 1.
	if faults[0].Line != 1 || faults[0].Col != 7 {
		t.Errorf("diagnostic at %d:%d, want the block indicator at 1:7", faults[0].Line, faults[0].Col)
	}
}
