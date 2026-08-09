package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The provenance map: what stage D still knows about a value once it has been substituted.
// interpolate.go produces originalTexts; W1 in warn.go is its sole consumer.
//
// W1 asks one question -- was this signing secret written
// down, or referenced? The three claims here are what W1 depends on: membership means the
// bytes came from outside the repository, the key is the node W1 will be holding, and nothing
// else in the package may read the map at all.

// provenanceCases pairs one written value with whether the map records it. What decides the
// answer is where the bytes came from, never which syntax was written. The last two rows are
// the pair that says so: one default text, two environments, two answers -- because a fallback
// the author committed to the repository *is* a literal, and W1 has to warn about it.
var provenanceCases = []struct {
	name         string
	written      string
	vars         map[string]string
	wantRecorded bool
}{
	{
		name:         "a reference the environment resolved is recorded",
		written:      "${SECRET}",
		vars:         map[string]string{"SECRET": suppliedFromTheEnvironment},
		wantRecorded: true,
	},
	{
		name:    "a value written down is not recorded",
		written: "written-down",
	},
	{
		// The escape resolves nothing: `$${SECRET}` is the literal `${SECRET}`, and W1
		// has to see it as one.
		name:    "a value the escape merely unescaped is not recorded",
		written: "$${SECRET}",
		vars:    map[string]string{"SECRET": suppliedFromTheEnvironment},
	},
	{
		// Every byte of this value came from the document. Recording it as referenced is
		// how a secret committed beside its own reference stops being warned about.
		name:    "a default the author committed is not recorded",
		written: "${SECRET:-committed}",
	},
	{
		name:    "a default used for a set-empty variable is not recorded",
		written: "${SECRET:-committed}",
		vars:    map[string]string{"SECRET": ""},
	},
	{
		name:         "the same default is recorded once the environment supplies the bytes",
		written:      "${SECRET:-committed}",
		vars:         map[string]string{"SECRET": suppliedFromTheEnvironment},
		wantRecorded: true,
	},
}

// suppliedFromTheEnvironment is the value the rows above resolve to when the environment is
// what supplied it, named so that a row's answer and its cause read as one thing.
const suppliedFromTheEnvironment = "from-the-environment"

// TestTheOriginalTextOfAnInterpolatedValueIsRecordedForW1 covers the map Step 11's W1 asks
// whether a signing secret was written down or referenced.
func TestTheOriginalTextOfAnInterpolatedValueIsRecordedForW1(t *testing.T) {
	for _, tc := range provenanceCases {
		t.Run(tc.name, func(t *testing.T) {
			_, root, originals, faults := stageD(t, "value: "+tc.written+"\n", tc.vars)
			if len(faults) != 0 {
				t.Fatalf("got %q, want nothing", messagesOf(faults))
			}

			written, recorded := originals[nodeIn(t, root, "$.value")]
			if recorded != tc.wantRecorded {
				t.Fatalf("recorded = %v, want %v", recorded, tc.wantRecorded)
			}
			if recorded && written != tc.written {
				t.Errorf("originals holds %q, want the text as written, %q", written, tc.written)
			}
		})
	}
}

// TestABlockScalarsOriginalIsKeyedOnTheNodeADiagnosticAnchorsOn pins which of a block
// scalar's two nodes W1 has to hold to find it in the map.
//
// A block scalar is the one value shape whose text and whose position live in different
// nodes: the stage writes the inner *ast.StringNode and keys the map on the outer
// *ast.LiteralNode, because that is where a diagnostic about the value points
// (Implementation Note 1). Step 11 reads membership as "the environment supplied this", so
// a W1 holding the inner node would find nothing there and report an interpolated signing
// secret as one committed to the repository -- silently, and about the one field the warning
// exists for.
func TestABlockScalarsOriginalIsKeyedOnTheNodeADiagnosticAnchorsOn(t *testing.T) {
	const written = "${SECRET}\n"

	_, root, originals, faults := stageD(t, "secret: |\n  ${SECRET}\n",
		map[string]string{"SECRET": suppliedFromTheEnvironment})
	if len(faults) != 0 {
		t.Fatalf("got %q, want nothing", messagesOf(faults))
	}

	scalar, holdsText := scalarTextOf(valueOfFirstEntry(root))
	if !holdsText {
		t.Fatalf("the block scalar is %T, which holds no text", valueOfFirstEntry(root))
	}
	// Without this the test would pass on a stage that keyed the map on either node.
	if scalar.anchor == ast.Node(scalar.holder) {
		t.Fatal("the two nodes are now one, so this test can no longer tell them apart")
	}

	recorded, isRecorded := originals[scalar.anchor]
	if !isRecorded {
		t.Error("the map does not hold the node a diagnostic anchors on, so W1 will not find this value")
	}
	if recorded != written {
		t.Errorf("originals holds %q, want the block's content as written, %q", recorded, written)
	}
	if _, alsoInner := originals[ast.Node(scalar.holder)]; alsoInner {
		t.Error("the map also holds the inner node, whose position points at the block's last line")
	}
}
