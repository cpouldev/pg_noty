package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func payloadListener(mode string) config.Listener {
	return config.Listener{Name: "orders", Trigger: config.TriggerSpec{Payload: config.Payload{Mode: mode}}}
}

func TestPayloadModesMatchTheContractLiterals(t *testing.T) {
	target := Target{PrimaryKeyColumns: []string{"id", "tenant_id"}}
	for _, tc := range []struct {
		name    string
		payload config.Payload
		want    string
	}{
		{"full", config.Payload{Mode: "full"}, "to_jsonb(NEW)"},
		{"full exclude", config.Payload{Mode: "full", Exclude: []string{"a", "b"}}, "to_jsonb(NEW) - 'a' - 'b'"},
		{
			"columns", config.Payload{Mode: "columns", Columns: []string{"status", "Status"}},
			`jsonb_build_object('status', NEW."status", 'Status', NEW."Status")`,
		},
		{
			"keys only", config.Payload{Mode: "keys_only"},
			`jsonb_build_object('id', NEW."id", 'tenant_id', NEW."tenant_id")`,
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				listener := payloadListener(tc.payload.Mode)
				listener.Trigger.Payload = tc.payload
				got, err := payloadExpressionsFor("insert", listener, target)
				if err != nil || got.new != tc.want {
					t.Fatalf("payload = %q, err=%v; want %q", got.new, err, tc.want)
				}
			},
		)
	}
}

func TestPayloadExcludeUsesNSingleKeySubtractions(t *testing.T) {
	listener := payloadListener("full")
	listener.Trigger.Payload.Exclude = []string{`a"b`, `c'd`, `Status`}
	got, err := payloadExpressionsFor("insert", listener, Target{})
	if err != nil || strings.Count(got.new, " - ") != 3 || strings.Contains(
		got.new,
		"::text[]",
	) || strings.Contains(got.new, "{") {
		t.Fatalf("exclude expression = %q, err=%v; want three scalar subtractions without array grammar", got.new, err)
	}
}

func TestPayloadPreservesAllThreeSuppliedOrders(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second config.Payload
	}{
		{
			"columns", config.Payload{Mode: "columns", Columns: []string{"a", "b"}},
			config.Payload{Mode: "columns", Columns: []string{"b", "a"}},
		},
		{"keys", config.Payload{Mode: "keys_only"}, config.Payload{Mode: "keys_only"}},
		{
			"exclude", config.Payload{Mode: "full", Exclude: []string{"a", "b"}},
			config.Payload{Mode: "full", Exclude: []string{"b", "a"}},
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				firstTarget := Target{PrimaryKeyColumns: []string{"a", "b"}}
				secondTarget := Target{PrimaryKeyColumns: []string{"b", "a"}}
				first, err := payloadExpressionsFor(
					"insert",
					config.Listener{Name: "n", Trigger: config.TriggerSpec{Payload: tc.first}},
					firstTarget,
				)
				if err != nil {
					t.Fatal(err)
				}
				second, err := payloadExpressionsFor(
					"insert",
					config.Listener{Name: "n", Trigger: config.TriggerSpec{Payload: tc.second}},
					secondTarget,
				)
				if err != nil {
					t.Fatal(err)
				}
				if first.new == second.new {
					t.Fatalf("two supplied %s orders produced identical %q", tc.name, first.new)
				}
			},
		)
	}
}

func TestPayloadPreservesUppercaseIdentifierSpelling(t *testing.T) {
	listener := payloadListener("columns")
	listener.Trigger.Payload.Columns = []string{"Status"}
	got, err := payloadExpressionsFor("insert", listener, Target{})
	if err != nil || got.new != `jsonb_build_object('Status', NEW."Status")` {
		t.Fatalf("uppercase payload = %q, err=%v; want quoted Status", got.new, err)
	}
}

func TestKeysOnlyRefusesEmptyPrimaryKeyBeforeRendering(t *testing.T) {
	listener := payloadListener("keys_only")
	got, err := payloadExpressionsFor("insert", listener, Target{})
	if err == nil || !strings.Contains(err.Error(), "orders") || !strings.Contains(
		err.Error(),
		"primary key",
	) || got != (payloadExpressions{}) {
		t.Fatalf("keys_only empty target returned %#v, err=%v; want named refusal and no output", got, err)
	}
}
