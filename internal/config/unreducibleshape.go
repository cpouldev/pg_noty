package config

import "github.com/goccy/go-yaml/ast"

// This file is what makes a shape unreducible to stage E, and what the stage does about it. Two
// conditions qualify and neither is reached by any document, so both are stated here rather than
// inside the walk: a shape the walk does not descend, and a sequence whose two element fields no
// longer hold the same nodes.
//
// Each is a predicate the walk reads and reports on, so the decision to stop is visible at the
// call site while the reason lives here with the fault that names it.

// unreducibleShape: a shape the walk cannot descend, or can descend but cannot rewrite whole.
var unreducibleShape = fault{
	message: "unsupported YAML shape where an alias could be written",
	hint:    "aliases are resolved inside mappings, sequences and tagged values; write the value as one of those",
}

// elementFieldsAgree reports whether a sequence still holds one element per slot in both of the
// fields that hold its elements.
//
// Values[i] is the very node Entries[i].Value is, measured and pinned in both spellings of a
// sequence by TestGoccyGivesASequenceTheSameNodesInValuesAndEntries -- so this is false for no
// document this library version parses. It is asked anyway, and its answer is a refusal rather
// than a skipped write, because the alternative is the quiet branch a guard hedging its own pin
// always has: skipping would leave the removed alias reachable through Entries, let it reach
// decode as a name, and fail nothing. The refused state is constructed directly by
// TestASequenceWhoseTwoElementFieldsDisagreeIsRefusedRatherThanPartlyReduced.
func elementFieldsAgree(sequence *ast.SequenceNode) bool {
	return len(sequence.Entries) == len(sequence.Values)
}

// refuseUnlessNothingIsHeld is this stage's fail-closed branch: a shape it does not descend has
// to prove it holds nothing, or an alias could survive in a position nothing rewrites and reach
// decode as the name it was written with.
//
// The proof is valuePositionsOf's, the package's one answer to "what does this node hold", so
// the arms here and the arms there cannot drift apart silently: a value-bearing shape added
// there and not here arrives at this refusal. Nothing a document can write reaches it today,
// which TestEveryShapeADocumentCanHoldSurvivesNormalizationUnreported asserts, and the refusal
// itself is reached directly by TestAShapeStageEDoesNotDescendIsRefusedRatherThanWalkedPast.
func (nz *normalizer) refuseUnlessNothingIsHeld(node ast.Node) {
	held, recognised := valuePositionsOf(node)
	if recognised && len(held) == 0 {
		return
	}
	nz.report(node, unreducibleShape)
}
