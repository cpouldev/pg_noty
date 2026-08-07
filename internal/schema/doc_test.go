package schema

import (
	goast "go/ast"
	gotoken "go/token"
	"slices"
	"strings"
	"testing"
)

// TestTheObjectSetSizeIsPinned keeps the inventory closed. Step 12's criterion-3 assertion ranges
// over this set, so a seventh table joining the package without joining Objects would make that
// inventory silently incomplete (.claude/rules/assert-a-set-wide-invariant-over-the-set.md).
func TestTheObjectSetSizeIsPinned(t *testing.T) {
	if len(Objects) != 11 {
		t.Fatalf("Objects holds %d entries, want the six tables, the DEFAULT partition and the four "+
			"indexes; update this count with the set", len(Objects))
	}
}

func TestEveryObjectNameIsUsableAsAnIdentifier(t *testing.T) {
	seen := map[string]int{}
	for i, object := range Objects {
		if object.Name == "" {
			t.Errorf("object %d has no name", i)
		}
		if len(object.Name) > MaxIdentifierBytes {
			t.Errorf("%q is %d bytes, over PostgreSQL's %d-byte identifier limit (M9)",
				object.Name, len(object.Name), MaxIdentifierBytes)
		}
		if object.Name != strings.ToLower(object.Name) {
			t.Errorf("%q is not all lower case, so an unquoted reference to it would fold to a "+
				"different name than a quoted one", object.Name)
		}
		if first, repeated := seen[object.Name]; repeated {
			t.Errorf("objects %d and %d are both named %q", first, i, object.Name)
		}
		seen[object.Name] = i
	}
}

// TestEveryObjectDeclaresAKindAndTheMigrationThatCreatesIt is what lets Step 12 cross-check this set
// against the DDL without per-table knowledge of its own: an index is written into the catalog
// differently from a table, and the ledger is created by migration 1 while everything else waits for
// migration 2 (ADR-10's split).
func TestEveryObjectDeclaresAKindAndTheMigrationThatCreatesIt(t *testing.T) {
	kinds := map[ObjectKind]int{}
	inLedgerMigration := 0
	for _, object := range Objects {
		if !slices.Contains([]ObjectKind{KindTable, KindPartition, KindIndex}, object.Kind) {
			t.Errorf("%q declares the kind %q, which is none of the three", object.Name, object.Kind)
		}
		if object.Migration != 1 && object.Migration != 2 {
			t.Errorf("%q is created by migration %d, and this phase writes two",
				object.Name, object.Migration)
		}
		kinds[object.Kind]++
		if object.Migration == 1 {
			inLedgerMigration++
		}
	}

	if kinds[KindTable] != 6 || kinds[KindPartition] != 1 || kinds[KindIndex] != 4 {
		t.Errorf("the set holds %d tables, %d partitions and %d indexes, want 6, 1 and 4",
			kinds[KindTable], kinds[KindPartition], kinds[KindIndex])
	}
	if inLedgerMigration != 1 {
		t.Errorf("%d objects are created by migration 1, want only the ledger: migration 1 creates "+
			"schema_version alone so the runner can read it before applying anything else",
			inLedgerMigration)
	}
}

// TestEveryExportedObjectNameConstantJoinedTheSet reconciles in both directions, because each
// direction hides a different mistake: a constant nothing creates, and an object phases 3 to 6
// cannot name (.claude/rules/falsify-the-assertion-not-a-copy-of-it.md).
func TestEveryExportedObjectNameConstantJoinedTheSet(t *testing.T) {
	declared := exportedUntypedStringConstants(t)
	if len(declared) == 0 {
		t.Fatal("doc.go declares no untyped exported string constant, so this reconciliation " +
			"would pass in both directions vacuously")
	}

	named := map[string]bool{}
	for _, object := range Objects {
		named[object.Name] = true
	}
	for constant, value := range declared {
		if !named[value] {
			t.Errorf("doc.go declares %s = %q and Objects holds no object of that name",
				constant, value)
		}
	}

	valued := map[string]bool{}
	for _, value := range declared {
		valued[value] = true
	}
	for _, object := range Objects {
		if !valued[object.Name] {
			t.Errorf("Objects holds %q and doc.go declares no exported constant for it, so a "+
				"caller in phase 3 to 6 would have to write the name out", object.Name)
		}
	}
}

// exportedUntypedStringConstants reads doc.go's object-name constants. The filter is the declared
// type: an object name is an untyped string constant, and the kinds carry ObjectKind, so the two
// groups cannot be confused for one another. TestTheKindConstantsAreTypedSoTheFilterHolds asserts
// that premise rather than assuming it.
func exportedUntypedStringConstants(t *testing.T) map[string]string {
	t.Helper()

	found := map[string]string{}
	for _, spec := range constantSpecsOf(t) {
		if spec.Type != nil {
			continue
		}
		for i, name := range spec.Names {
			literal, isLiteral := spec.Values[i].(*goast.BasicLit)
			if name.IsExported() && isLiteral && literal.Kind == gotoken.STRING {
				found[name.Name] = strings.Trim(literal.Value, `"`)
			}
		}
	}
	return found
}

func TestTheKindConstantsAreTypedSoTheFilterHolds(t *testing.T) {
	typed := 0
	for _, spec := range constantSpecsOf(t) {
		if identified, isIdent := spec.Type.(*goast.Ident); isIdent && identified.Name == "ObjectKind" {
			typed += len(spec.Names)
		}
	}

	if typed != 3 {
		t.Errorf("doc.go declares %d ObjectKind constants, want the three kinds; an untyped one "+
			"would be read as an object name by the reconciliation above", typed)
	}
}

// constantSpecsOf is every constant specification doc.go declares.
func constantSpecsOf(t *testing.T) []*goast.ValueSpec {
	t.Helper()

	var specs []*goast.ValueSpec
	for _, declaration := range parsedSource(t, "doc.go", sourceBytes(t, "doc.go")).Decls {
		general, isGeneral := declaration.(*goast.GenDecl)
		if !isGeneral || general.Tok != gotoken.CONST {
			continue
		}
		for _, spec := range general.Specs {
			valued, isValued := spec.(*goast.ValueSpec)
			if isValued && len(valued.Values) == len(valued.Names) {
				specs = append(specs, valued)
			}
		}
	}
	return specs
}
