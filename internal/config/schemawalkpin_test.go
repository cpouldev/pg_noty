package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// TestGoccyGivesEveryMappingAMappingNode pins the library behaviour the schema walk depends on:
// it descends only into *ast.MappingNode, so a version that returned a bare
// *ast.MappingValueNode for a single-entry mapping would silently walk past every secret
// beneath one. Both sides of the count
// are covered, because it is the one-entry case that could differ.
func TestGoccyGivesEveryMappingAMappingNode(t *testing.T) {
	tests := []struct {
		name   string
		source string
		path   string
	}{
		{name: "one entry at the root", source: "database: x\n", path: "$"},
		{name: "one entry nested", source: "database:\n  url: x\n", path: "$.database"},
		{name: "two entries nested", source: "database:\n  url: x\n  schema: y\n", path: "$.database"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, node := nodeAt(t, tc.source, tc.path)

			if _, isMapping := node.(*ast.MappingNode); !isMapping {
				t.Fatalf("%s is a %T, not an *ast.MappingNode; the schema walk would skip it", tc.path, node)
			}
		})
	}
}

// TestGoccyReportsAClosingIndicatorForFlowContainersOnly pins the library behaviour containerEndLine
// is built on, and which sensitiveextent.go's comment has stated without asserting.
//
// Only a flow container writes a closer, so only a flow container has one to report. Everything that
// reads containerEndLine reads that: the reach walk falls back to indentation for a block container,
// containerinterior.go runs its interior to the end of the line rather than stopping past the last
// child, and the containment oracle bounds a flow extent at the closer and a block extent at the
// line. A version that began populating End for block containers would change all three silently, so
// both directions are asserted -- a nil where one is expected is as much a change as a token where
// none is.
func TestGoccyReportsAClosingIndicatorForFlowContainersOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  string
		path    string
		closes  bool
		through int
	}{
		{name: "a flow mapping", source: "a: {b: 1}\n", path: "$.a", closes: true, through: 1},
		{name: "a flow sequence", source: "a: [1, 2]\n", path: "$.a", closes: true, through: 1},
		{
			name:   "a flow sequence closing on a later line",
			source: "a: [1,\n  2]\n",
			path:   "$.a", closes: true, through: 2,
		},
		{name: "a block mapping", source: "a:\n  b: 1\n", path: "$.a"},
		{name: "a block sequence", source: "a:\n  - 1\n  - 2\n", path: "$.a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, node := nodeAt(t, tc.source, tc.path)

			got := containerEndLine(text, node)
			if !tc.closes {
				if got != noClosingIndicator {
					t.Fatalf("containerEndLine = %d, want noClosingIndicator (%d); this library "+
						"version reported no closer for a block container, and three readers depend "+
						"on that", got, noClosingIndicator)
				}
				return
			}
			if got != tc.through {
				t.Errorf("containerEndLine = %d, want the line the closer is written on (%d)",
					got, tc.through)
			}
		})
	}
}
