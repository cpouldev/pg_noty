//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// theWhenLegalityRows are PostgreSQL's own answers, attempted directly against the server. The two
// refusals alone cannot tell three readings of the `WHEN` rule apart, and two of those readings
// would justify designs this package forbids -- so each acceptance records which one it refutes.
var theWhenLegalityRows = []struct {
	name, sql, refutes string
	accepted           bool
}{
	{
		name: "old across insert",
		sql: `CREATE TRIGGER when_old_bad AFTER INSERT OR UPDATE OR DELETE ON public.when_target ` +
			`FOR EACH ROW WHEN (OLD.status <> 'paid') EXECUTE FUNCTION public.when_noop()`,
		refutes: "nothing; this is the rule itself -- the INSERT arm has no OLD row",
	},
	{
		name: "new across delete",
		sql: `CREATE TRIGGER when_new_bad AFTER INSERT OR UPDATE OR DELETE ON public.when_target ` +
			`FOR EACH ROW WHEN (NEW.status = 'paid') EXECUTE FUNCTION public.when_noop()`,
		refutes: "nothing; the mirrored refusal -- the DELETE arm has no NEW row",
	},
	{
		name: "old update delete",
		sql: `CREATE TRIGGER when_old_near AFTER UPDATE OR DELETE ON public.when_target ` +
			`FOR EACH ROW WHEN (OLD.status <> 'paid') EXECUTE FUNCTION public.when_noop()`,
		accepted: true,
		refutes:  `"any combination refuses WHEN" -- a rule PostgreSQL does not have; both arms here have OLD`,
	},
	{
		name: "update of combined",
		sql: `CREATE TRIGGER when_columns_ok AFTER INSERT OR UPDATE OF status, total OR DELETE ` +
			`ON public.when_target FOR EACH ROW EXECUTE FUNCTION public.when_noop()`,
		accepted: true,
		refutes:  `"column filters force the split" -- a reading that would justify a design this package forbids`,
	},
}

func TestPostgresWhenLegalityHasBothRefusalsAndAcceptances(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, `CREATE TABLE public.when_target (id int, status text, total int)`)
	mustExecOn(
		t,
		pool,
		`CREATE FUNCTION public.when_noop() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END; $$`,
	)
	accepted := 0
	for _, row := range theWhenLegalityRows {
		t.Run(
			row.name, func(t *testing.T) {
				if row.refutes == "" {
					t.Fatalf(
						"the %s row records no reading, so a later reader can re-derive the wrong "+
							"rule from the refusals alone", row.name,
					)
				}
				_, err := pool.Exec(t.Context(), row.sql)
				if (err == nil) != row.accepted {
					t.Fatalf("WHEN legality error=%v, accepted=%v, want accepted=%v", err, err == nil, row.accepted)
				}
			},
		)
		if row.accepted {
			accepted++
		}
	}
	if accepted != 2 {
		t.Fatalf(
			"%d of the four rows are acceptances, want 2; the refusals alone leave the three "+
				"readings of the WHEN rule indistinguishable", accepted,
		)
	}
}

// TestTheObjectLayoutIsTheSameWithAndWithoutAWhenClause is what stops the WHEN legality rows from
// being read as the split's justification. The rows above pin the *reason* this package splits by
// operation; they do not pin the requirement, and the split stands whether or not a listener
// declares a `when:`. internal/reconcile hashes each trigger's DDL separately, so a listener that
// changed its object layout by adding a condition would have no reconcilable identity.
func TestTheObjectLayoutIsTheSameWithAndWithoutAWhenClause(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, theShapeTargetTable)

	sets := installedShapeSets(
		t, pool, config.Operation{Kind: "insert"},
		config.Operation{Kind: "update", When: "NEW.status <> 'paid'"}, config.Operation{Kind: "delete"},
	)
	want := map[string]int16{}
	for _, set := range sets {
		want[set.TriggerName] = theAfterRowTypeOf[set.Operation]
	}
	if len(want) != 3 {
		t.Fatalf(
			"a listener declaring a when: produced %d distinct triggers, want the same three a "+
				"listener without one produces", len(want),
		)
	}
	if issues := closedInventoryIssues(want, triggerInventoryOf(t, pool, "public", theShapeTarget)); len(issues) != 0 {
		t.Fatalf("declaring a when: changed the object layout: %v", issues)
	}
}
