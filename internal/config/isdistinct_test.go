package config

import (
	"strings"
	"testing"
)

func TestIsDistinctDefaultsAndParsesAsABoolean(t *testing.T) {
	tests := []struct {
		name, filter string
		want         bool
	}{
		{name: "omitted", filter: "      update: {}\n"},
		{name: "false", filter: "      update:\n        is_distinct: false\n"},
		{name: "true", filter: "      update:\n        is_distinct: true\n", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, warnings, errs := Parse([]byte(operationsOf("    operations:\n"+tc.filter)),
				"is_distinct.yaml", MapEnv(nil))
			if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
				t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
			}
			operation, found := listenerOperation(*cfg, 0, updateOperation)
			if !found || operation.IsDistinct != tc.want {
				t.Fatalf("update operation = %+v, found=%t; want is_distinct=%t", operation, found, tc.want)
			}
		})
	}
}

func TestIsDistinctRejectsInvalidScalarValues(t *testing.T) {
	for _, value := range []string{"1", "perhaps", `"not-a-boolean"`} {
		t.Run(value, func(t *testing.T) {
			document := operationsOf("    operations:\n      update:\n        is_distinct: " + value + "\n")
			cfg, warnings, errs := Parse([]byte(document), "is_distinct.yaml", MapEnv(nil))
			if cfg != nil || len(warnings) != 0 || len(errs) != 1 {
				t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
			}
			if errs[0].Rule != R43 || errs[0].Path != "listeners[0].operations.update.is_distinct" ||
				!strings.Contains(errs[0].Msg, "expected true or false") {
				t.Fatalf("diagnostic = %+v, want R43 boolean refusal at is_distinct", errs[0])
			}
		})
	}
}

func TestIsDistinctIsLegalOnlyUnderUpdate(t *testing.T) {
	tests := []struct {
		operation string
		value     string
	}{{"insert", "true"}, {"delete", "false"}}
	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			document := operationsOf("    operations:\n      " + tc.operation +
				":\n        is_distinct: " + tc.value + "\n")
			cfg, warnings, errs := Parse([]byte(document), "is_distinct.yaml", MapEnv(nil))
			if cfg != nil || len(warnings) != 0 || len(errs) != 1 {
				t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
			}
			if errs[0].Rule != R43 || errs[0].Path != "listeners[0].operations."+tc.operation+".is_distinct" ||
				errs[0].Msg != `"is_distinct" is legal only under "update"` {
				t.Fatalf("diagnostic = %+v, want R43 update-only refusal", errs[0])
			}
		})
	}
}
