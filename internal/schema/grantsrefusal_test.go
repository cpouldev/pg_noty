package schema

import (
	"strings"
	"testing"
)

// GrantStatements answers with statements a DBA applies by hand, and its signature has no room for
// a reason. So it emits nothing at all rather than a shorter set: an incomplete list that looks
// complete is the one answer that gets applied and then trusted.
//
// Each row below is unusable in exactly one position and ordinary in every other, so a clause
// silently dropped fails its own row rather than passing on a neighbour's refusal.
func TestAnUnusableConfigurationEmitsNoStatementAtAll(t *testing.T) {
	for _, tc := range []struct{ name, schema, role, target string }{
		{name: "an empty schema", schema: "", role: fixtureRole, target: "public.orders"},
		{name: "a schema holding a NUL the driver would delete", schema: "no\x00ty",
			role: fixtureRole, target: "public.orders"},
		{name: "a schema one byte past the identifier limit",
			schema: nameOfBytes(MaxIdentifierBytes + 1), role: fixtureRole, target: "public.orders"},
		{name: "a schema claiming the reserved prefix", schema: "pg_noty",
			role: fixtureRole, target: "public.orders"},
		{name: "the reserved prefix on its own", schema: reservedSchemaPrefix,
			role: fixtureRole, target: "public.orders"},

		{name: "an empty role", schema: fixtureSchema, role: "", target: "public.orders"},
		{name: "a role holding a NUL", schema: fixtureSchema, role: "pg_\x00noty", target: "public.orders"},
		{name: "a role one byte past the identifier limit", schema: fixtureSchema,
			role: nameOfBytes(MaxIdentifierBytes + 1), target: "public.orders"},

		{name: "a target that is not schema-qualified", schema: fixtureSchema,
			role: fixtureRole, target: "orders"},
		{name: "a target qualified twice", schema: fixtureSchema,
			role: fixtureRole, target: "db.public.orders"},
		{name: "a target with no schema part", schema: fixtureSchema,
			role: fixtureRole, target: ".orders"},
		{name: "a target with no table part", schema: fixtureSchema,
			role: fixtureRole, target: "public."},
		{name: "a target whose table part holds a NUL", schema: fixtureSchema,
			role: fixtureRole, target: "public.ord\x00ers"},
		{name: "an empty target", schema: fixtureSchema, role: fixtureRole, target: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if statements := GrantStatements(tc.schema, tc.role, []string{tc.target}); len(statements) != 0 {
				t.Errorf("an unusable configuration emitted %q; a DBA applying that grants "+
					"privileges the configuration did not name", statements)
			}
		})
	}
}

// TestOneUnusableTargetWithdrawsTheWholeSet is the half a per-entry skip would get wrong. Dropping
// only the offending statement leaves a set that reads as the documented minimum and silently omits
// one table's TRIGGER, which surfaces in internal/source as a permission denied on that table
// alone.
func TestOneUnusableTargetWithdrawsTheWholeSet(t *testing.T) {
	mixed := []string{"public.orders", "orders", "sales.invoices"}

	if statements := GrantStatements(fixtureSchema, fixtureRole, mixed); len(statements) != 0 {
		t.Errorf("a list holding one unusable target emitted %d statements %q, want none",
			len(statements), statements)
	}
}

// TestTheOrdinaryConfigurationIsNotRefused is the other side of every guard above: each row there
// differs from this one in a single property, so a guard widened past its clause fails here rather
// than silently emitting nothing for a valid configuration.
func TestTheOrdinaryConfigurationIsNotRefused(t *testing.T) {
	for _, tc := range []struct{ name, schema, role, target string }{
		{name: "the documented configuration", schema: fixtureSchema, role: fixtureRole, target: "public.orders"},
		// The role opens with the prefix reserved for system schemas, and it is a role.
		{name: "a role under the reserved schema prefix", schema: fixtureSchema,
			role: "pg_noty", target: "public.orders"},
		{name: "a schema at exactly the identifier limit", schema: nameOfBytes(MaxIdentifierBytes),
			role: fixtureRole, target: "public.orders"},
		{name: "a target at exactly the identifier limit in both parts", schema: fixtureSchema,
			role: fixtureRole, target: nameOfBytes(MaxIdentifierBytes) + "." + nameOfBytes(MaxIdentifierBytes)},
		{name: "a schema that merely opens with the prefix's first letters", schema: "pgnoty",
			role: fixtureRole, target: "public.orders"},
		{name: "a target whose table name carries a metacharacter", schema: fixtureSchema,
			role: fixtureRole, target: `public.ord"ers`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statements := GrantStatements(tc.schema, tc.role, []string{tc.target})

			if len(statements) != 4 {
				t.Fatalf("a usable configuration emitted %d statements %q, want 4",
					len(statements), statements)
			}
			for _, statement := range statements {
				if familyOf(statement) == familyUnknown {
					t.Errorf("%s is outside the documented set", statement)
				}
			}
		})
	}
}

// TestNoTargetAtAllStillEmitsTheSchemaStatements records the shape of a configuration with no
// listeners yet: the schema still has to be owned and usable, and there is simply no table to grant
// TRIGGER on. Emitting nothing here would be a refusal, and nothing is wrong with the input.
func TestNoTargetAtAllStillEmitsTheSchemaStatements(t *testing.T) {
	for _, targets := range [][]string{nil, {}} {
		statements := GrantStatements(fixtureSchema, fixtureRole, targets)

		if len(statements) != 3 {
			t.Fatalf("an empty target list emitted %d statements %q, want the three schema ones",
				len(statements), statements)
		}
		if joined := strings.Join(statements, "\n"); strings.Contains(joined, "ON TABLE") {
			t.Errorf("an empty target list emitted a table grant:\n%s", joined)
		}
	}
}
