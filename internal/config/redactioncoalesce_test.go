package config

import "testing"

func TestSharedAliasExtentsAreReplacedOnceAtTheirStrictestSensitivity(t *testing.T) {
	values := []sensitiveValue{
		{line: 3, column: 12, text: "shared", throughLine: 4, kind: urlPassword},
		{line: 3, column: 12, text: "shared", throughLine: 8, kind: entireValue},
		{line: 3, column: 12, text: "shared", throughLine: 6, kind: urlPassword},
		{line: 7, column: 4, text: "other", kind: entireValue},
	}

	got := coalescedSensitiveValues(values)
	if len(got) != 2 {
		t.Fatalf("coalesced %d values, want one per source extent: %+v", len(got), got)
	}
	if got[0].line != 3 || got[0].column != 12 || got[0].text != "shared" {
		t.Fatalf("first extent = %+v, want the shared alias target", got[0])
	}
	if got[0].kind != entireValue {
		t.Errorf("shared extent sensitivity = %d, want strictest %d", got[0].kind, entireValue)
	}
	if got[0].throughLine != 8 {
		t.Errorf("shared extent closing line = %d, want furthest 8", got[0].throughLine)
	}
	if got[1] != values[3] {
		t.Errorf("independent extent = %+v, want %+v", got[1], values[3])
	}
}
