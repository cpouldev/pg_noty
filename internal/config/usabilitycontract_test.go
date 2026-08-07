package config

import (
	"fmt"
	"slices"
	"testing"
)

func TestEveryDiagnosticCategoryOwnsAUsabilityContract(t *testing.T) {
	rules := slices.Concat(staticRuleIDs, warningRuleIDs, structuralRuleIDs)
	if len(diagnosticContractSignals) != len(rules) {
		t.Errorf("usability table has %d RuleIDs, inventory has %d",
			len(diagnosticContractSignals), len(rules))
	}
	for _, rule := range rules {
		signals, declared := diagnosticContractSignals[rule]
		if !declared || len(signals) == 0 {
			t.Errorf("%s has no usability contract", rule)
		}
	}
	for rule := range diagnosticContractSignals {
		if !slices.Contains(rules, rule) {
			t.Errorf("usability table declares unknown RuleID %q", rule)
		}
	}
}

func TestUsabilityRejectsTheReviewerConcreteNounCounterexample(t *testing.T) {
	assertUnusableDiagnostic(t, Error{
		Path: "value",
		Msg:  "the configuration document must be considered problematic",
	})
}

func TestUsabilityRejectsTheSameRuleEnvironmentVariablePrefix(t *testing.T) {
	assertUnusableDiagnostic(t, Error{
		Rule: RuleInterpolate,
		Path: "value",
		Msg:  "environment variable value encountered an issue",
	})
}

func TestUsabilityRejectsEveryContractSignalWithFiller(t *testing.T) {
	fillers := []string{
		" value encountered an issue",
		" input encountered an issue",
		" item encountered an issue",
	}
	for rule, signals := range diagnosticContractSignals {
		for _, signal := range signals {
			for _, filler := range fillers {
				message := signal.example + filler
				t.Run(string(rule)+"/"+message, func(t *testing.T) {
					assertUnusableDiagnostic(t, Error{
						Rule: rule, Path: "value", Msg: message,
					})
				})
			}
		}
	}
}

func TestUsabilityRejectsConcreteNounsWithoutACondition(t *testing.T) {
	relations := []string{
		"%s must be considered problematic",
		"%s should be considered problematic",
		"%s cannot be considered satisfactory",
		"%s is expected to be considered problematic",
		"%s is required to be considered problematic",
		"%s is only considered problematic",
		"%s is not considered satisfactory",
	}
	subjects := []struct {
		name string
		rule RuleID
	}{
		{name: "configuration document", rule: RuleDocument},
		{name: "header", rule: R19},
		{name: "token", rule: RuleSyntax},
		{name: "schema", rule: R4},
		{name: "mapping", rule: RuleShape},
		{name: "URL", rule: R36},
	}

	for _, relation := range relations {
		for _, subject := range subjects {
			message := fmt.Sprintf(relation, subject.name)
			t.Run(string(subject.rule)+"/"+message, func(t *testing.T) {
				assertUnusableDiagnostic(t, Error{
					Rule: subject.rule, Path: "value", Msg: message,
				})
			})
		}
	}
}

func TestUsabilityContractDoesNotTransferBetweenRules(t *testing.T) {
	tests := []Error{
		{Rule: R1, Path: "value", Msg: `unknown field "value"`},
		{Rule: R41, Path: "value", Msg: "must equal 1"},
		{Rule: RuleDocument, Path: "value", Msg: "expected an integer"},
		{Rule: RuleDecode, Path: "value", Msg: "configuration root must be a mapping"},
		{Rule: R19, Path: "value", Msg: `missing required key "header"`},
		{Rule: R36, Path: "value", Msg: "header name must be a valid HTTP field name"},
	}
	for _, diagnostic := range tests {
		t.Run(string(diagnostic.Rule)+"/"+diagnostic.Msg, func(t *testing.T) {
			assertUnusableDiagnostic(t, diagnostic)
		})
	}
}
