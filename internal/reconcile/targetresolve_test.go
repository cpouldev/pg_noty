package reconcile

import (
	"go/ast"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestTargetOutcomesAreClosedAndClassifyEachSignalCell(t *testing.T) {
	// assert-a-set-wide-invariant-over-the-set.md makes the zero and size pins test obligations.
	if got, want := len(targetOutcomes), 5; got != want {
		t.Fatalf("target outcome count = %d, want %d", got, want)
	}
	var zero targetOutcome
	if slices.Contains(targetOutcomes[:], zero) {
		t.Fatal("target outcomes contain their unresolved zero value")
	}
	if len(uniqueTargetOutcomes()) != len(targetOutcomes) {
		t.Fatalf("target outcomes are not distinct: %#v", targetOutcomes)
	}
	cases := []struct {
		name  string
		state targetState
		want  targetOutcome
	}{
		{"new name exists", targetState{named: true}, targetNew},
		{
			"healthy OID and same name", targetState{recorded: true, oid: true, named: true, sameName: true},
			targetHealthy,
		},
		{"renamed OID and changed name", targetState{recorded: true, oid: true, named: true}, targetRenamed},
		{"dropped OID and name absent", targetState{recorded: true}, targetDropped},
		{"recreated OID absent and name exists", targetState{recorded: true, named: true}, targetDroppedAndRecreated},
	}
	for _, testCase := range cases {
		t.Run(
			testCase.name, func(t *testing.T) {
				if got := classifyTarget(testCase.state); got != testCase.want {
					t.Fatalf("classifyTarget(%+v) = %q, want %q", testCase.state, got, testCase.want)
				}
			},
		)
	}
}

func TestCompileIsTheOnlyProductionSourceGenerateCaller(t *testing.T) {
	// close-a-substitute-over-the-thing-it-substitutes-for.md requires the guarded population reader.
	// A repository-wide caller count is unsatisfiable: the required test corpora call Generate too,
	// and forcing it to one would invite deleting the corpus that supplies the safety evidence.
	callers := make([]string, 0)
	for _, name := range productionSourceNames(t) {
		if callsSourceGenerate(parsedSource(t, name, sourceBytes(t, name))) {
			callers = append(callers, name)
		}
	}
	t.Logf("source.Generate caller scan examined %d production files: %v", len(productionSourceNames(t)), callers)
	if !slices.Equal(callers, []string{"compile.go"}) {
		t.Fatalf("source.Generate callers = %v, want [compile.go]", callers)
	}
	if bytes := string(sourceBytes(t, "compile.go")); strings.Contains(bytes, "Quoted") || strings.Contains(
		bytes,
		"Qualified",
	) {
		t.Fatal("compile.go re-quotes resolved catalog metadata")
	}
}

func TestTargetResolverUsesOnlyTheSharedCatalogResolution(t *testing.T) {
	bytes := string(sourceBytes(t, "targetresolve.go"))
	for _, required := range []string{"catalog.ResolveTargetOID", "catalog.ResolveTarget"} {
		if !strings.Contains(bytes, required) {
			t.Fatalf("targetresolve.go does not call %s", required)
		}
	}
	for _, forbidden := range []string{"SELECT ", "to_regclass", "pg_catalog"} {
		if strings.Contains(bytes, forbidden) {
			t.Fatalf("targetresolve.go declares catalog resolution text %q", forbidden)
		}
	}
}

func TestCompileNamesAGenerationRefusalForItsListener(t *testing.T) {
	listener := config.Listener{
		Name: "orders", Trigger: config.TriggerSpec{
			Operations: config.Operations{{Kind: "not-an-operation"}}, Payload: config.Payload{Mode: "full"},
		},
	}
	_, err := compileListener(
		"alpha",
		"noty",
		listener,
		TargetReading{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id"}},
	)
	if err == nil || !strings.Contains(err.Error(), `generate listener "orders"`) {
		t.Fatalf("compileListener() error = %v, want a named generation refusal", err)
	}
}

func uniqueTargetOutcomes() map[targetOutcome]struct{} {
	unique := make(map[targetOutcome]struct{}, len(targetOutcomes))
	for _, outcome := range targetOutcomes {
		unique[outcome] = struct{}{}
	}
	return unique
}

func callsSourceGenerate(file *ast.File) bool {
	found := false
	ast.Inspect(
		file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Generate" {
				return true
			}
			name, ok := selector.X.(*ast.Ident)
			found = found || ok && name.Name == "source"
			return true
		},
	)
	return found
}
