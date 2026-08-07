package config

import "testing"

func TestLineNumbersRightAlignToTheWidestNumberInTheBlock(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: "nine to eleven", lines: []string{"nine", "ten", "eleven"},
			want: `gutter.yaml:11:1
    9 | nine
   10 | ten
>  11 | eleven
        ^ message
`,
		},
		{
			name:  "ninety-nine to a hundred and one",
			lines: []string{"ninety-nine", "one hundred", "one hundred and one"},
			want: `gutter.yaml:101:1
    99 | ninety-nine
   100 | one hundred
>  101 | one hundred and one
         ^ message
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			offending := blockLastLine(t, tc.want)
			source := linesEndingWith(tc.lines, offending)
			diag := Error{File: "gutter.yaml", Line: offending, Col: 1, Msg: "message"}
			if got := (Errors{diag}).Render([]byte(source)); got != tc.want {
				t.Errorf("rendered output:\n%s\nwant:\n%s\n%s",
					got, tc.want, goldenDiff(tc.want, got))
			}
		})
	}
}

func TestATabRendersAsOneSpaceAndTheCaretHolds(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		col        int
		want       string
		wantTarget rune
	}{
		{
			name: "a tab before the token", source: "mode:\tfull\n", col: 7, wantTarget: 'f',
			want: "gutter.yaml:1:7\n" +
				">  1 | mode: full\n" +
				"             ^ message\n",
		},
		{
			name:   "a multi-byte character before the token",
			source: "schéma: nöty\n", col: 9, wantTarget: 'n',
			want: "gutter.yaml:1:9\n" +
				">  1 | schéma: nöty\n" +
				"               ^ message\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diag := Error{File: "gutter.yaml", Line: 1, Col: tc.col, Msg: "message"}
			got := Errors{diag}.Render([]byte(tc.source))
			if got != tc.want {
				t.Errorf("rendered output:\n%s\nwant:\n%s\n%s",
					got, tc.want, goldenDiff(tc.want, got))
			}
			if target := caretTarget(t, got); target != tc.wantTarget {
				t.Errorf("caret points at %q, want %q", target, tc.wantTarget)
			}
		})
	}
}

func TestTheCaretLandsOnTheTokenForAPositionThePackageDerived(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		path       string
		wantTarget rune
	}{
		{name: "a tab before the token", source: "mode:\tfull\n", path: "$.mode", wantTarget: 'f'},
		{name: "a multi-byte character before the token", source: "schéma: nöty\n", path: "$.schéma", wantTarget: 'n'},
		{name: "a run of tabs before the token", source: "a:\t\t\tb\n", path: "$.a", wantTarget: 'b'},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.source, tc.path)
			diag := NewError(R41, positionOf(src, node), "message")
			block := Errors{diag}.Render([]byte(tc.source))
			if target := caretTarget(t, block); target != tc.wantTarget {
				t.Errorf("caret points at %q, want %q\n%s", target, tc.wantTarget, block)
			}
		})
	}
}

func TestTwoLeadingContextLinesAndNoTrailingContext(t *testing.T) {
	source := "one\ntwo\nthree\nfour\nfive\n"
	tests := []struct {
		name string
		line int
		want string
	}{
		{
			name: "three context lines available, two shown, none trailing", line: 4,
			want: `snippet.yaml:4:1
   2 | two
   3 | three
>  4 | four
       ^ message
`,
		},
		{
			name: "one context line available at line two", line: 2,
			want: `snippet.yaml:2:1
   1 | one
>  2 | two
       ^ message
`,
		},
		{
			name: "no context line available at line one", line: 1,
			want: `snippet.yaml:1:1
>  1 | one
       ^ message
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diag := Error{File: "snippet.yaml", Line: tc.line, Col: 1, Msg: "message"}
			if got := (Errors{diag}).Render([]byte(source)); got != tc.want {
				t.Errorf("rendered output:\n%s\nwant:\n%s\n%s",
					got, tc.want, goldenDiff(tc.want, got))
			}
		})
	}
}
