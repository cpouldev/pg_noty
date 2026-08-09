package config

import "testing"

func TestRuneColumnClampsAColumnThatIsNotOnTheLine(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		reportedColumn int
		wantCol        int
	}{
		{"one column past the last rune", "payload:", 9, 9},
		{"further past the last rune", "payload:", 12, 9},
		{"line outside the source", "", 4, 1},
		{"past a line carrying a tab", "mode:\tfull", 10, 11},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runeColumn(tc.line, tc.reportedColumn); got != tc.wantCol {
				t.Errorf("runeColumn(%q, %d) = %d, want %d",
					tc.line, tc.reportedColumn, got, tc.wantCol)
			}
		})
	}
}

func TestRuneColumnAnswersNoColumnWhenTheReportedColumnIsBelowOne(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		reportedColumn int
	}{
		{"column zero on a line", "payload:", 0},
		{"negative column on a line", "payload:", -1},
		{"column zero on an empty line", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runeColumn(tc.line, tc.reportedColumn); got != noColumn {
				t.Errorf("runeColumn(%q, %d) = %d, want no column",
					tc.line, tc.reportedColumn, got)
			}
		})
	}
}

func TestTheReportedColumnIgnoresACarriageReturn(t *testing.T) {
	src, node := nodeAt(t, "payload:\r\n", "$.payload")
	got := positionOf(src, node)
	if got.Line() != 1 || got.Col() != 9 {
		t.Errorf("positionOf(...) = %d:%d, want 1:9", got.Line(), got.Col())
	}
}

func TestComputedColumnDiffersFromTheLibraryColumnWhenTabsPrecedeTheToken(t *testing.T) {
	tests := []struct{ name, src, path string }{
		{"one tab before a block value", "mode:\tfull\n", "$.mode"},
		{"tab after a multi-byte key", "a: 1\né:\tvalue\n", "$.é"},
		{"run of tabs before a value", "a:\t\t\tb\n", "$.a"},
		{"tab inside a flow mapping", "a: {b:\tc}\n", "$.a.b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.src, tc.path)
			if got, library := positionOf(src, node).Col(), parserReportedColumn(node.GetToken()); got == library {
				t.Errorf("computed column %d still equals the library column", got)
			}
		})
	}
}

func TestComputedColumnMatchesTheLibraryColumnWhenNoTabPrecedesTheToken(t *testing.T) {
	src, node := nodeAt(t, "payload:\n    mode: full\n", "$.payload.mode")
	if got, want := positionOf(src, node).Col(), parserReportedColumn(node.GetToken()); got != want {
		t.Errorf("computed column %d, want library column %d", got, want)
	}
}
