//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPlantedUnmarkedTriggerIsRefusedAndSurvivesByteIdentical(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_survival_target")
	markerAbsent := plantedCatalogCases[0]
	plantCatalogObject(t, pool, fixture, "trigger", markerForPlantedCase(t, fixture, markerAbsent))

	before := readPinnedDefinition(t, pool, fixture, "trigger")
	got := DetermineOwnership(registryFor(fixture, true), catalogObjectOf(t, pool, fixture, "trigger"), fixture.instance)
	if got.Owned || got.Disagreement != DisagreementMarkerAbsent {
		t.Fatalf("unmarked generated-name trigger ownership = %+v, want marker-absent refusal", got)
	}
	after := readPinnedDefinition(t, pool, fixture, "trigger")
	assertDefinitionPreserved(t, before, after)

	// This closes the catalog half: the exact generated-name unmarked trigger is refused and byte-identical.
	// TestAnUnmarkedCollisionLeavesBothListenersUnchanged in collisionapply_integration_test.go must
	// additionally prove Apply performs neither its destructive action on the other listener nor any database change.
}

func TestEveryPlantedObjectSurvivesByteIdenticalForBothKinds(t *testing.T) {
	skipIfShort(t)
	for _, kind := range plantedOwnershipKinds {
		for _, testCase := range plantedCatalogCases {
			t.Run(kind+"/"+testCase.name, func(t *testing.T) {
				pool := freshDatabase(t)
				prepareOwnershipDatabase(t, pool)
				fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_identity_target")
				plantCatalogObject(t, pool, fixture, kind, markerForPlantedCase(t, fixture, testCase))
				before := readPinnedDefinition(t, pool, fixture, kind)
				got := DetermineOwnership(registryFor(fixture, testCase.registryPresent), catalogObjectOf(t, pool, fixture, kind), fixture.instance)
				if got.Owned || got.Disagreement != testCase.want {
					t.Fatalf("ownership = %+v, want refusal %q", got, testCase.want)
				}
				assertDefinitionPreserved(t, before, readPinnedDefinition(t, pool, fixture, kind))
			})
		}
	}
}

func TestByteIdentityDetectsAnEquivalentTriggerRecreation(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_recreated_target")
	plantCatalogObject(t, pool, fixture, "trigger", nil)
	before := readPinnedDefinition(t, pool, fixture, "trigger")

	trigger, table := quotedOwnershipIdentifier(t, fixture.set.TriggerName), quotedOwnershipIdentifier(t, fixture.table)
	mustExecOn(t, pool, "DROP TRIGGER "+trigger+" ON \"public\"."+table)
	recreated := strings.Replace(fixture.set.CreateTrigger, " FOR EACH ROW EXECUTE FUNCTION", " FOR EACH ROW WHEN (NEW.id IS NOT NULL OR NEW.id IS NULL) EXECUTE FUNCTION", 1)
	if recreated == fixture.set.CreateTrigger {
		t.Fatal("generated trigger lacks the FOR EACH ROW seam for the equivalent-recreate control")
	}
	mustExecOn(t, pool, recreated)

	// The real comparison uses definitionDifference; this control must make that exact predicate see
	// the equivalent drop-and-recreate, not a second assertion of the same idea.
	if difference := definitionDifference(before, readPinnedDefinition(t, pool, fixture, "trigger")); difference == "" {
		t.Fatal("equivalent trigger recreation left no byte difference for the survival assertion to detect")
	}
}

func TestASecondInstancesCollisionDoesNotAffectOurOtherTable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	ours := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_shared_target")
	// The naming rule is live here: the marker carries the instance while the derived object name does not.
	others := generatedOwnershipCatalogFixture(t, "bravo", "orders", ours.table)
	if ours.set.TriggerName != others.set.TriggerName {
		t.Fatalf("instances generated %q and %q; the derived name must omit the instance", ours.set.TriggerName, others.set.TriggerName)
	}
	plantCatalogObject(t, pool, others, "trigger", &others.set.Marker)
	foreign := DetermineOwnership(registryFor(ours, true), catalogObjectOf(t, pool, others, "trigger"), ours.instance)
	if foreign.Owned || foreign.Disagreement != DisagreementForeignInstance {
		t.Fatalf("second instance's fully marked collision = %+v, want foreign-instance refusal", foreign)
	}

	sibling := newOwnershipCatalogFixture(t, pool, "alpha", "receipts", "ownership_sibling_target")
	plantCatalogObject(t, pool, sibling, "trigger", &sibling.set.Marker)
	before := readPinnedDefinition(t, pool, sibling, "trigger")
	owned := DetermineOwnership(registryFor(sibling, true), catalogObjectOf(t, pool, sibling, "trigger"), sibling.instance)
	if !owned.Owned {
		t.Fatalf("our other-table object = %+v, want owned", owned)
	}
	assertDefinitionPreserved(t, before, readPinnedDefinition(t, pool, sibling, "trigger"))
}

func readPinnedDefinition(t *testing.T, pool *pgxpool.Pool, fixture ownershipCatalogFixture, kind string) string {
	t.Helper()
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire comparison connection: %v", err)
	}
	defer connection.Release()
	tx, err := connection.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin comparison read: %v", err)
	}
	defer tx.Rollback(t.Context())
	// M3's asymmetry is session state, so every before/after read pins search_path empty.
	mustExecOn(t, tx, "SET LOCAL search_path = ''")
	var definition string
	if kind == "trigger" {
		err = tx.QueryRow(t.Context(), "SELECT pg_get_triggerdef(t.oid) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=$1 AND t.tgname=$2", fixture.table, fixture.set.TriggerName).Scan(&definition)
	} else if kind == "function" {
		err = tx.QueryRow(t.Context(), "SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1 AND p.proname=$2", harnessSchema, fixture.set.FunctionName).Scan(&definition)
	} else {
		t.Fatalf("unknown catalog kind %q", kind)
	}
	if err != nil {
		t.Fatalf("read pinned %s definition: %v", kind, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit comparison read: %v", err)
	}
	return definition
}

func definitionDifference(before, after string) string {
	if before == after {
		return ""
	}
	return "catalog definition changed"
}

func assertDefinitionPreserved(t *testing.T, before, after string) {
	t.Helper()
	if difference := definitionDifference(before, after); difference != "" {
		t.Fatalf("%s: before %q, after %q", difference, before, after)
	}
}
