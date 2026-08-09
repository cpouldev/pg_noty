package reconcile

import (
	goast "go/ast"
	"testing"
)

func TestNamedRefusalsArePinnedByMessageAndRemediation(t *testing.T) {
	marker := generatedMarker(t, "other", "orders", "insert")
	rows := []struct {
		name string
		got  Refusal
		want Refusal
	}{
		{"ownership", ownershipRefusal("trigger \"public\".\"orders\".\"owned_trigger\"", &marker), Refusal{message: "refusing trigger \"public\".\"orders\".\"owned_trigger\": found ownership marker \"" + marker + "\"", remediation: "leave the object untouched or remove the conflicting marker only after confirming its owner"}},
		{"absent bootstrap", absentBootstrapRefusal("schema \"noty\""), Refusal{message: "cannot reconcile: required bootstrap object schema \"noty\" is absent", remediation: "run bootstrap for this service schema before reconciling"}},
		{"dropped target", droppedTargetRefusal("orders", "\"public\".\"orders\""), Refusal{message: "listener \"orders\" targets missing table \"public\".\"orders\"", remediation: "restore the target table or remove the listener from configuration"}},
		{"table lock timeout", tableLockTimeoutRefusal("\"public\".\"orders\"", "812"), Refusal{message: "timed out waiting for table lock on \"public\".\"orders\"; blocker backend is 812", remediation: "wait for backend 812 or end its transaction, then retry reconciliation"}},
		{"run lock wait expired", runLockWaitExpiredRefusal("alpha", "813"), Refusal{message: "timed out waiting for reconcile run lock for instance \"alpha\"; holder backend is 813", remediation: "wait for backend 813 to finish or investigate that instance before retrying"}},
		{"unknown deferred kind", unknownDeferredKindRefusal("future_check"), Refusal{message: "cannot validate unknown deferred check kind \"future_check\"", remediation: "upgrade the reconciler to a version that understands \"future_check\" or remove that check"}},
	}
	const namedRefusalCount = 6
	if got := len(rows); got != namedRefusalCount {
		t.Fatalf("named refusal constructors = %d, want %d", got, namedRefusalCount)
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if row.got != row.want {
				t.Fatalf("refusal = %#v, want %#v", row.got, row.want)
			}
		})
	}
}

func TestOwnershipRefusalNamesTheAbsenceOfAMarker(t *testing.T) {
	got := ownershipRefusal("function \"noty\".\"owned_trigger\"", nil)
	want := Refusal{
		message:     "refusing function \"noty\".\"owned_trigger\": no ownership marker was found",
		remediation: "leave the object untouched or remove the conflicting marker only after confirming its owner",
	}
	if got != want {
		t.Fatalf("refusal = %#v, want %#v", got, want)
	}
}

func TestRefusalHasExactlySevenConstructors(t *testing.T) {
	want := map[string]bool{
		"ownershipRefusal": true, "absentBootstrapRefusal": true, "droppedTargetRefusal": true,
		"tableLockTimeoutRefusal": true, "runLockWaitExpiredRefusal": true, "unknownDeferredKindRefusal": true,
		"retargetedListenerRefusal": true,
	}
	got := map[string]bool{}
	for _, name := range productionSourceNames(t) {
		file := parsedSource(t, name, sourceBytes(t, name))
		for _, declaration := range file.Decls {
			function, ok := declaration.(*goast.FuncDecl)
			if ok && refusalConstructor(function) {
				if name != "refusal.go" {
					t.Errorf("%s declares refusal constructor %q outside refusal.go", name, function.Name.Name)
				}
				got[function.Name.Name] = true
			}
		}
		goast.Inspect(file, func(node goast.Node) bool {
			literal, ok := node.(*goast.CompositeLit)
			if ok && refusalLiteral(literal) && name != "refusal.go" {
				t.Errorf("%s constructs Refusal directly outside refusal.go", name)
			}
			return true
		})
	}
	if len(got) != len(want) || !sameRefusalConstructors(got, want) {
		t.Fatalf("refusal constructors = %v, want %v", got, want)
	}
}

func refusalLiteral(literal *goast.CompositeLit) bool {
	name, isIdentifier := literal.Type.(*goast.Ident)
	return isIdentifier && name.Name == "Refusal"
}

func refusalConstructor(function *goast.FuncDecl) bool {
	if function.Recv != nil || function.Type.Results == nil || len(function.Type.Results.List) != 1 {
		return false
	}
	result, isIdentifier := function.Type.Results.List[0].Type.(*goast.Ident)
	return isIdentifier && result.Name == "Refusal"
}

func sameRefusalConstructors(got, want map[string]bool) bool {
	for name := range got {
		if !want[name] {
			return false
		}
	}
	for name := range want {
		if !got[name] {
			return false
		}
	}
	return true
}
