package config

import (
	"strings"
	"testing"
)

// The oracle the coalescing cases are judged by, kept beside them but apart from them: what makes a
// document a coalescing case (one extent, two strengths, in a named order), and what makes its
// rendered output correct (one replacement, and a caret that moved with the text it points at).

// assertOneExtentTwoStrengths pins the three facts that make a fixture a coalescing case: both
// paths resolve, they reach one written extent, and they declare two different strengths.
func assertOneExtentTwoStrengths(t *testing.T, document string, firstKind sensitivity) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture never reaches the path-aware branch: %+v", diags)
	}
	_ = normalize(text, root)

	found := sharedExtentRecords(t, sensitiveValues(text, schemaLevels[levelRoot], root))
	if len(found) != 2 {
		t.Fatalf("the schema walk found %d sensitive values, want the two declared paths: %+v",
			len(found), found)
	}
	if found[0].line != found[1].line || found[0].column != found[1].column ||
		found[0].text != found[1].text {
		t.Fatalf("the two paths reached separate extents %+v and %+v, want one written value",
			found[0], found[1])
	}
	if found[0].kind == found[1].kind {
		t.Fatalf("both paths declared strength %d, want one urlPassword and one entireValue",
			found[0].kind)
	}
	if found[0].kind != firstKind {
		t.Fatalf("the walk recorded strength %d first, want %d; this case is named for that order",
			found[0].kind, firstKind)
	}
}

// sharedExtentRecords drops the one record every fixture here contributes besides the shared
// extent: each writes `secrets: [*shared]`, and an alias name is author-written text inside a
// container the table declares secret in full, so that container reports its own extent
// (containerinterior.go). It is a different written extent, not a second reading of the shared one,
// and letting it into the count would say the two paths reached three things.
//
// Exactly one is required rather than filtered away silently, so a fixture that stops writing its
// alias inside a container -- or one that starts reporting a second container -- fails here by name
// instead of quietly changing what the rows below are measured over.
func sharedExtentRecords(t *testing.T, found []sensitiveValue) []sensitiveValue {
	t.Helper()

	shared := make([]sensitiveValue, 0, len(found))
	containers := 0
	for _, value := range found {
		if value.writtenAsAContainer && value.ownsUnclaimedBytes {
			containers++
			continue
		}
		shared = append(shared, value)
	}
	if containers != 1 {
		t.Fatalf("the walk reported %d containers holding unclaimed runes, want the one the alias "+
			"is written inside: %+v", containers, found)
	}
	return shared
}

// sharedExtentMarkerPosition derives the diagnostic's position from the fixture rather than recording one:
// the marker's line is its 1-based index among the document's lines, and its column is the runes written
// before it plus one.
func sharedExtentMarkerPosition(t *testing.T, document string) (int, int) {
	t.Helper()
	for index, line := range strings.Split(document, "\n") {
		if at := strings.Index(line, sharedExtentTailMarker); at >= 0 {
			return index + firstLine, runeCount(line[:at]) + firstColumn
		}
	}
	t.Fatal("fixture has no tail marker for the caret to be derived against")
	return noColumn, noColumn
}

// assertCaretAtMarker checks the geometry a length-changing replacement puts at risk. The
// placeholder is shorter than the value it replaces, so every rune to its right moves left; the
// caret has to move with them. Both offsets are counted in runes over the whole rendered line, and
// the snippet and caret lines share one gutter, so the two are directly comparable.
func assertCaretAtMarker(t *testing.T, rendered string) {
	t.Helper()
	var quoted, caret string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, sharedExtentTailMarker) {
			quoted = line
		}
		if strings.Contains(line, "^ "+sharedExtentMessage) {
			caret = line
		}
	}
	if quoted == "" || caret == "" {
		t.Fatalf("the renderer omitted the governed line or its caret:\n%s", rendered)
	}

	marker := runeCount(quoted[:strings.Index(quoted, sharedExtentTailMarker)])
	pointer := runeCount(caret[:strings.Index(caret, "^")])
	if pointer != marker {
		t.Errorf("caret at rune %d, marker at rune %d:\n%s", pointer, marker, rendered)
	}
	replacement := strings.Index(quoted, redactionPlaceholder)
	if replacement < 0 || replacement >= strings.Index(quoted, sharedExtentTailMarker) {
		t.Errorf("the replacement did not leave its source tail legible:\n%s", rendered)
	}
}
