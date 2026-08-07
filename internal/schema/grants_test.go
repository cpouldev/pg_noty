package schema

import (
	"slices"
	"strings"
	"testing"
)

// The configuration the documented grant set is emitted for. The role opens with the prefix
// PostgreSQL reserves for system *schemas*, deliberately: it is the near-miss that keeps the
// reserved-prefix refusal scoped to the position R4 gives it, so a predicate widened to every
// identifier fails here rather than refusing the role the service is named after.
const (
	fixtureSchema = "noty"
	fixtureRole   = "pg_noty"
)

// fixtureTargets is written out of order, so the emitted order can only come from the emitter.
var fixtureTargets = []string{"sales.invoices", "public.orders", "public.shipments"}

// grantFamily is one of the three statement families AC 41 permits, read as a closed set: anything
// outside them is a privilege the DBA did not need to grant.
type grantFamily string

const (
	familyOwnership grantFamily = "ownership of the service schema"
	familyCreate    grantFamily = "CREATE on the service schema"
	familyUsage     grantFamily = "USAGE on the service schema"
	familyTrigger   grantFamily = "TRIGGER on a target table"
	familyUnknown   grantFamily = "outside the documented set"
)

// familyOf classifies one emitted statement by the leader it opens with.
func familyOf(statement string) grantFamily {
	switch {
	case strings.HasPrefix(statement, "ALTER SCHEMA "):
		return familyOwnership
	case strings.HasPrefix(statement, "GRANT CREATE ON SCHEMA "):
		return familyCreate
	case strings.HasPrefix(statement, "GRANT USAGE ON SCHEMA "):
		return familyUsage
	case strings.HasPrefix(statement, "GRANT TRIGGER ON TABLE "):
		return familyTrigger
	default:
		return familyUnknown
	}
}

// TestTheEmittedSetIsClosedOverTheThreeDocumentedFamilies reads the output as a closed set rather
// than as a checklist of things that must appear: every statement is classified, and a statement
// nothing classifies is the failure.
func TestTheEmittedSetIsClosedOverTheThreeDocumentedFamilies(t *testing.T) {
	statements := GrantStatements(fixtureSchema, fixtureRole, fixtureTargets)

	counted := map[grantFamily]int{}
	for _, statement := range statements {
		counted[familyOf(statement)]++
	}

	for _, tc := range []struct {
		family grantFamily
		want   int
	}{
		{family: familyOwnership, want: 1},
		{family: familyCreate, want: 1},
		{family: familyUsage, want: 1},
		{family: familyTrigger, want: len(fixtureTargets)},
		{family: familyUnknown, want: 0},
	} {
		if counted[tc.family] != tc.want {
			t.Errorf("%d statements are %s, want %d: %q", counted[tc.family], tc.family, tc.want, statements)
		}
	}
	if len(statements) != 3+len(fixtureTargets) {
		t.Errorf("the set holds %d statements %q, want %d", len(statements), statements, 3+len(fixtureTargets))
	}
}

// TestEveryStatementGrantsToTheServiceRoleAndToNoOtherPrincipal is the counter-intuitive
// deliverable, asserted as a property of the set rather than as the absence of one name: the
// application role needs nothing, so the emitted set names exactly one grantee.
func TestEveryStatementGrantsToTheServiceRoleAndToNoOtherPrincipal(t *testing.T) {
	quotedRole, fault := Quoted(fixtureRole)
	if fault != IdentifierOK {
		t.Fatalf("the fixture role is unusable: %q", fault)
	}

	for _, statement := range GrantStatements(fixtureSchema, fixtureRole, fixtureTargets) {
		if !strings.HasSuffix(statement, " TO "+quotedRole+";") {
			t.Errorf("%s does not end by naming the service role, so it grants to someone else", statement)
		}
		if strings.Contains(statement, "PUBLIC") {
			t.Errorf("%s grants to PUBLIC", statement)
		}
	}
}

// TestNoStatementNamesAnObjectThisPackageDoesNotOwn keeps internal/source's trigger and function
// names out. The words below are the vocabulary those statements would have to use, so a grant
// written for a generated trigger or a SECURITY DEFINER function fails here rather than documenting
// a privilege this package cannot justify.
func TestNoStatementNamesAnObjectThisPackageDoesNotOwn(t *testing.T) {
	emitted := strings.Join(GrantStatements(fixtureSchema, fixtureRole, fixtureTargets), "\n")

	for _, sourceOwned := range []string{
		"CREATE TRIGGER", "FUNCTION", "EXECUTE", "SECURITY DEFINER", "ROUTINE",
	} {
		if strings.Contains(emitted, sourceOwned) {
			t.Errorf("the emitted set names %s, which belongs to internal/source:\n%s", sourceOwned, emitted)
		}
	}
}

// TestADuplicatedTargetProducesOneStatementForIt gives "each distinct target table" something to
// mean: without this row the word is untested and a per-entry emitter passes.
func TestADuplicatedTargetProducesOneStatementForIt(t *testing.T) {
	repeated := []string{"public.orders", "sales.invoices", "public.orders", "public.orders"}

	statements := GrantStatements(fixtureSchema, fixtureRole, repeated)
	if want := GrantStatements(fixtureSchema, fixtureRole, []string{"public.orders", "sales.invoices"}); !slices.Equal(statements, want) {
		t.Errorf("a list repeating a target emitted %q, want %q", statements, want)
	}
}

// TestTheEmittedOrderComesFromTheEmitterAndNotFromTheInput closes the map-iteration hole: an
// emitter that ranged over a map would pass on one machine and make the golden disagree on another.
// Two orderings and two calls are both asserted, because a stable-but-input-ordered emitter passes
// the repeat row alone.
func TestTheEmittedOrderComesFromTheEmitterAndNotFromTheInput(t *testing.T) {
	first := GrantStatements(fixtureSchema, fixtureRole, fixtureTargets)

	if again := GrantStatements(fixtureSchema, fixtureRole, fixtureTargets); !slices.Equal(first, again) {
		t.Errorf("two calls on one input emitted %q then %q", first, again)
	}

	reversed := slices.Clone(fixtureTargets)
	slices.Reverse(reversed)
	if other := GrantStatements(fixtureSchema, fixtureRole, reversed); !slices.Equal(first, other) {
		t.Errorf("%q emitted %q and %q emitted %q", fixtureTargets, first, reversed, other)
	}

	sorted := slices.Clone(fixtureTargets)
	slices.Sort(sorted)
	if other := GrantStatements(fixtureSchema, fixtureRole, sorted); !slices.Equal(first, other) {
		t.Errorf("a sorted input emitted %q, want %q", other, first)
	}
}

// TestTheEmitterDoesNotReorderTheCallersOwnSlice is the ordinary courtesy a caller's list is owed:
// Step 16 reads the same targets from configuration and applies them, and a sort in place would
// change what the caller sees afterwards.
//
// The subject is written here rather than taken from fixtureTargets, and that is the whole
// assertion rather than a style choice: an emitter that sorted in place would already have sorted
// the shared fixture in an earlier test, so a clone of it would compare equal to itself and this
// test would pass against the very defect it is named for.
func TestTheEmitterDoesNotReorderTheCallersOwnSlice(t *testing.T) {
	given := []string{"sales.invoices", "public.orders", "public.shipments"}
	asWritten := slices.Clone(given)

	GrantStatements(fixtureSchema, fixtureRole, given)

	if !slices.Equal(given, asWritten) {
		t.Errorf("the caller's slice reads %q after the call, want %q", given, asWritten)
	}
}
