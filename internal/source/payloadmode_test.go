package source

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type phaseOneDeclarations struct {
	operations, modes []string
	count             int
}

func phaseOneSources(t *testing.T, source string) phaseOneDeclarations {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "phase1.go", "package config\n"+source, 0)
	if err != nil {
		t.Fatalf("parse synthetic Phase 1 source: %v", err)
	}
	return declarationsIn(file)
}

func realPhaseOneDeclarations(t *testing.T) phaseOneDeclarations {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "config", "schema.go"))
	if err != nil {
		t.Fatalf("read Phase 1 schema.go: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "schema.go", data, 0)
	if err != nil {
		t.Fatalf("parse Phase 1 schema.go: %v", err)
	}
	return declarationsIn(file)
}

func declarationsIn(file *ast.File) phaseOneDeclarations {
	var got phaseOneDeclarations
	constants := stringConstantsIn(file)
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			switch value.Names[0].Name {
			case "schemaLevels":
				got.count++
				got.operations = operationNamesInSchema(value.Values[0], constants)
			case "payloadModes":
				got.count++
				got.modes = stringLiteralsIn(value.Values[0], constants)
			}
		}
	}
	return got
}

func operationNamesInSchema(expression ast.Expr, constants map[string]string) []string {
	root, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	for _, element := range root.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok || !isIdent(entry.Key, "levelOperations") {
			continue
		}
		level, ok := entry.Value.(*ast.CompositeLit)
		if !ok {
			return nil
		}
		for _, field := range level.Elts {
			entry, ok := field.(*ast.KeyValueExpr)
			if ok && isIdent(entry.Key, "keys") {
				return operationNamesInKeys(entry.Value, constants)
			}
		}
	}
	return nil
}

func operationNamesInKeys(expression ast.Expr, constants map[string]string) []string {
	var names []string
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 || !isIdent(call.Fun, "key") {
			return true
		}
		if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
			if name, err := strconv.Unquote(literal.Value); err == nil {
				names = append(names, name)
			}
		} else if ident, ok := call.Args[0].(*ast.Ident); ok {
			if name, found := constants[ident.Name]; found {
				names = append(names, name)
			}
		}
		return true
	})
	return names
}

func stringConstantsIn(file *ast.File) map[string]string {
	constants := make(map[string]string)
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			if text, err := strconv.Unquote(literal.Value); err == nil {
				constants[value.Names[0].Name] = text
			}
		}
	}
	return constants
}

func stringLiteralsIn(expression ast.Expr, constants map[string]string) []string {
	var values []string
	ast.Inspect(expression, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			if value, err := strconv.Unquote(literal.Value); err == nil {
				values = append(values, value)
			}
		}
		if ident, ok := node.(*ast.Ident); ok {
			if value, found := constants[ident.Name]; found {
				values = append(values, value)
			}
		}
		return true
	})
	return values
}

func isIdent(expression ast.Expr, want string) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == want
}
func TestPayloadModesReconcileWithPhaseOneSource(t *testing.T) {
	declarations := realPhaseOneDeclarations(t)
	if issues := payloadIssues(declarations, payloadModes[:]); len(issues) != 0 {
		t.Fatalf("payload-mode reconciliation failed after examining %d declarations: %s",
			declarations.count, strings.Join(issues, "; "))
	}
	t.Logf("payload-mode reconciliation examined %d Phase 1 declarations", declarations.count)
}

func TestPayloadModeVocabularyControlsBothDriftDirections(t *testing.T) {
	base := `var schemaLevels = map[int]struct{}{0: {key("insert", x)}}; var payloadModes = []string{"full", "columns", "keys_only"}`
	for _, tc := range []struct {
		name, source string
		want         string
	}{
		{"fourth mode", strings.Replace(base, `"keys_only"}`, `"keys_only", "compact"}`, 1), "compact"},
		{"missing mode", strings.Replace(base, `, "keys_only"}`, `}`, 1), "keys_only"},
		{"only one declaration", `var payloadModes = []string{"full", "columns", "keys_only"}`, "want at least 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := payloadIssues(phaseOneSources(t, tc.source), payloadModes[:])
			if !strings.Contains(strings.Join(issues, "; "), tc.want) {
				t.Fatalf("payloadIssues reported %v, want %q", issues, tc.want)
			}
		})
	}
}

func TestPayloadModeSetSizeIsPinned(t *testing.T) {
	if got := len(payloadModes); got != 3 {
		t.Fatalf("payload mode set has %d entries, want 3", got)
	}
}

func TestUnknownPayloadModeIsRefusedByName(t *testing.T) {
	err := payloadModeKnown("compact")
	if err == nil || !strings.Contains(err.Error(), "compact") {
		t.Fatalf("payloadModeKnown returned %v, want a named refusal containing compact", err)
	}
	if _, named := err.(unknownPayloadModeError); !named {
		t.Fatalf("payloadModeKnown returned %T, want unknownPayloadModeError", err)
	}
}
