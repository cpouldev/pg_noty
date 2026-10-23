package config

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

func TestEveryAcceptanceSubjectMutationDefeatsItsOwnObservation(t *testing.T) {
	for _, spec := range acceptanceSpecs() {
		path := filepath.Join(validCorpus, spec.fixture+fixtureExtension)
		data := readFixtureBytes(t, path)
		observations := observationsBySubject(t, spec)
		for _, subject := range spec.subjects {
			subject, observation := subject, observations[subject.id]
			t.Run(spec.claim.String()+"/"+subject.id, func(t *testing.T) {
				mutated, err := subject.mutate(data)
				if err != nil {
					t.Fatalf("mutate %s: %v", subject.source, err)
				}
				if bytes.Equal(mutated, data) {
					t.Fatal("subject mutation left the fixture unchanged")
				}
				root, err := parseAcceptanceRoot(mutated)
				if err != nil {
					t.Fatalf("subject mutation broke YAML syntax: %v", err)
				}
				if subject.matches(root) {
					t.Fatalf("mutation did not neutralize source subject %s", subject.source)
				}
				assertMutationDefeatsObservation(t, spec, observation, root, mutated)
			})
		}
	}
}

func assertMutationDefeatsObservation(t *testing.T, spec acceptanceSpec,
	observation acceptanceObservation, root ast.Node, data []byte,
) {
	t.Helper()
	cfg, warnings, errs := Parse(data, spec.fixture+fixtureExtension, coverageEnvironment())
	if observation.needsConfig && (cfg == nil || len(warnings) != 0 || len(errs) != 0) {
		t.Fatalf("semantic mutation is not a clean alternate: config=%v warnings=%v errors=%v",
			cfg != nil, warnings, errs)
	}
	evidence := acceptanceEvidence{root: root, config: cfg}
	if observation.holds(evidence) {
		t.Fatalf("mutation left observation %q true", observation.state)
	}
}

func observationsBySubject(t *testing.T, spec acceptanceSpec) map[string]acceptanceObservation {
	t.Helper()
	if issues := acceptanceOracleIssues(spec); len(issues) != 0 {
		t.Fatalf("%s has invalid subject oracle: %v", spec.claim, issues)
	}
	observations := make(map[string]acceptanceObservation, len(spec.observations))
	for _, observation := range spec.observations {
		observations[observation.subject] = observation
	}
	return observations
}
