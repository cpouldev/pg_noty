package reconcile

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

const reconcileDocAnchor = "doc.go"

func guardedSourceNames(paths []string, anchor string) ([]string, error) {
	if len(paths) == 0 {
		return nil, &sourcePopulationError{message: "reconcile source population is empty"}
	}
	slices.Sort(paths)
	if !slices.Contains(paths, anchor) {
		return nil, &sourcePopulationError{message: "reconcile source population lacks " + anchor}
	}
	return paths, nil
}

// productionSourceNames finds the production files used by source-level safety checks.
func productionSourceNames(t *testing.T) []string {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	production := paths[:0]
	for _, path := range paths {
		if !strings.HasSuffix(path, "_test.go") {
			production = append(production, path)
		}
	}
	names, err := guardedSourceNames(production, reconcileDocAnchor)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

type sourcePopulationError struct{ message string }

func (e *sourcePopulationError) Error() string { return e.message }

// sourceBytes reads one file by path, failing rather than answering short. Its members are the
// guarded population above and the config corpus fixtures listeneradjacency_test.go reads, which go
// through it rather than through a second reader (.claude/rules/reuse-the-helper-before-copying-it.md).
func sourceBytes(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func parsedSource(t *testing.T, name string, data []byte) *goast.File {
	t.Helper()

	file, err := goparser.ParseFile(gotoken.NewFileSet(), name, data, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}
