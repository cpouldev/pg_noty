//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTheFiveTargetOutcomesAreClosedAndEachIsReachedOnItsOwn(t *testing.T) {
	// test-both-sides-of-an-exclusion-guard.md and repair-the-class-not-the-reproduction.md require
	// the healthy rename control and the separate dropped-and-recreated row below.
	skipIfShort(t)
	for _, testCase := range targetOutcomeCases {
		t.Run(
			testCase.name, func(t *testing.T) {
				pool := freshDatabase(t)
				listener, recorded := targetOutcomeFixture(t, pool, testCase.name)
				catalog, tx := openCatalogForTest(t, pool)
				defer tx.Rollback(t.Context())
				got, err := resolveListenerTarget(t.Context(), catalog, listener, recorded)
				if err != nil || got.Outcome != testCase.want {
					t.Fatalf("resolveListenerTarget() = %#v, %v; want %q", got, err, testCase.want)
				}
				if testCase.want == targetDropped {
					want := droppedTargetRefusal(listener.Name, recorded.TargetTable)
					if got.Refusal == nil || *got.Refusal != want || got.Target.OID != 0 || got.Target.Schema != "" || got.Target.Table != "" {
						t.Fatalf(
							"dropped resolution = %#v, want shared refusal and no target for a destroy action",
							got,
						)
					}
					return
				}
				if got.Refusal != nil {
					t.Fatalf("%q returned unexpected refusal %#v", testCase.name, got.Refusal)
				}
			},
		)
	}
}

var targetOutcomeCases = []struct {
	name string
	want targetOutcome
}{
	// A new listener varies only registry presence: its configured name resolves.
	{"new", targetNew},
	// The healthy control varies the recorded OID from no record to the same table's OID.
	{"healthy", targetHealthy},
	// Rename varies only the current catalog name while the recorded OID still resolves.
	{"renamed", targetRenamed},
	// Drop varies only OID resolution from renamed; the recorded name remains absent.
	{"dropped", targetDropped},
	// Recreate varies only recorded-name existence from dropped; its old OID remains absent.
	{"dropped and recreated", targetDroppedAndRecreated},
}

func targetOutcomeFixture(t *testing.T, pool *pgxpool.Pool, kind string) (config.Listener, *registryListener) {
	t.Helper()
	table := "target_outcome"
	qualified := mustQualifiedTarget(t, table)
	mustExecOn(t, pool, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY)")
	listener := listenerForTarget(qualified)
	reading := resolvedCatalogTarget(t, pool, qualified)
	recorded := &registryListener{Name: listener.Name, TargetTable: qualified, TargetOID: reading.OID}
	switch kind {
	case "new":
		return listener, nil
	case "healthy":
		return listener, recorded
	case "renamed":
		mustExecOn(t, pool, "ALTER TABLE "+qualified+" RENAME TO \"target_renamed\"")
		return listener, recorded
	case "dropped":
		mustExecOn(t, pool, "DROP TABLE "+qualified)
		return listener, recorded
	case "dropped and recreated":
		mustExecOn(t, pool, "DROP TABLE "+qualified)
		mustExecOn(t, pool, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY)")
		return listener, recorded
	default:
		t.Fatalf("unknown target outcome fixture %q", kind)
		return config.Listener{}, nil
	}
}

func listenerForTarget(table string) config.Listener {
	return config.Listener{
		Name: "orders", Trigger: config.TriggerSpec{
			Table:      table,
			Operations: config.Operations{{Kind: "insert"}}, Payload: config.Payload{Mode: "full"},
		},
	}
}

func mustQualifiedTarget(t *testing.T, table string) string {
	t.Helper()
	qualified, fault := schema.Qualified("public", table)
	if fault != schema.IdentifierOK {
		t.Fatalf("qualified target %q: %s", table, fault)
	}
	return qualified
}
