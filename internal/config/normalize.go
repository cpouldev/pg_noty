package config

import "github.com/goccy/go-yaml/ast"

// This file is stage E: it reduces the several legal spellings of one document to a single
// shape. An alias becomes the value it names, an anchor becomes the value it declares, `<<`
// becomes the entries it merges (mergekey.go) and the operations list becomes the map form
// (listsugar.go) -- so no stage after this one carries a case for any of them.
//
// It runs after stage D and could not run before it. Substitution visits each anchored scalar
// once, whatever number of aliases reach it, which is what makes `$${` unescape exactly once
// and a resolved secret arrive once; expanding first would hand stage D the same scalar several
// times over.
//
// The tree is the only thing this stage writes. The source bytes are untouched, so every
// diagnostic it raises still quotes the line an author wrote (ADR-5).
//
// Every reason the stage declines is its own `fault` value, declared in the file that raises it:
// undefinedAlias and selfReferentialAnchor in anchor.go, because both are what a name resolving to
// nothing can mean and they mean different things; nonMappingMergeSource in mergekey.go; the
// fold's own two in listsugar.go; and unreducibleShape in unreducibleshape.go. Two more are not
// the stage's own -- the objections to a key holding `${` are keyreference.go's, asked by stage D
// of every key an author wrote and by this stage of every key the fold invents, because it is one
// condition and one remedy (AC #6). None of them quotes the document's own text -- the caret says
// which node, the message says what is wrong with it (Step 3's Note 6).

// normalizer is what one stage-E run carries: what every stage accumulates (stage.go), the
// anchors it has met, and the ones it is inside.
type normalizer struct {
	stage
	// anchors is every anchor defined so far, mapped to the node it names. It is filled as the
	// walk passes each anchor and read as the walk passes each alias, so YAML's own rule -- an
	// anchor is defined before it is used -- is the table's shape rather than a check.
	anchors map[string]ast.Node
	// reducing is every anchor whose own value the walk is currently inside, which is what tells
	// a self-reference apart from a name nobody declared (anchor.go).
	reducing map[string]bool
}

// newNormalizer is one stage-E run over src, with both of its tables empty and its diagnostics
// tagged with the rule this stage raises.
func newNormalizer(src *source) *normalizer {
	return &normalizer{
		stage:    stage{src: src, rule: RuleNormalize},
		anchors:  map[string]ast.Node{},
		reducing: map[string]bool{},
	}
}

// normalize runs stage E over the document rooted at root, and reports what it could not reduce.
//
// It rewrites root in place, and the policy is accumulate then stop: a document naming two
// anchors that do not exist is two diagnostics in one run, and no later stage runs, because a
// document still holding unresolved aliases has nothing a later stage could judge.
func normalize(src *source, root ast.Node) Errors {
	pass := newNormalizer(src)

	// CONFORMANCE with skill Pattern 4 (merge keys). `<<` is resolved here, in the AST, before
	// anything decodes the document; a key written directly in a mapping wins by construction
	// rather than by document order; and the marker is detected through the library's own
	// IsMergeKey() rather than by comparing a key's text. Pattern 4 prescribes exactly this
	// mechanism, and the expansion follows every step of it. The one thing the pattern leaves
	// available that this package rejects is yaml.AllowDuplicateMapKey().
	//
	// It is rejected for what it does, not for being a library path. V5 measured both of them:
	// decoding a mapping that holds `<<` fails outright with `duplicate key`, and enabling
	// AllowDuplicateMapKey() makes that decode succeed with precedence following *document
	// order* -- so `max_attempts: 10` written above `<<: *base` silently loses to the merged
	// value. That is a wrong-but-valid-looking configuration, and in this product it reaches DDL
	// against a user's tables. Both measurements are re-run by
	// TestGoccyWithDuplicateKeysAllowedFollowsDocumentOrderRatherThanMergePrecedence and
	// TestGoccyRefusesToDecodeAMergeKeyIntoAStruct, so a reader of this comment can reproduce
	// what justifies it rather than take it on trust.
	//
	// One detail of the pattern's own snippet is departed from, and only to keep the mechanism it
	// describes: Pattern 4 reaches IsMergeKey() through `mv.Key.(*ast.StringNode)`, and measured
	// against this version `<<` parses to an *ast.MergeKeyNode, so that assertion compiles, runs
	// and never fires. The predicate is reached through ast.MapKeyNode -- the type
	// MappingValueNode.Key already has -- and TestGoccyMarksAMergeKeyOnANodeTypeOfItsOwn records
	// the trap rather than merely avoiding it.
	pass.reduceWithin(root)

	return pass.diags
}

// reduceValue is the node that stands in a value position once this stage is done with it.
//
// Two shapes are replaced rather than descended, and both are replaced by a node that has
// already been reduced. Everything else stands for itself and is normalized in place.
func (nz *normalizer) reduceValue(node ast.Node) ast.Node {
	switch held := node.(type) {
	case *ast.AliasNode:
		anchored, defined := nz.anchoredBy(held)
		if !defined {
			nz.report(held, nz.unresolvable(held))
			// A refused alias keeps what its author wrote, for the reason stage D leaves a
			// refused scalar alone (Step 3's Implementation Note 9): the run stops here, so the
			// only thing a later reader of this tree could find is the alias itself.
			return held
		}
		// Deliberately not descended again. The anchored node was reduced where it was defined,
		// which YAML guarantees is earlier in this document, so a second descent would repeat
		// finished work -- and for a chain of anchors each aliasing the last, repeat it once per
		// alias per level.
		return anchored

	case *ast.AnchorNode:
		held.Value = nz.reduceAnchoredValue(held)
		nz.remember(held)
		// An anchor is a spelling of the value it names, so the value is what the document holds
		// afterwards. The name stays reachable through the anchor table, which is the only
		// reader that ever needed it.
		return held.Value
	}

	nz.reduceWithin(node)
	return node
}

// reduceWithin normalizes what a node holds, for the shapes that stand for themselves.
func (nz *normalizer) reduceWithin(node ast.Node) {
	if _, holdsText := scalarTextOf(node); holdsText {
		// A scalar holds no value beneath it and names no anchor. It is asked first for the
		// reason stage D asks it first: valuePositionsOf deliberately does not answer for the
		// two shapes that carry text, so a caller reading them in the other order would refuse
		// every string in the document.
		return
	}

	switch held := node.(type) {
	case *ast.MappingNode:
		nz.reduceMapping(held)
	case *ast.SequenceNode:
		nz.reduceSequence(held)
	case *ast.TagNode:
		held.Value = nz.reduceValue(held.Value)
	default:
		nz.refuseUnlessNothingIsHeld(node)
	}
}

// reduceMapping normalizes one mapping: every value beneath it, the operations sugar written in
// it, and the merge keys that say what else it holds. The entries are read once, in the order
// the author wrote them, because both rules that decide the result are order rules -- an anchor
// is available to the aliases below it, and an earlier `<<` outranks a later one. What a source
// may be and what it may override are mergekey.go's.
// The direct entries are collected by this loop rather than by a second pass over the same
// entries: the loop has already asked each key whether it is a merge key, and asking twice is how
// two answers to one question drift apart.
func (nz *normalizer) reduceMapping(mapping *ast.MappingNode) {
	var sources []*ast.MappingNode
	direct := make([]*ast.MappingValueNode, 0, len(mapping.Values))

	for _, entry := range mapping.Values {
		nz.rememberAnchoredKey(entry.Key)

		if entry.Key.IsMergeKey() {
			// A merge key's value names mappings rather than holding one, so it is resolved here
			// and the entry itself does not survive the expansion below.
			sources = append(sources, nz.mergedMappings(entry.Value)...)
			continue
		}

		entry.Value = nz.reduceValue(entry.Value)
		entry.Value = nz.foldedOperations(entry.Key, entry.Value)
		direct = append(direct, entry)
	}

	if len(direct) == len(mapping.Values) {
		// Every entry was written directly, so this mapping holds no merge key and keeps the
		// slice the parser built.
		return
	}
	mapping.Values = expandedWith(direct, sources)
}

// reduceSequence normalizes every element of a sequence, writing each replacement through both
// fields that hold it.
//
// Both writes are mandatory (Implementation Note 4): the decoder reads Values, so writing that
// one alone type-checks, decodes correctly, and leaves the alias this stage removed reachable
// through Entries. A sequence whose two fields no longer agree is therefore a shape this stage
// cannot reduce, and it is refused rather than partly reduced (unreducibleshape.go).
func (nz *normalizer) reduceSequence(sequence *ast.SequenceNode) {
	if !elementFieldsAgree(sequence) {
		nz.report(sequence, unreducibleShape)
		return
	}

	for i, element := range sequence.Values {
		reduced := nz.reduceValue(element)

		sequence.Values[i] = reduced
		sequence.Entries[i].Value = reduced
	}
}
