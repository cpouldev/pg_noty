package config

import "testing"

// TestGoccyGivesATabZeroWidth pins the parser accounting runeColumn corrects.
func TestGoccyGivesATabZeroWidth(t *testing.T) {
	tests := []struct {
		name, src, path string
		wantCol         int
		wantWidth       string
	}{
		{"block style tab", "mode:\tfull\n", "$.mode", 6, "zero"},
		{"block style space", "mode: full\n", "$.mode", 7, "one"},
		{"flow mapping tab", "a: {b:\tc}\n", "$.a.b", 7, "zero"},
		{"flow mapping space", "a: {b: c}\n", "$.a.b", 8, "one"},
		{"block scalar tab", "when:\t|\n  x\n", "$.when", 6, "zero"},
		{"block scalar space", "when: |\n  x\n", "$.when", 7, "one"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, node := nodeAt(t, tc.src, tc.path)
			if got := parserReportedColumn(node.GetToken()); got != tc.wantCol {
				t.Errorf("library column = %d, want %d; separator no longer has %s width",
					got, tc.wantCol, tc.wantWidth)
			}
		})
	}
}

func TestTheReportedColumnAppliesTheTabPolicy(t *testing.T) {
	const spaceColumns = 1
	tests := []struct {
		name, spaced, tabbed, path string
		tabRuns                    int
	}{
		{"one block tab", "mode: full\n", "mode:\tfull\n", "$.mode", 1},
		{"three block tabs", "a: b\n", "a:\t\t\tb\n", "$.a", 3},
		{"one flow tab", "a: {b: c}\n", "a: {b:\tc}\n", "$.a.b", 1},
		{"one block scalar tab", "when: |\n  x\n", "when:\t|\n  x\n", "$.when", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spacedSrc, spacedNode := nodeAt(t, tc.spaced, tc.path)
			tabbedSrc, tabbedNode := nodeAt(t, tc.tabbed, tc.path)
			moved := positionOf(tabbedSrc, tabbedNode).Col() - positionOf(spacedSrc, spacedNode).Col()
			if want := tc.tabRuns*tabColumns - spaceColumns; moved != want {
				t.Errorf("tabs moved column by %d, want %d", moved, want)
			}
		})
	}
}

func TestAPositionCannotCarryAColumnItDidNotDerive(t *testing.T) {
	where := sourcePos{
		file: "listeners.yaml", line: 1, lineText: "mode:\tfull",
		reportedColumn: 6, path: "mode",
	}
	if got := where.Col(); got != 7 {
		t.Errorf("Col() = %d, want runeColumn's 7", got)
	}
}
