//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGeneratedPrivilegeShapeComesFromTheCatalog(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.priv_target (id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "priv_target"
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	// The privilege shape is four independent clauses, so each is read from its own catalog source and
	// reported on its own: collapsed into one condition, a case tripping two would be
	// indistinguishable from one tripping the clause it is named for.
	var securityDefiner bool
	var configValues []string
	var owner string
	if err := pool.QueryRow(
		t.Context(), "SELECT p.prosecdef, p.proconfig, r.rolname FROM pg_proc p "+
			"JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_roles r ON r.oid=p.proowner "+
			"WHERE n.nspname=$1 AND p.proname=$2", harnessSchema, sets[0].FunctionName,
	).Scan(&securityDefiner, &configValues, &owner); err != nil {
		t.Fatal(err)
	}
	if !securityDefiner {
		t.Error(
			"pg_proc.prosecdef is false: the function runs as its caller, so the whole " +
				"privilege design below is inert",
		)
	}
	if owner != harnessUser {
		t.Errorf(
			"pg_proc.proowner joins pg_roles to %q, want the pg_noty role %q; a security-definer "+
				"function runs as its owner, so this clause decides whose rights the body gets",
			owner, harnessUser,
		)
	}
	if len(configValues) != 1 || configValues[0] != "search_path=pg_catalog, pg_temp" {
		t.Errorf("pg_proc.proconfig = %v, want exactly [search_path=pg_catalog, pg_temp]", configValues)
	}
	var publicExecute bool
	if err := pool.QueryRow(
		t.Context(),
		"SELECT has_function_privilege('public', $1::regprocedure, 'EXECUTE')",
		`noty.`+sets[0].FunctionName+`()`,
	).Scan(&publicExecute); err != nil {
		t.Fatal(err)
	}
	if publicExecute {
		t.Error(
			"PUBLIC retains EXECUTE on the generated function; the REVOKE was emitted and the " +
				"catalog does not reflect it",
		)
	}
}

// writeUnder runs one statement on a connection whose search_path is the caller's, and returns that
// connection to the session default before releasing it, so the next borrower of the pool does not
// inherit a hostile path -- the same reason applySourceMigrationsInto resets its own.
func writeUnder(t *testing.T, pool *pgxpool.Pool, searchPath, statement string) {
	t.Helper()
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	mustExecOn(t, connection, "SET search_path TO "+searchPath)
	defer mustExecOn(t, connection, "SET search_path TO DEFAULT")
	mustExecOn(t, connection, statement)
}

// TestTheGeneratedFunctionNamesTheServiceSchemaExplicitly is named for what its decoy can decide.
// The generated body writes "noty"."events" fully qualified, so no session search_path can reroute
// that write: the public.events decoy is a control on the qualification, and removing the
// function's own SET search_path would not change one thing this test observes. The clause that
// fixes the function's path is decided by the test below, where a path-sensitive reference actually
// lives.
func TestTheGeneratedFunctionNamesTheServiceSchemaExplicitly(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.events (id int)`)
	mustExecOn(t, pool, `CREATE TABLE public.orders (id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "orders"
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	// Before and after, because "the decoy is empty afterwards" is also true of a decoy that was
	// never reachable: only the pair says the write went past it.
	before := countOn(t, pool, `SELECT count(*) FROM public.events`)
	writeUnder(t, pool, "public, noty", `INSERT INTO public.orders VALUES (1)`)
	if after := countOn(t, pool, `SELECT count(*) FROM public.events`); after != before {
		t.Fatalf("public.events decoy went from %d rows to %d", before, after)
	}
	if service := countOn(t, pool, `SELECT count(*) FROM noty.events`); service != 1 {
		t.Fatalf("noty.events holds %d rows, want the one row the trigger wrote", service)
	}
}

// theShadowedCatalogFunction is a public to_jsonb taking the trigger's own row type. PostgreSQL
// resolves an exact argument match ahead of pg_catalog's polymorphic to_jsonb(anyelement), so this
// candidate wins whenever public is on the path in force.
const theShadowedCatalogFunction = `CREATE FUNCTION public.to_jsonb(public.shadow_target) RETURNS jsonb
	LANGUAGE sql IMMUTABLE AS $shadow$ SELECT jsonb_build_object('shadowed', true) $shadow$`

// TestTheFunctionsFixedSearchPathDefeatsAShadowedCatalogFunction is the case in which search_path
// genuinely decides. Every table the body names is qualified, so the only references left to
// resolve through a path are the catalog functions it calls -- and a shadow of one of those, placed
// in a schema the writing session has on its path, is reached by the body unless the function
// carries a path of its own. Deleting SET search_path = pg_catalog, pg_temp from statements.go
// fails this test and no other.
func TestTheFunctionsFixedSearchPathDefeatsAShadowedCatalogFunction(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.shadow_target (id int)`)
	mustExecOn(t, pool, theShadowedCatalogFunction)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "shadow_target"
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	// The shadow out-ranks the catalog's on a path holding public. Without this the assertions
	// below would also pass for a shadow that never resolves for anyone.
	reachable := countOn(
		t, pool,
		`SELECT count(*) FROM jsonb_object_keys(to_jsonb(NULL::public.shadow_target)) k WHERE k = 'shadowed'`,
	)
	if reachable != 1 {
		t.Fatal(
			"public.to_jsonb does not out-rank pg_catalog's on the session path, so the " +
				"assertions below cannot fail",
		)
	}

	writeUnder(t, pool, "public, noty", `INSERT INTO public.shadow_target VALUES (1)`)
	payload := eventPayload(t, pool)
	written, isObject := payload["new"].(map[string]any)
	if !isObject {
		t.Fatalf("the payload's new key is %T, want the target row as an object", payload["new"])
	}
	if _, shadowed := written["shadowed"]; shadowed {
		t.Fatalf("the body resolved to_jsonb through the writer's path and called public.to_jsonb: new = %v", written)
	}
	if _, own := written["id"]; !own {
		t.Fatalf("new = %v, want the target row's own columns", written)
	}
}
