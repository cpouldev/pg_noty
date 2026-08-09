package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collectCorpusCoverage(t *testing.T, root string) []fixtureCoverage {
	t.Helper()
	var fixtures []fixtureCoverage
	manifests := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && isPerformanceArtifactDirectory(root, path) {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != fixtureExtension {
			if !entry.IsDir() && filepath.Ext(path) == ".rules" {
				manifests[path] = true
			}
			return nil
		}
		fixtures = append(fixtures, observeFixture(t, path))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s failed: %v", root, err)
	}
	for manifest := range manifests {
		fixture := strings.TrimSuffix(manifest, ".rules") + fixtureExtension
		if _, err := os.Stat(fixture); err != nil {
			t.Errorf("%s has no sibling fixture: %v", manifest, err)
		}
	}
	if len(fixtures) == 0 {
		t.Fatalf("%s contains no regular YAML fixture", root)
	}
	return fixtures
}

func isPerformanceArtifactDirectory(root, path string) bool {
	return filepath.Clean(root) == "testdata" &&
		filepath.Clean(path) == filepath.Join("testdata", "gen")
}

func TestCoverageWalkQuantifiesEveryFixtureClassAndDepth(t *testing.T) {
	fixtures := collectCorpusCoverage(t, "testdata")
	got := map[string]int{}
	for _, fixture := range fixtures {
		relative := strings.TrimPrefix(fixture.name, "testdata/")
		parts := strings.Split(relative, "/")
		switch {
		case len(parts) == 2 && parts[0] == "invalid":
			got["invalid-root"]++
		case len(parts) > 2 && parts[0] == "invalid":
			got["invalid-nested"]++
		case len(parts) == 2 && parts[0] == "valid":
			got["valid"]++
		case len(parts) == 2 && parts[0] == "warnings":
			got["warnings"]++
		default:
			t.Errorf("coverage walk found an unaccounted YAML fixture class: %s", fixture.name)
		}
	}
	want := map[string]int{
		"invalid-root":   rejectingFixtures,
		"invalid-nested": step11NestedEnvironmentFixtures,
		"valid":          acceptingFixtures,
		"warnings":       step11WarningFixtures + step13WarningFixtures,
	}
	for class, count := range want {
		if got[class] != count {
			t.Errorf("coverage walk found %d %s fixtures, want exactly %d", got[class], class, count)
		}
	}
	wantTotal := rejectingFixtures + step11NestedEnvironmentFixtures +
		acceptingFixtures + step11WarningFixtures + step13WarningFixtures
	if len(fixtures) != wantTotal {
		t.Errorf("coverage walk found %d total fixtures, want exactly %d", len(fixtures), wantTotal)
	}
}

func observeFixture(t *testing.T, path string) fixtureCoverage {
	t.Helper()
	data := readFixtureBytes(t, path)
	cfg, warnings, errs := Parse(data, filepath.Base(path), coverageEnvironmentFor(path))
	all := append(append(Errors(nil), errs...), errorsFromWarnings(warnings)...)
	observed, halves := observedCoverage(all)
	return fixtureCoverage{
		name:     filepath.ToSlash(path),
		class:    classifyCorpusFixture(path),
		prefix:   filenameRule(filepath.Base(path)),
		manifest: readRuleManifest(t, path),
		observed: observed,
		clean:    cfg != nil && len(errs) == 0 && len(warnings) == 0,
		halves:   halves,
	}
}

func classifyCorpusFixture(path string) corpusFixtureClass {
	relative := strings.TrimPrefix(filepath.ToSlash(path), "testdata/")
	switch strings.Split(relative, "/")[0] {
	case "invalid":
		return invalidFixture
	case "valid":
		return validFixture
	case "warnings":
		return warningFixtureClass
	default:
		return syntheticFixture
	}
}

func coverageEnvironment() EnvLookup {
	return coverageEnvironmentFor("")
}

func coverageEnvironmentFor(path string) EnvLookup {
	values := make(map[string]string, len(corpusVariables)+4)
	for name, value := range corpusVariables {
		values[name] = value
	}
	if scenario, exists := coverageEnvironmentScenarios[filepath.Base(path)]; exists {
		for name, value := range scenario.values {
			values[name] = value
		}
	}
	return MapEnv(values)
}

func errorsFromWarnings(warnings Warnings) Errors {
	errs := make(Errors, 0, len(warnings))
	for _, warning := range warnings {
		errs = append(errs, Error(warning))
	}
	return errs
}

func observedCoverage(diags Errors) ([]RuleID, []ruleHalf) {
	var rules []RuleID
	var halves []ruleHalf
	for _, diag := range diags {
		if !numberedRule(diag.Rule) {
			continue
		}
		rules = append(rules, diag.Rule)
		if half := splitHalfOf(diag); half != "" {
			halves = append(halves, half)
		}
	}
	return compactRules(rules), compactHalves(halves)
}

func filenameRule(name string) RuleID {
	claim, present := ruleClaimedBy(strings.TrimSuffix(name, fixtureExtension))
	if present && numberedRule(claim) {
		return claim
	}
	return ""
}

func readRuleManifest(t *testing.T, fixture string) []RuleID {
	t.Helper()
	path := strings.TrimSuffix(fixture, fixtureExtension) + ".rules"
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading %s failed: %v", path, err)
	}
	var rules []RuleID
	for _, line := range strings.Split(string(data), "\n") {
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}
		for _, field := range strings.Fields(line) {
			rule := RuleID(field)
			if !numberedRule(rule) {
				t.Errorf("%s declares unknown numbered rule %q", path, rule)
				continue
			}
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 {
		t.Errorf("%s is empty", path)
	}
	return compactRules(rules)
}
