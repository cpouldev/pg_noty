//go:build integration

package schema

import (
	"slices"
	"testing"
)

// This file is criterion 1 against a real catalog, and criterion 2's other half: what a refused boot
// leaves behind, which is nothing.
//
// Both are closed forms. "The schema exists" would pass for a boot that also created three others,
// so what is asserted is the *difference* between two readings of the schema list -- and criterion
// 2's is the whole database compared for equality, because a refusal that created something
// somewhere else is still a refusal that acted.

// theBuiltInSchema and theConfiguredSchema are criterion 1's two answers, written here as the
// literals the criterion names. Deriving them from the configuration internal/config resolved would
// make the assertion a self-comparison that passes for whatever default the code happens to carry.
// `CREATE SCHEMA pg_noty` is refused by the server even as superuser, which is why the built-in one
// is `noty` and not `pg_noty`.
const (
	theBuiltInSchema    = "noty"
	theConfiguredSchema = "tenant_a"
)

// TestTheConfiguredSchemaIsTheOnlySchemaCreated is criterion 1.
func TestTheConfiguredSchemaIsTheOnlySchemaCreated(t *testing.T) {
	skipIfShort(t)

	for _, tc := range []struct {
		name, written, want string
	}{
		{name: "database.schema unset", written: "", want: theBuiltInSchema},
		{name: "database.schema set", written: theConfiguredSchema, want: theConfiguredSchema},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := freshDatabase(t)
			cfg := aParsedConfiguration(t, tc.written)

			before := schemaNames(t, pool)
			if slices.Contains(before, tc.want) {
				t.Fatalf("%s already exists in a freshly restored database, so this case would pass "+
					"whether or not the boot created it", tc.want)
			}

			mustBootstrap(t, pool, cfg)

			added := namesAddedBetween(before, schemaNames(t, pool))
			if !slices.Equal(added, []string{tc.want}) {
				t.Errorf("the boot added the schemas %v, want exactly [%s]", added, tc.want)
			}
			if cfg.Database.Schema != tc.want {
				t.Errorf("internal/config resolved database.schema to %q where this criterion names %q; the "+
					"boot created the right schema for the wrong configuration",
					cfg.Database.Schema, tc.want)
			}
		})
	}
}

// TestARefusedNameLeavesTheDatabaseExactlyAsItWas is criterion 2 against a server. The refusal is
// asserted container-free in bootstrap_test.go, because that is where "before any statement" can be
// established at all; what is asserted here is its consequence, by comparing the whole database
// rather than by inspecting the statements a refused boot did or did not issue.
func TestARefusedNameLeavesTheDatabaseExactlyAsItWas(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)
	cfg.Database.Schema = "PG_refused"

	before := catalogInventory(t, pool, noSchemaExcluded)
	schemasBefore := schemaNames(t, pool)

	if err := Bootstrap(t.Context(), pool, cfg); err == nil {
		t.Fatal("a boot onto a schema named out of PostgreSQL's reserved namespace succeeded")
	} else if err.Error() != reservedSchemaName(cfg.Database.Schema).Error() {
		t.Fatalf("the boot answered %q, want the reserved-name refusal; a mixed-case name reaching "+
			"the server is created rather than refused (M8)", err)
	}

	if added := namesAddedBetween(schemasBefore, schemaNames(t, pool)); len(added) != 0 {
		t.Errorf("the refused boot created the schemas %v", added)
	}
	assertInventoryUnchanged(t, before, catalogInventory(t, pool, noSchemaExcluded),
		"a boot refused for its schema name")
}

// namesAddedBetween is every name the second reading holds and the first did not, in the second
// reading's order.
func namesAddedBetween(before, after []string) []string {
	var added []string
	for _, name := range after {
		if !slices.Contains(before, name) {
			added = append(added, name)
		}
	}
	return added
}
