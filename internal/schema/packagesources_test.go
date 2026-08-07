package schema

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file is the one reader the three source-level gates in this package share -- the L1 import
// gate, the budget gate and doc.go's constant reconciliation. A second copy would drift the moment
// one of them gained a case. It cannot reach internal/config's or internal/goartifact's
// equivalents: those are unexported test helpers of packages this one may not import from a test
// either way.

// packageSourceNames is every Go source of this package, sorted. It fails rather than returning
// nothing, because a sweep over an empty list passes vacuously.
func packageSourceNames(t *testing.T) []string {
	t.Helper()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob this package's sources: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no Go sources found, so every sweep over them would pass vacuously")
	}
	slices.Sort(names)
	return names
}

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

// importedPaths is every path one source imports, with the quotes stripped. An alias and a blank
// import are spellings of the same reach, so neither is treated differently here.
func importedPaths(file *goast.File) []string {
	paths := make([]string, 0, len(file.Imports))
	for _, imported := range file.Imports {
		paths = append(paths, strings.Trim(imported.Path.Value, `"`))
	}
	return paths
}

// withinModule reports whether an import path is a module or a package beneath it. The separator is
// part of the prefix, so a module whose path merely begins with another's is a different module.
//
// It lives here with the other shared readers because four gates ask it -- the driver-reaching
// sweep, the quoting authority, the connection-string scan and the harness scans -- and a second
// copy would drift the moment one of them gained a case. It arrived here when Step 7's harness
// retired dependencylanding_test.go, which declared it.
//
// l1imports_test.go's forbiddenInL1 writes the same two clauses and answers a different question --
// membership of a fixed deny-list -- so that one stays where it is.
func withinModule(imported, module string) bool {
	return imported == module || strings.HasPrefix(imported, module+"/")
}
