//go:build integration

package reconcile

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rules: count-the-population-a-vacuity-guard-guards; a changing-statement count is taken at the server.

func TestNewListenerCreatesEveryRecordedObjectFromReadback(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "matrix_create_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, state text)")
	listener := matrixListener("matrix_create_target", "insert", "update", "delete")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || result.Verdict != VerdictClean || result.Statements == 0 {
		t.Fatalf("create apply = %+v, %v; want changed clean run", result, err)
	}
	assertRegistryColumns(t, pool)
	recorded, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	spec, specErr := deriveListenerSpec(listener.Trigger)
	var stored config.TriggerSpec
	storedErr := json.Unmarshal(recorded.Listener.Spec, &stored)
	storedSpec, storedSpecErr := deriveListenerSpec(stored)
	targetReading := resolvedCatalogTarget(t, pool, target)
	if err != nil || specErr != nil || storedErr != nil || storedSpecErr != nil || recorded.Listener.Name != listener.Name || !recorded.Listener.Enabled || recorded.Listener.TargetTable != target || recorded.Listener.TargetOID != targetReading.OID || storedSpec.Hash != spec.Hash || recorded.Listener.SpecHash != spec.Hash || recorded.Listener.AppliedAt.IsZero() {
		t.Fatalf("listener registry = %+v, %v; want every stored listener column", recorded.Listener, err)
	}
	if len(recorded.Triggers) != len(listener.Trigger.Operations) {
		t.Fatalf("trigger registry rows = %d, want %d", len(recorded.Triggers), len(listener.Trigger.Operations))
	}
	for _, trigger := range recorded.Triggers {
		pair := readCatalogPair(t, pool, targetReading.OID, trigger.TriggerName, 0, false)
		catalog := readCatalogTrigger(t, pool, targetReading.OID, trigger.TriggerName)
		function := readCatalogFunction(t, pool, catalog.FunctionOID)
		if trigger.Operation == "" || trigger.TriggerName == "" || trigger.FunctionName == "" || catalog.Marker == nil || function.Marker == nil || trigger.DDLHash != fingerprint(
			pair[0],
			pair[1],
		) {
			t.Fatalf(
				"operation %q registry/readback = %+v, trigger=%+v function=%+v",
				trigger.Operation,
				trigger,
				catalog,
				function,
			)
		}
	}
}

func TestMatchingListenerDoesNotRewriteAppliedAt(t *testing.T) {
	skipIfShort(t)
	pool, statements := tracedDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "matrix_noop_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := matrixListener("matrix_noop_target", "insert")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	before, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, "SELECT pg_sleep(0.001)")
	start := len(statements.values())
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	changes := changingStatements(statements.values()[start:])
	after, readErr := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || readErr != nil || result.Statements != 0 || len(changes) != 0 || !after.Listener.AppliedAt.Equal(before.Listener.AppliedAt) {
		t.Fatalf(
			"matching apply = %+v, %v; server changes=%q; registry timestamps %v -> %v, %v",
			result,
			err,
			changes,
			before.Listener.AppliedAt,
			after.Listener.AppliedAt,
			readErr,
		)
	}
}

func matrixListener(table string, operations ...string) config.Listener {
	listener := listenerForTarget("public." + table)
	listener.Enabled = true
	listener.Trigger.Operations = make(config.Operations, len(operations))
	for index, operation := range operations {
		listener.Trigger.Operations[index] = config.Operation{Kind: operation}
	}
	return listener
}

func changingStatements(statements []string) []string {
	var changes []string
	for _, statement := range statements {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(upper, "CREATE ") || strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(
			upper,
			"ALTER ",
		) || strings.HasPrefix(upper, "COMMENT ") || strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(
			upper,
			"UPDATE ",
		) || strings.HasPrefix(upper, "DELETE ") {
			changes = append(changes, statement)
		}
	}
	return changes
}

var registryColumnContracts = []struct {
	table string
	want  []string
}{
	{"listeners", []string{"name", "spec", "spec_hash", "target_table", "target_oid", "enabled", "applied_at"}},
	{"listener_triggers", []string{"listener", "operation", "trigger_name", "function_name", "ddl_hash"}},
}

func assertRegistryColumns(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, contract := range registryColumnContracts {
		rows, err := pool.Query(
			t.Context(),
			"SELECT column_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position",
			harnessSchema,
			contract.table,
		)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				t.Fatal(err)
			}
			got = append(got, column)
		}
		if err := rows.Err(); err != nil || !slices.Equal(got, contract.want) {
			t.Fatalf("%s columns = %v, %v; want contract %v", contract.table, got, err, contract.want)
		}
		rows.Close()
	}
}
