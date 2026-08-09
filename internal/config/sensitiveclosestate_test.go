package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var closeTailsThatEndAValue = []struct {
	name string
	text string
}{
	{name: "end of line"},
	{name: "space", text: " "},
	{name: "tab", text: "\t"},
	{name: "separated comment", text: " # public"},
	{name: "tabbed comment", text: "\t# public"},
	{name: "line feed", text: "\npublic"},
	{name: "carriage return", text: "\rpublic"},
}

// The document-level claims about what a flow close leaves covered are in flowclosecoverage_test.go,
// which partitions them by the mechanism that decides each. What follows is the byte-domain unit
// grid over sensitiveContinuation itself.

func flowContinuationOfDepth(kind byte, depth int) sensitiveContinuation {
	if kind == '[' {
		return sensitiveContinuation{sequences: depth}
	}
	return sensitiveContinuation{mappings: depth}
}

func flowClosersOfDepth(kind byte, depth int) string {
	close := "]"
	if kind == '{' {
		close = "}"
	}
	return strings.Repeat(close, depth)
}

func TestEveryByteAfterAFlowCloseDecidesWhetherTheValueContinues(t *testing.T) {
	for _, kind := range []byte{'[', '{'} {
		for depth := 1; depth <= 4; depth++ {
			for number := 0; number <= 255; number++ {
				name := fmt.Sprintf("%c/depth-%d/byte-%03d", kind, depth, number)
				t.Run(name, func(t *testing.T) {
					tail := string([]byte{byte(number)})
					got := flowContinuationOfDepth(kind, depth).after(flowClosersOfDepth(kind, depth) + tail)
					trusted := number == ' ' || number == '\t' || number == '\r' || number == '\n'
					if got.open() == trusted {
						t.Errorf("post-close byte %d open = %t, want %t", number, got.open(), !trusted)
					}
				})
			}
		}
	}
}

func TestAMultibyteRuneAfterAFlowCloseKeepsTheValueContinuing(t *testing.T) {
	for _, kind := range []byte{'[', '{'} {
		for _, tail := range multibyteRuneClasses {
			t.Run(fmt.Sprintf("%c/%s", kind, tail.name), func(t *testing.T) {
				got := flowContinuationOfDepth(kind, 2).after(flowClosersOfDepth(kind, 2) + tail.text)
				if !got.open() {
					t.Errorf("%s cleared the tainted-close provenance for %c", tail.name, kind)
				}
			})
		}
	}
}

// continuationsWithOneFieldSet holds one continuation per field of sensitiveContinuation, that field
// alone set away from its zero value.
var continuationsWithOneFieldSet = []struct {
	field string
	held  sensitiveContinuation
}{
	{field: "quote", held: sensitiveContinuation{quote: '"'}},
	{field: "sequences", held: sensitiveContinuation{sequences: 1}},
	{field: "mappings", held: sensitiveContinuation{mappings: 1}},
	{field: "taintedClose", held: sensitiveContinuation{taintedClose: true}},
}

// TestOnlyTheEmptyContinuationIsClosed pins the invariant that makes redactBySensitiveKeyName's
// closesNoBlock arm a restatement rather than a decision. That arm is reached only when the first arm
// did not fire, so the state is not open, and it then says "carries nothing" in code rather than by
// falling out of the block. That is true only while open() reads every field: a field added and left
// out of it would be carried silently past the arm. The rows are closed over the struct by its own
// field count (.claude/rules/close-a-generated-dimension-over-the-codes-own-arms.md).
func TestOnlyTheEmptyContinuationIsClosed(t *testing.T) {
	if noSensitiveContinuation.open() {
		t.Fatalf("the empty continuation reports itself open, so no state is closed at all")
	}
	if got := reflect.TypeOf(noSensitiveContinuation).NumField(); got != len(continuationsWithOneFieldSet) {
		t.Fatalf("sensitiveContinuation has %d fields and %d are set one at a time here; add the row "+
			"with the field", got, len(continuationsWithOneFieldSet))
	}

	for _, tc := range continuationsWithOneFieldSet {
		if !tc.held.open() {
			t.Errorf("a continuation with only %s set reports itself closed, so the arm that states it "+
				"carries nothing would carry it past the block instead", tc.field)
		}
	}
}

func TestATrustedTailAfterAFlowCloseEndsTheValue(t *testing.T) {
	for _, kind := range []byte{'[', '{'} {
		for _, tail := range closeTailsThatEndAValue {
			t.Run(fmt.Sprintf("%c/%s", kind, tail.name), func(t *testing.T) {
				got := flowContinuationOfDepth(kind, 2).after(flowClosersOfDepth(kind, 2) + tail.text)
				if got.open() {
					t.Errorf("%s kept the tainted-close provenance for %c", tail.name, kind)
				}
			})
		}
	}
}
