package config

import (
	"testing"
)

// Where a decoded value is: the Positioned every wrapper satisfies, and the position each element of a
// list keeps for itself.
//
// Satisfying Positioned is what lets a stage-H rule read a position without importing the parser, and
// keeping one per element is what lets a rule about a list's contents anchor on the element at fault
// (ADR-6, AC #33). Both are asserted through the wrapper table in scalar_test.go, so no wrapper can be
// left out of either claim.

// TestEveryWrapperSatisfiesPositioned is SC-5 at run time; the compile-time half is the assertion
// block in scalar.go, which is what lets stage-H rule code read a position without importing ast.
func TestEveryWrapperSatisfiesPositioned(t *testing.T) {
	for _, kind := range wrapperKinds {
		t.Run(kind.name, func(t *testing.T) {
			got := kind.read(nodeReadBy(t, "outer:\n  value: x\n", "$.outer.value"))

			if got.written.File() == "" {
				t.Error("File() is empty, so a diagnostic about this value would name no file")
			}
			// `  value: ` is nine runes, so the value begins at rune 10 of line 2.
			if got.written.Line() != 2 || got.written.Col() != 10 {
				t.Errorf("position is %d:%d, want the value's own 2:10", got.written.Line(), got.written.Col())
			}
			if got.written.Path() != "outer.value" {
				t.Errorf("Path() = %q, want %q", got.written.Path(), "outer.value")
			}
		})
	}
}
