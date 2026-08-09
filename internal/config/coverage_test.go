package config

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fixtureCoverage keeps AC #28's three evidence sources separate. A prefix and a
// manifest are declarations; observed is populated only from returned diagnostics.
// Declarations therefore cannot count themselves as raising coverage.
type fixtureCoverage struct {
	name     string
	class    corpusFixtureClass
	prefix   RuleID
	manifest []RuleID
	observed []RuleID
	clean    bool
	halves   []ruleHalf
}

type corpusFixtureClass uint8

const (
	syntheticFixture corpusFixtureClass = iota
	invalidFixture
	validFixture
	warningFixtureClass
)

type ruleHalf string

const (
	r1PresenceHalf  ruleHalf = "R1:presence"
	r1ValueHalf     ruleHalf = "R1:value"
	r3PresenceHalf  ruleHalf = "R3:presence"
	r3ValueHalf     ruleHalf = "R3:value"
	r23PresenceHalf ruleHalf = "R23:presence"
	r23ValueHalf    ruleHalf = "R23:value"
	r26PresenceHalf ruleHalf = "R26:presence"
	r26ValueHalf    ruleHalf = "R26:value"
	r29LegalityHalf ruleHalf = "R29:legality"
	r29ContentHalf  ruleHalf = "R29:content"
	r36PresenceHalf ruleHalf = "R36:presence"
	r36ValueHalf    ruleHalf = "R36:value"
	r39RangeHalf    ruleHalf = "R39:range"
	r39CompareHalf  ruleHalf = "R39:comparison"
)

var splitRuleHalves = map[RuleID][]ruleHalf{
	R1:  {r1PresenceHalf, r1ValueHalf},
	R3:  {r3PresenceHalf, r3ValueHalf},
	R23: {r23PresenceHalf, r23ValueHalf},
	R26: {r26PresenceHalf, r26ValueHalf},
	R29: {r29LegalityHalf, r29ContentHalf},
	R36: {r36PresenceHalf, r36ValueHalf},
	R39: {r39RangeHalf, r39CompareHalf},
}

func TestTheRealCorpusMechanicallyCoversEveryNumberedRule(t *testing.T) {
	fixtures := collectCorpusCoverage(t, "testdata")
	if issues := coverageIssues(fixtures, verifiedAcceptanceClaims(t)); len(issues) != 0 {
		t.Fatalf("fixture coverage has %d gap(s):\n%s", len(issues), strings.Join(issues, "\n"))
	}
}

func coverageIssues(fixtures []fixtureCoverage, accepted []acceptanceClaim) []string {
	if len(fixtures) == 0 {
		return []string{"the corpus is empty"}
	}

	raising := make(map[RuleID]bool)
	raisingHalves := make(map[ruleHalf]bool)
	acceptedRules := make(map[RuleID]bool)
	acceptedHalves := make(map[ruleHalf]bool)
	for _, claim := range accepted {
		acceptedRules[claim.rule] = true
		if claim.half != "" {
			acceptedHalves[claim.half] = true
		}
	}
	var issues []string
	observedFixtures := 0
	for _, fixture := range fixtures {
		declared := fixture.declared()
		observed := compactRules(fixture.observed)
		if len(observed) != 0 {
			observedFixtures++
			for _, rule := range observed {
				raising[rule] = true
			}
			for _, half := range fixture.halves {
				raisingHalves[half] = true
			}
			if !slices.Equal(declared, observed) {
				issues = append(issues, fmt.Sprintf(
					"%s declares %v but actually reports %v", fixture.name, declared, observed))
			}
			if len(observed) > 1 && len(fixture.manifest) == 0 {
				issues = append(issues, fmt.Sprintf(
					"%s reports multiple rules %v but has no .rules manifest", fixture.name, observed))
			}
			continue
		}
		if fixture.clean && (fixture.class == invalidFixture ||
			fixture.class == warningFixtureClass) {
			issues = append(issues, fmt.Sprintf(
				"%s unexpectedly became clean outside the valid corpus", fixture.name))
		} else if !fixture.clean && len(declared) != 0 {
			issues = append(issues, fmt.Sprintf(
				"%s declares %v but reaches no numbered rule and is not clean", fixture.name, declared))
		}
	}
	if observedFixtures == 0 {
		issues = append(issues, "no fixture produces a numbered diagnostic")
	}

	for _, rule := range slices.Concat(staticRuleIDs, warningRuleIDs) {
		if !raising[rule] {
			issues = append(issues, fmt.Sprintf("%s lacks a raising fixture", rule))
		}
		if !acceptedRules[rule] {
			issues = append(issues, fmt.Sprintf("%s lacks a non-raising fixture", rule))
		}
	}
	for rule, required := range splitRuleHalves {
		for _, half := range required {
			if !raisingHalves[half] {
				issues = append(issues, fmt.Sprintf("%s lacks raising coverage for %s", rule, half))
			}
			if !acceptedHalves[half] {
				issues = append(issues, fmt.Sprintf("%s lacks non-raising evidence for %s", rule, half))
			}
		}
	}
	slices.Sort(issues)
	return issues
}

func (fixture fixtureCoverage) declared() []RuleID {
	declared := append([]RuleID(nil), fixture.manifest...)
	if fixture.prefix != "" {
		declared = append(declared, fixture.prefix)
	}
	return compactRules(declared)
}

func compactRules(rules []RuleID) []RuleID {
	rules = append([]RuleID(nil), rules...)
	slices.Sort(rules)
	return slices.Compact(rules)
}

func numberedRule(rule RuleID) bool {
	return slices.Contains(staticRuleIDs, rule) || slices.Contains(warningRuleIDs, rule)
}
