package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryDurationSubjectHasARejectingCorpusFixture(t *testing.T) {
	tests := []struct {
		fixture string
		rule    RuleID
		path    string
	}{
		{"R8_worker_poll_interval_zero", R8, "worker.poll_interval"},
		{"R8_worker_lease_timeout_zero", R8, "worker.lease_timeout"},
		{"R8_worker_drain_timeout_zero", R8, "worker.drain_timeout"},
		{"R10_retention_keep_negative", R10, "retention.keep"},
		{"R10_retention_partition_interval_negative", R10, "retention.partition_interval"},
		{"R10_retention_precreate_negative", R10, "retention.precreate"},
		{"R13_defaults_timeout_zero", R13, "defaults.timeout"},
		{"R13_listener_timeout_zero", R13, "listeners[0].timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			path := fixture(tc.fixture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s failed: %v", path, err)
			}
			_, _, errs := Parse(data, filepath.Base(path), coverageEnvironment())
			if len(errs) != 1 || errs[0].Rule != tc.rule || errs[0].Path != tc.path {
				t.Fatalf("%s returned %+v, want one %s at %s", path, errs, tc.rule, tc.path)
			}
		})
	}
}

func TestDurationSubjectsHaveCommittedAcceptingTwins(t *testing.T) {
	tests := []struct {
		fixture string
		written string
	}{
		{"R8_ok_worker_durations_positive", "poll_interval:"},
		{"R8_ok_worker_durations_positive", "lease_timeout:"},
		{"R8_ok_worker_durations_positive", "drain_timeout:"},
		{"R10_ok_retention_durations_positive", "keep:"},
		{"R10_ok_retention_durations_positive", "partition_interval:"},
		{"R10_ok_retention_durations_positive", "precreate:"},
		{"R13_ok_timeouts_positive", "defaults:\n  timeout:"},
		{"R13_ok_timeouts_positive", "    timeout:"},
	}
	for _, tc := range tests {
		t.Run(tc.fixture+"/"+tc.written, func(t *testing.T) {
			path := filepath.Join(validCorpus, tc.fixture+fixtureExtension)
			data := readFixtureBytes(t, path)
			if !strings.Contains(string(data), tc.written) {
				t.Fatalf("%s does not independently write %q", path, tc.written)
			}
			cfg, warnings, errs := Parse(data, filepath.Base(path), coverageEnvironment())
			if cfg == nil || len(errs) != 0 || len(warnings) != 0 {
				t.Fatalf("%s returned config=%v warnings=%+v errors=%+v", path, cfg, warnings, errs)
			}
		})
	}
}

func TestBothMicrosecondSpellingsAreRepresentedInTheCorpus(t *testing.T) {
	rejected := fixture("R40_worker_poll_interval_greek_mu")
	rejectedData := readFixtureBytes(t, rejected)
	if !strings.Contains(string(rejectedData), "μs") || strings.Contains(string(rejectedData), "µs") {
		t.Fatalf("%s must contain Greek U+03BC and not micro sign U+00B5", rejected)
	}
	_, _, errs := Parse(rejectedData, filepath.Base(rejected), coverageEnvironment())
	if len(errs) != 1 || errs[0].Rule != R40 || !strings.Contains(errs[0].Msg, "us/µs") {
		t.Fatalf("Greek U+03BC fixture returned %+v, want one R40 naming the ratified U+00B5 spelling", errs)
	}

	accepted := filepath.Join(validCorpus, "R40_ok_duration_boundaries"+fixtureExtension)
	data := readFixtureBytes(t, accepted)
	if !strings.Contains(string(data), "µs") || strings.Contains(string(data), "μs") {
		t.Fatalf("%s must contain micro sign U+00B5 and not Greek U+03BC", accepted)
	}
	if cfg, warnings, errs := Parse(data, filepath.Base(accepted), coverageEnvironment()); cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		t.Fatalf("accepted micro-sign fixture returned config=%v warnings=%+v errors=%+v",
			cfg, warnings, errs)
	}
}
