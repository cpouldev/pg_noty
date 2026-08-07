package config

import (
	"slices"
	"strings"
	"testing"
)

func TestCoverageEvidenceUnionsPrefixManifestAndObservation(t *testing.T) {
	fixture := fixtureCoverage{
		name:     "synthetic/R1_multi.yaml",
		prefix:   R1,
		manifest: []RuleID{R2},
		observed: []RuleID{R2, R1},
	}
	if got, want := fixture.declared(), []RuleID{R1, R2}; !slices.Equal(got, want) {
		t.Errorf("declared union = %v, want %v", got, want)
	}
	if issues := coverageIssues(completeSyntheticCoverage(fixture), acceptanceUniverse()); len(issues) != 0 {
		t.Fatalf("prefix + manifest declarations did not agree with observed rules: %v", issues)
	}
}

func TestCoverageMechanismFailsOnEverySyntheticGap(t *testing.T) {
	tests := []struct {
		name string
		edit func([]fixtureCoverage, []acceptanceClaim) ([]fixtureCoverage, []acceptanceClaim)
		want string
	}{
		{"missing raising", editSyntheticFixtures(removeSynthetic("R42/raising")),
			"R42 lacks a raising fixture"},
		{"missing non-raising", removeSyntheticAcceptance(acceptanceClaim{rule: R42}),
			"R42 lacks a non-raising fixture"},
		{"mislabel", editSyntheticFixtures(mislabelSynthetic),
			"declares [R2] but actually reports [R1]"},
		{"empty corpus", func([]fixtureCoverage, []acceptanceClaim) ([]fixtureCoverage, []acceptanceClaim) {
			return nil, nil
		}, "the corpus is empty"},
		{"half-covered split rule", editSyntheticFixtures(removeSynthetic("R1/value")),
			"R1 lacks raising coverage for R1:value"},
		{"missing non-raising split half", removeSyntheticAcceptance(
			acceptanceClaim{rule: R1, half: r1ValueHalf}),
			"R1 lacks non-raising evidence for R1:value"},
		{"unrelated clean candidate grants no credit", unrelatedCleanCandidate,
			"R42 lacks a non-raising fixture"},
		{"invalid fixture becomes clean", invalidBecomesClean,
			"unexpectedly became clean outside the valid corpus"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixtures, accepted := tc.edit(completeSyntheticCoverage(), acceptanceUniverse())
			issues := coverageIssues(fixtures, accepted)
			if !anyIssueContains(issues, tc.want) {
				t.Fatalf("issues = %v, want one containing %q", issues, tc.want)
			}
		})
	}
}

func completeSyntheticCoverage(extra ...fixtureCoverage) []fixtureCoverage {
	var fixtures []fixtureCoverage
	for _, rule := range slices.Concat(staticRuleIDs, warningRuleIDs) {
		halves := splitRuleHalves[rule]
		if len(halves) == 0 {
			fixtures = append(fixtures, fixtureCoverage{
				name: string(rule) + "/raising", prefix: rule, observed: []RuleID{rule},
			})
			continue
		}
		for _, half := range halves {
			kind := strings.TrimPrefix(string(half), string(rule)+":")
			fixtures = append(fixtures, fixtureCoverage{
				name: string(rule) + "/" + kind, prefix: rule,
				observed: []RuleID{rule}, halves: []ruleHalf{half},
			})
		}
	}
	return append(fixtures, extra...)
}

func editSyntheticFixtures(edit func([]fixtureCoverage) []fixtureCoverage,
) func([]fixtureCoverage, []acceptanceClaim) ([]fixtureCoverage, []acceptanceClaim) {
	return func(fixtures []fixtureCoverage, accepted []acceptanceClaim,
	) ([]fixtureCoverage, []acceptanceClaim) {
		return edit(fixtures), accepted
	}
}

func removeSynthetic(name string) func([]fixtureCoverage) []fixtureCoverage {
	return func(fixtures []fixtureCoverage) []fixtureCoverage {
		kept := fixtures[:0]
		for _, fixture := range fixtures {
			if fixture.name != name {
				kept = append(kept, fixture)
			}
		}
		return kept
	}
}

func removeSyntheticAcceptance(want acceptanceClaim,
) func([]fixtureCoverage, []acceptanceClaim) ([]fixtureCoverage, []acceptanceClaim) {
	return func(fixtures []fixtureCoverage, accepted []acceptanceClaim,
	) ([]fixtureCoverage, []acceptanceClaim) {
		kept := accepted[:0]
		for _, claim := range accepted {
			if claim != want {
				kept = append(kept, claim)
			}
		}
		return fixtures, kept
	}
}

func unrelatedCleanCandidate(fixtures []fixtureCoverage, accepted []acceptanceClaim,
) ([]fixtureCoverage, []acceptanceClaim) {
	_, accepted = removeSyntheticAcceptance(acceptanceClaim{rule: R42})(fixtures, accepted)
	fixtures = append(fixtures, fixtureCoverage{
		name: "valid/R42_ok_unrelated.yaml", class: validFixture, prefix: R42, clean: true,
	})
	return fixtures, accepted
}

func invalidBecomesClean(fixtures []fixtureCoverage, accepted []acceptanceClaim,
) ([]fixtureCoverage, []acceptanceClaim) {
	fixtures = append(fixtures, fixtureCoverage{
		name: "invalid/R42_duplicate.yaml", class: invalidFixture, prefix: R42, clean: true,
	})
	return fixtures, accepted
}

func mislabelSynthetic(fixtures []fixtureCoverage) []fixtureCoverage {
	for i := range fixtures {
		if fixtures[i].name == "R1/presence" {
			fixtures[i].prefix = R2
			return fixtures
		}
	}
	return fixtures
}

func anyIssueContains(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}
