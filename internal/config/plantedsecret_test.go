package config

import (
	"slices"
	"strings"
	"testing"
)

// The oracle for the risk this step's own diagnostics widen: a snippet quotes context lines around
// its caret, so an unknown key written beside a declared secret puts that secret within quoting
// distance for the first time. Nothing in the corpus may render one.

// TestNoGoldenQuotesAPlantedSecret is this step's answer to the risk its own diagnostics widen: a
// snippet quotes context lines around its caret, so an unknown key beside a declared secret puts
// that secret within quoting distance for the first time.
//
// The oracle is the whole corpus rather than the fixtures this step added, because a golden is a
// golden whoever wrote it, and every marker planted is searched for rather than only the sentinel
// that opens it.
func TestNoGoldenQuotesAPlantedSecret(t *testing.T) {
	planted := plantedMarkers(t)
	if len(planted) == 0 {
		t.Fatal("no fixture plants a marked secret, so this oracle would see nothing")
	}

	goldens := goldenPaths(t)
	if len(goldens) == 0 {
		t.Fatal("no golden files found, so this test would pass vacuously")
	}
	for _, path := range goldens {
		rendered := readGolden(t, path)

		for _, marker := range planted {
			if strings.Contains(rendered, marker) {
				t.Errorf("%s quotes %s, which a fixture planted inside a declared secret", path, marker)
			}
		}
	}
}

// plantedMarkers is every leak marker any fixture of either corpus writes, read from the fixtures
// themselves so that planting one in a new fixture extends the oracle by existing.
func plantedMarkers(t *testing.T) []string {
	t.Helper()

	var found []string
	for _, dir := range []string{invalidCorpus, validCorpus} {
		for _, path := range fixturesIn(t, dir) {
			found = append(found, markersIn(string(readFixtureBytes(t, path)))...)
		}
	}

	slices.Sort(found)
	return slices.Compact(found)
}

// markersIn is every leak marker written in a text: the sentinel plus the run of marker characters
// that follows it. A marker ends at the first character that cannot be part of one, which keeps a
// marker planted inside a URL from swallowing the rest of the host.
func markersIn(text string) []string {
	var found []string

	for rest := text; ; {
		at := strings.Index(rest, leakSentinel)
		if at < 0 {
			return found
		}

		rest = rest[at:]
		end := len(leakSentinel)
		for end < len(rest) && isMarkerCharacter(rest[end]) {
			end++
		}
		found = append(found, rest[:end])
		rest = rest[end:]
	}
}

// isMarkerCharacter reports whether a byte can be part of a leak marker, which is written as the
// sentinel followed by a line number, a hyphen and an end name.
func isMarkerCharacter(char byte) bool {
	switch {
	case char >= '0' && char <= '9', char >= 'A' && char <= 'Z', char == '-':
		return true
	}
	return false
}
