package source

import (
	goast "go/ast"
	"maps"
	"slices"
	"testing"
)

// generationEntryPoint is the pure compiler's exported entry point. Everything the gates below
// scan is defined as what it reaches.
const generationEntryPoint = "Generate"

func productionRoutingFiles(t *testing.T) []routingFile {
	t.Helper()
	files := make([]routingFile, 0, len(productionSourceNames(t)))
	for _, name := range productionSourceNames(t) {
		files = append(files, routingFile{name: name, file: parsedSource(t, name, sourceBytes(t, name))})
	}
	return files
}

func declaringFiles(files []routingFile) map[string]string {
	declaredIn := make(map[string]string)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*goast.FuncDecl); ok {
				declaredIn[function.Name.Name] = source.name
			}
		}
	}
	return declaredIn
}

// generationChainSources is every production file declaring a function Generate's closure reaches,
// read from this package's own call graph through the reader crossphase_test.go already owns
// (.claude/rules/reuse-the-helper-before-copying-it.md).
func generationChainSources(t *testing.T) []string {
	t.Helper()
	files := productionRoutingFiles(t)
	graph, declaredIn := routingGraph(files), declaringFiles(files)

	reached, pending := map[string]bool{}, []string{generationEntryPoint}
	within := map[string]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if reached[current] {
			continue
		}
		reached[current] = true
		within[declaredIn[current]] = true
		pending = append(pending, slices.Sorted(maps.Keys(graph[current]))...)
	}
	delete(within, "")
	return slices.Sorted(maps.Keys(within))
}

// TestGenerationPurityHasNoPoolCallOrContextParameter keeps the chain offline: criterion 1 makes
// generation a pure function of its request, so no chain file may reach the pool or take a context.
func TestGenerationPurityHasNoPoolCallOrContextParameter(t *testing.T) {
	for _, name := range generationChainSources(t) {
		for _, issue := range sourcePurityIssues(parsedSource(t, name, sourceBytes(t, name))) {
			t.Errorf("%s generation closure is impure: %v", name, issue)
		}
	}
}

func sourcePurityIssues(file *goast.File) []string {
	var issues []string
	goast.Inspect(file, func(node goast.Node) bool {
		switch expression := node.(type) {
		case *goast.CallExpr:
			selector, ok := expression.Fun.(*goast.SelectorExpr)
			if !ok {
				return true
			}
			packageName, isIdent := selector.X.(*goast.Ident)
			if isIdent && packageName.Name == "pgxpool" {
				issues = append(issues, "pgxpool call")
			}
		case *goast.FuncDecl:
			if expression.Type.Params != nil && containsContextType(expression.Type.Params) {
				issues = append(issues, expression.Name.Name+" takes context.Context")
			}
		}
		return true
	})
	return issues
}

func containsContextType(fields *goast.FieldList) bool {
	found := false
	goast.Inspect(fields, func(node goast.Node) bool {
		selector, ok := node.(*goast.SelectorExpr)
		if ok && selector.Sel.Name == "Context" {
			if packageName, ok := selector.X.(*goast.Ident); ok && packageName.Name == "context" {
				found = true
			}
		}
		return !found
	})
	return found
}
