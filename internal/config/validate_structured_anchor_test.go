package config

import "testing"

func TestStructuredRulesDeclareEveryAnchorClassInTheSharedTable(t *testing.T) {
	want := map[RuleID]map[semanticAnchorUse]anchorClass{
		R3:  {structuredValueUse: valueAnchor},
		R5:  {structuredValueUse: valueAnchor},
		R19: {headerNameUse: keyAnchor, headerValueUse: valueAnchor},
		R21: {headerNameUse: keyAnchor},
		R24: {structuredValueUse: valueAnchor},
		R26: {structuredValueUse: valueAnchor},
		R29: {listElementUse: elementAnchor},
		R30: {structuredValueUse: valueAnchor},
		R32: {structuredValueUse: valueAnchor, listElementUse: elementAnchor},
		R33: {structuredValueUse: valueAnchor, listElementUse: elementAnchor},
		R36: {structuredValueUse: valueAnchor},
		R38: {listElementUse: elementAnchor},
	}
	for rule, uses := range want {
		for use, class := range uses {
			got, assigned := stageHAnchorClass[rule][use]
			if !assigned {
				t.Errorf("%s has no %q anchor assignment", rule, use)
			} else if got != class {
				t.Errorf("%s %q anchor = %q, want %q", rule, use, got, class)
			}
		}
	}
}
