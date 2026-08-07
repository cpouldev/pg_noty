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

func TestConcreteMaskedExtentsAreNamedRegistrySubjects(t *testing.T) {
	want := map[acceptanceSubjectClaim]bool{}
	for claim, subjects := range concreteMaskedAcceptanceSubjects() {
		for _, subject := range subjects {
			want[acceptanceSubjectClaim{claim, subject}] = true
		}
	}
	got, _ := acceptanceRegistryKeys(acceptanceSpecs())
	for _, key := range got {
		delete(want, key)
	}
	for missing := range want {
		t.Errorf("concrete masked extent is not a subject: %s/%s", missing.claim, missing.subject)
	}
}

func concreteMaskedAcceptanceSubjects() map[acceptanceClaim][]string {
	return map[acceptanceClaim][]string{
		{rule: R1, half: r1PresenceHalf}: {"version.presence"},
		{rule: R1, half: r1ValueHalf}:    {"version.value"},
		{rule: R13}:                      {"defaults.timeout", "listener.timeout"},
		{rule: R14}:                      {"defaults.max_attempts", "listener.max_attempts"},
		{rule: R15}:                      {"defaults.exponential", "listener.linear", "listener.fixed"},
		{rule: R17}:                      {"defaults.max_interval", "listener.max_interval"},
		{rule: R18}:                      {"defaults.jitter.true", "listener.jitter.false"},
		{rule: R40}: {"worker.poll_interval", "worker.lease_timeout",
			"worker.drain_timeout", "retention.keep", "defaults.timeout"},
	}
}
