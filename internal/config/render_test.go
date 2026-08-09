package config

import (
	"strconv"
	"testing"
)

const authoritativeSource = `version: 1
instance: noty

database:
  url: ${DATABASE_URL}
  schema: noty

listeners:
- name: order_paid
  table: public.orders
  operations: [update]
  payload:
    mode: columns
    colums: [id, total]
`

const authoritativeBlock = `listeners.yaml:14:5
   12 |   payload:
   13 |     mode: columns
>  14 |     colums: [id, total]
            ^ unknown field "colums"
              did you mean "columns"?
`

func unknownPayloadKey() Error {
	return Error{
		Rule: R41,
		File: "listeners.yaml",
		Line: 14,
		Col:  5,
		Path: "listeners[0].payload.colums",
		Msg:  `unknown field "colums"`,
		Hint: `did you mean "columns"?`,
	}
}

func TestRenderReproducesTheSpecificationsBlockByteForByte(t *testing.T) {
	got := Errors{unknownPayloadKey()}.Render([]byte(authoritativeSource))
	if got != authoritativeBlock {
		t.Errorf("rendered output differs from the specification's block\n got:\n%s\nwant:\n%s\n%s",
			got, authoritativeBlock, goldenDiff(authoritativeBlock, got))
	}
	if target := caretTarget(t, got); target != 'c' {
		t.Errorf("caret points at %q, want the %q of colums", target, 'c')
	}
}

func TestCaretAndHintFollowThePrefixWidth(t *testing.T) {
	const (
		markerWidth = 3
		barWidth    = 3
		hintOffset  = 2
	)
	tests := []struct {
		name string
		line int
		col  int
	}{
		{name: "one digit at the first line and column", line: 1, col: 1},
		{name: "one digit with one context line above", line: 2, col: 3},
		{name: "one digit at the last one-digit line", line: 9, col: 1},
		{name: "two digits just past the transition", line: 11, col: 7},
		{name: "two digits at the specification's line", line: 14, col: 5},
		{name: "two digits at the last two-digit line", line: 99, col: 2},
		{name: "three digits just past the transition", line: 101, col: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := numberedLines(tc.line)
			diag := Error{File: "gutter.yaml", Line: tc.line, Col: tc.col, Msg: "message", Hint: "hint"}
			block := Errors{diag}.Render([]byte(source))
			wantPrefix := markerWidth + len(strconv.Itoa(tc.line)) + barWidth
			wantCaret := wantPrefix + tc.col
			if got := caretColumn(t, block); got != wantCaret {
				t.Errorf("caret at column %d, want %d (prefix %d + column %d)",
					got, wantCaret, wantPrefix, tc.col)
			}
			if got := hintColumn(t, block); got != wantCaret+hintOffset {
				t.Errorf("hint at column %d, want %d (caret %d + %d)",
					got, wantCaret+hintOffset, wantCaret, hintOffset)
			}
		})
	}
}

func TestAColumnBelowOneClampsToTheStartOfTheLine(t *testing.T) {
	tests := []struct {
		name string
		col  int
	}{
		{name: "zero", col: 0},
		{name: "negative", col: -3},
	}
	want := "gutter.yaml:1:1\n" +
		">  1 | one\n" +
		"       ^ message\n"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diag := Error{File: "gutter.yaml", Line: 1, Col: tc.col, Msg: "message"}
			if got := (Errors{diag}).Render([]byte("one\n")); got != want {
				t.Errorf("rendered output:\n%s\nwant:\n%s\n%s",
					got, want, goldenDiff(want, got))
			}
		})
	}
}
