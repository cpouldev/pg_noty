package config

import (
	"path/filepath"
	"strings"
	"testing"
)

type semanticFixture struct {
	rule RuleID
	path string
}

var stageHRejectingFixtures = map[string]semanticFixture{
	"R1_version_not_one":                     {R1, "version"},
	"R2_instance_bad_charset":                {R2, "instance"},
	"R4_schema_reserved_prefix":              {R4, "database.schema"},
	"R4_schema_over_63_bytes":                {R4, "database.schema"},
	"R6_worker_concurrency_zero":             {R6, "worker.concurrency"},
	"R6_worker_concurrency_5000":             {R6, "worker.concurrency"},
	"R7_worker_batch_size_zero":              {R7, "worker.batch_size"},
	"R8_worker_poll_interval_zero":           {R8, "worker.poll_interval"},
	"R10_retention_precreate_negative":       {R10, "retention.precreate"},
	"R13_defaults_timeout_zero":              {R13, "defaults.timeout"},
	"R14_retry_max_attempts_zero":            {R14, "defaults.retry.max_attempts"},
	"R14_listener_retry_max_attempts_zero":   {R14, "listeners[0].retry.max_attempts"},
	"R15_retry_backoff_unknown":              {R15, "defaults.retry.backoff"},
	"R15_listener_retry_backoff_unknown":     {R15, "listeners[0].retry.backoff"},
	"R17_retry_max_interval_zero":            {R17, "defaults.retry.max_interval"},
	"R17_listener_retry_max_interval_zero":   {R17, "listeners[0].retry.max_interval"},
	"R18_retry_jitter_not_boolean":           {R18, "defaults.retry.jitter"},
	"R18_listener_retry_jitter_not_boolean":  {R18, "listeners[0].retry.jitter"},
	"R23_listener_name_bad_charset":          {R23, "listeners[0].name"},
	"R25_listener_enabled_not_boolean":       {R25, "listeners[0].enabled"},
	"R31_payload_mode_unknown":               {R31, "listeners[0].payload.mode"},
	"R34_include_old_not_boolean":            {R34, "listeners[0].payload.include_old"},
	"R35_max_bytes_zero":                     {R35, "listeners[0].payload.max_bytes"},
	"R37_destination_method_unknown":         {R37, "listeners[0].destination.method"},
	"R39_listener_concurrency_zero":          {R39, "listeners[0].concurrency"},
	"R39_listener_concurrency_1025":          {R39, "listeners[0].concurrency"},
	"R40_retention_precreate_days":           {R40, "retention.precreate"},
	"R1_version_absent_plus_batch_size_zero": {R1, "version"},
}

var stageHAcceptingFixtures = []string{
	"R1_ok_version_one",
	"R2_ok_instance_max_length",
	"R4_ok_schema_63_bytes",
	"R6_ok_worker_concurrency_lower_bound",
	"R6_ok_worker_concurrency_upper_bound",
	"R7_ok_worker_batch_size_upper_bound",
	"R8_ok_worker_durations_positive",
	"R10_ok_retention_durations_positive",
	"R13_ok_timeouts_positive",
	"R14_ok_retry_max_attempts_one",
	"R15_ok_retry_backoffs",
	"R17_ok_retry_max_interval_positive",
	"R18_ok_retry_jitter_boolean",
	"R23_ok_listener_name_charset",
	"R25_ok_listener_enabled_boolean",
	"R31_ok_payload_modes",
	"R34_ok_include_old_boolean",
	"R35_ok_max_bytes_positive",
	"R37_ok_destination_methods",
	"R39_ok_listener_concurrency_boundaries",
	"R40_ok_duration_boundaries",
}

func TestEveryStageHRejectingFixtureRaisesExactlyItsClaim(t *testing.T) {
	for name, want := range stageHRejectingFixtures {
		t.Run(name, func(t *testing.T) {
			path := fixture(name)
			_, _, diags := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())
			wantCount := 1
			if name == "R1_version_absent_plus_batch_size_zero" {
				wantCount = 2
			}
			if len(diags) != wantCount {
				t.Fatalf("got %d diagnostics %q, want %d", len(diags), messagesOf(diags), wantCount)
			}
			if !anyCarries(diags, want.rule) {
				t.Errorf("rules in %q do not include %s", messagesOf(diags), want.rule)
			}
			for _, diag := range diags {
				if diag.Msg == "" || diag.Path == "" {
					t.Errorf("non-actionable diagnostic: %+v", diag)
				}
			}
			if name != "R1_version_absent_plus_batch_size_zero" &&
				diags[0].Path != want.path {
				t.Errorf("Path = %q, want %q", diags[0].Path, want.path)
			}
		})
	}
}

func TestEveryStageHEntryHasNamedRejectingAndAcceptingFixtures(t *testing.T) {
	for rule := range stageHRuleEntries {
		rejecting, accepting := false, false
		for name := range stageHRejectingFixtures {
			rejecting = rejecting || strings.HasPrefix(name, string(rule)+"_")
		}
		for _, name := range stageHAcceptingFixtures {
			accepting = accepting || strings.HasPrefix(name, string(rule)+"_ok_")
		}
		if !rejecting || !accepting {
			t.Errorf("%s fixture pair: rejecting=%t accepting=%t", rule, rejecting, accepting)
		}
	}
	// Each owned entry needs one accepting fixture; R6 needs a second because one worker scalar
	// cannot simultaneously write both ends of its 1..1024 boundary.
	if want := len(stageHRuleEntries) + 1; len(stageHAcceptingFixtures) != want {
		t.Errorf("%d accepting fixtures, want %d derived from entries plus R6's second boundary",
			len(stageHAcceptingFixtures), want)
	}
	// Twenty base rejections + the extra R4/R6/R39 boundary cases + the combined R1/R7 policy
	// fixture + four listener retry extents added by the independent partition audits.
	if want := len(stageHRuleEntries) + 3 + 1 + 4; len(stageHRejectingFixtures) != want {
		t.Errorf("%d rejecting fixtures, want the derived corpus extent %d", len(stageHRejectingFixtures), want)
	}
}
