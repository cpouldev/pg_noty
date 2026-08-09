package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The refusal stage E owes a shape it cannot descend, and the enumeration that makes refusing
// everything unlisted affordable.
//
// Both halves are here because neither means anything alone: the enumeration says nothing
// arrives at the refusal today, and the refusal says what happens on the library upgrade that
// changes that.

// TestEveryShapeADocumentCanHoldSurvivesNormalizationUnreported is the reason the refusal below
// is safe, and it closes stage E's walk over the same enumeration stage D is closed over.
//
// Every shape a configuration can write is normalized, and a shape stage E cannot descend
// reports the refusal below -- so a value-bearing arm added to valuePositionsOf without a
// matching arm in reduceWithin fails here, by the name of the shape that lost its descent.
// The alias and the two merge rows carry the other half: those must come out reduced.
func TestEveryShapeADocumentCanHoldSurvivesNormalizationUnreported(t *testing.T) {
	for name, document := range everyValueShapeDocument() {
		t.Run(name, func(t *testing.T) {
			src, root, diags := stageE(t, document, nil)

			if len(diags) != 0 {
				t.Fatalf("normalizing reported %q\nsource:\n%s", messagesOf(diags), document)
			}
			if survivor := firstAliasUnder(root); survivor != nil {
				t.Errorf("an alias survived at %q; stage E does not descend the shape holding it",
					positionOf(src, survivor).Path())
			}
		})
	}
}

// TestNoneOfTheThreeReducedShapesSurvivesNormalization is what stage F is promised, stated as
// one assertion rather than inferred from three. A document writing an anchor, an alias and a
// merge key comes out of this stage holding none of them, so the structural pass needs a case
// for none of the three -- which is the whole reason stage E is ordered before it.
func TestNoneOfTheThreeReducedShapesSurvivesNormalization(t *testing.T) {
	const document = "base: &base\n  inherited: yes\nlist: [*base]\nuse:\n  <<: *base\n  own: yes\n"

	src, root, diags := stageE(t, document, nil)
	if len(diags) != 0 {
		t.Fatalf("normalizing reported %q", messagesOf(diags))
	}

	descendValuePositions(root, func(node ast.Node) {
		switch node.(type) {
		case *ast.AliasNode, *ast.AnchorNode:
			t.Errorf("a %T survived at %q; stage F would need a case for it", node, positionOf(src, node).Path())
		}
		for _, key := range keyPositionsOf(node) {
			if named, isKey := key.(ast.MapKeyNode); isKey && named.IsMergeKey() {
				t.Errorf("a merge key survived at %q; stage F would have to skip one", positionOf(src, key).Path())
			}
		}
	})
}

// TestAShapeStageEDoesNotDescendIsRefusedRatherThanWalkedPast asserts the refusal itself rather
// than only its absence. The enumeration above says nothing arrives here today; it says nothing
// about what happens when something does, and a silent return there would leave an alias in the
// tree for decode to read as a name.
//
// The branch's state is constructed directly, from the two entry wrappers this library version
// hands to neither valuePositionsOf nor scalarTextOf (Step 3's Implementation Note 4). Both rows
// use one document, so the positions asserted are two tokens of one line: `  - k: v` puts the
// sequence entry's `-` at rune 3 and the mapping entry's colon -- a mapping entry's own token --
// at rune 6.
func TestAShapeStageEDoesNotDescendIsRefusedRatherThanWalkedPast(t *testing.T) {
	const document = "list:\n  - k: v\n"

	shapes := map[string]struct {
		node   func(root ast.Node) ast.Node
		column int
	}{
		"a sequence entry": {node: firstSequenceEntry, column: 3},
		"a mapping entry":  {node: nestedMappingEntry, column: 6},
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			src := newSource(interpolationFixture, []byte(document))
			pass := newNormalizer(src)

			pass.reduceWithin(shape.node(parsedRoot(t, document)))

			if len(pass.diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want the one refusing a shape nothing descends",
					len(pass.diags), messagesOf(pass.diags))
			}
			assertDiagnostic(t, pass.diags[0], unreducibleShape, 2, shape.column)
		})
	}
}

// TestASequenceWhoseTwoElementFieldsDisagreeIsRefusedRatherThanPartlyReduced is the other branch
// no document reaches, and it is a branch rather than a guard because of what the quiet
// alternative would cost.
//
// A resolved alias has to be written through Values[i] *and* Entries[i].Value or the removed
// alias stays reachable through the other field and reaches decode as a name (Implementation
// Note 4). TestGoccyGivesASequenceTheSameNodesInValuesAndEntries pins that the two fields agree,
// in both spellings of a sequence -- so a tree where they do not is a shape this stage cannot
// reduce, and skipping the write there would satisfy the type checker, the suite and nothing
// else.
//
// The state is constructed by truncating Entries, because no document this library version parses
// produces it.
func TestASequenceWhoseTwoElementFieldsDisagreeIsRefusedRatherThanPartlyReduced(t *testing.T) {
	const document = "base: &shared text\nlist:\n  - *shared\n"

	src := newSource(interpolationFixture, []byte(document))
	root := parsedRoot(t, document)
	sequence := valueOfEntry(root, 1).(*ast.SequenceNode)
	sequence.Entries = nil

	// The whole document is walked rather than the sequence alone, so the anchor above is in the
	// table and the alias below it is resolvable: the one diagnostic that comes back is then the
	// refusal this test is about rather than an undefined name.
	pass := newNormalizer(src)
	pass.reduceWithin(root)

	if len(pass.diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a sequence whose fields disagree",
			len(pass.diags), messagesOf(pass.diags))
	}
	// Line 3 is `  - *shared`, and a block sequence reports its own token at the first `-`, rune 3.
	assertDiagnostic(t, pass.diags[0], unreducibleShape, 3, 3)

	if _, unresolved := sequence.Values[0].(*ast.AliasNode); !unresolved {
		t.Error("the element was reduced anyway; a shape this stage refuses keeps what its author wrote")
	}
}

// nestedMappingEntry is the mapping entry nested inside the sequence of the document above.
// valueposition_test.go's firstMappingEntry reads a root-level entry; this stage's two rows need
// both wrappers from one line, so the mapping entry is reached through the sequence.
func nestedMappingEntry(root ast.Node) ast.Node {
	return firstSequenceEntry(root).(*ast.SequenceEntryNode).Value.(*ast.MappingNode).Values[0]
}

// firstAliasUnder is the first alias node reachable from root, or nil when the tree holds none.
//
// It walks with the package's own value-position rule rather than with a walk of its own: an
// alias in a position that rule does not reach is one no stage would have resolved either, which
// is the same blind spot rather than a second one.
func firstAliasUnder(root ast.Node) ast.Node {
	var found ast.Node
	descendValuePositions(root, func(node ast.Node) {
		if _, isAlias := node.(*ast.AliasNode); isAlias && found == nil {
			found = node
		}
	})
	return found
}
