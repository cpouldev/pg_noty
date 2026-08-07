//go:build integration

package source

import (
	"errors"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// theHostileTableName is the injection class's member: a double quote that would close the
// identifier, a semicolon, a trailing comment sequence and whitespace. Its authority is
// PostgreSQL's identifier grammar -- every byte here is one a quoted identifier may hold -- never
// internal/config's charset rule, which refuses all four.
const theHostileTableName = `orders"; DROP TABLE users; -- `

// theSyntaxErrorCode is PostgreSQL's syntax_error. A control is pinned by code rather than by the
// absence of success, so its refusal is the server saying "this text is not one legal statement"
// rather than any failure at all.
const theSyntaxErrorCode = "42601"

// executeObjectSet applies one object set in the order internal/reconcile applies it, asserting per
// statement that the server ran exactly one. The statement list is generatedStatements' rather than a second
// copy of it, so the two cannot drift about which statements a set carries or what order they
// arrive in.
//
// The count lives here rather than in each suite because the clause is "every statement
// executes as exactly one statement" for *every* case: every container-backed case in this package
// applies its DDL through this function, so each one carries the assertion without restating it.
func executeObjectSet(t *testing.T, pool *pgxpool.Pool, set ObjectSet) {
	t.Helper()
	for _, statement := range generatedStatements(set) {
		execAsExactlyOneStatement(t, pool, statement)
	}
}

// plantTheDecoy creates the `users` table an injection through a table name reaches, and puts a row
// in it. Both are asserted afterwards: a DROP-shaped injection removes the table, a TRUNCATE-shaped
// one leaves it empty, and only the row count tells the second apart from a pass.
func plantTheDecoy(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	mustExecOn(t, pool, `CREATE TABLE public.users (id int)`)
	mustExecOn(t, pool, `INSERT INTO public.users (id) VALUES (1)`)
}

// assertTheDecoySurvived is the near-miss turning "the DDL executed" into "the DDL executed and
// nothing else did". Without it a hostile name that closed its identifier and appended
// `DROP TABLE users; --` would execute against a database where `users` does not exist, produce no
// error, and pass every other assertion in the suite.
func assertTheDecoySurvived(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var rows int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM public.users`).Scan(&rows); err != nil {
		t.Fatalf("users decoy changed: it no longer exists: %v", err)
	}
	if rows != 1 {
		t.Fatalf("users decoy changed: it holds %d rows, want the one row planted in it", rows)
	}
}

func mustQuoteIdentifier(t *testing.T, name string) string {
	t.Helper()
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		t.Fatalf("identifier %q is unusable: %s", name, fault)
	}
	return quoted
}

// refusalCodeOf reports the SQLSTATE a refusal carries, or "" for an error the server did not send.
func refusalCodeOf(err error) string {
	var refused *pgconn.PgError
	if errors.As(err, &refused) {
		return refused.Code
	}
	return ""
}

// TestCatalogHostileTableIsQuotedAndInert is the injection claim on route one, the resolved metadata.
//
// Route: resolved metadata. The target table reaches Generate as the catalog spells it and
// passes through no configuration rule -- internal/reconcile reads it out of pg_class during OID
// resolution and rename detection -- so this is a live class, asserted end to end against a real server rather than
// a guarded one asserted at the boundary. theHostileCases records the same declaration in a form a
// test can read.
func TestCatalogHostileTableIsQuotedAndInert(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	plantTheDecoy(t, pool)

	quotedTable := mustQuoteIdentifier(t, theHostileTableName)
	execAsExactlyOneStatement(t, pool, "CREATE TABLE public."+quotedTable+" (id int)")

	request := generationRequest(configOperation("insert"))
	request.Target.Table = theHostileTableName
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	assertTheObjectsExistOn(t, pool, sets[0], theHostileTableName)

	execAsExactlyOneStatement(t, pool, "INSERT INTO public."+quotedTable+" (id) VALUES (1)")
	assertStoredTableNameRoundTrips(t, pool, "public", theHostileTableName)
	assertTheDecoySurvived(t, pool)
}

// assertTheObjectsExistOn reads the two objects out of the catalog rather than trusting that the
// DDL returned no error, matching the trigger's table by pg_class.relname against the hostile
// name's own bytes.
func assertTheObjectsExistOn(t *testing.T, pool *pgxpool.Pool, set ObjectSet, table string) {
	t.Helper()
	if got := countOn(
		t, pool, "SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace "+
			"WHERE n.nspname=$1 AND p.proname=$2", harnessSchema, set.FunctionName,
	); got != 1 {
		t.Fatalf("the catalog holds %d functions named %q in %s, want 1", got, set.FunctionName, harnessSchema)
	}
	if got := countOn(
		t, pool, "SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid "+
			"JOIN pg_namespace n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal AND n.nspname='public' "+
			"AND c.relname=$1 AND t.tgname=$2", table, set.TriggerName,
	); got != 1 {
		t.Fatalf("the catalog holds %d triggers named %q on public.%q, want 1", got, set.TriggerName, table)
	}
}

// assertStoredTableNameRoundTrips unquotes events.table_name through SplitQualified -- the one
// parser for that spelling, called and never re-declared -- and compares both halves against the
// catalog's own bytes. A substring check passes for a value quoted wrongly, which is the defect
// this exists to catch; comparing against the test's input string would pass for a name the server normalised.
func assertStoredTableNameRoundTrips(t *testing.T, pool *pgxpool.Pool, schemaName, table string) {
	t.Helper()
	var stored string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT table_name FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&stored); err != nil {
		t.Fatalf("read the stored table_name: %v", err)
	}
	gotSchema, gotTable, ok := SplitQualified(stored)
	if !ok {
		t.Fatalf("stored table_name %q is not the spelling schema.Qualified emits", stored)
	}
	var catalogSchema, catalogTable string
	if err := pool.QueryRow(
		t.Context(), "SELECT n.nspname, c.relname FROM pg_class c "+
			"JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2",
		schemaName, table,
	).Scan(&catalogSchema, &catalogTable); err != nil {
		t.Fatalf("the catalog holds no %s.%s: %v", schemaName, table, err)
	}
	if gotSchema != catalogSchema || gotTable != catalogTable {
		t.Fatalf(
			"stored table_name round-trips to %q.%q, want the catalog's own %q.%q",
			gotSchema, gotTable, catalogSchema, catalogTable,
		)
	}
}

func configOperation(kind string) config.Operation { return config.Operation{Kind: kind} }
