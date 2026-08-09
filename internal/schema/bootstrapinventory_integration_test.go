//go:build integration

package schema

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds the two inventories every negative claim in this step is established by. Both are
// comparisons rather than statement counts: counting the statements a boot issued measures the
// bookkeeping a defective boot gets wrong, while an inventory measures the database, which is what
// the criteria are about.
//
// bootInventory is the inside one -- criterion 18's bar -- and reads through Step 10's shared schema
// inventory so the two steps cannot come to disagree about what "unchanged" means. catalogInventory
// is the outside one, and the blast-radius NFR's.

// noSchemaExcluded is a name no schema has, so an inventory taken with it covers the whole database
// -- the configured schema included. Criterion 17's "leaves the database unmodified" needs exactly
// that, because an inventory blind to the configured schema could not see one created wrongly.
const noSchemaExcluded = ""

// theCatalogInventoryQuery is everything outside one named schema, as one sorted list: the schemas
// themselves with their owner and ACL, every relation with its kind and partition bound, every
// column with its type and nullability, every index with its definition, every role, and every
// default privilege. Ownership, roles and default privileges are in it because the blast-radius
// claim is about the *database* and not only about its relations.
//
// System schemas are excluded rather than compared: creating a table inside the configured schema
// creates its TOAST relation in pg_toast, which is a consequence of work this boot is entitled to do
// rather than a blast outside it.
const theCatalogInventoryQuery = `
WITH outside AS (
  SELECT oid, nspname FROM pg_catalog.pg_namespace
   WHERE nspname <> $1 AND nspname <> 'information_schema' AND nspname NOT LIKE 'pg\_%')
SELECT 'schema ' || o.nspname || ' owner=' || pg_catalog.pg_get_userbyid(n.nspowner) ||
       ' acl=' || coalesce(n.nspacl::text, '-')
  FROM outside o JOIN pg_catalog.pg_namespace n ON n.oid = o.oid
UNION ALL
SELECT 'relation ' || o.nspname || '.' || c.relname || ' ' || c.relkind::text || ' ' ||
       coalesce(pg_catalog.pg_get_expr(c.relpartbound, c.oid), '-')
  FROM pg_catalog.pg_class c JOIN outside o ON o.oid = c.relnamespace
UNION ALL
SELECT 'column ' || o.nspname || '.' || c.relname || '.' || a.attname || ' ' ||
       pg_catalog.format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull::text
  FROM pg_catalog.pg_attribute a
  JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
  JOIN outside o ON o.oid = c.relnamespace
 WHERE a.attnum > 0 AND NOT a.attisdropped
UNION ALL
SELECT 'index ' || i.schemaname || '.' || i.indexname || ' ' || i.indexdef
  FROM pg_catalog.pg_indexes i JOIN outside o ON o.nspname = i.schemaname
UNION ALL
SELECT 'role ' || rolname || ' super=' || rolsuper::text || ' createdb=' || rolcreatedb::text ||
       ' login=' || rolcanlogin::text
  FROM pg_catalog.pg_roles
UNION ALL
SELECT 'defaultacl ' || coalesce(n.nspname, '-') || ' ' || d.defaclobjtype::text || ' ' ||
       d.defaclacl::text
  FROM pg_catalog.pg_default_acl d
  LEFT JOIN pg_catalog.pg_namespace n ON n.oid = d.defaclnamespace
ORDER BY 1`

// theDefinitionInventoryQuery is what criterion 18's bar names and Step 10's shared schema inventory
// does not carry: a relation's kind and partition bound, a constraint's *definition* rather than its
// name, and a column's default. An existence-only inventory passes a constraint or an index silently
// redefined, which is precisely the class a second boot could introduce.
const theDefinitionInventoryQuery = `
SELECT 'relation ' || c.relname || ' ' || c.relkind::text || ' ' ||
       coalesce(pg_catalog.pg_get_expr(c.relpartbound, c.oid), '-')
  FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = $1
UNION ALL
SELECT 'constraintdef ' || rel.relname || '.' || con.conname || ' ' ||
       pg_catalog.pg_get_constraintdef(con.oid)
  FROM pg_catalog.pg_constraint con
  JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = rel.relnamespace
 WHERE n.nspname = $1
UNION ALL
SELECT 'default ' || c.relname || '.' || a.attname || ' ' ||
       pg_catalog.pg_get_expr(d.adbin, d.adrelid)
  FROM pg_catalog.pg_attrdef d
  JOIN pg_catalog.pg_class c ON c.oid = d.adrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
 WHERE n.nspname = $1
ORDER BY 1`

// theSchemaNamesQuery is every schema a boot could have created, which is a narrower question than
// the inventory above and is asked on its own so criterion 1's closed form fails on the name.
const theSchemaNamesQuery = `
SELECT nspname FROM pg_catalog.pg_namespace
 WHERE nspname <> 'information_schema' AND nspname NOT LIKE 'pg\_%' ORDER BY nspname`

// theOIDDerivedConstraintName matches the name PostgreSQL 17.10 synthesises for a NOT NULL
// constraint in information_schema.table_constraints:
// `<relation oid>_<relation oid>_<attnum>_not_null`. Measured: those identifiers are stable inside
// one database and different in the next, so an inventory carrying them can compare two boots into
// one database and not one boot against another database's -- which is what the concurrent case has
// to do.
//
// Dropping them costs this comparison nothing. A column's nullability is carried by its own entry,
// and every constraint PostgreSQL stores in pg_constraint keeps its whole definition through the
// query above. TestPostgresStillNamesNotNullConstraintsFromObjectIdentifiers pins the measurement,
// so a release that stops synthesising them fails there rather than leaving this filter quietly
// inert.
var theOIDDerivedConstraintName = regexp.MustCompile(`^constraint \S+ CHECK \d+_\d+_\d+_not_null$`)

// inventoryRows is one inventory query's rows, in the order it ordered them. It reads through the
// pool rather than a pinned connection because every inventory here is taken outside the episode it
// brackets, never inside it.
func inventoryRows(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), query, arguments...)
	if err != nil {
		t.Fatalf("read an inventory: %v", err)
	}
	defer rows.Close()

	collected, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect an inventory: %v", err)
	}
	return collected
}

// assertEveryDimensionIsPresent fails an inventory that lost one of the kinds of entry it claims to
// carry. A query reading the wrong thing answers nothing, which is also what an unchanged database
// answers.
func assertEveryDimensionIsPresent(t *testing.T, found, dimensions []string, subject string) {
	t.Helper()

	for _, dimension := range dimensions {
		if !slices.ContainsFunc(found, func(entry string) bool {
			return strings.HasPrefix(entry, dimension)
		}) {
			t.Fatalf("the inventory of %s holds no %sentry among its %d, so a comparison against it "+
				"would report that dimension unchanged whatever happened", subject, dimension, len(found))
		}
	}
}

// catalogInventory is the whole database outside one schema.
func catalogInventory(t *testing.T, pool *pgxpool.Pool, exceptSchema string) []string {
	t.Helper()

	found := inventoryRows(t, pool, theCatalogInventoryQuery, exceptSchema)
	assertEveryDimensionIsPresent(t, found, []string{"schema ", "role "}, "the database")
	return found
}

// bootInventory is criterion 18's bar for one schema: the columns, constraints and indexes Step 10
// declared as this module's shared definition of "unchanged", plus the definitions that definition
// does not carry, less the synthesised names above.
func bootInventory(t *testing.T, pool *pgxpool.Pool, schemaName string) []string {
	t.Helper()

	found := append(schemaInventory(t, pool, schemaName),
		inventoryRows(t, pool, theDefinitionInventoryQuery, schemaName)...)
	found = slices.DeleteFunc(found, theOIDDerivedConstraintName.MatchString)
	slices.Sort(found)

	assertEveryDimensionIsPresent(t, found,
		[]string{"column ", "constraint ", "constraintdef ", "index ", "relation "}, schemaName)
	return found
}

// schemaNames is every schema the database holds that PostgreSQL did not put there.
func schemaNames(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	return inventoryRows(t, pool, theSchemaNamesQuery)
}

// TestPostgresStillNamesNotNullConstraintsFromObjectIdentifiers pins the measurement the filter
// above rests on. Without it a release that stopped synthesising those names would leave the filter
// removing nothing, and the concurrent case's cross-database comparison would be carrying a
// dimension it was never meant to.
func TestPostgresStillNamesNotNullConstraintsFromObjectIdentifiers(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)

	synthesised := slices.DeleteFunc(schemaInventory(t, pool, harnessSchema),
		func(entry string) bool { return !theOIDDerivedConstraintName.MatchString(entry) })
	if len(synthesised) == 0 {
		t.Fatalf("no entry of %s's constraint inventory is named from object identifiers, so the "+
			"filter bootInventory applies removes nothing; re-derive it against this server",
			harnessSchema)
	}
}
