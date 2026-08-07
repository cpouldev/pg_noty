package config

import (
	"path/filepath"
	"slices"
	"testing"
)

type coverageEnvironmentScenario struct {
	path   string
	values map[string]string
	rules  []RuleID
}

var coverageEnvironmentScenarios = map[string]coverageEnvironmentScenario{
	"R41_ac29_two_references_three_later.yaml": {
		path: filepath.Join("testdata", "invalid", "R41_ac29_two_references_three_later.yaml"),
		values: map[string]string{
			"DATABASE_URL": "postgres://noty@db.internal/noty", "LISTENER_TIMEOUT": "10s",
		},
		rules: []RuleID{R36, R37, R41},
	},
	"decode_interpolated_value_of_the_wrong_type.yaml": {
		path: filepath.Join("testdata", "invalid",
			"decode_interpolated_value_of_the_wrong_type.yaml"),
		values: map[string]string{"WORKER_CONCURRENCY": "abc"},
	},
	"R9_lease_timeout_reference_below_listener_timeout.yaml": {
		path: filepath.Join("testdata", "invalid", "step11",
			"R9_lease_timeout_reference_below_listener_timeout.yaml"),
		values: map[string]string{"LEASE": "5s"},
		rules:  []RuleID{R9},
	},
	"unresolved_before_later_mistakes.yaml": {
		path: filepath.Join("testdata", "invalid", "unresolved_before_later_mistakes.yaml"),
		values: map[string]string{
			"DATABASE_URL": "postgres://noty@db.internal/noty", "LISTENER_TIMEOUT": "10s",
		},
		rules: []RuleID{R36, R37, R41},
	},
	"W2_secret_outputs.yaml": {
		path: filepath.Join("testdata", "warnings", "W2_secret_outputs.yaml"),
		values: map[string]string{
			"DATABASE_URL":       databaseURLWithStep13Secret(),
			"SIGNING_SECRET":     step13Secret + "\r" + step13PhysicalTail,
			"SIGNING_SECRET_OLD": step13Secret + "\r" + step13PhysicalTail,
		},
		rules: []RuleID{R38, W2},
	},
}

func TestEveryEnvironmentDependentCoverageScenarioIsExplicitAndExact(t *testing.T) {
	if len(coverageEnvironmentScenarios) != 5 {
		t.Fatalf("%d coverage environment scenarios, want the five explicit fixtures",
			len(coverageEnvironmentScenarios))
	}
	for name, scenario := range coverageEnvironmentScenarios {
		t.Run(name, func(t *testing.T) {
			data := readFixtureBytes(t, scenario.path)
			_, warnings, errs := Parse(data, name, coverageEnvironmentFor(scenario.path))
			all := append(append(Errors(nil), errs...), errorsFromWarnings(warnings)...)
			observed, _ := observedCoverage(all)
			want := compactRules(scenario.rules)
			if !slices.Equal(observed, want) {
				t.Errorf("canonical environment observes %v, want %v", observed, want)
			}
			declared := readRuleManifest(t, scenario.path)
			if prefix := filenameRule(filepath.Base(scenario.path)); prefix != "" {
				declared = append(declared, prefix)
			}
			if declared = compactRules(declared); !slices.Equal(declared, want) {
				t.Errorf("declared rules = %v, want canonical environment rules %v", declared, want)
			}
		})
	}
}
