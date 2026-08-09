package config

import (
	"fmt"
	"slices"
	"testing"
)

// staticRuleIDs and warningRuleIDs enumerate the inventory that fixture coverage is
// counted against. They live here because counting the inventory is all anything does
// with them: no production code consults a list of rules, it names the rule it raised.
var (
	staticRuleIDs = []RuleID{
		R1, R2, R3, R4, R5, R6, R7, R8, R9, R10,
		R11, R12, R13, R14, R15, R16, R17, R18, R19, R20,
		R21, R22, R23, R24, R25, R26, R27, R28, R29, R30,
		R31, R32, R33, R34, R35, R36, R37, R38, R39, R40,
		R41, R42, R43,
	}
	warningRuleIDs = []RuleID{W1, W2}
)

func TestRuleIDEnumeratesTheWholeInventory(t *testing.T) {
	if len(staticRuleIDs) != 43 {
		t.Fatalf("staticRuleIDs has %d entries, want 43", len(staticRuleIDs))
	}
	if len(warningRuleIDs) != 2 {
		t.Fatalf("warningRuleIDs has %d entries, want 2", len(warningRuleIDs))
	}

	seen := make(map[RuleID]bool, 45)
	for i, rule := range staticRuleIDs {
		if want := RuleID(fmt.Sprintf("R%d", i+1)); rule != want {
			t.Errorf("staticRuleIDs[%d] = %q, want %q", i, rule, want)
		}
		if seen[rule] {
			t.Errorf("rule %q is listed twice", rule)
		}
		seen[rule] = true
	}
	for i, rule := range warningRuleIDs {
		if want := RuleID(fmt.Sprintf("W%d", i+1)); rule != want {
			t.Errorf("warningRuleIDs[%d] = %q, want %q", i, rule, want)
		}
	}
}

// TestStructuralRuleIDsStayOutsideTheNumberedInventory keeps `Rule` meaningful now that
// values beyond R1-R43 and W1-W2 exist. The structural stages report conditions that are
// not numbered rules, so a coverage walker counting the inventory must be able to tell them
// apart by value rather than by knowing the list.
//
// It quantifies over structuralRuleIDs rather than over a list written out here, and pins
// that set's size, so a stage added later cannot join the constants without joining the
// claim.
func TestStructuralRuleIDsStayOutsideTheNumberedInventory(t *testing.T) {
	numbered := make(map[RuleID]bool, 45)
	for _, rule := range slices.Concat(staticRuleIDs, warningRuleIDs) {
		numbered[rule] = true
	}

	if len(structuralRuleIDs) != 7 {
		t.Fatalf("structuralRuleIDs holds %d rules; a stage was added or removed, so update this count with it",
			len(structuralRuleIDs))
	}
	// noRule is the absence of one rather than a structural condition, so it must never join the
	// set a coverage walker excludes -- a diagnostic carrying it would be excluded from the
	// inventory instead of failing it.
	if slices.Contains(structuralRuleIDs, noRule) {
		t.Error("noRule is in structuralRuleIDs, so an untagged diagnostic would pass as a structural one")
	}
	for _, structural := range structuralRuleIDs {
		if numbered[structural] {
			t.Errorf("structural rule %q is also in the numbered inventory, so a coverage walker would count it as a rule", structural)
		}
		if structural == "" {
			t.Error("a structural rule is the empty RuleID, which would make a diagnostic untraceable")
		}
	}
}
