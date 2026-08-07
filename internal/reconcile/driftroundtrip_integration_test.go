//go:build integration

package reconcile

import (
	"maps"
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

const roundTripTable = "drift_roundtrip_target"

// Rule: count-the-population-a-vacuity-guard-guards; the comparison count and zero-drift claim share one assertion.
func TestTheWholeGeneratedCorpusRoundTripsWithZeroDrift(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, roundTripTable)
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, state text)")
	cfg := harnessConfig(t)
	cfg.Listeners = roundTripCorpus()
	generated := generatedCorpusSize(t, cfg)
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	triggers := appliedCorpusTriggers(t, pool, cfg)
	first := catalogPairReadbacks(t, pool, target, triggers)
	assertStablePairReadbacks(t, pool, target, triggers, first)
	compared := comparedRoundTripPairs(t, pool, target, triggers)
	plan, err := Plan(t.Context(), pool, cfg, Options{})
	if err != nil || compared != generated || compared == 0 || plan.Verdict != VerdictClean || len(plan.Actions) != 0 {
		t.Fatalf(
			"round-trip compared=%d corpus=%d plan=%+v err=%v; want every non-zero generated member clean",
			compared,
			generated,
			plan,
			err,
		)
	}
	t.Logf("readback corpus members compared=%d over %d payload modes", compared, len(cfg.Listeners))
}

// roundTripCorpus is the cross the readback round trip needs: internal/source's generated DDL over
// every payload mode, crossed with the operation kinds, the update column filter and the `when`
// clauses. A payload mode is a property of the listener rather than of an operation, so crossing it
// needs one listener per mode; a corpus built from a single mode still satisfies the
// compared-equals-corpus assertion, because that assertion certifies the size of whatever product
// was taken rather than its breadth.
func roundTripCorpus() []config.Listener {
	payloads := []config.Payload{
		{Mode: "full", IncludeOld: true},
		{Mode: "columns", Columns: []string{"id", "state"}, IncludeOld: true},
		{Mode: "keys_only"},
	}
	corpus := make([]config.Listener, 0, len(payloads))
	for _, payload := range payloads {
		listener := matrixListener(roundTripTable, "insert", "update", "delete")
		listener.Name = "roundtrip_" + payload.Mode
		listener.Trigger.Payload = payload
		listener.Trigger.Operations[1].Columns = []string{"state"}
		listener.Trigger.Operations[1].When = "NEW.id != 1"
		listener.Trigger.Operations[2].When = "OLD.state != 'old'"
		corpus = append(corpus, listener)
	}
	return corpus
}

// generatedCorpusSize is how many object sets internal/source generates for the whole corpus. It is
// summed per listener because Generate answers for one listener, and it is the number the compared
// count is asserted equal to.
func generatedCorpusSize(t *testing.T, cfg config.Config) int {
	t.Helper()
	size := 0
	for _, listener := range cfg.Listeners {
		generated, err := source.Generate(
			source.Request{
				Instance: cfg.Instance, ServiceSchema: cfg.Database.Schema,
				Listener: listener,
				Target:   source.Target{Schema: "public", Table: roundTripTable, PrimaryKeyColumns: []string{"id"}},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		size += len(generated)
	}
	return size
}

// appliedCorpusTriggers is every registry row the corpus wrote, read back through the production
// reader, so every reading below covers the whole cross rather than one member of it.
func appliedCorpusTriggers(t *testing.T, pool *pgxpool.Pool, cfg config.Config) []registryTrigger {
	t.Helper()
	var triggers []registryTrigger
	for _, listener := range cfg.Listeners {
		reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
		if err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, reading.Triggers...)
	}
	return triggers
}

func catalogPairReadbacks(
	t *testing.T,
	pool *pgxpool.Pool,
	target string,
	triggers []registryTrigger,
) map[string][2]string {
	t.Helper()
	oid := resolvedCatalogTarget(t, pool, target).OID
	got := make(map[string][2]string, len(triggers))
	for _, trigger := range triggers {
		got[trigger.TriggerName] = readCatalogPair(t, pool, oid, trigger.TriggerName, 0, false)
	}
	return got
}

func assertStablePairReadbacks(
	t *testing.T,
	pool *pgxpool.Pool,
	target string,
	triggers []registryTrigger,
	want map[string][2]string,
) {
	t.Helper()
	for run := 0; run < 5; run++ {
		got := freshCatalogPairReadbacks(t, pool, target, reverseRegistryTriggers(triggers, run%2 == 1))
		if !maps.Equal(got, want) {
			t.Fatalf("fresh pinned reading %d = %#v, want %#v", run+1, got, want)
		}
	}
	trigger := triggers[len(triggers)-1].TriggerName
	got := driftReadbackInSubprocess(t, pool, target, trigger)
	if got != want[trigger] {
		t.Fatalf("second process pair = %#v, want %#v", got, want[trigger])
	}
}

func freshCatalogPairReadbacks(
	t *testing.T,
	pool *pgxpool.Pool,
	target string,
	triggers []registryTrigger,
) map[string][2]string {
	t.Helper()
	// A new pool makes every premise reading a new server connection, rather than merely a new transaction.
	fresh, err := pgxpool.New(t.Context(), harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	return catalogPairReadbacks(t, fresh, target, triggers)
}

func reverseRegistryTriggers(triggers []registryTrigger, reverse bool) []registryTrigger {
	if !reverse {
		return triggers
	}
	reversed := slices.Clone(triggers)
	slices.Reverse(reversed)
	return reversed
}

func comparedRoundTripPairs(t *testing.T, pool *pgxpool.Pool, target string, triggers []registryTrigger) int {
	t.Helper()
	pairs := catalogPairReadbacks(t, pool, target, triggers)
	for _, trigger := range triggers {
		if got := fingerprint(pairs[trigger.TriggerName][0], pairs[trigger.TriggerName][1]); got != trigger.DDLHash {
			t.Fatalf(
				"pair %s/%q fingerprint = %q, want stored readback %q",
				trigger.TriggerName,
				trigger.Operation,
				got,
				trigger.DDLHash,
			)
		}
	}
	return len(pairs)
}
