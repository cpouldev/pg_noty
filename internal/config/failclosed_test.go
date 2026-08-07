package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The two arms of stage F that no document reaches: a key whose shape cannot be read, and a value
// whose shape the contract has no word for. Both are fail-closed refusals, and an enumeration
// proving nothing reaches them says nothing about what they do when something finally does, so
// each is reached by construction.

// TestAKeyThisStageCannotReadIsRefusedRatherThanWalkedPast reaches the walk's fail-closed arm for
// a key, which no document reaches: stage D refuses every unreadable key an author wrote and the
// run stops there, and stage E refuses every key the operations fold invents.
//
// The state is therefore constructed directly, because an enumeration proving nothing reaches the
// branch says nothing about what the branch does when something finally does. Measured: deleting
// the refusal and returning silently leaves the rest of the suite green.
func TestAKeyThisStageCannotReadIsRefusedRatherThanWalkedPast(t *testing.T) {
	// An alias in a key position is the one shape keyTextOf declines. Stage D refuses it, so the
	// pass is driven directly rather than through the pipeline.
	src := newSource("listeners.yaml", []byte("a: &k version\n*k : 1\n"))
	root, parsed := parseDocument(src)
	if len(parsed) != 0 {
		t.Fatalf("the fixture does not parse: %q", messagesOf(parsed))
	}

	entry := root.(*ast.MappingNode).Values[1]
	if _, readable := keyTextOf(entry.Key); readable {
		t.Fatalf("the second key is %T and is readable, so this asserts nothing", entry.Key)
	}

	pass := newShapePass(src)
	pass.checkEntry(schemaLevels[levelRoot], entry, "")

	if len(pass.diags) != 1 || pass.diags[0].Msg != unrecognisedKeyShapeMessage {
		t.Fatalf("diagnostics = %+v, want one refusing the shape", pass.diags)
	}
	if pass.diags[0].Line != 2 {
		t.Errorf("the refusal is at line %d, want line 2, where the key is written", pass.diags[0].Line)
	}
	if !pass.undecodable {
		t.Error("a key this stage cannot read left the document decodable")
	}
}

// TestAValueShapeThisStageCannotNameIsRefusedRatherThanWalkedPast is the same for the value side.
// Every shape a document can hold reaches nodeKindOf with a name, so this arm is reached by
// construction too -- and its cost is the whole point: a value walked past is a value the contract
// never judged and the decoder is handed anyway.
func TestAValueShapeThisStageCannotNameIsRefusedRatherThanWalkedPast(t *testing.T) {
	// An alias is a shape stage E removes from every value position, so the contract has no word
	// for it by the time this stage runs.
	src := newSource("listeners.yaml", []byte("a: &k {}\nversion: *k\n"))
	root, parsed := parseDocument(src)
	if len(parsed) != 0 {
		t.Fatalf("the fixture does not parse: %q", messagesOf(parsed))
	}

	alias := root.(*ast.MappingNode).Values[1].Value
	if _, named := nodeKindOf(alias); named {
		t.Fatalf("the contract now has a word for %T, so this asserts nothing", alias)
	}

	pass := newShapePass(src)
	version, declared := schemaLevels[levelRoot].key("version")
	if !declared {
		t.Fatal("the schema no longer declares version, so this fixture has no scalar expectation")
	}
	pass.checkValue(version, alias)

	if len(pass.diags) != 1 || !strings.Contains(pass.diags[0].Msg, "value position") {
		t.Fatalf("diagnostics = %+v, want one refusing the shape", pass.diags)
	}
	if !pass.undecodable {
		t.Error("a value shape this stage cannot name left the document decodable")
	}
}

// TestEveryDeclaredKindHasAName closes the wording of a wrong-shape diagnostic over the kinds the
// contract can declare. A kind with no name renders as the empty string, so the message would
// read `"x" must be ` and the failure would be a wording nobody notices rather than a test.
func TestEveryDeclaredKindHasAName(t *testing.T) {
	declared := []nodeKinds{scalarValue, mappingValue, sequenceValue}

	if len(kindNames) != len(declared) {
		t.Fatalf("%d kinds are named for %d the contract declares", len(kindNames), len(declared))
	}
	for _, kind := range declared {
		if namesOfKinds(kind) == "" {
			t.Errorf("kind %b has no name, so a diagnostic about it would name nothing", kind)
		}
	}
	if got := namesOfKinds(mappingValue | sequenceValue); got != "a mapping or a list" {
		t.Errorf("namesOfKinds(mapping|sequence) = %q, want both named", got)
	}
}
