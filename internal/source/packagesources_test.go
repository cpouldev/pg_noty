package source

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

func guardedSourceNames(paths []string, anchor string) ([]string, error) {
	if len(paths) == 0 {
		return nil, &sourcePopulationError{message: "source population is empty"}
	}
	slices.Sort(paths)
	if !slices.Contains(paths, anchor) {
		return nil, &sourcePopulationError{message: "source population lacks " + anchor}
	}
	return paths, nil
}

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
	names, err := guardedSourceNames(production, "doc.go")
	if err != nil {
		t.Fatal(err)
	}
	return names
}

type sourcePopulationError struct{ message string }

func (e *sourcePopulationError) Error() string { return e.message }

// sourceBytes reads one member of the guarded population.
func sourceBytes(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// parsedSource parses a source from the real tree or a synthetic control.
func parsedSource(t *testing.T, name string, data []byte) *goast.File {
	t.Helper()

	file, err := goparser.ParseFile(gotoken.NewFileSet(), name, data, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}
