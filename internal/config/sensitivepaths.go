package config

import (
	"cmp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file is D3's path-aware branch: while the document parses, which values are secret is
// decided by where they sit, so `database.url` keeps everything but its password and
// `destination.url`, whose public diagnostic sensitivity keeps its source text verbatim.
//
// It replaces what `schemawalk.go` found, and holds the only writes to the render copy on this
// branch. Which values those are is that file's question; this one answers what happens to each and
// in what order, because a replacement's coordinates stay true only if the replacements are applied
// in the right one.

// redactDeclaredPaths replaces every value the schema declares sensitive, in the order that
// keeps each replacement's coordinates true.
//
// It makes the render copy it writes rather than being handed one, so no slice it did not create is
// written here and the source's lines cannot be the ones a replacement lands in. redactValue
// reports its rewritten line instead of writing it, so the copy is written in two statements, both
// below.
func redactDeclaredPaths(text *source, root ast.Node) quotableLines {
	copied := newRenderCopy(text)

	// Stage E is the package's one reduction of anchors, aliases, and merges. Reusing it here
	// makes a sensitive use point back to the source node its anchor declared, which is the line
	// later diagnostics inherit. A refused alias remains its own harmless reference token; these
	// diagnostics are deliberately discarded because rendering reports only its caller's run.
	_ = normalize(text, root)
	found := coalescedSensitiveValues(sensitiveValues(text, schemaLevels[levelRoot], root))

	slices.SortFunc(found, rightmostOnItsLineFirst)
	for _, value := range found {
		rewritten, spansLines, replacements := redactValue(copied.line(value.line), text, value)

		copied.rewrite(value.line, rewritten)
		for _, replacement := range replacements {
			copied.carryReplacement(value.line,
				replacement.from, replacement.past, replacement.runes)
		}
		// Above the value and below it are two different questions, and the second is asked only
		// of a value whose own line could not contain it. The first is asked of every value: the
		// runes an author wrote between the key's colon and the value's first token are the
		// value's whatever redactValue was able to do with the line it is anchored on.
		blankTheOpenerGapAbove(copied, value)
		if spansLines {
			blankValueReach(copied, text, value)
		}
	}
	return copied.quotable()
}

// coalescedSensitiveValues applies one replacement at each source extent. Alias and merge
// reduction can make several sensitive paths share one node; when their declarations differ,
// the strictest one wins.
func coalescedSensitiveValues(values []sensitiveValue) []sensitiveValue {
	type extent struct {
		line   int
		column columnInRunes
		text   string
	}

	found := make([]sensitiveValue, 0, len(values))
	indexes := make(map[extent]int, len(values))
	for _, value := range values {
		key := extent{line: value.line, column: value.column, text: value.text}

		index, alreadyFound := indexes[key]
		if !alreadyFound {
			indexes[key] = len(found)
			found = append(found, value)
			continue
		}
		found[index] = strictestOf(found[index], value)
	}
	return found
}

// strictestOf is the stricter answer of two declarations that located one source extent, field by
// field. Each field is merged in the direction that hides more, which is the whole of what
// "the strictest one wins" means above.
func strictestOf(kept, value sensitiveValue) sensitiveValue {
	kept.kind = max(kept.kind, value.kind)
	kept.throughLine = max(kept.throughLine, value.throughLine)
	// Merged the same way the extent is, and for the same reason: two declarations reaching
	// one node must leave the strictest answer standing, and "the container around it holds
	// runes nobody claims" is strictly stricter than not.
	kept.ownsUnclaimedBytes = kept.ownsUnclaimedBytes || value.ownsUnclaimedBytes
	// The same rule in the other direction: the earlier of two extents reaches further
	// back. Without it, a value located both as its container's only element -- which
	// reaches back no further than its own text -- and as that container reaches back only
	// as far as whichever arrived first, and the note above them both stays rendered.
	if value.reachesBackTo >= firstLine &&
		(kept.reachesBackTo < firstLine || value.reachesBackTo < kept.reachesBackTo) {
		kept.reachesBackTo = value.reachesBackTo
	}
	return kept
}

// rightmostOnItsLineFirst orders replacements so that each is applied to text no earlier
// replacement has moved: within a line the rightmost value goes first, which leaves every column to
// its left exactly where it was derived. Two secrets share a line whenever they are written as a
// flow sequence.
func rightmostOnItsLineFirst(a, b sensitiveValue) int {
	if order := cmp.Compare(a.line, b.line); order != 0 {
		return order
	}
	return cmp.Compare(b.column, a.column)
}

// redactValue is the render-only copy's line with one sensitive value replaced, whether that
// value's remaining lines must go too, and the rune delta carried to surviving text on its right.
// It returns the rewritten line rather than writing it, so the copy is written by its one owner.
func redactValue(copied string, text *source, value sensitiveValue) (string, bool, []redactionEdit) {
	if value.text == "" {
		// The parser reports nothing written *as* this value, so its own line holds nothing to
		// replace: the key before it is public and no value follows it there. The block beneath the
		// key is still this value's, though, and an author can write in it -- `secrets:` followed by
		// an indented comment is a value the table declares secret in full whose whole written
		// content is that comment, and it reached rendered output while this branch answered
		// "nothing was written to hide".
		//
		// So the line stays and the reach goes. That costs nothing where the value really is absent:
		// the next line dedents, and blankValueReach then blanks no line at all. Both directions are
		// run by TestASensitiveKeyWhoseWrittenContentIsOnlyACommentStillLosesIt.
		return copied, true, nil
	}

	// Read from the line as it was read, never from the copy being edited. Right-to-left
	// ordering is what makes that sound: no earlier replacement has touched this column or
	// anything before it.
	written := text.Line(value.line)
	start := value.column
	if start < firstColumn {
		// The value has a line and no column, which is a state no document produces today and
		// which nothing here can slice a line at. The whole line goes rather than none of it: this
		// sits inside the component whose contract is to over-redact, and a fail-open answer in it
		// is the shape of defect the last three rounds were spent on. Reached directly by
		// TestRedactingAValueWithNoColumnBlanksItsWholeLine.
		return blankFromColumn(copied, firstColumn), true, nil
	}

	if !strings.HasPrefix(runesFrom(written, start), value.text) || value.ownsUnclaimedBytes {
		// Two reasons to take the rest of the line and every line this value reaches, either of
		// which is enough, because going wholesale is the over-redaction D3 prefers to a guess.
		//
		// The first is that the value is not what its line holds from here on, which is how the
		// parser reports the two shapes that span lines: a block scalar's node is its `|`
		// indicator, and a multi-line quoted scalar is folded onto one line.
		//
		// The second is that the container this value was split out of holds runes no child of it
		// claims. Replacing the children alone left those runes rendered -- the commas and brackets
		// harmlessly, but also a `&anchor` property stage E drops from the tree before this walk
		// sees it, a `!!tag`, and a comment written between two elements, all of them author-written
		// text inside a value the table declares secret in full. That condition used to read
		// `throughLine > line`, a property of the reproduction rather than of the class: the
		// reproduction's container closed on a later line, while the same runes fit between two
		// elements of a one-line container, and no block container reports a closing line at all.
		// TestASensitiveContainerHoldingRunesNoChildClaimsIsBlankedWholesale runs the class, one row
		// per property, in both container styles and with and without the incidental newline.
		return blankFromColumn(copied, start), true, nil
	}

	// The value matched what its line holds, so its extent is known -- but "its line" is a *parser* line,
	// and a lone carriage return ends one of those without ending the line the author wrote. A value
	// finishing exactly at such a return is contained by this test and its tail is on the next parser
	// line, which nothing on this branch would then blank: `secrets: PART1\r other: BBB` replaced the
	// value and rendered the tail. So containment here is only containment if the next line is a line of
	// its own.
	spansIntoTheSplitTail := text.continuesTheLineAbove(value.line + 1)

	replacement, edits := redactionGeometry(value)
	if replacement == value.text {
		return copied, spansIntoTheSplitTail, nil
	}
	count := runeCount(value.text)
	return replaceRunes(copied, start, count, replacement), spansIntoTheSplitTail, edits
}
