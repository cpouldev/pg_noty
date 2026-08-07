package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The rule entries this step owns, reconciled against the task's rule-ownership table rather than
// against its own Expected Output, and tied to the corpus that covers them.
//
// The two disagree on one entry and the table governs. The Expected Output lists R27 among "the
// presence halves of R1/R3/R23/R26/R27/R36"; the ownership table's arithmetic names seven
// deliberately split rules -- R1, R3, R23, R26, R29, R36 and R39 -- and R27 is not among them, so
// R27 is a **whole** rule owned here. Both of its clauses are implemented: presence, through the
// required-key walk, and "with at least one entry", through the emptiness rule the table declares
// on the key. Resolving it the other way would leave the at-least-one-entry clause owned by nobody.

// ownedRuleEntries is the eleven the ownership table assigns to this step. R41, R20, R22, R27 and
// R28 are whole rules; the rest are the halves named beside them.
var ownedRuleEntries = map[RuleID]string{
	R41: "no unknown keys at any nesting level",
	R20: "no two header names differing only in case",
	R22: "the listeners key is present; an empty list is valid",
	R27: "operations is present and names at least one statement",
	R28: "operation keys are insert, update or delete",
	R1:  "the presence half: version is written",
	R3:  "the presence half: database.url is written",
	R23: "the presence half: a listener is named",
	R26: "the presence half: a listener names a table",
	R36: "the presence half: a destination has a url",
	R29: "the legality half: a column filter is legal only under update",
}

// TestThisStepOwnsElevenRuleEntries pins the count the ownership table's arithmetic depends on:
// 1 + 11 + 20 + 12 + 5 = 49 entries for 42 rules across seven splits. An entry added or dropped
// here without the table moving with it breaks that reconciliation silently.
func TestThisStepOwnsElevenRuleEntries(t *testing.T) {
	if len(ownedRuleEntries) != 11 {
		t.Errorf("this step claims %d rule entries, want the 11 the ownership table assigns it",
			len(ownedRuleEntries))
	}
}

// TestEveryOwnedRuleHasARejectingAndAnAcceptingFixture is AC #28's requirement applied to this
// step's own share of it, one step early: Step 13 inventories the corpus by unioning filename
// prefixes with observed `Rule` values, so a rule with no fixture -- or one whose fixture is
// misnamed -- is a gap that surfaces there rather than here unless it is asserted here.
func TestEveryOwnedRuleHasARejectingAndAnAcceptingFixture(t *testing.T) {
	rejecting := fixtureNamesIn(t, invalidCorpus)
	accepting := fixtureNamesIn(t, validCorpus)

	for rule, clause := range ownedRuleEntries {
		t.Run(string(rule), func(t *testing.T) {
			if !hasPrefixed(rejecting, string(rule)+"_") {
				t.Errorf("no fixture named %s_*.yaml under %s rejects %q", rule, invalidCorpus, clause)
			}
			if !hasPrefixed(accepting, string(rule)+"_ok_") {
				t.Errorf("no fixture named %s_ok_*.yaml under %s accepts %q", rule, validCorpus, clause)
			}
		})
	}
}

// TestEveryOwnedRuleIsRaisedByTheCorpusItClaims is the other direction, and the one a naming
// convention cannot give: a fixture called `R27_…` proves only that somebody meant it to cover
// R27. This asserts the rule each fixture's name claims is a rule its diagnostics actually carry,
// so a fixture renamed or rewritten past its own rule fails here.
func TestEveryOwnedRuleIsRaisedByTheCorpusItClaims(t *testing.T) {
	raised := make(map[RuleID]bool, len(ownedRuleEntries))

	for name, want := range stageFCorpus {
		path := fixture(name)
		_, _, got := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())

		if len(got) != len(want) {
			t.Fatalf("%s produced %d diagnostics, want %d", name, len(got), len(want))
		}
		for _, diag := range got {
			raised[diag.Rule] = true
		}

		claimed, claimsOne := ruleClaimedBy(name)
		if !claimsOne {
			t.Errorf("%s does not follow the <rule-id>_<slug> convention, so it claims no rule and "+
				"Step 13's inventory cannot find what it covers", name)
			continue
		}
		if _, owned := ownedRuleEntries[claimed]; owned && !anyCarries(got, claimed) {
			t.Errorf("%s claims %s in its name and raises %q", name, claimed, messagesOf(got))
		}
	}

	for rule, clause := range ownedRuleEntries {
		if !raised[rule] {
			t.Errorf("no fixture of this step's corpus raises %s (%q)", rule, clause)
		}
	}
}

// ruleClaimedBy is the rule a fixture's filename claims to cover, and whether it claims one.
//
// The convention is `<rule-id>_<slug>.yaml`, so the claim is the text before the first separator. A
// name with no separator claims nothing -- and that is precisely the misnaming this file exists to
// catch, so it has to be an answer the caller handles rather than an index into the name. Deriving
// it here rather than at the call site is what lets the not-found half be asserted without
// misnaming a real fixture to reach it.
func ruleClaimedBy(name string) (RuleID, bool) {
	separator := strings.IndexByte(name, '_')
	if separator < 0 {
		return "", false
	}
	return RuleID(name[:separator]), true
}

// TestAFixtureNameOutsideTheConventionClaimsNoRule is the not-found half of that answer, and the
// reason this step's convention guard cannot fail by panicking on its own target case.
func TestAFixtureNameOutsideTheConventionClaimsNoRule(t *testing.T) {
	if claimed, claimsOne := ruleClaimedBy("R41_ten_unknown_keys"); !claimsOne || claimed != R41 {
		t.Errorf("ruleClaimedBy(a name in convention) = %q, %t; want %q, true", claimed, claimsOne, R41)
	}
	if _, claimsOne := ruleClaimedBy("listeners"); claimsOne {
		t.Error("a name with no separator claims a rule; the misnaming has to be reportable, not indexable")
	}
}
