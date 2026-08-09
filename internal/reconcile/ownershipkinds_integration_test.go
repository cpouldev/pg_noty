//go:build integration

package reconcile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

const plantedOwnershipKindCount, plantedOwnershipGridSize = 2, 4 * 2

var plantedOwnershipKinds = []string{"trigger", "function"}

func TestEveryPlantedDisagreementIsRefusedForBothCatalogObjectKinds(t *testing.T) {
	skipIfShort(t)
	// The case table supplies the four proof clauses and this value supplies both catalog kinds; cross
	// them rather than transcribing rows.
	if got, want := len(plantedCatalogCases), 4; got != want {
		t.Fatalf("planted disagreement cases = %d, want %d", got, want)
	}
	if got, want := len(plantedOwnershipKinds), plantedOwnershipKindCount; got != want {
		t.Fatalf("catalog object kinds = %d, want %d", got, want)
	}
	refused := 0
	for _, kind := range plantedOwnershipKinds {
		for _, testCase := range plantedCatalogCases {
			t.Run(
				kind+"/"+testCase.name, func(t *testing.T) {
					pool := freshDatabase(t)
					prepareOwnershipDatabase(t, pool)
					fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "ownership_kinds_target")
					plantCatalogObject(t, pool, fixture, kind, markerForPlantedCase(t, fixture, testCase))
					got := DetermineOwnership(
						registryFor(fixture, testCase.registryPresent),
						catalogObjectOf(t, pool, fixture, kind),
						fixture.instance,
					)
					if got.Owned || got.Disagreement != testCase.want {
						t.Fatalf(
							"DetermineOwnership(%s, %s) = %+v, want refusal %q",
							kind,
							testCase.name,
							got,
							testCase.want,
						)
					}
					refused++
				},
			)
		}
	}
	// The cells are counted as they refuse rather than multiplied out: len(cases)*len(kinds) equals
	// plantedOwnershipGridSize by the two pins above whatever the loops did, so it cannot report a
	// grid that stopped running.
	if refused != plantedOwnershipGridSize {
		t.Fatalf("the grid refused %d cells, want 4 disagreements × 2 kinds = %d", refused, plantedOwnershipGridSize)
	}
}

func prepareOwnershipDatabase(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	quoted, fault := schema.Quoted(harnessSchema)
	if fault != schema.IdentifierOK {
		t.Fatalf("harness schema %q is unusable: %s", harnessSchema, fault)
	}
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer connection.Release()
	// The corpus is unqualified, so use its configured service schema rather than public.
	mustExecOn(t, connection, "CREATE SCHEMA "+quoted)
	mustExecOn(t, connection, "SET search_path TO "+quoted+", public")
	defer mustExecOn(t, connection, "SET search_path TO DEFAULT")
	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "*.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("read internal/schema migration corpus: %v", err)
	}
	slices.Sort(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		mustExecOn(t, connection, string(data))
	}
}
