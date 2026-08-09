package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The corpus inventory, derived from each owning step rather than counted from the directories.
// fixturesIn is deliberately shallow: the top-level totals exclude the nested environment fixture
// and the separate warning corpus, so those two extents are pinned independently.
const (
	step8RejectingFixtures          = 66
	step8AcceptingFixtures          = 39
	step9RejectingFixtures          = 33 // 32 single-defect fixtures plus the five-mistake fixture
	step9AcceptingFixtures          = 12
	step10AcceptingFixtures         = 2 // three-layer and partial field-level merge fixtures
	step11RejectingFixtures         = 7 // six AC #34 cases plus the inherited-retry dedupe case
	step11AcceptingFixtures         = 7 // comparisons plus three warning nonraising controls in valid/
	step11NestedEnvironmentFixtures = 1
	step11WarningFixtures           = 5 // only raising/error scenarios remain under warnings/
	step12AcceptingFixtures         = 2
	step13RejectingFixtures         = 8
	step13AcceptingFixtures         = 4 // R42, three reference-contract roles
	step13WarningFixtures           = 1
	isDistinctRejectingFixtures     = 3 // insert/delete placement and scalar conversion
	isDistinctAcceptingFixtures     = 1 // true and false update-operation values
	rejectingFixtures               = step8RejectingFixtures + step9RejectingFixtures +
		step11RejectingFixtures + step13RejectingFixtures + isDistinctRejectingFixtures
	acceptingFixtures = step8AcceptingFixtures + step9AcceptingFixtures +
		step10AcceptingFixtures + step11AcceptingFixtures + step12AcceptingFixtures +
		step13AcceptingFixtures + isDistinctAcceptingFixtures
)

// TestTheCorpusHoldsTheFixturesTheNotesInventory ties the two together.
func TestTheCorpusHoldsTheFixturesTheNotesInventory(t *testing.T) {
	tests := []struct {
		dir  string
		want int
	}{
		{invalidCorpus, rejectingFixtures},
		{validCorpus, acceptingFixtures},
		{filepath.Join(invalidCorpus, "step11"), step11NestedEnvironmentFixtures},
		{filepath.Join("testdata", "warnings"), step11WarningFixtures + step13WarningFixtures},
	}
	for _, tc := range tests {
		if got := len(fixtureNamesIn(t, tc.dir)); got != tc.want {
			t.Errorf("%s holds %d fixtures; its owners declare %d", tc.dir, got, tc.want)
		}
	}
}

// fixtureNamesIn returns the base names of the immediate YAML children of one corpus directory.
func fixtureNamesIn(t *testing.T, dir string) []string {
	t.Helper()
	paths := fixturesIn(t, dir)
	if len(paths) == 0 {
		t.Fatalf("%s holds no fixtures, so this assertion would pass vacuously", dir)
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}

func hasPrefixed(names []string, prefix string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func anyCarries(diags Errors, rule RuleID) bool {
	for _, diag := range diags {
		if diag.Rule == rule {
			return true
		}
	}
	return false
}
