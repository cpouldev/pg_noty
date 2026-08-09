package config

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// This file answers two questions for the redaction walk, and keeps them apart: where does a
// value's written text begin, and how far back does its redactable extent reach?
//
// It is a file of its own because that is not the question positionOf answers. positionOf answers
// where a *caret* belongs, and a mapping's own token is the colon of its first entry -- measured, and
// pinned by TestGoccyGivesAMappingTheColonOfItsFirstEntry. The two answers coincide for every scalar,
// which is how one derivation came to serve both: a value the schema declares sensitive, written in a
// shape the parser reads as a block mapping, was blanked from its colon and rendered its first key in
// the clear.
//
// Every position that has to know where a value's text begins reads this one function rather than
// deriving it from whatever node it happens to hold. Carets keep positionOf: it was never the wrong
// answer for them, and the two questions are separated here rather than by changing what a caret
// gets.

// writtenStart is where a value's own text begins, and which of the two readings gave the answer.
type writtenStart struct {
	line   int
	column columnInRunes

	// beginsOnALineOfItsOwn reports whether the anchor line is a line of the value's own text
	// rather than the line its key is written on.
	//
	// The reach walk needs this as much as the column does, and that it needed it was missed for a
	// round: the two readings put a value's own continuation on opposite sides of the anchor line's
	// indentation. A value anchored on its key's line is reached only by lines indented *past* that
	// key; one anchored inside itself is reached by every line indented as far as its own first,
	// because that is where its sibling entries sit. Answering both from the key's rule stopped a
	// block mapping's redaction at its second entry, and rendered every entry after it.
	beginsOnALineOfItsOwn bool

	// reachesBackTo is the first line of the value's redactable *extent*, which is not always the
	// line its own text begins on. It is line for everything writtenStartOf alone answers, and
	// earlier only for a value written below its key with lines in between -- the opener gap.
	//
	// It is a second field rather than a wider `line` because the two questions have different
	// consumers: `line` and `column` are what redactValue replaces at and what every reader of a
	// caret inherits, and they keep the answer they had. Only the readers that ask how much of the
	// document a value owns -- the interior scan and the reach walk -- read this one.
	reachesBackTo int
}

// writtenStartOf is where a value's own text begins. A line below firstLine is a node this text did
// not produce, and its column is not a column either.
//
// A block mapping is the one shape whose token sits *inside* its text instead of at the start of it,
// so it is the one shape whose column is answered from its line rather than from its token: its text
// begins at that line's first non-space rune, which is its first key, or the `- ` of the sequence
// item holding it.
//
// Answering it from the line rather than from its first *key* is deliberate, and is the fail-closed
// direction. A mapping need not have a key of its own to offer: stage E removes the entry of a merge
// key it refuses, and `- secret<<: tail` -- which this library reads as a merge key, measured by
// TestGoccyReadsATrailingMergeKeyIndicatorAsAMergeKey -- then leaves an empty mapping whose only
// token is that colon. Redacting from a key that may not exist puts the answer back on the colon for
// exactly the documents where the text before it is the secret. Taking a `- ` marker or a line's
// indentation with the value is the over-redaction D3 prefers to a guess, and is what the key-scoped
// fallback does with the same line.
//
// Node properties are deliberately not read through: `&anchor value` is written from its `&`, so an
// anchored or tagged value's text begins at the property rather than at the value beneath it -- and
// a property is written beside the key, which is why such a node's text does not begin on a line of
// its own however the value beneath it is written.
func writtenStartOf(text *source, node ast.Node) writtenStart {
	at := positionOf(text, node)
	line := at.Line()
	if line < firstLine {
		return writtenStart{line: line, column: columnInRunes(at.Col()), reachesBackTo: line}
	}

	if mapping, isMapping := node.(*ast.MappingNode); isMapping && !mapping.IsFlowStyle {
		return writtenStart{
			line:                  line,
			column:                columnInRunes(indentWidth(text.Line(line))) + firstColumn,
			beginsOnALineOfItsOwn: true,
			reachesBackTo:         line,
		}
	}
	return writtenStart{
		line:                  line,
		column:                columnInRunes(at.Col()),
		beginsOnALineOfItsOwn: writtenInBlockStyle(node),
		reachesBackTo:         line,
	}
}

// writtenStartReachingItsBlockOpener is writtenStartOf with the extent question answered too, for a
// value a mapping key introduced.
//
// The parser anchors a value on the first token of its own text, so `secrets:` holding a list is
// anchored on that list's first `- `. What an author writes between the key's colon and that token
// -- a rotation note, an anchor property, several of each -- belongs to the block the colon opened
// and to no other value, because YAML permits nothing else there. Reading the extent from the
// value's own first token therefore left every one of those runes outside a scan that read as if it
// covered the whole value, and the path-aware branch rendered them while the key-scoped fallback
// blanked them.
//
// The bound is the block's own two delimiters -- the key's colon above, the value's first token
// below -- and not a property of the note that found it: nothing here reads a `#`, a line's shape,
// or how many lines the gap holds, so a comment, an anchor property and a three-line mixture are
// one case.
//
// Only `reachesBackTo` moves. `line` and `column` are what redactValue replaces at, and every
// caret in the package reads positionOf rather than this file, so no diagnostic's coordinates
// change with this answer.
func writtenStartReachingItsBlockOpener(text *source, node ast.Node) writtenStart {
	written := writtenStartOf(text, node)

	opener, found := blockOpenerLineOf(text, node)
	if !found || opener >= written.line {
		return written
	}
	// The colon's own line is the key's, which is public and stays; the gap begins below it.
	written.reachesBackTo = opener + 1
	return written
}

// blockOpenerLineOf is the line the key that introduced this value wrote its `:` on.
//
// It is read from the parser's own token stream rather than re-derived from the text, because
// indentation cannot answer it: YAML lets a block sequence sit at its key's own column, so in
// `secrets:` followed by `# note` and `- one` all three lines are indented alike and no
// indentation rule can tell the key from the note. Walking back to the first mapping-value
// indicator is exact instead -- between a key's colon and the first token of its value the grammar
// admits only comments and node properties, so the first one found is that key's.
//
// Measured and pinned per shape by TestGoccyAnchorsAValueOnTheFirstTokenOfItsOwnText. Only the
// token's *line* is read; its column is position.go's to derive (ADR-4).
func blockOpenerLineOf(text *source, node ast.Node) (int, bool) {
	first := node.GetToken()
	if first == nil {
		return noBlockOpener, false
	}

	for at := first.Prev; at != nil; at = at.Prev {
		if at.Type != token.MappingValueType {
			continue
		}
		return tokenPosition(text, at, pathOf(node)).Line(), true
	}
	// A value under no key at all -- a root-level sequence -- has nothing above it that is its own.
	// Reached by TestAValueUnderNoKeyAtAllReportsNoBlockOpener rather than by any document.
	return noBlockOpener, false
}

// noBlockOpener is the answer for a value no key introduced. It is negative because no line is
// numbered below firstLine, so it cannot be mistaken for one.
const noBlockOpener = -1

// writtenInBlockStyle reports whether a node is a container YAML writes without delimiters. Such a
// container is written beneath its key rather than beside it, so the line it is anchored on is a
// line of its own text -- which is the distinction the reach walk reads.
//
// A block sequence shares that with a block mapping without sharing the column rule: its own token
// is its first `-`, which already is the start of its text, so only the reach answer differs from a
// scalar's. Both are named here rather than the one that was reproduced, because a walk that stopped
// at a block mapping's second entry stops at a block sequence's second element for the same reason.
func writtenInBlockStyle(node ast.Node) bool {
	switch held := node.(type) {
	case *ast.MappingNode:
		return !held.IsFlowStyle
	case *ast.SequenceNode:
		return !held.IsFlowStyle
	default:
		return false
	}
}
