package config

import "testing"

type columnCase struct {
	name     string
	src      string
	path     string
	wantLine int
	wantCol  int
}

var columnCases = []columnCase{
	{"plain ascii", "payload:\n    mode: full\n", "$.payload.mode", 2, 11},
	{"multi-byte character before the token", "a: 1\nkéy: vàlue\n", "$.kéy", 2, 6},
	// m1 o2 d3 e4 :5 tab6 f7: goccy reports 6; the rendered rune column is 7.
	{"leading tab before the token", "mode:\tfull\n", "$.mode", 1, 7},
	// é1 :2 tab3 v4. A byte-counting implementation answers 5.
	{"tab plus multi-byte before the token", "a: 1\né:\tvalue\n", "$.é", 2, 4},
	{"run of tabs before the token", "a:\t\t\tb\n", "$.a", 1, 6},
	{"token inside a flow mapping",
		"retry: { max_attempts: 10, backoff: exponential }\n", "$.retry.backoff", 1, 37},
	// a1 :2 space3 {4 b5 :6 tab7 c8 }9.
	{"token inside a flow mapping after a tab", "a: {b:\tc}\n", "$.a.b", 1, 8},
	// The block scalar anchors on `|`, not on its content.
	{"token inside a block scalar after a tab",
		"when:\t|\n  OLD.status <> 'x'\nname: order_paid\n", "$.when", 1, 7},
	{"token on the line after a block scalar",
		"when: |\n  OLD.status <> 'x'\nname: order_paid\n", "$.name", 3, 7},
	{"token after two comment lines", "# comment line\n# another\nname: x\n", "$.name", 3, 7},
	{"quoted token anchors on its opening quote", "a: \"é\"\n", "$.a", 1, 4},
	// The omitted value belongs one column after `payload:`.
	{"value omitted after the key", "payload:\nname: x\n", "$.payload", 1, 9},
	{"nested list element",
		"listeners:\n  - name: a\n    payload:\n      columns: [id, total]\n",
		"$.listeners[0].payload.columns[1]", 4, 21},
}

// TestColumnIsTheRuneColumnOfTheTokensFirstCharacter covers every character class a
// reported position can fall into, with expected values counted from the raw line.
func TestColumnIsTheRuneColumnOfTheTokensFirstCharacter(t *testing.T) {
	for _, tc := range columnCases {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.src, tc.path)
			got := positionOf(src, node)
			if got.Line() != tc.wantLine {
				t.Errorf("Line() = %d, want %d", got.Line(), tc.wantLine)
			}
			if got.Col() != tc.wantCol {
				t.Errorf("Col() = %d, want %d (line %q)",
					got.Col(), tc.wantCol, src.Line(got.Line()))
			}
		})
	}
}
