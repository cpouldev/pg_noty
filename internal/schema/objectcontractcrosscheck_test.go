package schema

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// The cross-check between doc.go's exported object-name constants and the object names the shipped
// DDL creates, in both directions. The packages above this one write the constants and the database
// holds the objects, so a constant naming nothing and an object no constant names are two different
// failures and each has to be able to fail on its own.
//
// The direction of authority is the point. The shipped DDL is what the database will hold and the
// constant set is the claim about it under test, so the expectation is taken from the DDL and from
// the contract -- never computed as len(Objects) or len(the constants), which would compare doc.go
// with itself and make the one thing this check exists to report the one thing it could not.

// theObjectsMigration is the migration this cross-check counts. Migration 1 creates the ledger
// alone so the runner can read it before applying anything else (ADR-10), so every other object
// the packages above this one name is written in 0002_objects.sql.
const theObjectsMigration = 2

// objectsTheContractPutsInTheObjectsMigration is how many objects 0002_objects.sql must create,
// counted from the external contract: the contract table's six tables less the ledger migration 1
// creates, criterion 29's one permanent DEFAULT partition, and the Requirements' four indexes.
func objectsTheContractPutsInTheObjectsMigration() int {
	const theLedgerMigrationOneCreates, thePermanentDefaultPartition = 1, 1

	return len(theContractTables) - theLedgerMigrationOneCreates +
		thePermanentDefaultPartition + len(theDeclaredIndexes)
}

// namesCreatedBy is every object name one shipped migration's DDL creates, read through the
// package's own DDL reader -- whose closure TestEveryStatementTheCorpusWritesIsReadByTheseAssertions
// asserts, so a CREATE written in a shape that reader cannot see is reported there rather than
// falling silently outside this check.
func namesCreatedBy(t *testing.T, version int) []string {
	t.Helper()

	created, unread := createdObjectsIn(migrationSQL(t, version))
	for _, line := range unread {
		t.Errorf("migration %d creates something this cross-check cannot read: %q", version, line)
	}

	named := make([]string, 0, len(created))
	for _, object := range created {
		named = append(named, object.name)
	}
	slices.Sort(named)
	return named
}

// namesTheShippedCorpusCreates is the authority side of the check: every object name the two
// migrations create between them.
func namesTheShippedCorpusCreates(t *testing.T) []string {
	t.Helper()

	var named []string
	for _, found := range embeddedCorpusOrFail(t) {
		named = append(named, namesCreatedBy(t, found.version)...)
	}
	slices.Sort(named)
	return named
}

// declaredObjectNameConstants is the subject: the values of doc.go's exported object-name
// constants, read through doc_test.go's reader rather than through a second one.
func declaredObjectNameConstants(t *testing.T) []string {
	t.Helper()

	return slices.Sorted(maps.Values(exportedUntypedStringConstants(t)))
}

// crossCheckIssues is the check itself, both directions through one function so that the
// falsifiability case below drives the assertion's own body. created is the authority and declared
// is the subject; swapping them would not change the issues reported, which is why the caller names
// which is which and this function never derives one from the other.
func crossCheckIssues(created, declared []string) []string {
	var issues []string
	for _, name := range created {
		if !slices.Contains(declared, name) {
			issues = append(issues, "the DDL creates "+name+" and doc.go declares no exported "+
				"constant for it, so a caller in a package above this one would have to write the name out")
		}
	}
	for _, name := range declared {
		if !slices.Contains(created, name) {
			issues = append(issues, "doc.go declares "+name+" and no shipped migration creates an "+
				"object of that name, so the packages above this one compile against something absent")
		}
	}
	return issues
}

// TestDocGoAndTheShippedDDLNameTheSameObjects is SC-8. The count is asserted first and separately:
// both name lists agreeing says nothing about whether either covers the contract, and a corpus that
// lost an object together with its constant would satisfy the reconciliation while leaving the
// schema short.
func TestDocGoAndTheShippedDDLNameTheSameObjects(t *testing.T) {
	declared := declaredObjectNameConstants(t)
	if len(declared) == 0 {
		t.Fatal("doc.go declares no exported object-name constant, so both directions below would " +
			"pass vacuously")
	}

	if got, want := len(namesCreatedBy(t, theObjectsMigration)),
		objectsTheContractPutsInTheObjectsMigration(); got != want {
		t.Errorf("migration %d creates %d objects and the contract puts %d there -- five tables, "+
			"the permanent DEFAULT partition and the four indexes; the count is derived from the "+
			"contract table and the Requirements rather than from doc.go, so this fails when the "+
			"DDL and the constants are jointly short", theObjectsMigration, got, want)
	}

	for _, issue := range crossCheckIssues(namesTheShippedCorpusCreates(t), declared) {
		t.Error(issue)
	}
}

// TestTheCrossCheckFailsInEachDirectionSeparately gives the assertion above its falsifiability, and
// does it by driving the very function that assertion runs. Each row starts from the shipped inputs
// and adds exactly one synthetic entry, so no row can pass against a degenerate pair of empty
// lists, and the control row proves the shipped pair is the reason the others are the only
// failures.
func TestTheCrossCheckFailsInEachDirectionSeparately(t *testing.T) {
	const phantom = "phantom_object"
	created, declared := namesTheShippedCorpusCreates(t), declaredObjectNameConstants(t)

	for _, tc := range []struct {
		name              string
		created, declared []string
		want              string
	}{
		{name: "the shipped pair", created: created, declared: declared},
		{name: "a declared constant naming nothing the DDL creates",
			created: created, declared: append(slices.Clone(declared), phantom),
			want: "doc.go declares " + phantom},
		{name: "an object the DDL creates and no constant names",
			created: append(slices.Clone(created), phantom), declared: declared,
			want: "the DDL creates " + phantom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := crossCheckIssues(tc.created, tc.declared)
			switch {
			case tc.want == "" && len(issues) != 0:
				t.Fatalf("the shipped pair diverges: %v", issues)
			case tc.want != "" && (len(issues) != 1 || !strings.Contains(issues[0], tc.want)):
				t.Fatalf("issues = %v, want exactly one naming %q", issues, tc.want)
			}
		})
	}
}
