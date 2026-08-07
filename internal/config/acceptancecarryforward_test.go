package config

import (
	"path/filepath"
	"testing"
)

func TestR1ValueObservationIncludesResolvedVersion(t *testing.T) {
	var r1 acceptanceSpec
	found := false
	for _, spec := range acceptanceSpecs() {
		if spec.claim == (acceptanceClaim{rule: R1, half: r1ValueHalf}) {
			r1, found = spec, true
			break
		}
	}
	if !found {
		t.Fatal("acceptance registry has no R1:value specification")
	}

	path := filepath.Join(validCorpus, r1.fixture+fixtureExtension)
	data := readFixtureBytes(t, path)
	root, err := parseAcceptanceRoot(data)
	if err != nil {
		t.Fatalf("parse R1:value evidence: %v", err)
	}
	cfg, warnings, errs := Parse(data, filepath.Base(path), coverageEnvironment())
	if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		t.Fatalf("R1:value evidence is not clean: config=%v warnings=%v errors=%v",
			cfg != nil, warnings, errs)
	}
	if len(r1.observations) != 1 {
		t.Fatalf("R1:value has %d observations, want its one keyed observation",
			len(r1.observations))
	}
	observation := r1.observations[0]
	if !observation.needsConfig {
		t.Fatal("R1:value observation does not require resolved configuration evidence")
	}
	if !observation.holds(acceptanceEvidence{root: root, config: cfg}) {
		t.Fatal("R1:value observation rejects its raw and resolved baseline")
	}

	mutated := *cfg
	mutated.Version = 0
	if observation.holds(acceptanceEvidence{root: root, config: &mutated}) {
		t.Fatal("R1:value observation accepts Config.Version = 0 with an unchanged raw token")
	}
}
