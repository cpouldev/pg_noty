package config

import (
	"slices"
	"testing"
)

func TestPayloadPrecedenceClosesEveryModeAndPresenceCombination(t *testing.T) {
	tests := []struct {
		mode             string
		columns, exclude bool
		want             []RuleID
	}{
		{payloadModeFull, false, false, nil},
		{payloadModeFull, true, false, []RuleID{R32}},
		{payloadModeFull, false, true, nil},
		{payloadModeFull, true, true, []RuleID{R33}},
		{payloadModeCols, false, false, []RuleID{R32}},
		{payloadModeCols, true, false, nil},
		{payloadModeCols, false, true, []RuleID{R32, R33}},
		{payloadModeCols, true, true, []RuleID{R33}},
		{payloadModeKeys, false, false, nil},
		{payloadModeKeys, true, false, []RuleID{R32}},
		{payloadModeKeys, false, true, []RuleID{R33}},
		{payloadModeKeys, true, true, []RuleID{R33}},
		{"sideways", false, false, []RuleID{R31}},
		{"sideways", true, false, []RuleID{R31}},
		{"sideways", false, true, []RuleID{R31}},
		{"sideways", true, true, []RuleID{R31, R33}},
		{"", false, false, nil},
		{"", true, false, []RuleID{R32}},
		{"", false, true, nil},
		{"", true, true, []RuleID{R33}},
	}
	for _, tc := range tests {
		name := tc.mode
		if name == "" {
			name = "omitted"
		}
		name += "/columns=" + decisionBoolText(tc.columns) + "/exclude=" + decisionBoolText(tc.exclude)
		t.Run(name, func(t *testing.T) {
			var payload string
			if tc.mode != "" {
				payload += "      mode: " + tc.mode + "\n"
			}
			if tc.columns {
				payload += "      columns: [id]\n"
			}
			if tc.exclude {
				payload += "      exclude: [internal_note]\n"
			}
			lines := []string{requiredName, requiredTable, requiredOperations}
			if payload != "" {
				lines = append(lines, "    payload:\n"+payload)
			}
			lines = append(lines, requiredDestination)
			document := aListenerOf(lines...)
			_, diags := stageH(t, document)
			got := make([]RuleID, 0, len(diags))
			for _, diag := range diags {
				got = append(got, diag.Rule)
			}
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("rules = %v, want %v; diagnostics %q", got, want, messagesOf(diags))
			}
		})
	}
}

func decisionBoolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
