//go:build integration

package reconcile

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

// measuredDivergenceCount is the ten measured divergences. The set is pinned by size so an
// implementation that reverted to comparing generator text against a catalog reading fails here
// rather than as a replacement proposed on every listener carrying a `when:` clause.
const measuredDivergenceCount = 10

const (
	divergenceTable   = "drift_divergence_target"
	divergenceWhen    = "NEW.state != '1'"
	divergenceLiteral = "'1'"
)

// divergenceUpdateColumns is deliberately not in the table's declaration order, so the one case
// measured NOT to diverge has something to be about.
var divergenceUpdateColumns = []string{"state", "id"}

// Rule: falsify-the-assertion-not-a-copy-of-it. PostgreSQL, not an expected string, supplies every reading.
func TestCatalogDeparseDivergencesAreMeasuredAndSizePinned(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, divergenceTable)
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, state text)")
	listener := matrixListener(divergenceTable, "update")
	listener.Trigger.Operations[0] = config.Operation{
		Kind: "update", Columns: divergenceUpdateColumns, When: divergenceWhen,
	}
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	sets, err := source.Generate(
		source.Request{
			Instance: cfg.Instance, ServiceSchema: cfg.Database.Schema, Listener: listener,
			Target: source.Target{Schema: "public", Table: divergenceTable, PrimaryKeyColumns: []string{"id"}},
		},
	)
	if err != nil || len(sets) != 1 {
		t.Fatalf("generate = %v sets=%d", err, len(sets))
	}
	applied, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || applied.Verdict != VerdictClean {
		t.Fatalf("apply=%+v err=%v", applied, err)
	}
	pair := readCatalogPair(t, pool, resolvedCatalogTarget(t, pool, target).OID, sets[0].TriggerName, 0, false)
	version := postgresVersion(t, pool)
	cases := catalogDivergences(t, cfg.Database.Schema, sets[0], pair)
	if len(cases) != measuredDivergenceCount {
		t.Fatalf("divergence cases=%d, want pinned %d", len(cases), measuredDivergenceCount)
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		if !cases[name] {
			t.Fatalf(
				"%s did not measure its named deparse behaviour on %s: generated trigger=%q function=%q catalog trigger=%q function=%q",
				name, version, sets[0].CreateTrigger, sets[0].CreateFunction, pair[0], pair[1],
			)
		}
	}
	t.Logf("PostgreSQL %s returned trigger=%q function=%q", version, pair[0], pair[1])
	assertAgreementFailsEveryDivergence(t, cfg.Database.Schema, sets[0])
}

// assertAgreementFailsEveryDivergence is what makes the set above a comparison rather than a
// description of the reading. It answers the divergences over a pair that agrees with the generated
// text exactly -- the arm the set exists for, "a case that silently started agreeing fails by name" --
// and requires every one of them except the measured exception to answer false. Two entries used to
// read only the catalog side. `execute-function` asked whether the reading said EXECUTE FUNCTION,
// which the generated text says too, so it answered true under exact agreement and certified the
// design it exists to falsify. `schema-qualification` asked whether the reading said
// `ON public.<table>`; that answered false here only because the generator happens to quote both
// halves, so it was falsified by the generator's habit rather than by any comparison.
func assertAgreementFailsEveryDivergence(t *testing.T, serviceSchema string, set source.ObjectSet) {
	t.Helper()
	agreed := catalogDivergences(t, serviceSchema, set, [2]string{set.CreateTrigger, set.CreateFunction})
	for _, name := range slices.Sorted(maps.Keys(agreed)) {
		if name == "update-of-order-exception" {
			continue
		}
		if agreed[name] {
			t.Errorf(
				"%s holds against a reading identical to the generated text, so it measures what "+
					"the reading contains rather than how the two differ", name,
			)
		}
	}
}

// catalogDivergences is the measured set: one entry per way the server's reading of an object
// differs from the text the generator wrote for it. Every entry compares the two texts. An entry
// asserting only what the reading contains would hold if the two agreed exactly, which is the arm
// the set exists to catch -- "so a case that silently started agreeing fails by name".
func catalogDivergences(t *testing.T, serviceSchema string, set source.ObjectSet, pair [2]string) map[string]bool {
	t.Helper()
	trigger, function := pair[0], pair[1]
	tag := generatedDollarTag(set.CreateFunction)
	return map[string]bool{
		"trailing-semicolon": noReadingCarriesATrailingSemicolon(set, pair),
		"set-search-path": diverges(
			set.CreateFunction, function,
			"SET search_path = pg_catalog, pg_temp", "SET search_path TO 'pg_catalog', 'pg_temp'",
		),
		"dollar-quote-tag":    tag != "" && diverges(set.CreateFunction, function, "AS $"+tag+"$", "AS $function$"),
		"comparison-operator": diverges(set.CreateTrigger, trigger, "!=", "<>"),
		"bare-literal-cast":   aCastWasInserted(set.CreateTrigger, trigger, divergenceLiteral),
		// The generator writes one parenthesis for the WHEN condition and an empty argument list; the
		// reading wraps the condition again, so it carries three where the generated text carries two.
		"parentheses": strings.Count(trigger, "(") > strings.Count(set.CreateTrigger, "("),
		// The keyword measurement and the name's spelling in one case: this server renders the clause
		// `EXECUTE FUNCTION` rather than the pre-11 `EXECUTE PROCEDURE`, and names the function
		// schema-qualified but unquoted where the generator qualified and quoted it.
		"execute-function": !strings.Contains(trigger, "EXECUTE PROCEDURE") &&
			diverges(
				set.CreateTrigger, trigger,
				"EXECUTE FUNCTION "+mustDDLQualified(t, serviceSchema, set.FunctionName)+"()",
				"EXECUTE FUNCTION "+serviceSchema+"."+set.FunctionName+"()",
			),
		// The generator quotes every identifier; the server returns the ones that need no quoting bare.
		"quoting-only-where-needed": diverges(
			set.CreateTrigger, trigger,
			"CREATE TRIGGER "+mustDDLQuoted(t, set.TriggerName)+" ", "CREATE TRIGGER "+set.TriggerName+" ",
		),
		// The measured asymmetry from the target's side: a non-pretty reading always keeps
		// the table's schema qualifier, and drops the generator's quotes around both halves of it.
		"schema-qualification": diverges(
			set.CreateTrigger, trigger,
			"ON "+mustDDLQualified(t, "public", divergenceTable)+" ", "ON public."+divergenceTable+" ",
		),
		"update-of-order-exception": updateColumnsStayInDeclarationOrder(set.CreateTrigger) &&
			updateColumnsStayInDeclarationOrder(trigger),
	}
}

// updateColumnsStayInDeclarationOrder is the one case measured NOT to diverge. The order is read from
// the fixture's own column list rather than written out a second time, so reordering the fixture
// cannot leave this case asserting an order nothing generates.
func updateColumnsStayInDeclarationOrder(definition string) bool {
	previous := -1
	for _, column := range divergenceUpdateColumns {
		at := strings.Index(definition, column)
		if at <= previous {
			return false
		}
		previous = at
	}
	return true
}

func postgresVersion(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var version string
	if err := pool.QueryRow(t.Context(), "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// The accepted cost of comparing readings: a change in how the server renders a definition makes
// every stored fingerprint stale at once. What that proposes must be shown to be a recreate of an
// *identical* object, so the readings are compared across the re-baselining apply as well as the
// actions counted.
func TestAWholeStaleFingerprintSetIsApprovalGatedAndSelfHeals(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg, listener, target := appliedTargetFixture(t, pool, "drift_rebaseline", "insert", "update")
	before := rebaselineReadbacks(t, pool, target, listener.Name)
	mustExecOn(
		t,
		pool,
		"UPDATE "+harnessSchema+".listener_triggers SET ddl_hash = '1:stale' WHERE listener = '"+listener.Name+"'",
	)
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || len(plan.Actions) != len(before) || plan.Destructive() {
		t.Fatalf("stale hash plan=%+v err=%v; want every pair replace", plan, err)
	}
	for _, action := range plan.Actions {
		if action.Kind != ActionReplace {
			t.Fatalf("stale hash proposed %+v; want a replace of an object the configuration still describes", action)
		}
	}
	gated, err := Apply(t.Context(), pool, cfg, Approval{}, Options{})
	if err != nil || gated.Stopped != StopApproval {
		t.Fatalf("stale hash approval=%+v err=%v; want approval gate", gated, err)
	}
	if _, err := Apply(
		t.Context(),
		pool,
		cfg,
		Approval{Approved: true, DestructionPermitted: true},
		Options{},
	); err != nil {
		t.Fatal(err)
	}
	if after := rebaselineReadbacks(t, pool, target, listener.Name); !maps.Equal(after, before) {
		t.Fatalf("re-baseline left %#v where it found %#v; the recreate must reproduce the same objects", after, before)
	}
	clean, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || clean.Verdict != VerdictClean {
		t.Fatalf("re-baselined plan=%+v err=%v; want clean", clean, err)
	}
}

func rebaselineReadbacks(t *testing.T, pool *pgxpool.Pool, target, listener string) map[string][2]string {
	t.Helper()
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if err != nil {
		t.Fatal(err)
	}
	return catalogPairReadbacks(t, pool, target, reading.Triggers)
}
