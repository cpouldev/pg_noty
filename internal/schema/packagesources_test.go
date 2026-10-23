package schema

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"testing"
)

// sourceBytes reads one source of this package.
func sourceBytes(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// parsedSource parses one Go source, real or synthetic, into the syntax the gates read.
func parsedSource(t *testing.T, name string, data []byte) *goast.File {
	t.Helper()

	file, err := goparser.ParseFile(gotoken.NewFileSet(), name, data, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}
