package config

import (
	"testing"

	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

func TestPositionIsAnchoredInTheSourceItCameFrom(t *testing.T) {
	src, node := nodeAt(t, "a: 1\nb: 2\nc: 3\n", "$.c")
	got := positionOf(src, node)
	if got.File() != "fixture.yaml" {
		t.Errorf("File() = %q, want fixture.yaml", got.File())
	}
	if got.Line() != 3 {
		t.Errorf("Line() = %d, want 3", got.Line())
	}
}

func TestPathDropsTheYAMLPathRootPrefix(t *testing.T) {
	tests := []struct{ name, src, path, wantPath string }{
		{
			"nested list element",
			"listeners:\n  - name: a\n    payload:\n      columns: [id, total]\n",
			"$.listeners[0].payload.columns[1]",
			"listeners[0].payload.columns[1]",
		},
		{
			"list element mapping",
			"listeners:\n  - name: a\n    payload:\n      columns: [id, total]\n",
			"$.listeners[0].payload.columns",
			"listeners[0].payload.columns",
		},
		{"top level key", "version: 1\n", "$.version", "version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.src, tc.path)
			if got := positionOf(src, node).Path(); got != tc.wantPath {
				t.Errorf("Path() = %q, want %q", got, tc.wantPath)
			}
		})
	}
}

func TestPathOfTheDocumentRootIsEmpty(t *testing.T) {
	file, err := parser.ParseBytes([]byte("version: 1\n"), parser.ParseComments)
	if err != nil {
		t.Fatalf("parser.ParseBytes failed: %v", err)
	}
	src := newSource("fixture.yaml", []byte("version: 1\n"))
	if got := positionOf(src, file.Docs[0].Body).Path(); got != "" {
		t.Errorf("root Path() = %q, want empty", got)
	}
}

func TestPositionOfANilNodeIsPositionless(t *testing.T) {
	src := newSource("fixture.yaml", []byte("a: 1\n"))
	got := positionOf(src, nil)
	if got.Line() != 0 || got.Col() != 0 || got.Path() != "" {
		t.Errorf("positionOf(src, nil) = %+v, want positionless", got)
	}
	if got.File() != "fixture.yaml" {
		t.Errorf("File() = %q, want fixture.yaml", got.File())
	}
}

func TestTokenPositionIsPositionlessWithoutAToken(t *testing.T) {
	src := newSource("fixture.yaml", []byte("a: 1\n"))
	tests := []struct {
		name string
		at   *token.Token
	}{
		{"no token", nil},
		{"token without position", &token.Token{Value: "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenPosition(src, tc.at, "listeners[0].name")
			if got.Line() != 0 || got.Col() != 0 {
				t.Errorf("position = %d:%d, want none", got.Line(), got.Col())
			}
			if got.File() != "fixture.yaml" || got.Path() != "listeners[0].name" {
				t.Errorf("locator = %q %q, want fixture and given path", got.File(), got.Path())
			}
		})
	}
}
