package schema

import (
	goast "go/ast"
	"testing"
)

func TestReconcileLockKeyDiffersFromBootstrap(t *testing.T) {
	for _, tc := range []struct{ schema, instance string }{
		{"noty", "orders"},
		{"noty", ""},
		{"", ""},
	} {
		if got, bootstrap := ReconcileLockKey(tc.schema, tc.instance), lockKey(tc.schema, tc.instance); got == bootstrap {
			t.Errorf("ReconcileLockKey(%q, %q) = bootstrap key %d; a long apply must not block a replica's boot",
				tc.schema, tc.instance, got)
		}
	}
}

func TestReconcileLockKeySeparatesInstances(t *testing.T) {
	if left, right := ReconcileLockKey("noty", "orders"), ReconcileLockKey("noty", "billing"); left == right {
		t.Errorf("reconcile keys %d and %d are equal; instances must have distinct run locks", left, right)
	}
}

func TestReconcileLockKeySeparatesSchemas(t *testing.T) {
	if left, right := ReconcileLockKey("noty_orders", "orders"), ReconcileLockKey("noty_billing", "orders"); left == right {
		t.Errorf("reconcile keys %d and %d are equal; schemas must have distinct run locks", left, right)
	}
}

func TestLockKeySeparatorSeparatesPreimagesInBothDomains(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  func(string, string) int64
	}{
		{name: "bootstrap", key: lockKey},
		{name: "reconcile", key: ReconcileLockKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if left, right := tc.key("not", "yorders"), tc.key("noty", "orders"); left == right {
				t.Errorf("%s keys %d and %d are equal; the separator must distinguish named collision rows", tc.name, left, right)
			}
		})
	}
}

// The literals below were measured by a throwaway package test before this step changed lockKey.
// Recording is legitimate here because the value under protection is the old bootstrap behaviour.
func TestBootstrapLockKeyKeepsPreStep1Bytes(t *testing.T) {
	for _, tc := range []struct {
		schema, instance string
		want             int64
	}{
		// Pre-edit: lockKey("noty", "orders") = 2758251823123625242.
		{schema: "noty", instance: "orders", want: 2758251823123625242},
		// Pre-edit: lockKey("noty", "") = 4656215628282574891.
		{schema: "noty", instance: "", want: 4656215628282574891},
		// Pre-edit: lockKey("", "") = -5808590958014384161.
		{schema: "", instance: "", want: -5808590958014384161},
	} {
		if got := lockKey(tc.schema, tc.instance); got != tc.want {
			t.Errorf("lockKey(%q, %q) = %d, want pre-Step-1 bootstrap key %d", tc.schema, tc.instance, got, tc.want)
		}
	}
}

func TestLockDomainsAreTheSpecifiedClosedSet(t *testing.T) {
	if got := len(lockDomains); got != 2 {
		t.Fatalf("lockDomains has %d entries, want 2; a third domain must join this closed set deliberately", got)
	}
	if bootstrapDomain != "" {
		t.Errorf("bootstrapDomain = %q, want empty domain", bootstrapDomain)
	}
	if reconcileDomain != "reconcile\x00" {
		t.Errorf("reconcileDomain = %q, want %q", reconcileDomain, "reconcile\x00")
	}
	if lockDomains[0] != bootstrapDomain || lockDomains[1] != reconcileDomain {
		t.Errorf("lockDomains = %q, want bootstrap and reconcile domains", lockDomains)
	}
}

func TestAdvisoryLockExportsOnlyReconcileLockKey(t *testing.T) {
	var exported []string
	for _, declaration := range parsedSource(t, "advisorylock.go", sourceBytes(t, "advisorylock.go")).Decls {
		exported = append(exported, exportedDeclarationNames(declaration)...)
	}
	if len(exported) != 1 || exported[0] != "ReconcileLockKey" {
		t.Errorf("advisorylock.go exports %v, want only ReconcileLockKey; a generic LockKey permits bootstrap collisions", exported)
	}
}

func exportedDeclarationNames(declaration goast.Decl) []string {
	switch declaration := declaration.(type) {
	case *goast.FuncDecl:
		return exportedName(declaration.Name)
	case *goast.GenDecl:
		return exportedSpecNames(declaration.Specs)
	}
	return nil
}

func exportedSpecNames(specs []goast.Spec) []string {
	var exported []string
	for _, spec := range specs {
		switch spec := spec.(type) {
		case *goast.TypeSpec:
			exported = append(exported, exportedName(spec.Name)...)
		case *goast.ValueSpec:
			exported = append(exported, exportedNames(spec.Names)...)
		}
	}
	return exported
}

func exportedName(name *goast.Ident) []string {
	return exportedNames([]*goast.Ident{name})
}

func exportedNames(names []*goast.Ident) []string {
	var exported []string
	for _, name := range names {
		if name.IsExported() {
			exported = append(exported, name.Name)
		}
	}
	return exported
}
