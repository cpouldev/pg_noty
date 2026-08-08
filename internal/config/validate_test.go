package config

import (
	"strings"
	"testing"
)

// stageHRuleEntries is the ownership table's scalar entries. Keeping the clauses here makes the
// fixture inventory and anchor assignment fail together if either side drifts.
var stageHRuleEntries = map[RuleID]string{
	R1:  "version equals 1",
	R2:  "instance is a legal name",
	R4:  "database.schema is a legal non-system identifier",
	R6:  "worker.concurrency is between 1 and 1024",
	R7:  "worker.batch_size is between 1 and 10000",
	R8:  "worker durations are positive",
	R10: "retention durations are positive",
	R13: "default and listener timeouts are positive",
	R14: "retry.max_attempts is at least 1",
	R15: "retry.backoff is exponential, linear or fixed",
	R17: "retry.max_interval is positive",
	R18: "retry.jitter is boolean",
	R23: "listener name is a legal name",
	R25: "listener enabled is boolean",
	R31: "payload.mode is full, columns or keys_only",
	R34: "payload.include_old is boolean",
	R35: "payload.max_bytes is positive",
	R37: "destination.method is POST, PUT or PATCH",
	R39: "listener concurrency is between 1 and 1024",
	R40: "durations use Go duration units",
	R43: "operations.update.is_distinct is boolean",
}

func TestStageHOwnsTwentyOneEntriesAndAssignsEveryOneAnAnchor(t *testing.T) {
	if len(stageHRuleEntries) != 21 {
		t.Fatalf("stage H owns %d entries, want 21", len(stageHRuleEntries))
	}
	if len(semanticAnchors) != 4 {
		t.Fatalf("anchor table has %d classes, want the four ADR-6 classes", len(semanticAnchors))
	}
	for _, class := range []anchorClass{keyAnchor, mappingAnchor, valueAnchor, elementAnchor} {
		if _, declared := semanticAnchors[class]; !declared {
			t.Errorf("anchor table does not declare %q", class)
		}
	}
	for rule := range stageHRuleEntries {
		uses, assigned := stageHAnchorClass[rule]
		if !assigned {
			t.Errorf("%s has no anchor assignment", rule)
			continue
		}
		if got, assigned := uses[scalarValueUse]; !assigned {
			t.Errorf("%s has no scalar-value anchor assignment", rule)
		} else if got != valueAnchor {
			t.Errorf("%s uses %q, want the value token", rule, got)
		}
		wantUses := 1
		if rule == R39 {
			wantUses++ // Step 11's comparison half has its own effective-value use.
		}
		if len(uses) != wantUses {
			t.Errorf("%s has %d anchor uses, want %d", rule, len(uses), wantUses)
		}
	}
	scalarAssignments := 0
	for _, uses := range stageHAnchorClass {
		if _, assigned := uses[scalarValueUse]; assigned {
			scalarAssignments++
		}
	}
	if scalarAssignments != len(stageHRuleEntries) {
		t.Errorf("scalar anchor assignments = %d, Step-8 entries = %d",
			scalarAssignments, len(stageHRuleEntries))
	}
}

func TestNumberedConversionLeafNamesAreUnique(t *testing.T) {
	seen := make(map[string]RuleID)
	for _, level := range schemaLevels {
		for _, spec := range level.keys {
			if spec.conversion == noRule {
				continue
			}
			if prior, duplicate := seen[spec.name]; duplicate {
				t.Errorf("conversion leaf %q declares both %s and %s", spec.name, prior, spec.conversion)
			}
			seen[spec.name] = spec.conversion
		}
	}
	if len(seen) != 4 {
		t.Errorf("%d numbered conversion leaves, want jitter, enabled, include_old and is_distinct", len(seen))
	}
}

func TestEveryScalarConstraintRejectsItsOutsideClassOnce(t *testing.T) {
	base := stageHExtensionSafeValue("  concurrency: 8\n")
	tests := []struct {
		name, from, to, path, constraint string
		rule                             RuleID
	}{
		{"version", "version: 1", "version: 2", "version", "equal 1", R1},
		{"instance", "instance: noty", "instance: Noty", "instance", "match", R2},
		{"schema", "schema: public", "schema: pg_noty", "database.schema", "reserved", R4},
		{"worker lower", "concurrency: 8", "concurrency: 0", "worker.concurrency", "1 to 1024", R6},
		{"worker upper", "concurrency: 8", "concurrency: 1025", "worker.concurrency", "1 to 1024", R6},
		{"batch lower", "batch_size: 100", "batch_size: 0", "worker.batch_size", "1 to 10000", R7},
		{"batch upper", "batch_size: 100", "batch_size: 10001", "worker.batch_size", "1 to 10000", R7},
		{"worker duration", "poll_interval: 1s", "poll_interval: 0s", "worker.poll_interval", "greater than zero", R8},
		{"retention duration", "precreate: 48h", "precreate: -1s", "retention.precreate", "greater than zero", R10},
		{"timeout", "timeout: 10s", "timeout: 0s", "defaults.timeout", "greater than zero", R13},
		{"attempts", "max_attempts: 5", "max_attempts: 0", "defaults.retry.max_attempts", "at least 1", R14},
		{"backoff", "backoff: exponential", "backoff: fibonacci", "defaults.retry.backoff", "exponential", R15},
		{"max interval", "max_interval: 1m", "max_interval: 0s", "defaults.retry.max_interval", "greater than zero", R17},
		{"jitter", "jitter: true", "jitter: perhaps", "defaults.retry.jitter", "true or false", R18},
		{"name", "name: order_paid", "name: Order", "listeners[0].name", "match", R23},
		{"enabled", "enabled: true", "enabled: perhaps", "listeners[0].enabled", "true or false", R25},
		{"mode", "mode: full", "mode: sideways", "listeners[0].payload.mode", "full", R31},
		{"include old", "include_old: true", "include_old: perhaps", "listeners[0].payload.include_old", "true or false", R34},
		{"max bytes", "max_bytes: 4096", "max_bytes: 0", "listeners[0].payload.max_bytes", "greater than zero", R35},
		{"method", "method: POST", "method: DELETE", "listeners[0].destination.method", "POST", R37},
		{"listener range", stageHListenerConcurrency, "concurrency: 0", "listeners[0].concurrency", "1 to 1024", R39},
		{"duration units", "precreate: 48h", "precreate: 7d", "retention.precreate", "ns", R40},
		{"is distinct", "is_distinct: true", "is_distinct: perhaps", "listeners[0].operations.update.is_distinct", "true or false", R43},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := stageH(t, replaceOnce(t, base, tc.from, tc.to))
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want exactly one", len(diags), messagesOf(diags))
			}
			if got := diags[0]; got.Rule != tc.rule || got.Path != tc.path ||
				!strings.Contains(got.Msg, tc.constraint) {
				t.Errorf("diagnostic = %+v, want %s at %s stating %q", got, tc.rule, tc.path, tc.constraint)
			}
		})
	}
}

func stageH(t *testing.T, text string) (*rawConfig, Errors) {
	t.Helper()
	src, root, diags := stageE(t, text, corpusVariables)
	if len(diags) != 0 {
		t.Fatalf("fixture does not reach stage F: %q", messagesOf(diags))
	}
	shape, readable := checkShape(src, root)
	if !readable {
		t.Fatalf("fixture does not reach stage G: %q", messagesOf(shape))
	}
	raw, conversions := decodeDocument(src, root)
	return raw, append(append(shape, conversions...), validate(src, raw, shape)...)
}

func replaceOnce(t *testing.T, text, from, to string) string {
	t.Helper()
	if strings.Count(text, from) != 1 {
		t.Fatalf("%q occurs %d times, want once", from, strings.Count(text, from))
	}
	return strings.Replace(text, from, to, 1)
}
