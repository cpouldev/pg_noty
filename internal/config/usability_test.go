package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var vacuousDiagnosticMessages = []string{
	"invalid value",
	"bad value",
	"invalid",
	"error",
	"unexpected value",
}

var diagnosticSubjects = []string{
	"configuration",
	"environment variable",
	"alias",
	"merge key",
	"interpolation",
	"token",
	"header",
}

type usabilityScenario struct {
	name string
	path string
	env  EnvLookup
}

func TestEveryInvalidFixtureDiagnosticIsUsable(t *testing.T) {
	scenarios := invalidUsabilityScenarios(t)
	total := 0
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			data := readFixtureBytes(t, scenario.path)
			_, warnings, errs := Parse(data, filepath.ToSlash(scenario.path), scenario.env)
			diags := append(append(Errors(nil), errs...), errorsFromWarnings(warnings)...)
			if len(diags) == 0 {
				t.Fatalf("%s produced no diagnostic", scenario.path)
			}
			total += len(diags)
			for index, diag := range diags {
				for _, issue := range diagnosticUsabilityIssues(diag) {
					t.Errorf("diagnostic %d (%s): %s", index, diag.Rule, issue)
				}
			}
		})
	}
	if total == 0 {
		t.Fatal("usability gate observed no diagnostics")
	}
}

func invalidUsabilityScenarios(t *testing.T) []usabilityScenario {
	t.Helper()
	fixtures := recursiveYAMLFixtures(t, invalidCorpus)
	scenarios := make([]usabilityScenario, 0, len(fixtures)+len(coverageEnvironmentScenarios))
	for _, path := range fixtures {
		scenarios = append(scenarios, usabilityScenario{
			name: "empty-env/" + filepath.ToSlash(strings.TrimPrefix(path, invalidCorpus+"/")),
			path: path,
			env:  MapEnv(nil),
		})
	}
	for name, scenario := range coverageEnvironmentScenarios {
		scenarios = append(scenarios, usabilityScenario{
			name: "declared-env/" + name, path: scenario.path,
			env: coverageEnvironmentFor(scenario.path),
		})
	}
	return scenarios
}

// The four reasons the gate can give, named once so that a case may state which clause must reject
// it and a rewording cannot leave a test asserting a phrase the gate no longer produces. Each is a
// clause of one accumulator, and asking only "did anything fire?" makes a case that trips two
// indistinguishable from one that trips the clause it is named for.
const (
	emptyPathIssue         = "Path is empty"
	deniedPhraseIssue      = "Msg is denied phrase"
	shortMessageIssue      = "Msg is shorter than 12 characters"
	statesNoConditionIssue = "Msg does not state a contracted condition and Hint is empty"
)

func diagnosticUsabilityIssues(diag Error) []string {
	var issues []string
	if strings.TrimSpace(diag.Path) == "" {
		issues = append(issues, emptyPathIssue)
	}
	message := strings.TrimSpace(diag.Msg)
	lower := strings.ToLower(message)
	for _, denied := range vacuousDiagnosticMessages {
		if lower == denied {
			issues = append(issues, fmt.Sprintf("%s %q", deniedPhraseIssue, denied))
		}
	}
	if utf8.RuneCountInString(message) < 12 {
		issues = append(issues, shortMessageIssue)
	}
	if !matchesDiagnosticContract(diag) && strings.TrimSpace(diag.Hint) == "" {
		issues = append(issues, statesNoConditionIssue)
	}
	return issues
}

func assertUsableDiagnostic(t *testing.T, diag Error) {
	t.Helper()
	if issues := diagnosticUsabilityIssues(diag); len(issues) != 0 {
		t.Fatalf("diagnostic should pass usability gate: %v", issues)
	}
}

func assertUnusableDiagnostic(t *testing.T, diag Error) {
	t.Helper()
	if issues := diagnosticUsabilityIssues(diag); len(issues) == 0 {
		t.Fatal("diagnostic should fail usability gate")
	}
}
