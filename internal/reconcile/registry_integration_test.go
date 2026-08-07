//go:build integration

package reconcile

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestRegistryRoundTripUsesServerProvenanceAndCascade(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target, _ := schema.Qualified("public", "registry_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id int PRIMARY KEY)")
	var oid uint32
	if err := pool.QueryRow(t.Context(), "SELECT $1::regclass::oid", target).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	var catalogSchema, catalogTable string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT namespace.nspname, relation.relname FROM pg_catalog.pg_class AS relation JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace WHERE relation.oid = $1",
		oid,
	).Scan(&catalogSchema, &catalogTable); err != nil {
		t.Fatal(err)
	}
	catalogTarget, fault := schema.Qualified(catalogSchema, catalogTable)
	if fault != schema.IdentifierOK {
		t.Fatalf("catalog target name is unusable: %s", fault)
	}
	var before time.Time
	if err := pool.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	written, err := writeRegistry(
		t.Context(),
		tx,
		harnessSchema,
		registryListener{
			Name: "orders", Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: catalogTarget,
			TargetOID: oid, Enabled: true,
		},
		[]registryTrigger{
			{
				Operation: "insert", TriggerName: "orders_ins", FunctionName: "orders_ins_fn", DDLHash: "ddl",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err := pool.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if written.TargetTable != catalogTarget || written.AppliedAt.Before(before) || written.AppliedAt.After(after) {
		t.Fatalf("written registry = %+v, want catalog spelling and server applied_at", written)
	}
	rows, err := readRegistry(t.Context(), pool, harnessSchema, "orders")
	var spec map[string]string
	if json.Unmarshal(rows.Listener.Spec, &spec) != nil || !reflect.DeepEqual(
		spec,
		map[string]string{"mode": "full"},
	) || err != nil || len(rows.Triggers) != 1 || rows.Listener.Name != "orders" || rows.Listener.SpecHash != "hash" || rows.Listener.TargetTable != catalogTarget || rows.Listener.TargetOID != oid || !rows.Listener.Enabled || rows.Listener.AppliedAt.IsZero() || rows.Triggers[0] != (registryTrigger{
		Operation: "insert", TriggerName: "orders_ins", FunctionName: "orders_ins_fn", DDLHash: "ddl",
	}) {
		t.Fatalf("read registry = %+v, %v", rows, err)
	}
	tx, err = pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := writeRegistry(
		t.Context(),
		tx,
		harnessSchema,
		registryListener{
			Name: "orders", Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: catalogTarget,
			TargetOID: oid, Enabled: true,
		},
		[]registryTrigger{
			{
				Operation: "insert", TriggerName: "orders_ins", FunctionName: "orders_ins_fn", DDLHash: "ddl",
			},
		},
	)
	if err != nil || !unchanged.AppliedAt.Equal(written.AppliedAt) {
		t.Fatalf("unchanged registry write = %+v, %v; want original applied_at", unchanged, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	mustExecOn(t, pool, "SELECT pg_sleep(0.001)")
	tx, err = pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := writeRegistry(
		t.Context(),
		tx,
		harnessSchema,
		registryListener{
			Name: "orders", Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: catalogTarget,
			TargetOID: oid, Enabled: true,
		},
		[]registryTrigger{
			{
				Operation: "insert", TriggerName: "orders_ins", FunctionName: "orders_ins_fn", DDLHash: "changed",
			},
		},
	)
	if err != nil || !changed.AppliedAt.After(unchanged.AppliedAt) {
		t.Fatalf("trigger change registry write = %+v, %v; want later applied_at", changed, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	listeners, _ := schema.Qualified(harnessSchema, schema.TableListeners)
	triggers, _ := schema.Qualified(harnessSchema, schema.TableListenerTriggers)
	mustExecOn(t, pool, "DELETE FROM "+listeners+" WHERE name=$1", "orders")
	if got := countOn(t, pool, "SELECT count(*) FROM "+listeners+" WHERE name=$1", "orders"); got != 0 {
		t.Fatalf("delete left %d listener rows", got)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM "+triggers+" WHERE listener=$1", "orders"); got != 0 {
		t.Fatalf("cascade left %d trigger rows", got)
	}
}

func TestRegistryWriteBelongsToTheOuterTransaction(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target, _ := schema.Qualified("public", "registry_rollback_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id int PRIMARY KEY)")
	var oid uint32
	if err := pool.QueryRow(t.Context(), "SELECT $1::regclass::oid", target).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeRegistry(
		t.Context(),
		tx,
		harnessSchema,
		registryListener{
			Name: "rolled_back", Spec: []byte(`{"mode":"full"}`), SpecHash: "hash", TargetTable: target, TargetOID: oid,
			Enabled: true,
		},
		[]registryTrigger{
			{
				Operation: "insert", TriggerName: "rolled_back_ins", FunctionName: "rolled_back_ins_fn", DDLHash: "ddl",
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	listeners, _ := schema.Qualified(harnessSchema, schema.TableListeners)
	triggers, _ := schema.Qualified(harnessSchema, schema.TableListenerTriggers)
	// Counted inside the transaction first. The two zeros after the rollback are equally true of a
	// writeRegistry that wrote nothing at all, so without these the assertion measures no
	// rollback.
	if got := countOn(t, tx, "SELECT count(*) FROM "+listeners+" WHERE name=$1", "rolled_back"); got != 1 {
		t.Fatalf("inside the transaction the write left %d listener rows, want 1", got)
	}
	if got := countOn(t, tx, "SELECT count(*) FROM "+triggers+" WHERE listener=$1", "rolled_back"); got != 1 {
		t.Fatalf("inside the transaction the write left %d trigger rows, want 1", got)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM "+listeners+" WHERE name=$1", "rolled_back"); got != 0 {
		t.Fatalf("outer rollback left %d listener rows", got)
	}
	if got := countOn(t, pool, "SELECT count(*) FROM "+triggers+" WHERE listener=$1", "rolled_back"); got != 0 {
		t.Fatalf("outer rollback left %d trigger rows", got)
	}
}

func TestRegistryReadsEveryStoredListener(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target, _ := schema.Qualified("public", "registry_all_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id int PRIMARY KEY)")
	var oid uint32
	if err := pool.QueryRow(t.Context(), "SELECT $1::regclass::oid", target).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"orders", "retired"} {
		listener := registryListener{
			Name: name, Spec: []byte(`{}`), SpecHash: name, TargetTable: target, TargetOID: oid, Enabled: true,
		}
		triggers := []registryTrigger{
			{
				Operation: "insert", TriggerName: name + "_insert", FunctionName: name + "_fn", DDLHash: name,
			},
		}
		if _, err := writeRegistry(t.Context(), tx, harnessSchema, listener, triggers); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	readings, err := readRegistries(t.Context(), pool, harnessSchema)
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 2 || readings[0].Listener.Name != "orders" || readings[1].Listener.Name != "retired" || len(readings[0].Triggers) != 1 || len(readings[1].Triggers) != 1 {
		t.Fatalf("whole registry reading = %+v, want both listeners with their operations", readings)
	}
	tx, err = pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteRegistry(t.Context(), tx, harnessSchema, "retired"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rows, err := readRegistries(
		t.Context(),
		pool,
		harnessSchema,
	); err != nil || len(rows) != 1 || rows[0].Listener.Name != "orders" {
		t.Fatalf("records after delete = %+v, %v", rows, err)
	}
}
