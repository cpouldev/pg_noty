package reconcile

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

const reconcileDocAnchor = "doc.go"

// This is the deliberately recorded twin of internal/source/packagesources_test.go. Unlike that
// finished package, this reader has no counted production equality: step 24 owns E3. It asserts a
// nonempty, doc.go-anchored population now so every intervening gate includes later source files.
// This follows count-the-population-a-vacuity-guard-guards.md and
// record-a-test-case-you-cannot-yet-implement.md.
func packageSourceNames(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob internal/reconcile sources: %v", err)
	}
	names, err := guardedSourceNames(paths, reconcileDocAnchor)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

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

// productionSourceNames supplies the future E3 equality. Step 24, not this step, adds its exact
// production-file count; this step asserts only the guarded population from which it will be read.
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

func TestTheReconcilePopulationIsGuarded(t *testing.T) {
	names := packageSourceNames(t)
	t.Logf("reconcile population reader examined %d source files", len(names))
}

func TestTheReconcilePopulationReaderRejectsAnEmptyGlob(t *testing.T) {
	if _, err := guardedSourceNames(nil, reconcileDocAnchor); err == nil {
		t.Fatal("an empty reconcile source population was accepted")
	}
}

func TestTheReconcilePopulationReaderRejectsAMissingAnchor(t *testing.T) {
	if _, err := guardedSourceNames([]string{"other.go"}, reconcileDocAnchor); err == nil {
		t.Fatal("a reconcile source population without doc.go was accepted")
	}
}
