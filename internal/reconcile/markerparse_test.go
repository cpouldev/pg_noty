package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

const generatedMarkerCorpusMembers = 3

func TestMarkerParserRecoversEveryMarkerSourceGenerateEmits(t *testing.T) {
	sets := generatedMarkerCorpus(t)
	if got, want := len(sets), generatedMarkerCorpusMembers; got != want {
		t.Fatalf("source.Generate corpus has %d members, want %d operations", got, want)
	}
	t.Logf("source.Generate marker corpus members = %d", len(sets))
	operations := map[string]bool{}
	for _, set := range sets {
		parsed, ok := parseCatalogMarker(&set.Marker)
		if !ok {
			t.Fatalf("parse generated marker %q: refused", set.Marker)
		}
		if parsed.Instance != strings.Repeat("i", 41) || parsed.Listener != strings.Repeat(
			"l",
			41,
		) || parsed.Operation != set.Operation {
			t.Fatalf("parsed marker = %+v, want generated fields recovered", parsed)
		}
		if got := len(set.Marker); got != 101 {
			t.Fatalf("marker length = %d, want literal 101", got)
		}
		operations[set.Operation] = true
	}
	for _, operation := range []string{"insert", "update", "delete"} {
		if !operations[operation] {
			t.Fatalf("source.Generate corpus omitted %q", operation)
		}
	}
}

func TestAnAbbreviatedObjectOperationParserWouldFailTheGeneratedMarkerCorpus(t *testing.T) {
	for _, set := range generatedMarkerCorpus(t) {
		parsed, ok := parseCatalogMarker(&set.Marker)
		if !ok || parsed.Operation != set.Operation {
			t.Fatalf(
				"generated marker %q parsed as %+v; a parser using an abbreviated object operation would lose the long spelling %q",
				set.Marker,
				parsed,
				set.Operation,
			)
		}
		if strings.HasSuffix(set.FunctionName, "_"+parsed.Operation) {
			t.Fatalf(
				"marker operation %q copied the abbreviated object-name suffix in %q",
				parsed.Operation,
				set.FunctionName,
			)
		}
	}
}

func TestMarkerParserRefusesEveryUnrecognisedShape(t *testing.T) {
	rows := []struct {
		name   string
		marker string
	}{
		{"two fields", strings.TrimSuffix(schema.MarkerPrefix, ":")},
		{"three fields", schema.MarkerPrefix + "alpha"},
		{"four fields", schema.MarkerPrefix + "alpha:orders"},
		{"six fields", schema.MarkerPrefix + "alpha:orders:insert:extra"},
		{"empty instance", schema.MarkerPrefix + ":orders:insert"},
		{"empty listener", schema.MarkerPrefix + "alpha::insert"},
		{"empty operation", schema.MarkerPrefix + "alpha:orders:"},
		{"field carrying separator", schema.MarkerPrefix + "alpha:orders:in:sert"},
		{"prefix only", schema.MarkerPrefix},
		{"not a marker", "audit trigger"},
	}
	const malformedMarkerCases = 10
	if got := len(rows); got != malformedMarkerCases {
		t.Fatalf("malformed marker rows = %d, want %d", got, malformedMarkerCases)
	}
	for _, row := range rows {
		t.Run(
			row.name, func(t *testing.T) {
				if _, ok := parseCatalogMarker(&row.marker); ok {
					t.Fatalf("unrecognised marker %q was accepted", row.marker)
				}
			},
		)
	}
}

func generatedMarkerCorpus(t *testing.T) []source.ObjectSet {
	t.Helper()
	return generatedMarkerSets(
		t, strings.Repeat("i", 41), strings.Repeat("l", 41),
		config.Operations{{Kind: "insert"}, {Kind: "update"}, {Kind: "delete"}},
	)
}

func generatedMarker(t *testing.T, instance, listener, operation string) string {
	t.Helper()
	sets := generatedMarkerSets(t, instance, listener, config.Operations{{Kind: operation}})
	return sets[0].Marker
}

func generatedMarkerSets(t *testing.T, instance, listener string, operations config.Operations) []source.ObjectSet {
	t.Helper()
	listenerConfig := config.Listener{
		Name: listener, Trigger: config.TriggerSpec{
			Operations: operations, Payload: config.Payload{Mode: "full"},
		},
	}
	sets, err := source.Generate(
		source.Request{
			Instance: instance, ServiceSchema: "noty", Listener: listenerConfig,
			Target: source.Target{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id"}},
		},
	)
	if err != nil {
		t.Fatalf("source.Generate: %v", err)
	}
	return sets
}
