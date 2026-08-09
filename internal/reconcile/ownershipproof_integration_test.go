//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

type plantedCatalogCase struct {
	name, markerInstance, markerListener string
	want                                 Disagreement
	hasMarker, registryPresent           bool
}

// Each row satisfies the other three ownership facts: a present registry row for the expected
// pair, a readable marker naming the expected instance, and the collision derived by source.Generate.
var plantedCatalogCases = []plantedCatalogCase{
	// Only the catalog comment is absent; the registry row and expected pair remain valid.
	{name: "marker absent", want: DisagreementMarkerAbsent, registryPresent: true},
	// Only the marker's instance differs; its listener, operation, and registry row still agree.
	{
		name: "foreign instance", markerInstance: "bravo", want: DisagreementForeignInstance, hasMarker: true,
		registryPresent: true,
	},
	// Only the marker pair differs; its instance and the registry row remain the expected ones.
	{
		name: "marker names another pair", markerListener: "payments", want: DisagreementAnotherPair, hasMarker: true,
		registryPresent: true,
	},
	// Only the registry-row presence differs; this is otherwise this instance's exact generated marker.
	{name: "no registry row", want: DisagreementNoRegistryRow, hasMarker: true},
}

type ownershipCatalogFixture struct {
	instance, listener, table string
	set                       source.ObjectSet
}

func TestTheFourPlantedTriggerDisagreementsAndTheirPositiveControl(t *testing.T) {
	skipIfShort(t)
	// Each row varies one proof fact while keeping the other facts real and
	// agreeing.
	for _, testCase := range plantedCatalogCases {
		t.Run(
			testCase.name, func(t *testing.T) {
				pool := freshDatabase(t)
				prepareOwnershipDatabase(t, pool)
				fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_target")
				plantCatalogObject(t, pool, fixture, "trigger", markerForPlantedCase(t, fixture, testCase))

				found := catalogObjectOf(t, pool, fixture, "trigger")
				got := DetermineOwnership(registryFor(fixture, testCase.registryPresent), found, fixture.instance)
				if got.Owned || got.Disagreement != testCase.want {
					t.Fatalf("DetermineOwnership() = %+v, want refusal %q", got, testCase.want)
				}
				// The matching object is the nearest input that must survive each refusal
				// guard.
				commentOnCatalogObject(t, pool, fixture, "trigger", &fixture.set.Marker)
				owned := DetermineOwnership(
					registryFor(fixture, true),
					catalogObjectOf(t, pool, fixture, "trigger"),
					fixture.instance,
				)
				if !owned.Owned || owned.Disagreement != "" {
					t.Fatalf("all-agreeing ownership = %+v, want owned", owned)
				}
			},
		)
	}
}

func newOwnershipCatalogFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	instance, listener, table string,
) ownershipCatalogFixture {
	t.Helper()
	quoted, fault := schema.Quoted(table)
	if fault != schema.IdentifierOK {
		t.Fatalf("target table %q is unusable: %s", table, fault)
	}
	mustExecOn(t, pool, "CREATE TABLE public."+quoted+" (id int, status text)")
	return generatedOwnershipCatalogFixture(t, instance, listener, table)
}

func generatedOwnershipCatalogFixture(t *testing.T, instance, listener, table string) ownershipCatalogFixture {
	t.Helper()
	// Derive the exact collision name from source.Generate rather than transcribing
	// it.
	sets, err := source.Generate(
		source.Request{
			Instance: instance, ServiceSchema: harnessSchema,
			Listener: config.Listener{
				Name: listener, Trigger: config.TriggerSpec{
					Operations: config.Operations{{Kind: "insert"}}, Payload: config.Payload{Mode: "full"},
				},
			},
			Target: source.Target{Schema: "public", Table: table, PrimaryKeyColumns: []string{"id"}},
		},
	)
	if err != nil || len(sets) != 1 {
		t.Fatalf("source.Generate() = %d sets, %v; want one generated collision name", len(sets), err)
	}
	return ownershipCatalogFixture{instance: instance, listener: listener, table: table, set: sets[0]}
}

func markerForPlantedCase(t *testing.T, fixture ownershipCatalogFixture, testCase plantedCatalogCase) *string {
	t.Helper()
	if !testCase.hasMarker {
		return nil
	}
	instance, listener := fixture.instance, fixture.listener
	if testCase.markerInstance != "" {
		instance = testCase.markerInstance
	}
	if testCase.markerListener != "" {
		listener = testCase.markerListener
	}
	marker := generatedOwnershipMarker(t, instance, listener, fixture.table)
	return &marker
}

func generatedOwnershipMarker(t *testing.T, instance, listener, table string) string {
	t.Helper()
	fixture := generatedOwnershipCatalogFixture(t, instance, listener, table)
	return fixture.set.Marker
}

func plantCatalogObject(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture ownershipCatalogFixture,
	kind string,
	marker *string,
) {
	t.Helper()
	mustExecOn(t, pool, fixture.set.CreateFunction)
	if kind == "function" {
		commentOnCatalogObject(t, pool, fixture, kind, marker)
		return
	}
	if kind != "trigger" {
		t.Fatalf("unknown catalog kind %q", kind)
	}
	mustExecOn(t, pool, fixture.set.CreateTrigger)
	commentOnCatalogObject(t, pool, fixture, kind, marker)
}

func commentOnCatalogObject(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture ownershipCatalogFixture,
	kind string,
	marker *string,
) {
	t.Helper()
	function := quotedOwnershipIdentifier(t, fixture.set.FunctionName)
	if kind == "function" {
		commentCatalogObject(t, pool, "COMMENT ON FUNCTION \""+harnessSchema+"\"."+function+"() IS ", marker)
		return
	}
	if kind != "trigger" {
		t.Fatalf("unknown catalog kind %q", kind)
	}
	table, trigger := quotedOwnershipIdentifier(t, fixture.table), quotedOwnershipIdentifier(t, fixture.set.TriggerName)
	commentCatalogObject(t, pool, "COMMENT ON TRIGGER "+trigger+" ON \"public\"."+table+" IS ", marker)
}

func quotedOwnershipIdentifier(t *testing.T, name string) string {
	t.Helper()
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		t.Fatalf("generated identifier %q is unusable: %s", name, fault)
	}
	return quoted
}

func commentCatalogObject(t *testing.T, pool *pgxpool.Pool, statement string, marker *string) {
	t.Helper()
	if marker != nil {
		mustExecOn(t, pool, statement+harnessLiteral(*marker))
	}
}

func registryFor(fixture ownershipCatalogFixture, present bool) RegistryRow {
	return RegistryRow{Present: present, Listener: fixture.listener, Operation: fixture.set.Operation}
}
