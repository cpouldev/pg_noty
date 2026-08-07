package config

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// What the shape table declares *about* its keys and levels, as opposed to which keys and levels
// it declares (schema_test.go). Every one of these is a fact stage F reads as data rather than
// deciding in code, so a fact that drifted would move a caret, a rule tag or a hint with nothing
// else failing.

// requiredKeyRules is the contract's seven required keys with the rule that reports each of them
// missing. It is one declaration read by the two tests below, because the set and the rule each
// member carries are two claims about one list and a second copy of the list would let them drift.
var requiredKeyRules = map[string]RuleID{
	"version":                     R1,
	"database.url":                R3,
	"listeners":                   R22,
	"listeners[].name":            R23,
	"listeners[].table":           R26,
	"listeners[].operations":      R27,
	"listeners[].destination.url": R36,
}

// TestExactlySevenKeysAreRequired pins the count the specification states, over the whole
// table rather than over the levels this test remembered to visit.
func TestExactlySevenKeysAreRequired(t *testing.T) {
	want := slices.Sorted(maps.Keys(requiredKeyRules))

	got := pathsWhere(t, func(spec keySpec) bool { return spec.required() })

	if len(want) != 7 {
		t.Fatalf("the expectation itself names %d keys, want the seven the contract requires", len(want))
	}
	if !slices.Equal(got, want) {
		t.Errorf("required keys %v, want exactly %v", got, want)
	}
}

// TestEveryRequiredKeyNamesTheRuleThatReportsItMissing is the other claim about that list. A
// required key reported under the wrong rule tells its author the right thing while telling Step
// 13's inventory the wrong one, and the inventory counts rules rather than diagnostics -- so the
// rule it credits would gain a fixture it does not own and the rule it owes one would lose it.
func TestEveryRequiredKeyNamesTheRuleThatReportsItMissing(t *testing.T) {
	for path, spec := range declaredKeys(t) {
		want, isRequired := requiredKeyRules[path]

		if spec.required() != isRequired {
			t.Errorf("%s declares required = %t, want %t", path, spec.required(), isRequired)
			continue
		}
		if isRequired && spec.presence != want {
			t.Errorf("%s reports missing under %q, want %q", path, spec.presence, want)
		}
	}
}

// TestOnlyTheTwoListsTheContractRequiresToHoldSomethingSaySo pins the asymmetry AC #31 and AC #16
// are two halves of: an empty `listeners` list is the deliberate way to say this instance manages
// nothing, while an empty `operations` mapping is a listener that reacts to no statement. A key
// swept into the emptiness check by accident would reject the first; one dropped from it would
// accept the second.
//
// The two lists whose emptiness no numbered rule names -- `payload.columns` and
// `signing.secrets` -- are absent deliberately: the rules that judge their entries are Step 9's,
// and declaring their emptiness here would implement them one step early.
func TestOnlyTheTwoListsTheContractRequiresToHoldSomethingSaySo(t *testing.T) {
	want := map[string]RuleID{
		"listeners[].operations":                R27,
		"listeners[].operations.update.columns": R29,
		"listeners[].operations.insert.columns": R29,
		"listeners[].operations.delete.columns": R29,
	}

	for path, spec := range declaredKeys(t) {
		expected, declared := want[path]

		if (spec.emptiness != noRule) != declared {
			t.Errorf("%s declares emptiness = %q, want declared = %t", path, spec.emptiness, declared)
			continue
		}
		if declared && spec.emptiness != expected {
			t.Errorf("%s reports emptiness under %q, want %q", path, spec.emptiness, expected)
		}
	}
	if _, swept := want["listeners[].payload.columns"]; swept {
		t.Error("payload.columns declares its emptiness here; R32 is Step 9's rule")
	}
}

// TestTheOnlyKeyLegalBeneathOneSiblingIsTheColumnFilter scopes R29's legality half to the one key
// the contract states it for. The three operations share one filter shape, so `columns` is
// declared at all three and legal at one; a second key acquiring that qualifier would need its own
// rule rather than R29 by default.
func TestTheOnlyKeyLegalBeneathOneSiblingIsTheColumnFilter(t *testing.T) {
	for path, spec := range declaredKeys(t) {
		if spec.legalUnder == "" {
			if spec.legality != noRule {
				t.Errorf("%s names rule %q for being written elsewhere but no sibling it is legal under",
					path, spec.legality)
			}
			continue
		}
		if leafOf(path) != "columns" || spec.legalUnder != updateOperation || spec.legality != R29 {
			t.Errorf("%s is legal only under %q by rule %q; the contract states that of operations.update.columns alone",
				path, spec.legalUnder, spec.legality)
		}
	}
}

// TestOnlyTheOperationsLevelRefusesAnUndeclaredNameInItsOwnWords keeps R41's reach whole. Every
// level's key list is a vocabulary that R41 closes; the operations level's three names are a
// numbered rule of their own (R28), and a second level quietly acquiring a refusal would move its
// unknown keys out of R41's count without anything saying so.
func TestOnlyTheOperationsLevelRefusesAnUndeclaredNameInItsOwnWords(t *testing.T) {
	for name, level := range schemaLevels {
		refusal := level.refusalOfAnUndeclaredName()

		if name == levelOperations {
			if refusal.rule != R28 || !strings.Contains(refusal.hint, updateOperation) {
				t.Errorf("the operations level refuses under %q hinting %q, want R28 naming the three statements",
					refusal.rule, refusal.hint)
			}
			continue
		}
		if refusal.rule != R41 || refusal.hint != "" {
			t.Errorf("level %q refuses an undeclared name under %q hinting %q, want R41 and no hint of its own",
				name, refusal.rule, refusal.hint)
		}
	}
}

// TestNoLeafNameDeclaresTwoNumberedConversions pins conversionRuleAt's precondition, which its own
// comment used to claim "schema tests pin" without naming one -- and none did.
//
// The lookup ranges over schemaLevels, which is a map, so two levels declaring a numbered conversion
// for one leaf name would let Go's per-range randomisation choose which RuleID a diagnostic carries.
// That is the determinism NFR failing in the one place a golden would show it.
func TestNoLeafNameDeclaresTwoNumberedConversions(t *testing.T) {
	declaredBy := map[string]RuleID{}
	converted := 0

	for name, level := range schemaLevels {
		for _, spec := range level.keys {
			if spec.conversion == noRule {
				continue
			}
			converted++
			if first, twice := declaredBy[spec.name]; twice && first != spec.conversion {
				t.Errorf("leaf %q is declared with conversion %q and %q (the second at level %q); "+
					"conversionRuleAt ranges over a map and would choose between them per run",
					spec.name, first, spec.conversion, name)
			}
			declaredBy[spec.name] = spec.conversion
		}
	}

	// Without this the claim is satisfied by a table declaring no numbered conversion at all.
	if converted == 0 {
		t.Fatal("no key declares a numbered conversion, so this proves nothing about the lookup")
	}
}
