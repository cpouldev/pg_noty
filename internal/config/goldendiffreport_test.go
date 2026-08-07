package config

import (
	"slices"
	"strings"
	"testing"
)

func TestGoldenDiffNamesOnlyTheLinesThatDiffer(t *testing.T) {
	const golden = "one\ntwo\nthree\nfour\nfive\nsix"
	tests := []struct {
		name        string
		got         string
		wantRemoved []string
		wantAdded   []string
	}{
		{name: "a line inserted in the middle",
			got:       "one\ntwo\nINSERTED\nthree\nfour\nfive\nsix",
			wantAdded: []string{`"INSERTED"`}},
		{name: "a line inserted at the top",
			got:       "INSERTED\none\ntwo\nthree\nfour\nfive\nsix",
			wantAdded: []string{`"INSERTED"`}},
		{name: "a line removed from the middle",
			got: "one\ntwo\nfour\nfive\nsix", wantRemoved: []string{`"three"`}},
		{name: "a line changed in place",
			got:         "one\ntwo\nCHANGED\nfour\nfive\nsix",
			wantRemoved: []string{`"three"`}, wantAdded: []string{`"CHANGED"`}},
		{name: "a line differing only in trailing whitespace",
			got:         "one\ntwo\nthree \nfour\nfive\nsix",
			wantRemoved: []string{`"three"`}, wantAdded: []string{`"three "`}},
		{name: "no difference at all", got: golden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := goldenDiff(golden, tc.got)
			if removed := reportedLines(report, "- "); !slices.Equal(removed, tc.wantRemoved) {
				t.Errorf("the report names %v as only in the golden, want %v:\n%s",
					removed, tc.wantRemoved, report)
			}
			if added := reportedLines(report, "+ "); !slices.Equal(added, tc.wantAdded) {
				t.Errorf("the report names %v as only in the output, want %v:\n%s",
					added, tc.wantAdded, report)
			}
		})
	}
}

func reportedLines(report, marker string) []string {
	var found []string
	for _, line := range strings.Split(report, "\n") {
		if after, carries := strings.CutPrefix(line, marker); carries {
			found = append(found, after)
		}
	}
	return found
}
