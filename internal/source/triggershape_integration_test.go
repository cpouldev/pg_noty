//go:build integration

package source

import (
	"math/bits"
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The bits pg_trigger.tgtype carries, from PostgreSQL's own catalog/pg_trigger.h. AFTER is the
// absence of the BEFORE bit, so an AFTER ... FOR EACH ROW trigger is ROW plus its one event bit.
const (
	triggerTypeRow      int16 = 1 << 0
	triggerTypeBefore   int16 = 1 << 1
	triggerTypeInsert   int16 = 1 << 2
	triggerTypeDelete   int16 = 1 << 3
	triggerTypeUpdate   int16 = 1 << 4
	triggerTypeTruncate int16 = 1 << 5

	triggerEventBits = triggerTypeInsert | triggerTypeDelete | triggerTypeUpdate | triggerTypeTruncate
)

// theAfterRowTypeOf is the tgtype the one-trigger-per-operation layout promises, composed from the
// bit definitions above rather than recorded from a run.
var theAfterRowTypeOf = map[string]int16{
	"insert": triggerTypeRow | triggerTypeInsert,
	"update": triggerTypeRow | triggerTypeUpdate,
	"delete": triggerTypeRow | triggerTypeDelete,
}

// installedTrigger is one row of the catalog's answer -- what the server holds, never what was
// emitted, because a statement that was emitted and silently did nothing is byte-identical in the
// text to one that took effect.
type installedTrigger struct {
	tgtype   int16
	filtered []string
}

// triggerInventoryOf reads every non-internal trigger on one table, resolving the columns an
// UPDATE OF filter names back to their attnames so the filter is compared against configured names
// rather than against attribute ordinals.
func triggerInventoryOf(t *testing.T, pool *pgxpool.Pool, schemaName, table string) map[string]installedTrigger {
	t.Helper()
	rows, err := pool.Query(
		t.Context(),
		"SELECT g.tgname, g.tgtype, coalesce((SELECT array_agg(a.attname ORDER BY a.attnum) "+
			"FROM pg_attribute a WHERE a.attrelid=g.tgrelid AND a.attnum = ANY(g.tgattr::int2[])), '{}') "+
			"FROM pg_trigger g JOIN pg_class c ON c.oid=g.tgrelid "+
			"JOIN pg_namespace n ON n.oid=c.relnamespace "+
			"WHERE NOT g.tgisinternal AND n.nspname=$1 AND c.relname=$2", schemaName, table,
	)
	if err != nil {
		t.Fatalf("read the trigger inventory of %s.%s: %v", schemaName, table, err)
	}
	defer rows.Close()

	inventory := map[string]installedTrigger{}
	for rows.Next() {
		var name string
		var held installedTrigger
		if err := rows.Scan(&name, &held.tgtype, &held.filtered); err != nil {
			t.Fatalf("scan a trigger row: %v", err)
		}
		inventory[name] = held
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the trigger rows: %v", err)
	}
	return inventory
}

// closedInventoryIssues is the predicate the real assertion and its control both run. It closes in
// both directions on purpose: asserting the presence of the three would pass for a fourth combined
// trigger sitting beside them, which is the object layout internal/reconcile's per-trigger
// ddl_hash cannot express.
func closedInventoryIssues(want map[string]int16, got map[string]installedTrigger) []string {
	var issues []string
	for name, wantType := range want {
		held, present := got[name]
		switch {
		case !present:
			issues = append(issues, "no trigger named "+name+" is installed")
		case held.tgtype != wantType:
			issues = append(issues, name+" covers a different statement set than it was generated for")
		case bits.OnesCount16(uint16(held.tgtype&triggerEventBits)) != 1:
			issues = append(issues, name+" covers more than one statement type")
		}
	}
	for name := range got {
		if _, declared := want[name]; !declared {
			issues = append(issues, "the table carries trigger "+name+", which no object set declares")
		}
	}
	return issues
}

// theCombinedTrigger is the fourth object a presence check cannot see: one trigger covering all
// three statement types, which is exactly the layout the one-trigger-per-operation rule forbids.
const theCombinedTrigger = `CREATE TRIGGER pg_noty_combined_decoy AFTER INSERT OR UPDATE OR DELETE ` +
	`ON public.shape_target FOR EACH ROW EXECUTE FUNCTION noty.pg_noty_order_paid_ins()`

const theSurplusTriggerIssue = "the table carries trigger pg_noty_combined_decoy, which no object set declares"

func TestGeneratedThreeOperationInventoryIsClosed(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, theShapeTargetTable)
	sets := installedShapeSets(
		t, pool, config.Operation{Kind: "insert"},
		config.Operation{Kind: "update"}, config.Operation{Kind: "delete"},
	)

	want, functions := map[string]int16{}, map[string]bool{}
	for _, set := range sets {
		want[set.TriggerName] = theAfterRowTypeOf[set.Operation]
		functions[set.FunctionName] = true
	}
	if len(want) != 3 || len(functions) != 3 {
		t.Fatalf(
			"three operations produced %d trigger names and %d function names, want three of "+
				"each; a shared name would leave the inventory below unable to tell them apart",
			len(want), len(functions),
		)
	}
	for name := range functions {
		if got := countOn(
			t, pool, "SELECT count(*) FROM pg_proc p JOIN pg_namespace n "+
				"ON n.oid=p.pronamespace WHERE n.nspname=$1 AND p.proname=$2", harnessSchema, name,
		); got != 1 {
			t.Errorf("the catalog holds %d functions named %q in %s, want 1", got, name, harnessSchema)
		}
	}
	if issues := closedInventoryIssues(want, triggerInventoryOf(t, pool, "public", theShapeTarget)); len(issues) != 0 {
		t.Fatalf("the installed inventory is not the generated one: %v", issues)
	}

	// The control, run through the same predicate rather than a restatement of it: a fourth combined
	// trigger passes every presence check and must fail this one.
	mustExecOn(t, pool, theCombinedTrigger)
	issues := closedInventoryIssues(want, triggerInventoryOf(t, pool, "public", theShapeTarget))
	if !slices.Equal(issues, []string{theSurplusTriggerIssue}) {
		t.Fatalf(
			"a fourth combined trigger produced %v, want exactly [%q]; the inventory is a "+
				"presence check rather than a closed one", issues, theSurplusTriggerIssue,
		)
	}
}

const (
	theShapeTarget      = "shape_target"
	theShapeTargetTable = `CREATE TABLE public.shape_target (id int, status text, total int, other int)`
)

// installedShapeSets generates for the shape target and applies every object set, returning them so
// each assertion compares the catalog against the names the generator chose.
func installedShapeSets(t *testing.T, pool *pgxpool.Pool, operations ...config.Operation) []ObjectSet {
	t.Helper()
	request := generationRequest(operations...)
	request.Target.Table = theShapeTarget
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range sets {
		executeObjectSet(t, pool, set)
	}
	return sets
}
