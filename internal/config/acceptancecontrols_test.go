package config

import (
	"path/filepath"
	"testing"
)

func TestAcceptanceRegistryRejectsMislabelsUnrelatedBodiesAndBodySwaps(t *testing.T) {
	byClaim := acceptanceSpecsByClaim(t)
	tests := []struct {
		name   string
		target acceptanceClaim
		source acceptanceClaim
	}{
		{"rule mislabel", acceptanceClaim{rule: R2}, acceptanceClaim{rule: R1, half: r1ValueHalf}},
		{"half mislabel", acceptanceClaim{rule: R29, half: r29LegalityHalf},
			acceptanceClaim{rule: R29, half: r29ContentHalf}},
		{"unrelated clean YAML renamed", acceptanceClaim{rule: R42},
			acceptanceClaim{rule: R1, half: r1ValueHalf}},
		{"swapped R6 body", acceptanceClaim{rule: R6}, acceptanceClaim{rule: R7}},
		{"swapped R7 body", acceptanceClaim{rule: R7}, acceptanceClaim{rule: R6}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target, source := byClaim[tc.target], byClaim[tc.source]
			path := filepath.Join(validCorpus, source.fixture+fixtureExtension)
			data := readFixtureBytes(t, path)
			if issues := acceptanceIssues(target, target.fixture+fixtureExtension, data); len(issues) == 0 {
				t.Fatalf("%s body certified %s", source.fixture, target.claim)
			}
		})
	}
}

func acceptanceSpecsByClaim(t *testing.T) map[acceptanceClaim]acceptanceSpec {
	t.Helper()
	byClaim := make(map[acceptanceClaim]acceptanceSpec, len(acceptanceUniverse()))
	for _, spec := range acceptanceSpecs() {
		if _, duplicate := byClaim[spec.claim]; duplicate {
			t.Fatalf("duplicate acceptance claim %s", spec.claim)
		}
		byClaim[spec.claim] = spec
	}
	return byClaim
}
