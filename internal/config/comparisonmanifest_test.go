package config

import (
	"path/filepath"
	"strings"
	"testing"
)

type comparisonFixturePair struct {
	rejecting string
	accepting string
}

const ownedComparisonRuleCount = 5

var comparisonFixtureManifest = map[RuleID]comparisonFixturePair{
	R9: {
		rejecting: "R9_lease_timeout_equals_listener_timeout",
		accepting: "R9_ok_lease_timeout_strictly_above_listener_timeout",
	},
	R11: {
		rejecting: "R11_retention_keep_below_partition_interval",
		accepting: "R11_ok_retention_keep_equals_partition_interval",
	},
	R12: {
		rejecting: "R12_retention_precreate_below_partition_interval",
		accepting: "R12_ok_retention_precreate_equals_partition_interval",
	},
	R16: {
		rejecting: "R16_retry_initial_exceeds_max",
		accepting: "R16_ok_retry_initial_equals_max",
	},
	R39: {
		rejecting: "R39_listener_concurrency_exceeds_worker",
		accepting: "R39_ok_listener_concurrency_boundaries",
	},
}

func TestTheComparisonManifestNamesEveryOwnedRaisingAndCommittedAcceptingFixture(t *testing.T) {
	if len(comparisonFixtureManifest) != ownedComparisonRuleCount {
		t.Fatalf("manifest holds %d rules, want exactly %d", len(comparisonFixtureManifest),
			ownedComparisonRuleCount)
	}

	for rule, pair := range comparisonFixtureManifest {
		t.Run(string(rule), func(t *testing.T) {
			assertComparisonFixtureNames(t, rule, pair)

			rejecting := filepath.Join(invalidCorpus, pair.rejecting+fixtureExtension)
			_, _, errs := Parse(readFixtureBytes(t, rejecting), filepath.Base(rejecting),
				corpusEnvironment())
			if len(errs) != 1 || errs[0].Rule != rule {
				t.Fatalf("%s returned %+v, want exactly one %s", rejecting, errs, rule)
			}

			accepting := filepath.Join(validCorpus, pair.accepting+fixtureExtension)
			cfg, warnings, errs := Parse(readFixtureBytes(t, accepting), filepath.Base(accepting),
				corpusEnvironment())
			if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
				t.Fatalf("%s returned config=%v warnings=%+v errors=%+v, want a clean config",
					accepting, cfg, warnings, errs)
			}
			assertComparisonBoundary(t, rule, cfg)
		})
	}
}

func assertComparisonFixtureNames(t *testing.T, rule RuleID, pair comparisonFixturePair) {
	t.Helper()
	rejectingPrefix := string(rule) + "_"
	acceptingPrefix := string(rule) + "_ok_"
	if !strings.HasPrefix(pair.rejecting, rejectingPrefix) ||
		strings.HasPrefix(pair.rejecting, acceptingPrefix) {
		t.Errorf("rejecting fixture %q does not follow %s<slug>", pair.rejecting, rejectingPrefix)
	}
	if !strings.HasPrefix(pair.accepting, acceptingPrefix) {
		t.Errorf("accepting fixture %q does not follow %s<slug>", pair.accepting, acceptingPrefix)
	}
}

func assertComparisonBoundary(t *testing.T, rule RuleID, cfg *Config) {
	t.Helper()
	switch rule {
	case R9:
		if cfg.Worker.LeaseTimeout <= cfg.Listeners[0].Delivery.Timeout {
			t.Errorf("lease %s is not strictly above listener timeout %s",
				cfg.Worker.LeaseTimeout, cfg.Listeners[0].Delivery.Timeout)
		}
	case R11:
		if cfg.Retention.Keep != cfg.Retention.PartitionInterval {
			t.Errorf("keep %s != partition interval %s",
				cfg.Retention.Keep, cfg.Retention.PartitionInterval)
		}
	case R12:
		if cfg.Retention.Precreate != cfg.Retention.PartitionInterval {
			t.Errorf("precreate %s != partition interval %s",
				cfg.Retention.Precreate, cfg.Retention.PartitionInterval)
		}
	case R16:
		retry := cfg.Listeners[0].Delivery.Retry
		if retry.InitialInterval != retry.MaxInterval {
			t.Errorf("initial interval %s != max interval %s",
				retry.InitialInterval, retry.MaxInterval)
		}
	case R39:
		if cfg.Listeners[1].Delivery.Concurrency != cfg.Worker.Concurrency {
			t.Errorf("listener concurrency %d != worker concurrency %d",
				cfg.Listeners[1].Delivery.Concurrency, cfg.Worker.Concurrency)
		}
	default:
		t.Fatalf("manifest contains unowned comparison rule %s", rule)
	}
}
