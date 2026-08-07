package config

import (
	"slices"

	"github.com/goccy/go-yaml/ast"
)

// This file is what `<<` contributes to the mapping that writes it, what it may point at, and
// what it may never override. The rule it implements is YAML's own -- a key written directly in
// the mapping wins, and among several merge sources the earlier one wins -- and the whole of this
// file exists because neither of the library's two ways of getting there is safe (V5, pinned by
// goccymerge_test.go and cited at normalize.go's call site).
//
// When two keys are the same key is keyidentity.go's question, because the operations fold asks
// it too.

// nonMappingMergeSource is the one refusal this file owns, declared beside the code that raises
// it. The other reason a source can fail -- naming an anchor nothing declared -- is anchor.go's
// undefinedAlias, because that is a fact about the name rather than about what the name found.
var nonMappingMergeSource = fault{
	message: "a merge key can only merge a mapping",
	hint:    "point << at an anchor whose value is a mapping, or at a list of them",
}

// expandedWith is what a mapping holds once its merge keys have contributed: the keys its author
// wrote here, in the order written, then every key the sources bring in that none of them spells.
//
// The `<<` entries themselves are gone, which is skill Pattern 4's fourth step: a merge key is
// YAML syntax rather than a field of the schema, so the structural pass must never meet one.
//
// This is the precedence rule, and it is one invariant rather than a comparison of positions:
// `held` starts as every key the author wrote directly, so a merged key is added only where none
// was written. Whether a `<<` stands above or below the key it would have overridden cannot
// change the answer, which is exactly what neither library path achieves (V5). Recording each
// inherited key as it is added is the same rule applied between sources, which is what makes the
// earlier source of `<<: [*a, *b]` win.
//
// The inherited entries follow the direct ones, so the result is deliberately **not** in document
// order: an inherited entry carries the anchor's own earlier line while sitting last. Nothing
// after this stage may read entry order as document order (Implementation Note 6).
func expandedWith(direct []*ast.MappingValueNode, sources []*ast.MappingNode) []*ast.MappingValueNode {
	held := keyIdentitiesOf(direct)

	// A copy of its own rather than appends onto `direct`, whose spare capacity belongs to the
	// caller: an inherited entry written into it would be a write through an argument this
	// function's signature says it only reads.
	expanded := slices.Clone(direct)

	for _, source := range sources {
		for _, entry := range source.Values {
			identity := keyIdentity(entry.Key)
			if held[identity] {
				continue
			}
			held[identity] = true
			expanded = append(expanded, entry)
		}
	}
	return expanded
}

// mergedMappings is every mapping one `<<` names, in the order YAML applies them: within
// `<<: [*a, *b]`, the mapping *a names first.
//
// One `<<` yields at most one diagnostic, and this is the only place either of its two refusals
// is raised. Both are answered elsewhere and reported here, so a reader sees at one glance every
// way a merge key can be refused -- and a token cannot collect two of them, which is the
// multiplicity ADR-3 forbids.
//
// Sources are resolved here, where the walk meets the entry, rather than after the whole mapping
// has been read. That is what makes an anchor available to the aliases below it and to nothing
// above it -- the rule every other position already gets from the anchor table's shape.
func (nz *normalizer) mergedMappings(named ast.Node) []*ast.MappingNode {
	held, why, refused := nz.resolvedMergeSource(named)
	if refused {
		nz.report(held, why)
		return nil
	}

	sources, mergeable := mergeSourcesOf(held)
	if !mergeable {
		// Anchored on what the value turned out to be rather than on the alias or the wrapper
		// that named it. That is the line the author has to change, and it is what puts every
		// listener merging one broken anchor on a single position -- so the existing
		// de-duplication on (File, Line, Col, Msg) collapses them into one diagnostic rather than
		// one per listener (ADR-3, TestTwoListenersAliasingOneBrokenAnchorYieldOneDiagnostic).
		nz.report(held, nonMappingMergeSource)
		return nil
	}
	return sources
}

// mergeSourcesOf is the mappings a resolved `<<` value names, and whether it names mappings at
// all: the value itself when it is a mapping, and every element when it is a sequence of
// mappings. Those are the merge type's own two shapes, and it is asked of what the value
// *resolved to*, because alias resolution is transparent -- an anchor naming a list of mappings
// is a list of mappings.
//
// A sequence holding anything else is not a list of sources with a bad element in it; it is a
// value a `<<` cannot merge. Saying so once, about the value, is what keeps `<<: *columns` --
// where the anchor names `[id, total]` -- one diagnostic at the list the author pointed at rather
// than one per column name, none of which is itself a mistake.
func mergeSourcesOf(held ast.Node) ([]*ast.MappingNode, bool) {
	if mapping, isMapping := held.(*ast.MappingNode); isMapping {
		return []*ast.MappingNode{mapping}, true
	}
	sequence, isSequence := held.(*ast.SequenceNode)
	if !isSequence {
		return nil, false
	}

	sources := make([]*ast.MappingNode, 0, len(sequence.Values))
	for _, element := range sequence.Values {
		if alreadyRefusedAlias(element) {
			// It neither contributes a source nor disqualifies the list: the author reads the
			// refusal about the name that found nothing, and not a second one about the list
			// that held it.
			continue
		}

		mapping, isMapping := beneathNodeProperties(element).(*ast.MappingNode)
		if !isMapping {
			return nil, false
		}
		sources = append(sources, mapping)
	}
	return sources, true
}

// resolvedMergeSource is the node the whole `<<` value stands for, the reason it stands for
// nothing, and whether there is such a reason. The node returned is the one to report that reason
// against, so the caller needs nothing else to raise it.
//
// Presence is its own result rather than an empty message field, so a fault that happened to say
// nothing could not read as the absence of one.
//
// A value written out in place goes through reduceValue -- the reduction a value position gets --
// rather than through a switch of this position's own. A switch written for one position admits
// fewer wrappers than the grammar permits in it, so a legal document is refused, and refused by
// the arm meant for a different mistake. An alias is the one spelling that reduction must not be
// handed, because only the anchor table can tell its two failures apart.
//
// The replacement is used rather than written back, which is not Implementation Note 4's mistake:
// the `<<` entry holding it does not survive this mapping. What the value resolved to is merged
// and the entry is dropped, so there is no later reader for a write-back to serve.
func (nz *normalizer) resolvedMergeSource(named ast.Node) (held ast.Node, why fault, refused bool) {
	alias, isAlias := named.(*ast.AliasNode)
	if !isAlias {
		return beneathNodeProperties(nz.reduceValue(named)), fault{}, false
	}

	anchored, defined := nz.anchoredBy(alias)
	if !defined {
		return alias, nz.unresolvable(alias), true
	}
	return beneathNodeProperties(anchored), fault{}, false
}
