//go:build integration

package reconcile

import (
	"fmt"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// plantedFunctionOIDQuery resolves a planted function by name because Catalog has no by-name
// function reader: production reaches a function through the trigger whose tgfoid names it, and the
// function-only fixtures here have no trigger. Only the OID is resolved outside Catalog; the marker
// every ownership case turns on is Catalog.ReadFunction's own.
const plantedFunctionOIDQuery = `
SELECT function.oid
FROM pg_catalog.pg_proc AS function
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = function.pronamespace
WHERE namespace.nspname = $1 AND function.proname = $2`

// catalogObjectOf reads a planted object's ownership marker back through Catalog, the reader apply
// and plan use. Issuing a second obj_description query here instead would prove the harness's SQL
// and leave readTriggerQuery and readFunctionQuery free to answer NULL for every object, with the
// whole ownership corpus still green. Reading through Catalog also travels pinSearchPath, which a
// bare pool query does not.
func catalogObjectOf(t *testing.T, pool *pgxpool.Pool, fixture ownershipCatalogFixture, kind string) CatalogObject {
	t.Helper()
	return CatalogObject{
		Kind:     kind,
		Identity: fmt.Sprintf("%s %q", kind, fixture.set.FunctionName),
		Marker:   plantedMarkerOf(t, pool, fixture, kind),
	}
}

// plantedMarkerOf is the marker Catalog reports for the planted object, nil when it carries none.
func plantedMarkerOf(t *testing.T, pool *pgxpool.Pool, fixture ownershipCatalogFixture, kind string) *string {
	t.Helper()
	switch kind {
	case "trigger":
		target := resolvedCatalogTarget(t, pool, quotedOwnershipTarget(t, fixture))
		return readCatalogTrigger(t, pool, target.OID, fixture.set.TriggerName).Marker
	case "function":
		return readCatalogFunction(t, pool, plantedFunctionOID(t, pool, fixture)).Marker
	}
	t.Fatalf("unknown catalog kind %q", kind)
	return nil
}

func quotedOwnershipTarget(t *testing.T, fixture ownershipCatalogFixture) string {
	t.Helper()
	qualified, fault := schema.Qualified("public", fixture.table)
	if fault != schema.IdentifierOK {
		t.Fatalf("target table %q is unusable: %s", fixture.table, fault)
	}
	return qualified
}

func plantedFunctionOID(t *testing.T, pool *pgxpool.Pool, fixture ownershipCatalogFixture) uint32 {
	t.Helper()
	var oid uint32
	if err := pool.QueryRow(
		t.Context(),
		plantedFunctionOIDQuery,
		harnessSchema,
		fixture.set.FunctionName,
	).Scan(&oid); err != nil {
		t.Fatalf("resolve planted function oid: %v", err)
	}
	return oid
}

// TestTheCatalogReadsBackTheMarkerEachKindWasWrittenWith is the assertion the whole ownership corpus
// rests on and none of its cases can make: DetermineOwnership is total over CatalogObject, so a
// catalog reader answering nil for every object turns every managed object into a marker-absent
// refusal and refuses the configuration on every run, with nothing red. The unmarked reading is
// taken first, so the marked one cannot be a constant this reader was always going to return.
func TestTheCatalogReadsBackTheMarkerEachKindWasWrittenWith(t *testing.T) {
	skipIfShort(t)
	for _, kind := range plantedOwnershipKinds {
		t.Run(
			kind, func(t *testing.T) {
				pool := freshDatabase(t)
				prepareOwnershipDatabase(t, pool)
				fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "catalog_marker_target")

				plantCatalogObject(t, pool, fixture, kind, nil)
				if unmarked := plantedMarkerOf(t, pool, fixture, kind); unmarked != nil {
					t.Fatalf("Catalog reported marker %q for an uncommented %s, want none", *unmarked, kind)
				}

				commentOnCatalogObject(t, pool, fixture, kind, &fixture.set.Marker)
				marked := plantedMarkerOf(t, pool, fixture, kind)
				if marked == nil || *marked != fixture.set.Marker {
					t.Fatalf("Catalog reported %s marker %v, want the planted %q", kind, marked, fixture.set.Marker)
				}
			},
		)
	}
}
