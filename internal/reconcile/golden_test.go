package reconcile

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var updatePlanGoldens = flag.Bool("update-plan-goldens", false, "rewrite internal/reconcile plan goldens")

const (
	allActionPlanGolden      = "all-actions.golden"
	declaredGoldenFiles      = 1
	declaredFixtureActions   = 5
	declaredFixtureKinds     = 5
	declaredFixtureListeners = 3
)

var expectedPlanGoldens = []string{allActionPlanGolden}

func TestPlanRenderingMatchesCommittedGolden(t *testing.T) {
	reportPlanGoldenCorpusIssues(t)
	if err := comparePlanGolden(allActionPlanGolden, renderedPlanFixture().Render(), *updatePlanGoldens); err != nil {
		t.Fatal(err)
	}
	if *updatePlanGoldens {
		reportPlanGoldenCorpusIssues(t)
	}
}

func TestPlanGoldenCorpusIsSizePinnedAgainstItsFixtureAxes(t *testing.T) {
	reportPlanGoldenCorpusIssues(t)
}

// This drives comparePlanGolden, the same comparator the live golden test calls, with bytes altered
// by one appended control line.
func TestPlanGoldenComparisonRefusesAlteredExpectationWithoutUpdate(t *testing.T) {
	path := planGoldenPath(allActionPlanGolden)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	altered := renderedPlanFixture().Render() + "# deliberately altered expectation\n"
	if err := comparePlanGolden(allActionPlanGolden, altered, false); err == nil {
		t.Fatal("a deliberately altered expectation was accepted without -update-plan-goldens")
	} else if !strings.Contains(err.Error(), "differs from committed") {
		t.Fatalf("altered expectation failed as %v, want the committed-golden comparison", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.ModTime() != before.ModTime() {
		t.Fatal("a comparison without -update-plan-goldens rewrote the committed golden")
	}
}

// Both symbol directions are checked: -/+ must occur, and ~ may occur only for
// rename.
func TestNoReplaceRowInAnyGoldenRendersRenameSymbol(t *testing.T) {
	for _, name := range expectedPlanGoldens {
		golden, err := readPlanGolden(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(golden, "-/+") {
			t.Fatalf("%s has no -/+ replace row", name)
		}
		for _, row := range strings.Split(golden, "\n") {
			trimmed := strings.TrimSpace(row)
			if strings.HasPrefix(trimmed, "~") && !strings.HasSuffix(trimmed, "rename") {
				t.Errorf("%s renders ~ outside a rename row: %q", name, row)
			}
			if strings.Contains(row, "replace") && strings.HasPrefix(trimmed, "~") {
				t.Errorf("%s renders replace as ~: %q", name, row)
			}
		}
	}
}

func planGoldenPath(name string) string { return filepath.Join("testdata", name) }

func comparePlanGolden(name, got string, update bool) error {
	path := planGoldenPath(name)
	if update {
		return os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := readPlanGolden(name)
	if err != nil {
		return fmt.Errorf("%w; use -update-plan-goldens only for an intentional corpus change", err)
	}
	if got != want {
		return fmt.Errorf("%s differs from committed plan golden", path)
	}
	return nil
}

func readPlanGolden(name string) (string, error) {
	path := planGoldenPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

func reportPlanGoldenCorpusIssues(t *testing.T) {
	t.Helper()
	for _, issue := range planGoldenCorpusIssues() {
		t.Error(issue)
	}
}

func planGoldenCorpusIssues() []string {
	var issues []string
	paths, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
	if err != nil {
		return []string{fmt.Sprintf("glob plan goldens: %v", err)}
	}
	if len(paths) != declaredGoldenFiles {
		issues = append(issues, fmt.Sprintf("committed golden files = %d, want %d", len(paths), declaredGoldenFiles))
	}
	if len(expectedPlanGoldens) != declaredGoldenFiles {
		issues = append(issues, fmt.Sprintf("expected golden files = %d, want %d", len(expectedPlanGoldens), declaredGoldenFiles))
	}
	for _, name := range expectedPlanGoldens {
		if !slices.ContainsFunc(paths, func(path string) bool { return filepath.Base(path) == name }) {
			issues = append(issues, name+" is expected but not committed under testdata")
		}
	}
	plan := renderedPlanFixture()
	seenKinds, seenListeners := map[ActionKind]bool{}, map[string]bool{}
	for _, action := range plan.Actions {
		seenKinds[action.Kind], seenListeners[action.Pair.Listener] = true, true
	}
	if len(plan.Actions) != declaredFixtureActions || len(seenKinds) != declaredFixtureKinds || len(seenListeners) != declaredFixtureListeners {
		issues = append(issues, fmt.Sprintf("fixture has %d actions, %d kinds and %d listeners; want %d, %d and %d",
			len(plan.Actions), len(seenKinds), len(seenListeners), declaredFixtureActions, declaredFixtureKinds, declaredFixtureListeners))
	}
	return issues
}
