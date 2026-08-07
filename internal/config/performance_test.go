package config

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/goartifact"
)

const (
	generatedListenerCount = 200
	generatedMinimumLines  = 4_900
	generatedMaximumLines  = 5_100
	generatedBudget        = time.Second
)

var generatedConfigPath = filepath.Join("testdata", "gen", "config_200.yaml")

func TestGeneratedConfigurationStaysWithinPerformanceBudget(t *testing.T) {
	started := time.Now()
	cfg, warnings, errs := Load(generatedConfigPath, generatedConfigEnvironment())
	elapsed := time.Since(started)

	if len(errs) != 0 || len(warnings) != 0 || cfg == nil {
		t.Fatalf(
			"generated configuration failed: config=%v warnings=%+v errors=%+v",
			cfg, warnings, errs,
		)
	}
	assertGeneratedDimensions(
		t, goartifact.PhysicalLineCount(readFixtureBytes(t, generatedConfigPath)),
		len(cfg.Listeners),
	)
	if elapsed >= generatedBudget {
		t.Errorf(
			"full read/interpolate/validate/merge took %s, budget is under %s",
			elapsed, generatedBudget,
		)
	}
	t.Logf("full 200-listener pipeline completed in %s", elapsed)
}

func BenchmarkGeneratedConfigurationFullPipeline(b *testing.B) {
	data := readFixtureBytes(b, generatedConfigPath)
	b.SetBytes(int64(len(data)))
	b.ReportMetric(float64(goartifact.PhysicalLineCount(data)), "lines")
	b.ResetTimer()
	for range b.N {
		cfg, warnings, errs := Load(generatedConfigPath, generatedConfigEnvironment())
		if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
			b.Fatalf("config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
		}
	}
}

func TestPerformanceGeneratorIsDeterministicAndCommitted(t *testing.T) {
	first := runPerformanceGenerator(t)
	second := runPerformanceGenerator(t)
	committed := readFixtureBytes(t, generatedConfigPath)
	if !bytes.Equal(first, second) {
		t.Error("two generator runs produced different bytes")
	}
	if !bytes.Equal(first, committed) {
		t.Error("committed performance specimen differs from deterministic generator output")
	}
}

func TestGeneratedDimensionGateRejectsSyntheticDrift(t *testing.T) {
	tests := []struct {
		name             string
		lines, listeners int
	}{
		{"too few listeners", 5_000, generatedListenerCount - 1},
		{"too many listeners", 5_000, generatedListenerCount + 1},
		{"too few lines", generatedMinimumLines - 1, generatedListenerCount},
		{"too many lines", generatedMaximumLines + 1, generatedListenerCount},
	}
	for _, tc := range tests {
		t.Run(
			tc.name, func(t *testing.T) {
				if issues := generatedDimensionIssues(tc.lines, tc.listeners); len(issues) == 0 {
					t.Fatal("synthetically drifted generated dimensions passed")
				}
			},
		)
	}
}

func TestPerformanceArtifactCorpusExclusionIsExact(t *testing.T) {
	tests := []struct {
		root, path string
		want       bool
	}{
		{"testdata", filepath.Join("testdata", "gen"), true},
		{"testdata", filepath.Join("testdata", "generator"), false},
		{"testdata", filepath.Join("testdata", "valid", "gen"), false},
		{invalidCorpus, filepath.Join(invalidCorpus, "gen"), false},
	}
	for _, tc := range tests {
		if got := isPerformanceArtifactDirectory(tc.root, tc.path); got != tc.want {
			t.Errorf(
				"isPerformanceArtifactDirectory(%q, %q) = %t, want %t",
				tc.root, tc.path, got, tc.want,
			)
		}
	}
}

func runPerformanceGenerator(t *testing.T) []byte {
	t.Helper()
	command := exec.Command("go", "run", "./testdata/gen")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run deterministic performance generator: %v\n%s", err, output)
	}
	return output
}

func generatedConfigEnvironment() EnvLookup {
	return MapEnv(
		map[string]string{
			"DATABASE_URL":        "postgres://noty:pw@db.internal/noty",
			"DATABASE_LISTEN_URL": "postgres://noty:pw@db.internal/noty",
			"ORDER_WEBHOOK_URL":   "https://hooks.example.test/events",
			"SIGNING_SECRET":      "performance-secret",
		},
	)
}

func assertGeneratedDimensions(t *testing.T, lines, listeners int) {
	t.Helper()
	for _, issue := range generatedDimensionIssues(lines, listeners) {
		t.Error(issue)
	}
}

func generatedDimensionIssues(lines, listeners int) []string {
	var issues []string
	if listeners != generatedListenerCount {
		issues = append(
			issues, fmt.Sprintf(
				"listeners = %d, want %d",
				listeners, generatedListenerCount,
			),
		)
	}
	if lines < generatedMinimumLines || lines > generatedMaximumLines {
		issues = append(
			issues, fmt.Sprintf(
				"lines = %d, want %d..%d",
				lines, generatedMinimumLines, generatedMaximumLines,
			),
		)
	}
	return issues
}
