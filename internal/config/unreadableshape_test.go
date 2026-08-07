package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// What a shape this package cannot read costs, at each call site that meets one.
//
// Stage D refuses rather than walks past, on both the value path and the key path: walking
// past is exactly how `${SECRET}` would survive into a loaded configuration as literal text.
// The enumerations that prove nothing arrives at a refusal by accident are
// documentshape_test.go's; a refusal proves what happens when something finally does.

// TestAnUnrecognisedShapeIsRefusedRatherThanWalkedPast covers the branch that exists so a
// node shape this stage does not know cannot carry `${SECRET}` through it as literal text.
//
// The sequence entry wrapper is what makes the branch reachable from a test rather than
// reserved for a future library version, and it is also the case that decides the branch's
// shape: it renders as the empty string while holding the element it wraps, so a guard that
// asked whether the node's own text held a reference would pass it. The refusal is therefore
// unconditional, which this row proves by planting a reference the empty text cannot show.
func TestAnUnrecognisedShapeIsRefusedRatherThanWalkedPast(t *testing.T) {
	src, wrapper := unreadableShapeIn(t, "l:\n  - ${SECRET}\n")

	pass := newInterpolator(src, MapEnv(nil))
	pass.substituteValuesUnder(wrapper)

	assertRefusal(t, pass.diags, unrecognisedShapeMessage, unrecognisedShapeHint)
}

// TestAnUnrecognisedKeyShapeIsRefusedRatherThanWalkedPast is the key path's half of the same
// guard, and it exists for the same reason: this is what stops an unreadable key from
// carrying `${SECRET}` past the stage as literal text.
//
// The value path's twin above proves nothing about this branch -- the two report different
// messages at different call sites -- and the enumeration that proves nothing reaches it
// today proves only that. So the branch is entered by constructing its state directly, and
// what it emits is asserted rather than merely reached.
func TestAnUnrecognisedKeyShapeIsRefusedRatherThanWalkedPast(t *testing.T) {
	src, wrapper := unreadableShapeIn(t, "l:\n  - ${SECRET}\n")

	pass := newInterpolator(src, MapEnv(nil))
	pass.refuseReferenceInKey(wrapper)

	assertRefusal(t, pass.diags, unrecognisedKeyShapeMessage, unrecognisedKeyShapeHint)
}

// TestAnAliasKeyIsRefusedBecauseStageEResolvesItToASubstitutedValue closes AC #6 over what a
// later stage expands.
//
// An alias in a key position carries a name today, so a stage reading it as "a name rather
// than content" clears it. Stage E then replaces it with its anchor's value -- and this stage
// has already substituted the environment into that value, because an anchor's value is a
// value position. The key would become the environment's bytes after the only check that
// looks at keys has run, which is the one outcome AC #6 exists to prevent. This stage holds no
// anchor table, so it cannot say what the key will read as and must not clear it.
//
// The substitution into the anchor is asserted first. Without it the test would pass on a
// stage where the alias resolved to something that was never a reference at all.
func TestAnAliasKeyIsRefusedBecauseStageEResolvesItToASubstitutedValue(t *testing.T) {
	const planted = "X-Tenant-From-The-Environment"

	_, root, _, faults := stageD(t, "a: &k ${HEADER}\n*k : v\n", map[string]string{"HEADER": planted})

	anchor, isAnchor := valueOfFirstEntry(root).(*ast.AnchorNode)
	if !isAnchor {
		t.Fatalf("the anchored value is %T, want *ast.AnchorNode", valueOfFirstEntry(root))
	}
	if got := anchor.Value.(*ast.StringNode).Value; got != planted {
		t.Fatalf("the anchor holds %q, want %q: stage E would resolve this key to something harmless", got, planted)
	}

	if len(faults) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing the alias key", len(faults), messagesOf(faults))
	}
	if faults[0].Msg != unrecognisedKeyShapeMessage {
		t.Errorf("Msg = %q, want %q", faults[0].Msg, unrecognisedKeyShapeMessage)
	}
	// `*k` begins at rune 1 of line 2, which is where an author has to be pointed.
	if faults[0].Line != 2 || faults[0].Col != 1 {
		t.Errorf("the refusal is at %d:%d, want 2:1, where the alias key is written",
			faults[0].Line, faults[0].Col)
	}
}

// TestAnUnreadableKeyRedactsEverythingBeneathItRatherThanNothing is the third call site that
// meets a shape it cannot read, and the only one where walking past is a leak rather than a
// missing diagnostic.
//
// Redaction looks sensitivity up by key text (schemawalk.go), so a key with no readable text
// has no declaration to consult -- and defaulting to "nothing is declared here" renders
// whatever is beneath in the clear. An alias key is the shape that gets here, and the anchor
// it names could be `url` as easily as anything else, so the value goes wholesale: the
// over-redaction D3 permits, in place of a guess it does not.
func TestAnUnreadableKeyRedactsEverythingBeneathItRatherThanNothing(t *testing.T) {
	const password = "s3cret-under-an-unreadable-key"
	const document = "anchor: &k url\n*k : postgres://noty:" + password + "@db.internal/noty\n"

	rendered := renderEveryLineOf(document)

	if strings.Contains(rendered, password) {
		t.Errorf("the value under an unreadable key reached rendered output:\n%s", rendered)
	}
	if !strings.Contains(rendered, redactionPlaceholder) {
		t.Errorf("nothing was redacted, so the unreadable key was skipped rather than covered:\n%s", rendered)
	}
}

// unreadableShapeIn parses document and returns the sequence-entry wrapper inside it, which
// is the one shape neither valuePositionsOf nor keyTextOf recognises.
//
// It is also the case that decided both refusals' shape: the wrapper renders as the empty
// string while holding the element it wraps, so a guard that asked whether the node's own
// text held a reference would pass it. The rendering is asserted here, so a library version
// that gave the wrapper a rendering cannot leave either refusal quietly testing something
// else.
func unreadableShapeIn(t *testing.T, document string) (*source, ast.Node) {
	t.Helper()

	src := newSource(interpolationFixture, []byte(document))
	root, diags := parseDocument(src)
	if len(diags) != 0 {
		t.Fatalf("the fixture does not reach stage D: %+v", diags)
	}

	wrapper := firstSequenceEntry(root)
	if wrapper.String() != "" {
		t.Fatalf("the wrapper now renders as %q; both refusals are unconditional because it rendered as nothing",
			wrapper.String())
	}
	return src, wrapper
}

// assertRefusal is what a fail-closed branch owes an author: exactly one diagnostic, saying
// what it was written to say, suggesting the way out, and positioned on the shape it could
// not read.
//
// The position is derived rather than passed in, because every caller plants its shape on
// the second line of a two-line document; a caller whose fixture differs should say so by
// asserting its own position instead of widening this.
func assertRefusal(t *testing.T, diags Errors, message, hint string) {
	t.Helper()

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want one refusing the shape", len(diags), messagesOf(diags))
	}
	refusal := diags[0]

	if refusal.Msg != message {
		t.Errorf("Msg = %q, want %q", refusal.Msg, message)
	}
	if refusal.Hint != hint {
		t.Errorf("Hint = %q, want %q", refusal.Hint, hint)
	}
	// A refusal is only tolerable because it is positioned: an author has to be told which
	// part of the document the loader could not read. `  - ` puts the entry token at rune 3.
	if refusal.Line != 2 || refusal.Col != 3 {
		t.Errorf("the refusal is at %d:%d, want 2:3, where the shape is written", refusal.Line, refusal.Col)
	}
	if refusal.Rule != RuleInterpolate {
		t.Errorf("Rule = %q, want %q", refusal.Rule, RuleInterpolate)
	}
}
