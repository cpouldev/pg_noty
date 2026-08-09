package config

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// This file declares the package's YAML-node test helpers -- parse a document, select a node
// within one -- because position derivation was the first subject to need them. Every later
// subject reads through these rather than repeating the parse and the selector.

// parsedRoot is the body of a fixture's single document, parsed the way the loader parses.
func parsedRoot(t *testing.T, src string) ast.Node {
	t.Helper()

	file, err := parser.ParseBytes([]byte(src), parser.ParseComments)
	if err != nil {
		t.Fatalf("parser.ParseBytes(%q) failed: %v", src, err)
	}
	return file.Docs[0].Body
}

// nodeIn selects one node by YAML path within an already-parsed document, which is what a
// test asking about a tree it has since mutated -- an interpolated one, say -- has to do.
func nodeIn(t *testing.T, root ast.Node, path string) ast.Node {
	t.Helper()

	selector, err := yaml.PathString(path)
	if err != nil {
		t.Fatalf("yaml.PathString(%q) failed: %v", path, err)
	}
	node, err := selector.FilterNode(root)
	if err != nil {
		t.Fatalf("selecting %q failed: %v", path, err)
	}
	return node
}

// nodeAt selects one node of a fixture by YAML path, so every position case is
// driven by a real token produced by parser.ParseBytes rather than by a hand-built
// token that could not reproduce the library's own column accounting.
func nodeAt(t *testing.T, src, path string) (*source, ast.Node) {
	t.Helper()

	return newSource("fixture.yaml", []byte(src)), nodeIn(t, parsedRoot(t, src), path)
}
