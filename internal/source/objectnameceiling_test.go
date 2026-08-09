package source

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// The object-name ceiling and the unbounded-input property, kept beside the boundary rows in
// objectname_test.go rather than inside them: this file reads internal/config's own source for the
// R23 maximum, which is a different subject from the derivation's arithmetic.

func TestObjectNameCeilingReadsR23FromTheConfigPackage(t *testing.T) {
	maximum := r23Maximum(t)
	ceiling := len("pg_noty_") + maximum + 1 + len("ins")
	if ceiling != 53 || ceiling >= schema.MaxIdentifierBytes {
		t.Fatalf("derived name ceiling = %d from 8+%d+1+3; want 53 below the identifier limit", ceiling, maximum)
	}
}

func r23Maximum(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "config", "ident.go"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "ident.go", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var pattern string
	ast.Inspect(
		file, func(node ast.Node) bool {
			value, ok := node.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "namePattern" || len(value.Values) != 1 {
				return true
			}
			call, ok := value.Values[0].(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if ok {
				pattern, _ = strconv.Unquote(literal.Value)
			}
			return true
		},
	)
	match := regexp.MustCompile(`\{0,([0-9]+)\}`).FindStringSubmatch(pattern)
	if len(match) != 2 {
		t.Fatalf("could not read R23 bound from namePattern %q", pattern)
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return 1 + value
}

func FuzzObjectNameKeepsTheIdentifierBound(f *testing.F) {
	f.Add("a", "b")
	f.Add(strings.Repeat("a", 60), "bb")
	f.Add(strings.Repeat("a", 60), "bbb")
	// The seed the collision clause needs: 71 a's and 72 a's join to names agreeing on every byte
	// a prefix cut can keep, so under a cut alone `got` and `other` below are one string. Without
	// it every seed differs inside the first 63 bytes and the clause cannot fail.
	f.Add(strings.Repeat("a", 71), "s")
	f.Fuzz(
		func(t *testing.T, base, suffix string) {
			got := schema.ObjectName(base, suffix)
			if len(got) > schema.MaxIdentifierBytes {
				t.Fatalf("ObjectName output is %d bytes", len(got))
			}
			other := schema.ObjectName(base+"x", suffix)
			if base != base+"x" && got == other {
				t.Fatalf("distinct inputs collided: %q", got)
			}
		},
	)
}
