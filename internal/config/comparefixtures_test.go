package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestComparisonFixturesNameBothSidesAndTheRelationship(t *testing.T) {
	tests := []struct {
		name, left, right, relation string
		rule                        RuleID
		env                         EnvLookup
	}{
		{"R16_retry_initial_exceeds_max", "retry.initial_interval", "retry.max_interval", "no greater than", R16, MapEnv(nil)},
		{"R39_listener_concurrency_exceeds_worker", "listeners[].concurrency", "worker.concurrency", "no greater than", R39, MapEnv(nil)},
		{"R9_lease_timeout_equals_listener_timeout", "worker.lease_timeout", "listeners[].timeout", "greater than", R9, MapEnv(nil)},
		// lease_timeout is intentionally omitted: its inherited 5m is below the written 6m
		// listener timeout, and a Set gate would miss this effective-value R9 violation.
		{"R9_lease_timeout_below_listener_timeout", "worker.lease_timeout", "listeners[].timeout", "greater than", R9, MapEnv(nil)},
		{"R11_retention_keep_below_partition_interval", "retention.keep", "retention.partition_interval", "at least", R11, MapEnv(nil)},
		{"R12_retention_precreate_below_partition_interval", "retention.precreate", "retention.partition_interval", "at least", R12, MapEnv(nil)},
		{"step11/R9_lease_timeout_reference_below_listener_timeout", "worker.lease_timeout", "listeners[].timeout", "greater than", R9, MapEnv(map[string]string{"LEASE": "5s"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(invalidCorpus, tc.name+fixtureExtension)
			data := readFixtureBytes(t, path)
			_, _, errs := Parse(data, filepath.Base(path), tc.env)
			if len(errs) != 1 || errs[0].Rule != tc.rule {
				t.Fatalf("got %+v, want one %s", errs, tc.rule)
			}
			for _, want := range []string{tc.left, tc.right, tc.relation} {
				if !strings.Contains(errs[0].Msg, want) {
					t.Errorf("message %q does not name %q", errs[0].Msg, want)
				}
			}
			if tc.rule == R9 && !strings.Contains(errs[0].Msg, "duplicate deliveries") {
				t.Errorf("lease message does not explain duplicate deliveries: %q", errs[0].Msg)
			}
		})
	}
}

func TestTheLeaseReferenceGoldenRendersSourceNotItsEnvironmentValue(t *testing.T) {
	path := filepath.Join(invalidCorpus, "step11", "R9_lease_timeout_reference_below_listener_timeout.yaml")
	data := readFixtureBytes(t, path)
	_, _, errs := Parse(data, filepath.Base(path), MapEnv(map[string]string{"LEASE": "5s"}))
	rendered := errs.Render(data)
	if !strings.Contains(rendered, "${LEASE}") || strings.Contains(rendered, "lease_timeout: 5s") {
		t.Fatalf("rendered diagnostic does not preserve the source reference:\n%s", rendered)
	}
	compareGolden(t, goldenFor(path), rendered)
}

func TestInheritedRetryCandidatesAreCollapsedOnlyAtTheBoundary(t *testing.T) {
	path := filepath.Join(invalidCorpus, "R16_defaults_retry_inherited_by_three_listeners.yaml")
	data := readFixtureBytes(t, path)
	candidates := stageIComparisonCandidates(t, data)
	if len(candidates) != 3 {
		t.Fatalf("stage I produced %d raw candidates, want one per listener", len(candidates))
	}
	for _, candidate := range candidates[1:] {
		if candidate.complaint() != candidates[0].complaint() {
			t.Errorf("candidate complaint %+v differs from %+v", candidate.complaint(), candidates[0].complaint())
		}
	}
	_, _, errs := Parse(data, filepath.Base(path), MapEnv(nil))
	if len(errs) != 1 || errs[0].Rule != R16 {
		t.Fatalf("public boundary returned %+v, want one deduplicated R16", errs)
	}
}
