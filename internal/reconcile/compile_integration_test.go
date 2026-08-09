//go:build integration

package reconcile

import (
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompileUsesCatalogBytesForHostileTargetAndLeavesDecoyUntouched(t *testing.T) {
	// derive-expected-values.md requires catalog readback, not a transcribed target expectation.
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	actualName, primaryKey := "Order \" Space;", "Key \" Space;"
	actual := mustQualifiedTarget(t, actualName)
	key := mustQuotedIdentifier(t, primaryKey)
	mustExecOn(t, pool, "CREATE TABLE "+actual+" ("+key+" bigint PRIMARY KEY, note text)")
	decoy := mustQualifiedTarget(t, "order \" space;")
	mustExecOn(t, pool, "CREATE TABLE "+decoy+" ("+key+" bigint PRIMARY KEY, note text)")
	reading := resolvedCatalogTarget(t, pool, actual)
	quotedActual := mustQuotedIdentifier(t, actualName)
	listener := listenerForTarget("PUBLIC." + quotedActual)
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	resolution, err := resolveListenerTarget(t.Context(), catalog, listener, nil)
	if err != nil || resolution.Outcome != targetNew {
		t.Fatalf("resolveListenerTarget() = %#v, %v; want new catalog target", resolution, err)
	}
	catalogSpelling := "public." + quotedActual
	if configured := listener.Trigger.Table; configured == catalogSpelling || !strings.EqualFold(
		configured,
		catalogSpelling,
	) {
		t.Fatalf(
			"configured target %q and catalog spelling %q are not a case-only difference",
			configured,
			catalogSpelling,
		)
	}
	compiled, err := compileListener("alpha", harnessSchema, listener, resolution.Target)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Target.Schema != reading.Schema || compiled.Target.Table != reading.Table ||
		!slices.Equal(compiled.Target.PrimaryKeyColumns, reading.PrimaryKeyColumns) {
		t.Fatalf("compiled target = %#v, want catalog reading %#v", compiled.Target, reading)
	}
	assertAppendedStatementIsCounted(t, pool, compiled.Sets[0].CreateFunction)
	installCompiledStatements(t, pool, compiled)
	mustExecOn(t, pool, "INSERT INTO "+actual+" ("+key+", note) VALUES (1, 'actual')")
	if got := countOn(t, pool, "SELECT count(*) FROM \"noty\".events WHERE listener = 'orders'"); got != 1 {
		t.Fatalf("actual target events = %d, want 1", got)
	}
	mustExecOn(t, pool, "INSERT INTO "+decoy+" ("+key+", note) VALUES (1, 'decoy')")
	if got := countOn(t, pool, "SELECT count(*) FROM \"noty\".events WHERE listener = 'orders'"); got != 1 {
		t.Fatalf("decoy changed event count to %d, want 1", got)
	}
}

func installCompiledStatements(t *testing.T, pool *pgxpool.Pool, compiled compiledListener) {
	t.Helper()
	for _, set := range compiled.Sets {
		execCompiledStatement(t, pool, set.CreateFunction)
		execCompiledStatement(t, pool, set.RevokeExecute)
		execCompiledStatement(t, pool, set.CommentFunction)
		execCompiledStatement(t, pool, set.CreateTrigger)
		execCompiledStatement(t, pool, set.CommentTrigger)
	}
}

// The simple protocol is the generated-DDL route: one server CommandComplete reply arrives per
// statement. Counting replies proves the server's parse, unlike a semicolon scan or cached result.
func serverStatementCount(t *testing.T, pool *pgxpool.Pool, statement string) (int, error) {
	t.Helper()
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		return 0, err
	}
	defer connection.Release()
	results, err := connection.Conn().PgConn().Exec(t.Context(), statement).ReadAll()
	return len(results), err
}

func execCompiledStatement(t *testing.T, pool *pgxpool.Pool, statement string) {
	t.Helper()
	ran, err := serverStatementCount(t, pool, statement)
	if err != nil {
		t.Fatalf("execute generated DDL: %v", err)
	}
	if ran != 1 {
		t.Fatalf("server ran %d statements for generated DDL, want 1:\n%s", ran, statement)
	}
}

func assertAppendedStatementIsCounted(t *testing.T, pool *pgxpool.Pool, statement string) {
	t.Helper()
	ran, err := serverStatementCount(t, pool, statement+" SELECT 1")
	if err != nil || ran != 2 {
		t.Fatalf("appended statement ran %d, %v; want the server to count 2", ran, err)
	}
}

func mustQuotedIdentifier(t *testing.T, name string) string {
	t.Helper()
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		t.Fatalf("quoted identifier %q: %s", name, fault)
	}
	return quoted
}
