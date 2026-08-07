package config

import (
	"strings"
	"testing"
)

// This file covers snippet.go: where the pieces of a block land, at the edges where an off-by-one
// answers rather than fails. What a block contains is render_test.go's subject.

// TestALineNumberPastTheSourceQuotesNothing covers lineAt's out-of-range answer and both edges around
// it. That answer is reached whenever a diagnostic is rendered against bytes other than the ones it
// came from -- a hand-built Error in a test, or a diagnostic surviving a source that shrank -- and an
// off-by-one there would quote the *wrong line* rather than fail, which is a leak on a redacted copy
// whose line numbering had shifted.
func TestALineNumberPastTheSourceQuotesNothing(t *testing.T) {
	lines := []string{"one", "two"}

	tests := []struct {
		name   string
		number int
		want   string
	}{
		{name: "before the first line", number: firstLine - 1, want: ""},
		{name: "the first line", number: firstLine, want: "one"},
		{name: "the last line", number: len(lines), want: "two"},
		{name: "one past the last line", number: len(lines) + 1, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineAt(lines, tc.number); got != tc.want {
				t.Errorf("lineAt(%v, %d) = %q, want %q", lines, tc.number, got, tc.want)
			}
		})
	}
}

// TestADiagnosticPastTheSourcesEndRendersAnEmptyGutter is the same boundary through the renderer,
// which is the only place the out-of-range answer can be seen producing a block rather than a panic.
// The quoted lines are empty and the gutter still carries their numbers, so the caret column is
// unaffected by how much source there was.
func TestADiagnosticPastTheSourcesEndRendersAnEmptyGutter(t *testing.T) {
	const past = 9
	diags := Errors{{File: "listeners.yaml", Line: past, Col: firstColumn, Msg: "message"}}

	rendered := diags.Render([]byte("version: 1\n"))

	for _, want := range []string{
		"listeners.yaml:9:1\n",
		// Trailing whitespace is trimmed, so an empty quoted line is the bar and nothing more.
		"   7 |\n",
		"   8 |\n",
		">  9 |\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendering a diagnostic past the source's end does not hold %q:\n%s", want, rendered)
		}
	}
}
