package source

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A floor, not an equality: the invariant worth guarding is that the reader still sees this
// package's sources, and an equality answers "did anyone add a file" instead -- churn that costs an
// edit and a re-derived sentence every time, and has never once caught a defect.
const productionPopulationRead = 24

// packageSourceNames is the one guarded population reader for this package's source-level gates.
// It fails on an empty glob and on a missing doc.go anchor, so a gate pointed at the wrong tree
// cannot pass over no files or over another directory's Go sources.
func packageSourceNames(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob internal/source sources: %v", err)
	}
	names, err := guardedSourceNames(paths, "doc.go")
	if err != nil {
		t.Fatal(err)
	}
	return names
}

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

// importedPaths returns every import path in one parsed source, including aliased and blank imports.
func importedPaths(file *goast.File) []string {
	paths := make([]string, 0, len(file.Imports))
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func withinModule(imported, module string) bool {
	return imported == module || len(imported) > len(module) && imported[:len(module)] == module && imported[len(module)] == '/'
}

func TestTheSourcePopulationReaderRejectsAnEmptyGlob(t *testing.T) {
	if _, err := guardedSourceNames(nil, "doc.go"); err == nil {
		t.Fatal("an empty source population was accepted")
	}
}

func TestTheSourcePopulationReaderRejectsAMissingAnchor(t *testing.T) {
	if _, err := guardedSourceNames([]string{"other.go"}, "doc.go"); err == nil {
		t.Fatal("a source population without doc.go was accepted")
	}
}

func productionPopulationIssue(count int) string {
	if count < productionPopulationRead {
		return "production source count is below the floor the source scans need"
	}
	return ""
}

func TestTheProductionPopulationIsRead(t *testing.T) {
	if issue := productionPopulationIssue(len(productionSourceNames(t))); issue != "" {
		t.Fatal(issue)
	}
	if productionPopulationIssue(productionPopulationRead-1) == "" {
		t.Fatal("a population below the floor was accepted")
	}
}
