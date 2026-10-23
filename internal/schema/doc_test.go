package schema

import (
	goast "go/ast"
	gotoken "go/token"
	"slices"
	"strings"
	"testing"
)

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

// exportedUntypedStringConstants reads doc.go's object-name constants. The filter is the declared
// type: an object name is an untyped string constant, and the kinds carry ObjectKind, so the two
// groups cannot be confused for one another.
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
