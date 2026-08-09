//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// D1, asserted as an effect rather than as statement text. A search_path set on the connection
// instead of the transaction, or set after the first statement, both produce a statement that reads
// correctly and routes wrongly; only where the objects land can tell those apart.

// The two schemas one process migrates into, and the two that must stay empty. A second schema in
// the same process is what catches a path cached across runs, which a single-schema suite cannot.
const (
	firstTenantSchema  = "tenant_a"
	secondTenantSchema = "tenant_b"
)

// schemaObjectNamesQuery is every table, partition and explicitly created index one schema holds.
// It reads names only: the object inventory proper -- columns, types, bounds -- is Step 12's, and
// this step asserts which schema the objects landed in.
//
// An index that backs a constraint is excluded, and by asking whether it *is* one rather than by
// matching a _pkey suffix: PostgreSQL creates it for the declared PRIMARY KEY, so it is that
// constraint's rather than one of the four the corpus writes a CREATE INDEX for, and doc.go's set
// lists neither it nor a name it could be derived from.
const schemaObjectNamesQuery = `
SELECT tablename FROM pg_tables WHERE schemaname = $1
UNION ALL
SELECT i.indexname FROM pg_indexes i WHERE i.schemaname = $1
   AND NOT EXISTS (SELECT 1 FROM information_schema.table_constraints c
                    WHERE c.table_schema = i.schemaname AND c.constraint_name = i.indexname)
ORDER BY 1`

func objectNamesIn(t *testing.T, pool *pgxpool.Pool, schemaName string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), schemaObjectNamesQuery, schemaName)
	if err != nil {
		t.Fatalf("read the object names in %s: %v", schemaName, err)
	}
	defer rows.Close()

	var found []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan an object name in %s: %v", schemaName, err)
		}
		found = append(found, name)
	}
	return found
}

// declaredObjectNames is every name doc.go says this package creates, read from that set rather than
// written out again here -- the DDL and the exported contract drift the moment either re-lists what
// the other declares.
func declaredObjectNames() []string {
	names := make([]string, 0, len(Objects))
	for _, object := range Objects {
		names = append(names, object.Name)
	}
	slices.Sort(names)
	return names
}

// TestEveryObjectLandsInTheConfiguredSchemaAndNoneInAnother is D1's routing case. Two schemas in
// one process, because a search_path set once on a connection and reused would put the second run's
// objects in the first run's schema -- and a suite migrating into one schema could not see it.
func TestEveryObjectLandsInTheConfiguredSchemaAndNoneInAnother(t *testing.T) {
	skipIfShort(t)

	corpus := embeddedCorpusOrFail(t)
	pool := emptySchemas(t, harnessSchema, firstTenantSchema, secondTenantSchema)

	for _, tenant := range []string{firstTenantSchema, secondTenantSchema} {
		mustMigrate(t, pool, tenant, corpus)

		if got := objectNamesIn(t, pool, tenant); !slices.Equal(got, declaredObjectNames()) {
			t.Errorf("%s holds %v, want every declared object %v", tenant, got, declaredObjectNames())
		}
	}
	// public as well as noty, because a run whose search_path never took effect creates its objects
	// in the connection's default path rather than in the configured schema.
	for _, untouched := range []string{harnessSchema, "public"} {
		if got := objectNamesIn(t, pool, untouched); len(got) != 0 {
			t.Errorf("%s holds %v, and no run named it; the search_path is either set on the "+
				"connection or set after the first statement", untouched, got)
		}
	}
}

// TestTheSearchPathDoesNotOutliveTheTransactionThatSetIt is the SET LOCAL half. The runner is handed
// a pinned *pgxpool.Conn rather than the pool, which is also the shape ADR-4 needs: Step 13 holds
// one connection for the whole bootstrap because the advisory lock is session-scoped, so the runner
// has to be able to run on it.
//
// A plain SET would leave this connection resolving unqualified names in the migrated schema for as
// long as the pool keeps it, which every later statement on it would silently inherit.
func TestTheSearchPathDoesNotOutliveTheTransactionThatSetIt(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, firstTenantSchema)
	pinned, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire the connection the runner is to be pinned to: %v", err)
	}
	defer pinned.Release()

	mustMigrate(t, pinned, firstTenantSchema, embeddedCorpusOrFail(t))

	if got := objectNamesIn(t, pool, firstTenantSchema); !slices.Equal(got, declaredObjectNames()) {
		t.Fatalf("%s holds %v after a run on a pinned connection, want %v; the assertion below "+
			"would otherwise pass against a run that set no path at all",
			firstTenantSchema, got, declaredObjectNames())
	}

	var resolving string
	if err := pinned.QueryRow(t.Context(), "SHOW search_path").Scan(&resolving); err != nil {
		t.Fatalf("read the search_path the connection is left with: %v", err)
	}
	if strings.Contains(resolving, firstTenantSchema) {
		t.Errorf("the connection that ran the migrations still resolves through %s, so the path was "+
			"set on the session rather than on each transaction", resolving)
	}
}
