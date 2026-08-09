package config

import "github.com/goccy/go-yaml/ast"

// This file is the anchor table: what a name is written as, when a name becomes available, and
// what a name resolves to. The walk that fills it is normalize.go's; keeping the table here is
// what makes "an anchor is available only after its own value is reduced" a rule with one home
// rather than a comment on a switch arm.

// undefinedAlias is what a name resolving to nothing means, so it is declared beside the table
// that resolves names rather than in the two files that raise it. It quotes no text of the
// document -- not even the anchor name, which this stage has matched against no charset. The
// caret says which alias; the message says what is wrong with it (Step 3's Implementation Note
// 6).
var undefinedAlias = fault{
	message: "alias refers to an anchor that is not defined earlier in this document",
	hint:    "define the anchor with &name before the alias, or write the value out in full",
}

// selfReferentialAnchor is the other way a name finds nothing: the anchor of that name is the one
// the walk is inside. It is a second reason and therefore a second answer -- an author told to
// define the anchor before the alias would be told to do what they have already done, and the
// value they wrote has no finite text to stand for.
var selfReferentialAnchor = fault{
	message: "alias refers to the anchor whose own value it is written inside",
	hint:    "an anchor cannot contain an alias to itself; write the inner value out in full",
}

// remember records an anchor under the name it declares, and is called only once the anchored
// value has itself been reduced.
//
// That ordering is why the walk needs no set of visited nodes: an alias written inside an
// anchored value finds no anchor of that name yet, so `a: &a {self: *a}` is one refusal rather
// than a descent that never ends.
func (nz *normalizer) remember(anchor *ast.AnchorNode) {
	nz.anchors[anchorName(anchor)] = anchor.Value
}

// reduceAnchoredValue is an anchor's value, reduced while the walk records whose value it is
// inside.
//
// The record exists only to tell the two empty answers apart. Both a self-reference and a name
// nobody declared find nothing in the table -- the entry appears only once the value is reduced,
// which is what terminates the walk -- and they are different mistakes with different remedies.
// A name reused by a nested anchor needs no counting: by the time that inner anchor's value is
// reduced its own name is in the table, so nothing below it reaches this question at all.
func (nz *normalizer) reduceAnchoredValue(anchor *ast.AnchorNode) ast.Node {
	name := anchorName(anchor)

	nz.reducing[name] = true
	reduced := nz.reduceValue(anchor.Value)
	delete(nz.reducing, name)

	return reduced
}

// unresolvable is why this alias found nothing: the anchor of its name is the one the walk is
// inside, or no line above it declared one at all.
//
// It answers rather than reports, so both callers keep the refusal at the site that decides what
// else to do about it.
func (nz *normalizer) unresolvable(alias *ast.AliasNode) fault {
	if nz.reducing[aliasName(alias)] {
		return selfReferentialAnchor
	}
	return undefinedAlias
}

// rememberAnchoredKey records every anchor a key position declares. This stage rewrites no key --
// keys are stage D's, and an alias in one is refused there -- but an anchor written on a key
// still names a node, and a document aliasing it elsewhere is legal YAML.
//
// It descends the same three wrappers beneathKeyProperties reads past, and must: `!!str &k key`
// and `? !!str &k key` are legal, and an arm short of that set records nothing for them, so a
// later `*k` is refused as undefined -- a spurious diagnostic on a valid document. Measured on
// v1.19.2 before the tag arm was written: both of those spellings produced exactly that.
// A peel cannot be reused here because it discards the wrapper the anchor is, so the two sets are
// held together by their test instead (nodeproperties.go,
// TestAnAnchorWrittenOnAKeyIsAvailableToLaterAliases).
func (nz *normalizer) rememberAnchoredKey(key ast.Node) {
	for {
		switch held := key.(type) {
		case *ast.AnchorNode:
			nz.remember(held)
			key = held.Value
		case *ast.TagNode:
			key = held.Value
		case *ast.MappingKeyNode:
			// `? &name key` wraps the anchor one level down, and the wrapper's own token is only
			// the `?` (Step 3's Implementation Note 2).
			key = held.Value
		default:
			return
		}
	}
}

// alreadyRefusedAlias reports whether this node is an alias that survived the reduction, which
// means exactly one thing: the reduction refused it, at this very token.
//
// Two callers ask, and both owe the same answer -- an element of a merge list and an element of
// an operations list. Neither adds a second complaint about a token that already carries one,
// which is ADR-3's "at most one diagnostic per offending token"; each still declines to take the
// node for what it wanted it for. The rationale lives here rather than twice at the call sites.
//
// It is asked of the node as written, deliberately: YAML lets an alias carry no properties at
// all, so this is one of the few positions with nothing to read past, and
// TestGoccyRefusesToParseAnAliasCarryingANodeProperty keeps that a measurement.
func alreadyRefusedAlias(node ast.Node) bool {
	_, unresolved := node.(*ast.AliasNode)
	return unresolved
}

// anchoredBy is the node an anchor of this alias's name declared, when one did.
//
// It is a lookup and nothing else. What to do about a name nothing declared is the caller's to
// say, and the two callers say it in the same words but do different things with the answer, so
// the decision stays visible at each of them rather than hidden in here.
func (nz *normalizer) anchoredBy(alias *ast.AliasNode) (ast.Node, bool) {
	node, defined := nz.anchors[aliasName(alias)]
	return node, defined
}

// aliasName is the anchor an alias names. It is read from the node beneath the alias rather than
// from its own token, whose value is only the `*` introducer (measured, and pinned by
// TestGoccyResolvesNoAliasWhileParsing).
func aliasName(alias *ast.AliasNode) string { return alias.Value.String() }

// anchorName is the name an anchor declares, read from its own Name node for the same reason:
// an anchor's token is the `&`.
func anchorName(anchor *ast.AnchorNode) string { return anchor.Name.String() }
