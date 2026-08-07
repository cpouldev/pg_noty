package config

import "testing"

func TestMeaningfulHintSatisfiesTheUsabilityAlternative(t *testing.T) {
	tests := []struct {
		name string
		rule RuleID
	}{
		{name: "numbered RuleID", rule: R1},
		{name: "structural RuleID", rule: RuleDocument},
		{name: "no RuleID", rule: noRule},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertUsableDiagnostic(t, Error{
				Rule: tc.rule, Path: "value",
				Msg:  "the supplied input produced an unsatisfactory outcome",
				Hint: "set value to one of the documented choices",
			})
		})
	}
}

func TestWhitespaceOnlyHintCannotSatisfyTheUsabilityAlternative(t *testing.T) {
	assertUnusableDiagnostic(t, Error{
		Rule: R1, Path: "value",
		Msg:  "the supplied input produced an unsatisfactory outcome",
		Hint: " \t\n ",
	})
}
