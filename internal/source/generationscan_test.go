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

// theGenerationChainFloor is the smallest set of files Generate has ever reached. The chain is
// derived, not transcribed, so a file joining it is scanned without anyone remembering to extend a
// list; the floor exists so a derivation that silently stopped reaching most of the package fails
// here rather than reporting a clean scan of two files
// (.claude/rules/count-the-population-a-vacuity-guard-guards.md).
var theGenerationChainFloor = []string{
	"body.go", "dollarquote.go", "generate.go", "literal.go", "marker.go", "payload.go",
	"statements.go", "whenclause.go",
}

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

func TestTheGenerationChainStillReachesEveryFileTheGatesWereWrittenFor(t *testing.T) {
	chain := generationChainSources(t)
	for _, want := range theGenerationChainFloor {
		if !slices.Contains(chain, want) {
			t.Errorf("Generate no longer reaches %s, so the determinism and purity gates below no "+
				"longer scan it; chain = %v", want, chain)
		}
	}
	t.Logf("the generation chain gates examined %d production files: %v", len(chain), chain)
}

// TestTheGenerationChainDeclaresNoMapAndCallsNoOrderingRoutine is the determinism gate. It does not
// claim to recognise a range over a map -- that is a question about types, which this scan cannot
// answer -- so it forbids the precondition instead: a chain that declares no map type and calls
// nothing that orders a list or iterates a map has no non-deterministic iteration to perform.
func TestTheGenerationChainDeclaresNoMapAndCallsNoOrderingRoutine(t *testing.T) {
	for _, name := range generationChainSources(t) {
		for _, issue := range orderingHazards(parsedSource(t, name, sourceBytes(t, name))) {
			t.Errorf("%s: %s", name, issue)
		}
	}
}

// orderingRoutines are the qualified names whose result order is not the caller's. maps.Keys and
// maps.Values head the list because they are how this codebase's own scans iterate a map
// (quotingscan_test.go), and slices.Sort* because that is the spelling a chain file would reach for
// long before it reached for the sort package.
var orderingRoutines = map[string][]string{
	"sort":   nil, // every exported name in it orders something
	"slices": {"Sort", "SortFunc", "SortStableFunc", "Sorted", "SortedFunc", "SortedStableFunc"},
	"maps":   {"Keys", "Values", "All"},
}

func orderingHazards(file *goast.File) []string {
	var issues []string
	goast.Inspect(file, func(node goast.Node) bool {
		switch found := node.(type) {
		case *goast.MapType:
			issues = append(issues, "declares a map type, whose iteration order Go randomises")
		case *goast.CallExpr:
			if named, ordering := orderingCallName(found); ordering {
				issues = append(issues, "calls "+named+", which does not preserve the caller's order")
			}
		}
		return true
	})
	return issues
}

func orderingCallName(call *goast.CallExpr) (string, bool) {
	selector, isSelector := call.Fun.(*goast.SelectorExpr)
	if !isSelector {
		return "", false
	}
	packageName, isIdent := selector.X.(*goast.Ident)
	if !isIdent {
		return "", false
	}
	routines, watched := orderingRoutines[packageName.Name]
	if !watched || (routines != nil && !slices.Contains(routines, selector.Sel.Name)) {
		return "", false
	}
	return packageName.Name + "." + selector.Sel.Name, true
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
