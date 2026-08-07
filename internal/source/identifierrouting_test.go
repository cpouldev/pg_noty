package source

import (
	goast "go/ast"
	"path/filepath"
	"testing"
)

type routingFile struct {
	name string
	file *goast.File
}

func routingSources(t *testing.T) []routingFile {
	t.Helper()
	paths := []string{"internal/config/validatestructured.go", "internal/config/validate.go", "internal/config/validatelists.go", "internal/config/ident.go"}
	files := make([]routingFile, 0, len(paths))
	for _, path := range paths {
		data := sourceBytes(t, filepath.Join("..", "..", path))
		files = append(files, routingFile{name: path, file: parsedSource(t, path, data)})
	}
	return files
}

func routingGraph(files []routingFile) map[string]map[string]bool {
	declared := make(map[string]bool)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*goast.FuncDecl); ok {
				declared[function.Name.Name] = true
			}
		}
	}
	graph := make(map[string]map[string]bool)
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*goast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			graph[function.Name.Name] = referencedFunctions(function.Body, declared)
		}
	}
	return graph
}

func referencedFunctions(body *goast.BlockStmt, declared map[string]bool) map[string]bool {
	called := make(map[string]bool)
	goast.Inspect(body, func(node goast.Node) bool {
		switch function := node.(type) {
		case *goast.Ident:
			if declared[function.Name] {
				called[function.Name] = true
			}
		case *goast.SelectorExpr:
			if declared[function.Sel.Name] {
				called[function.Sel.Name] = true
			}
		}
		return true
	})
	return called
}

func reachesIdentifierDefect(graph map[string]map[string]bool, current string, seen map[string]bool) bool {
	if current == "identifierDefect" {
		return true
	}
	if seen[current] {
		return false
	}
	seen[current] = true
	for called := range graph[current] {
		if reachesIdentifierDefect(graph, called, seen) {
			return true
		}
	}
	return false
}
func hasIdentifier(body *goast.BlockStmt, wanted string) bool {
	found := false
	goast.Inspect(body, func(node goast.Node) bool {
		identifier, ok := node.(*goast.Ident)
		if ok && identifier.Name == wanted {
			found = true
		}
		return !found
	})
	return found
}
func routingPinIssues(files []routingFile) (int, []string) {
	graph := routingGraph(files)
	rules := [...]struct{ name, file string }{{"R29", "internal/config/validatestructured.go"}, {"R32", "internal/config/validate.go"}, {"R33", "internal/config/validate.go"}}
	examined := 0
	var issues []string
	for _, rule := range rules {
		found := false
		for _, source := range files {
			if source.name != rule.file {
				continue
			}
			for _, declaration := range source.file.Decls {
				function, ok := declaration.(*goast.FuncDecl)
				if !ok || function.Body == nil || !hasIdentifier(function.Body, rule.name) || !hasIdentifier(function.Body, "validateIdentifierList") {
					continue
				}
				found = true
				if !reachesIdentifierDefect(graph, function.Name.Name, make(map[string]bool)) {
					issues = append(issues, rule.name+" does not reach identifierDefect")
				}
			}
		}
		if !found {
			issues = append(issues, rule.name+" was not examined in "+rule.file)
			continue
		}
		examined++
	}
	return examined, issues
}
func TestRoutingPinExaminesAllThreeRules(t *testing.T) {
	examined, issues := routingPinIssues(routingSources(t))
	if examined != 3 || len(issues) != 0 {
		t.Fatalf("routing pin examined %d rules, issues: %v", examined, issues)
	}

	synthetic := parsedSource(t, "synthetic-routing.go", []byte("package config\nfunc syntheticRule() { R29() /* identifierDefect */ }\nfunc identifierDefect() {}\n"))
	if reachesIdentifierDefect(routingGraph([]routingFile{{name: "synthetic", file: synthetic}}), "syntheticRule", make(map[string]bool)) {
		t.Fatal("routing pin accepted identifierDefect mentioned only in a comment")
	}
}
