//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: assert-a-position-per-element-not-as-a-set; set-a-provenance-flag-from-the-provenance.
func TestRegistryAndCatalogDescribeEachOtherInBothDirections(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "registry_truth")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, state text)")
	listener := matrixListener("registry_truth", "insert", "update", "delete")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	stored, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	targetReading := resolvedCatalogTarget(t, pool, target)
	if stored.Listener.TargetTable != target || stored.Listener.TargetOID != targetReading.OID || !stored.Listener.Enabled {
		t.Fatalf("registry provenance=%+v, want catalog target and enabled configuration", stored.Listener)
	}
	if len(stored.Triggers) != len(listener.Trigger.Operations) {
		t.Fatalf("registry trigger rows=%d, want %d", len(stored.Triggers), len(listener.Trigger.Operations))
	}
	assertRegistryRowsMatchTheCatalog(t, pool, targetReading.OID, stored)
	assertAllOwnedCatalogObjectsHaveRows(t, pool, targetReading.OID, stored)
	assertAllOwnedCatalogFunctionsHaveRows(t, pool, recordedFunctionNames(stored), len(stored.Triggers))
}

// recordedFunctionNames is the function half of a listener's registry rows, keyed for the scan above.
func recordedFunctionNames(stored registryReading) map[string]bool {
	names := make(map[string]bool, len(stored.Triggers))
	for _, trigger := range stored.Triggers {
		names[trigger.FunctionName] = true
	}
	return names
}

// assertRegistryRowsMatchTheCatalog is the first direction of registry truth: every
// listener_triggers row names an object that exists in the catalog, with a matching ddl_hash. It is
// shared with the five-kind run in registryactionkinds_integration_test.go, which asserts the same
// direction over a tree four listeners wider.
func assertRegistryRowsMatchTheCatalog(t *testing.T, pool *pgxpool.Pool, oid uint32, stored registryReading) {
	t.Helper()
	for _, trigger := range stored.Triggers {
		pair := readCatalogPair(t, pool, oid, trigger.TriggerName, 0, false)
		if got := fingerprint(pair[0], pair[1]); got != trigger.DDLHash {
			t.Errorf("registry row %q hashes %q, catalog returns %q", trigger.Operation, trigger.DDLHash, got)
		}
	}
}

// assertAllOwnedCatalogFunctionsHaveRows is the other object kind of the same direction. The trigger
// half below cannot stand in for it: registry truth says "every trigger **and function** this instance
// owns in the catalog has a row", and a function whose trigger was recorded while its own row was not
// is exactly the orphan this direction exists to find -- pg_trigger cannot see it. The population is
// read from the marker comment through schema.MarkerPrefix, the one authority, so it is the functions
// *this instance* owns rather than every function in the schema. want is the caller's registry count,
// making the scan's own emptiness a failure rather than a pass.
func assertAllOwnedCatalogFunctionsHaveRows(t *testing.T, pool *pgxpool.Pool, recorded map[string]bool, want int) {
	t.Helper()
	rows, err := pool.Query(
		t.Context(),
		"SELECT function.proname FROM pg_catalog.pg_proc AS function "+
			"JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = function.pronamespace "+
			"WHERE namespace.nspname = $1 AND obj_description(function.oid, 'pg_proc') LIKE $2 "+
			"ORDER BY function.proname", harnessSchema, schema.MarkerPrefix+harnessSchema+":%",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	scanned := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan owned catalog function name: %v", err)
		}
		scanned++
		if !recorded[name] {
			t.Errorf("catalog function %q has no registry row; registry records %v", name, recorded)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if scanned != want {
		t.Fatalf(
			"read %d owned catalog functions against %d registry rows; this direction asserts "+
				"inside the row loop, so a query returning nothing would otherwise pass it vacuously",
			scanned, want,
		)
	}
}

// assertAllOwnedCatalogObjectsHaveRows is the second direction for triggers: no catalog object this package owns
// is unrecorded. The assertion lives inside the row loop, so over an empty result set the body never runs and the direction
// is satisfied by scanning nothing -- adding `AND false` to the query left the suite green. The scanned rows are therefore
// counted, and the count is asserted against the registry's own row count, which the caller has already pinned against the
// configuration's operation count. The row-count agreement is asserted in both directions only when this half can fail.
func assertAllOwnedCatalogObjectsHaveRows(t *testing.T, pool *pgxpool.Pool, oid uint32, stored registryReading) {
	t.Helper()
	rows, err := pool.Query(
		t.Context(),
		"SELECT tgname FROM pg_trigger WHERE tgrelid=$1 AND NOT tgisinternal ORDER BY tgname",
		oid,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for _, row := range stored.Triggers {
		seen[row.TriggerName] = true
	}

	scanned := 0
	for rows.Next() {
		var name string
		// A scan failure is its own reason and gets its own message: reporting it as a missing
		// registry row names an unread variable.
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan owned catalog trigger name: %v", err)
		}
		scanned++
		if !seen[name] {
			t.Errorf("catalog trigger %q has no registry row; registry records %v", name, seen)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if scanned != len(stored.Triggers) {
		t.Fatalf(
			"read %d owned catalog triggers against %d registry rows; this direction asserts "+
				"inside the row loop, so a query returning nothing would otherwise pass it vacuously",
			scanned, len(stored.Triggers),
		)
	}
}
