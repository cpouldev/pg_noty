package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type policyFinding struct {
	rule    RuleID
	message string
}

type policyOutcome struct {
	cfg      *Config
	warnings Warnings
	errs     Errors
}

type stagePolicyCase struct {
	stage        string
	policy       string
	run          func(*testing.T) policyOutcome
	want         []policyFinding
	warningRules []RuleID
}

func TestAllNineStagePoliciesAreOneEndToEndTable(t *testing.T) {
	for _, tc := range stagePolicyCases() {
		t.Run(tc.stage+" "+tc.policy, func(t *testing.T) {
			assertPolicyOutcome(t, tc.run(t), tc.want, tc.warningRules)
		})
	}
}

func TestAC13SyntaxFailureSuppressesFourSemanticMistakes(t *testing.T) {
	got := syntaxPolicyCase(t)
	if got.cfg != nil || len(got.warnings) != 0 || len(got.errs) != 1 {
		t.Fatalf("config=%v warnings=%+v errors=%+v, want one syntax diagnostic",
			got.cfg, got.warnings, got.errs)
	}
	if diag := got.errs[0]; diag.Rule != RuleSyntax || diag.Line == 0 || diag.Col == 0 ||
		!strings.Contains(diag.Msg, "sequence end token") {
		t.Errorf("syntax diagnostic = %+v, want positioned stage-B sequence error", diag)
	}
}

func TestAC29StopsAtTwoReferencesThenReportsExactlyThreeLaterMistakes(t *testing.T) {
	path := fixture("R41_ac29_two_references_three_later")
	data := readFixtureBytes(t, path)
	tests := []struct {
		name string
		env  EnvLookup
		want []policyFinding
	}{
		{"unset", MapEnv(nil), []policyFinding{
			{RuleInterpolate, "DATABASE_URL"}, {RuleInterpolate, "LISTENER_TIMEOUT"},
		}},
		{"set", MapEnv(map[string]string{
			"DATABASE_URL": "postgres://noty@db.internal/noty", "LISTENER_TIMEOUT": "10s",
		}), []policyFinding{
			{R41, "unknown field"}, {R36, "http or https"}, {R37, "POST, PUT or PATCH"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, warnings, errs := Parse(data, filepath.Base(path), tc.env)
			if cfg != nil || len(warnings) != 0 || len(errs) != len(tc.want) {
				t.Fatalf("config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
			}
			for i, want := range tc.want {
				if errs[i].Rule != want.rule || !strings.Contains(errs[i].Msg, want.message) {
					t.Errorf("diagnostic %d = %+v, want %s containing %q",
						i, errs[i], want.rule, want.message)
				}
			}
		})
	}
}

func TestAC12StillReportsFiveMistakesThroughAllNineStages(t *testing.T) {
	path := fixture("R41_five_independent_mistakes")
	_, warnings, errs := Parse(readFixtureBytes(t, path), filepath.Base(path), coverageEnvironment())
	want := []RuleID{R41, R14, R15, R33, R36}
	if len(warnings) != 0 || len(errs) != len(want) {
		t.Fatalf("warnings=%+v errors=%+v, want five diagnostics and no warnings", warnings, errs)
	}
	for i, rule := range want {
		if errs[i].Rule != rule {
			t.Errorf("diagnostic %d Rule = %s, want %s", i, errs[i].Rule, rule)
		}
	}
}

func assertPolicyOutcome(t *testing.T, got policyOutcome, want []policyFinding,
	warningRules []RuleID,
) {
	t.Helper()
	if got.cfg != nil {
		t.Error("policy case with diagnostics returned a configuration")
	}
	if len(got.errs) != len(want) {
		t.Fatalf("got %d diagnostics %+v, want %d", len(got.errs), got.errs, len(want))
	}
	for i, expected := range want {
		if got.errs[i].Rule != expected.rule ||
			!strings.Contains(got.errs[i].Msg, expected.message) {
			t.Errorf("diagnostic %d = %+v, want %s containing %q",
				i, got.errs[i], expected.rule, expected.message)
		}
	}
	if len(got.warnings) != len(warningRules) {
		t.Fatalf("got %d warnings %+v, want exactly %d", len(got.warnings), got.warnings,
			len(warningRules))
	}
	actual := sortedPolicyWarnings(got.warnings)
	expectedRules := append([]RuleID(nil), warningRules...)
	slices.Sort(expectedRules)
	for i, rule := range expectedRules {
		if actual[i].rule != rule ||
			!strings.Contains(actual[i].message, policyWarningMessage(rule)) {
			t.Errorf("warning %d = %+v, want %s containing %q",
				i, actual[i], rule, policyWarningMessage(rule))
		}
	}
}

func sortedPolicyWarnings(warnings Warnings) []policyFinding {
	findings := make([]policyFinding, len(warnings))
	for i, warning := range warnings {
		findings[i] = policyFinding{warning.Rule, warning.Msg}
	}
	slices.SortFunc(findings, func(a, b policyFinding) int {
		if a.rule < b.rule {
			return -1
		}
		if a.rule > b.rule {
			return 1
		}
		return strings.Compare(a.message, b.message)
	})
	return findings
}

func policyWarningMessage(rule RuleID) string {
	switch rule {
	case W1:
		return "interpolated environment reference"
	case W2:
		return "drain_timeout is smaller"
	default:
		return "unknown warning rule"
	}
}
