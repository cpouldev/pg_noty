//go:build integration

package reconcile

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogResolutionReturnsTheCatalogSpellingAndPrimaryKey(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.catalogcasetarget (id integer PRIMARY KEY, note text)")
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())

	got, found, err := catalog.ResolveTarget(t.Context(), "public.CatalogCaseTarget")
	if err != nil || !found {
		t.Fatalf("ResolveTarget() = %#v, %t, %v; want the case-folded catalog target", got, found, err)
	}
	if got.Schema != "public" || got.Table != "catalogcasetarget" {
		t.Errorf("catalog spelling = %q.%q, want public.catalogcasetarget", got.Schema, got.Table)
	}
	if !slices.Equal(got.Columns, []string{"id", "note"}) || !slices.Equal(got.PrimaryKeyColumns, []string{"id"}) {
		t.Errorf("target columns = %#v; primary key = %#v", got.Columns, got.PrimaryKeyColumns)
	}
	byOID, found, err := catalog.ResolveTargetOID(t.Context(), got.OID)
	if err != nil || !found || byOID.OID != got.OID || byOID.Schema != got.Schema || byOID.Table != got.Table ||
		!slices.Equal(byOID.Columns, got.Columns) || !slices.Equal(byOID.PrimaryKeyColumns, got.PrimaryKeyColumns) {
		t.Errorf("ResolveTargetOID() = %#v, %t, %v; want %#v", byOID, found, err, got)
	}
}

func TestCatalogReadsBothDefinitionsAndRelationLocks(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "catalog_read_target")
	plantCatalogObject(t, pool, fixture, "trigger", &fixture.set.Marker)
	target := resolvedCatalogTarget(t, pool, "public.catalog_read_target")
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())

	pair, found, err := catalog.ReadPair(t.Context(), target.OID, fixture.set.TriggerName)
	if err != nil || !found || pair.Trigger.Definition == "" || pair.Function.Definition == "" {
		t.Fatalf("ReadPair() = %#v, %t, %v; want both catalog definitions", pair, found, err)
	}
	mustExecOn(t, tx, "LOCK TABLE public.catalog_read_target IN SHARE ROW EXCLUSIVE MODE")
	locks, err := catalog.ReadLocks(t.Context(), target.OID)
	if err != nil || !hasCatalogLock(locks, "ShareRowExclusiveLock") {
		t.Fatalf("ReadLocks() = %#v, %v; want ShareRowExclusiveLock", locks, err)
	}
}

func TestCatalogDefinitionsAreByteIdenticalAcrossFiveFreshConnections(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "catalog_stable_target")
	plantCatalogObject(t, pool, fixture, "trigger", &fixture.set.Marker)
	target := resolvedCatalogTarget(t, pool, "public.catalog_stable_target")
	seedTrigger := readCatalogTrigger(t, pool, target.OID, fixture.set.TriggerName)
	seedFunction := readCatalogFunction(t, pool, seedTrigger.FunctionOID)
	seed := [2]string{seedTrigger.Definition, seedFunction.Definition}
	dsn := harnessConfig(t).Database.URL
	for _, functionFirst := range []bool{true, false, true, false, true} {
		fresh, err := pgxpool.New(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		got := readCatalogPair(t, fresh, target.OID, fixture.set.TriggerName, seedTrigger.FunctionOID, functionFirst)
		fresh.Close()
		if got != seed {
			t.Errorf("fresh reading = %#v, want byte-identical %#v", got, seed)
		}
	}
}

func TestCatalogRejectsThePrettyTriggerReading(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "catalog_pretty_target")
	plantCatalogObject(t, pool, fixture, "trigger", &fixture.set.Marker)
	target := resolvedCatalogTarget(t, pool, "public.catalog_pretty_target")
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	mustExecOn(t, tx, "SET LOCAL search_path TO noty, public")
	plain, pretty := outsideTriggerForms(t, tx, target.OID, fixture.set.TriggerName)
	reading, found, err := catalog.ReadTrigger(t.Context(), target.OID, fixture.set.TriggerName)
	t.Logf("PostgreSQL non-pretty trigger form: %s", plain)
	t.Logf("PostgreSQL pretty trigger form: %s", pretty)
	if err != nil || !found || reading.Definition == pretty || plain == pretty {
		t.Fatalf("non-pretty=%q; pretty=%q; catalog=%#v, found=%t, err=%v", plain, pretty, reading, found, err)
	}
	if strings.Contains(pretty, "public.") || !strings.Contains(reading.Definition, "public.") {
		t.Fatalf("pretty=%q; pinned non-pretty=%q; want only the pinned form to qualify the table", pretty, reading.Definition)
	}
}

func TestCatalogResolutionRejectsANonTable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE VIEW public.catalog_view_target AS SELECT 1 AS id")
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	if target, found, err := catalog.ResolveTarget(t.Context(), "public.catalog_view_target"); err != nil || found {
		t.Fatalf("ResolveTarget(view) = %#v, %t, %v; want no table", target, found, err)
	}
}

func openCatalogForTest(t *testing.T, pool *pgxpool.Pool) (Catalog, pgx.Tx) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return NewCatalog(tx), tx
}

func resolvedCatalogTarget(t *testing.T, pool *pgxpool.Pool, name string) TargetReading {
	t.Helper()
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	target, found, err := catalog.ResolveTarget(t.Context(), name)
	if err != nil || !found {
		t.Fatalf("resolve %s: %#v, %t, %v", name, target, found, err)
	}
	return target
}

func readCatalogPair(t *testing.T, pool *pgxpool.Pool, oid uint32, trigger string, functionOID uint32, functionFirst bool) [2]string {
	t.Helper()
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	if functionFirst {
		functionReading := readCatalogFunctionWith(t, catalog, functionOID)
		triggerReading := readCatalogTriggerWith(t, catalog, oid, trigger)
		return [2]string{triggerReading.Definition, functionReading.Definition}
	}
	triggerReading := readCatalogTriggerWith(t, catalog, oid, trigger)
	functionReading := readCatalogFunctionWith(t, catalog, triggerReading.FunctionOID)
	return [2]string{triggerReading.Definition, functionReading.Definition}
}

func readCatalogTrigger(t *testing.T, pool *pgxpool.Pool, oid uint32, trigger string) TriggerReading {
	t.Helper()
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	return readCatalogTriggerWith(t, catalog, oid, trigger)
}

func readCatalogTriggerWith(t *testing.T, catalog Catalog, oid uint32, trigger string) TriggerReading {
	t.Helper()
	reading, found, err := catalog.ReadTrigger(t.Context(), oid, trigger)
	if err != nil || !found {
		t.Fatalf("read trigger: %#v, %t, %v", reading, found, err)
	}
	return reading
}

func readCatalogFunction(t *testing.T, pool *pgxpool.Pool, oid uint32) FunctionReading {
	t.Helper()
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	return readCatalogFunctionWith(t, catalog, oid)
}

func readCatalogFunctionWith(t *testing.T, catalog Catalog, oid uint32) FunctionReading {
	t.Helper()
	reading, found, err := catalog.ReadFunction(t.Context(), oid)
	if err != nil || !found {
		t.Fatalf("read function: %#v, %t, %v", reading, found, err)
	}
	return reading
}

func outsideTriggerForms(t *testing.T, tx pgx.Tx, oid uint32, trigger string) (string, string) {
	t.Helper()
	var plain, pretty string
	if err := tx.QueryRow(t.Context(), "SELECT pg_get_triggerdef(oid), pg_get_triggerdef(oid, true) FROM pg_trigger WHERE tgrelid=$1 AND tgname=$2", oid, trigger).Scan(&plain, &pretty); err != nil {
		t.Fatal(err)
	}
	return plain, pretty
}

func hasCatalogLock(locks []LockReading, mode string) bool {
	return slices.ContainsFunc(locks, func(lock LockReading) bool { return lock.Granted && lock.Mode == mode })
}
