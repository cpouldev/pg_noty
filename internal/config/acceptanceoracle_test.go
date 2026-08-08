package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type acceptanceSubjectClaim struct {
	claim   acceptanceClaim
	subject string
}

func verifiedAcceptanceClaims(t *testing.T) []acceptanceClaim {
	t.Helper()
	specs := acceptanceSpecs()
	got := make([]acceptanceClaim, 0, len(specs))
	seen := map[acceptanceClaim]string{}
	for _, spec := range specs {
		if prior, duplicate := seen[spec.claim]; duplicate {
			t.Errorf("%s and %s both register %s", prior, spec.fixture, spec.claim)
		} else {
			seen[spec.claim] = spec.fixture
		}
		got = append(got, spec.claim)
		path := filepath.Join(validCorpus, spec.fixture+fixtureExtension)
		data := readFixtureBytes(t, path)
		issues := slices.Concat(acceptanceOracleIssues(spec),
			acceptanceIssues(spec, filepath.Base(path), data))
		if len(issues) != 0 {
			t.Errorf("%s acceptance evidence failed:\n%s", spec.claim, strings.Join(issues, "\n"))
		}
	}
	sortAcceptanceClaims(got)
	if want := acceptanceUniverse(); !slices.Equal(got, want) {
		t.Errorf("acceptance registry claims\n%v\nwant exact universe\n%v", got, want)
	}
	return got
}

func TestAcceptanceRegistryHasExactClaimsAndBidirectionalSubjectOracle(t *testing.T) {
	if got := len(acceptanceUniverse()); got != 52 {
		t.Fatalf("acceptance universe has %d claims, want 45 whole rules plus 7 split halves", got)
	}
	verifiedAcceptanceClaims(t)
	subjects, observations := acceptanceRegistryKeys(acceptanceSpecs())
	if got, want := len(subjects), 106; got != want {
		t.Fatalf("acceptance registry has %d subjects, want the audited %d", got, want)
	}
	if !slices.Equal(subjects, observations) {
		t.Fatalf("subjects\n%v\ndo not exactly equal observations\n%v", subjects, observations)
	}
}

func acceptanceIssues(spec acceptanceSpec, filename string, data []byte) []string {
	var issues []string
	wantName := spec.fixture + fixtureExtension
	if filename != wantName || !strings.HasPrefix(filename, string(spec.claim.rule)+"_ok_") {
		issues = append(issues, fmt.Sprintf(
			"fixture %q is not the registered %s candidate %q", filename, spec.claim, wantName))
	}
	root, err := parseAcceptanceRoot(data)
	if err != nil {
		return append(issues, "raw source does not parse: "+err.Error())
	}
	for _, subject := range spec.subjects {
		if subject.matches == nil || !subject.matches(root) {
			issues = append(issues, "source does not prove "+subject.source)
		}
	}
	cfg, warnings, errs := Parse(data, filename, coverageEnvironment())
	if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		return append(issues, fmt.Sprintf(
			"fixture is not clean: config=%v warnings=%v errors=%v", cfg != nil, warnings, errs))
	}
	evidence := acceptanceEvidence{root: root, config: cfg}
	for _, observation := range spec.observations {
		if observation.holds == nil || !observation.holds(evidence) {
			issues = append(issues, observation.subject+" does not prove "+observation.state)
		}
	}
	return issues
}

func acceptanceOracleIssues(spec acceptanceSpec) []string {
	var issues []string
	subjects := map[string]bool{}
	observations := map[string]bool{}
	for _, subject := range spec.subjects {
		if subject.id == "" || subjects[subject.id] {
			issues = append(issues, fmt.Sprintf("duplicate or empty subject id %q", subject.id))
		}
		subjects[subject.id] = true
		if subject.matches == nil || subject.mutate == nil {
			issues = append(issues, subject.id+" lacks a source check or mutation")
		}
	}
	for _, observation := range spec.observations {
		if observation.subject == "" || observations[observation.subject] {
			issues = append(issues, fmt.Sprintf(
				"duplicate or empty observation id %q", observation.subject))
		}
		observations[observation.subject] = true
		if observation.holds == nil {
			issues = append(issues, observation.subject+" lacks an observation")
		}
	}
	for subject := range subjects {
		if !observations[subject] {
			issues = append(issues, subject+" has no observation")
		}
	}
	for observation := range observations {
		if !subjects[observation] {
			issues = append(issues, observation+" observation has no subject")
		}
	}
	return issues
}

func acceptanceRegistryKeys(specs []acceptanceSpec) ([]acceptanceSubjectClaim,
	[]acceptanceSubjectClaim,
) {
	var subjects, observations []acceptanceSubjectClaim
	for _, spec := range specs {
		for _, subject := range spec.subjects {
			subjects = append(subjects, acceptanceSubjectClaim{spec.claim, subject.id})
		}
		for _, observation := range spec.observations {
			observations = append(observations,
				acceptanceSubjectClaim{spec.claim, observation.subject})
		}
	}
	sortAcceptanceSubjectClaims(subjects)
	sortAcceptanceSubjectClaims(observations)
	return subjects, observations
}

func sortAcceptanceSubjectClaims(claims []acceptanceSubjectClaim) {
	slices.SortFunc(claims, func(a, b acceptanceSubjectClaim) int {
		if byClaim := strings.Compare(a.claim.String(), b.claim.String()); byClaim != 0 {
			return byClaim
		}
		return strings.Compare(a.subject, b.subject)
	})
}
