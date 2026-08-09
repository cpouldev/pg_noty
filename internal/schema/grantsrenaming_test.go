package schema

import (
	"slices"
	"strings"
	"testing"
)

// The binding between the statements criterion 42 applies to a server and the contract Step 5 wrote
// down. It compares two values and reaches nothing, so it carries no build tag and runs in the
// container-free half where a `-short` run still checks it. grants_integration_test.go opens the
// roles named below and asserts the server measurement that makes the renaming necessary.

const (
	// theServiceRole is the role the documented set grants to, under a name a server will accept.
	//
	// Measured on PostgreSQL 17.10: `CREATE ROLE "pg_noty"` is refused with `role name "pg_noty" is
	// reserved` (SQLSTATE 42939) -- the pg_ prefix is reserved for role names as well as for schemas
	// -- and grants_test.go picks that name deliberately, as the near-miss that keeps R4's
	// reserved-prefix refusal scoped to the schema position. So the documented role cannot exist on
	// any cluster, and what is applied is the same emitter's output for a creatable name.
	// TestTheAppliedGrantsAreStep5sGoldenUnderOneRenaming binds the two, and
	// TestTheGoldensOwnRoleNameIsOneTheServerRefusesToCreate is the measurement that justifies the
	// indirection existing at all.
	theServiceRole     = "noty_service"
	theServicePassword = "noty-service-secret"
	// theApplicationRole is the customer's own role: it holds no privilege whatsoever on the service
	// schema, which is the counter-intuitive deliverable criterion 42 exists to prove.
	theApplicationRole     = "noty_application"
	theApplicationPassword = "noty-application-secret"
)

// theAppliedGrants is the statement set criterion 42 applies: the emitter's own output, for the
// schema and targets Step 5's fixture names and for a role a server will accept. Nothing in it is
// written out by hand, so the proved behaviour and the documented contract cannot be two claims.
func theAppliedGrants(t *testing.T) []string {
	t.Helper()

	statements := GrantStatements(fixtureSchema, theServiceRole, fixtureTargets)
	if len(statements) != 3+len(fixtureTargets) {
		t.Fatalf("the emitter answered %d statements %q for %s; it emits nothing at all when any "+
			"part of the configuration is unusable", len(statements), statements, theServiceRole)
	}
	return statements
}

// goldenGrantStatements is Step 5's contract as the DBA reads it, one statement per line.
func goldenGrantStatements(t *testing.T) []string {
	t.Helper()

	recorded := strings.TrimSuffix(documentText(t, grantsGolden), "\n")
	if recorded == "" {
		t.Fatalf("%s is empty, so binding the applied statements to it would assert nothing",
			grantsGolden)
	}
	return strings.Split(recorded, "\n")
}

// TestTheAppliedGrantsAreStep5sGoldenUnderOneRenaming is what keeps the contract and the behaviour
// one claim: every statement applied to a server is Step 5's golden line with the role rebound and
// nothing else changed. A privilege added, a target dropped or a family reworded on either side
// fails here.
//
// The last clause is the renaming's own falsifiability: were the two sets already equal, the
// renaming would be inert and this whole indirection could go.
func TestTheAppliedGrantsAreStep5sGoldenUnderOneRenaming(t *testing.T) {
	applied, golden := theAppliedGrants(t), goldenGrantStatements(t)
	if len(applied) != len(golden) {
		t.Fatalf("%d statements are applied and %s holds %d:\napplied %q\ngolden  %q",
			len(applied), grantsGolden, len(golden), applied, golden)
	}

	// The role is rebound by its plain name rather than through the quoting authority, which this
	// half of the package cannot reach: the emitter renders both names the same way, and the byte
	// comparison below is what checks that it did.
	renamed := make([]string, 0, len(applied))
	for _, statement := range applied {
		renamed = append(renamed, strings.ReplaceAll(statement, theServiceRole, fixtureRole))
	}

	for number, want := range golden {
		if renamed[number] != want {
			t.Errorf("statement %d is applied as\n\t%s\nwhich under the renaming reads\n\t%s\n"+
				"and %s records\n\t%s", number+1, applied[number], renamed[number], grantsGolden, want)
		}
	}
	if slices.Equal(applied, golden) {
		t.Errorf("the applied set is already %s byte for byte, so the renaming is inert and %q can "+
			"be applied directly", grantsGolden, fixtureRole)
	}
}
