package config

import (
	"path/filepath"
	"testing"
)

var warningEnvironment = MapEnv(map[string]string{
	"SIGNING_SECRET": "supplied-by-environment",
})

func warningFixture(name string) string {
	return filepath.Join("testdata", "warnings", name+fixtureExtension)
}

func TestRaisingWarningFixturesRenderTheirGoldens(t *testing.T) {
	tests := []struct {
		name string
		rule RuleID
	}{
		{"W1_literal_secret", W1},
		{"W2_drain_below_timeout", W2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := warningFixture(tc.name)
			data := readFixtureBytes(t, path)
			cfg, warnings, errs := Parse(data, filepath.Base(path), warningEnvironment)
			if cfg == nil || len(errs) != 0 || len(warnings) != 1 || warnings[0].Rule != tc.rule {
				t.Fatalf("config=%v warnings=%+v errors=%+v, want one %s warning", cfg, warnings, errs, tc.rule)
			}
			compareGolden(t, goldenFor(path), warnings.Render(data))
		})
	}
}

func TestEveryWarningHasAnOtherwiseIdenticalNonRaisingTwin(t *testing.T) {
	tests := []string{
		"W1_ok_environment_secret",
		"W2_ok_drain_equals_timeout",
		"W2_ok_drain_above_timeout",
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(validCorpus, name+fixtureExtension)
			cfg, warnings, errs := Parse(
				readFixtureBytes(t, path), filepath.Base(path), warningEnvironment,
			)
			if cfg == nil || len(errs) != 0 || len(warnings) != 0 {
				t.Fatalf("config=%v warnings=%+v errors=%+v, want a clean load", cfg, warnings, errs)
			}
		})
	}
}

func TestW1FollowsTheEnvironmentProvenanceRatherThanReferenceSyntax(t *testing.T) {
	base := string(readFixtureBytes(t, warningFixture("W1_literal_secret")))
	tests := []struct {
		name, secret string
		env          EnvLookup
		want         int
	}{
		{"literal", "local-development-secret", MapEnv(nil), 1},
		{"environment reference", "${SIGNING_SECRET}", warningEnvironment, 0},
		{"committed fallback", "${SIGNING_SECRET:-committed}", MapEnv(nil), 1},
		{"set-empty uses committed fallback", "${SIGNING_SECRET:-committed}",
			MapEnv(map[string]string{"SIGNING_SECRET": ""}), 1},
		{"environment wins over fallback", "${SIGNING_SECRET:-committed}", warningEnvironment, 0},
		{"escaped reference is literal", "$${SIGNING_SECRET}", warningEnvironment, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := replaceOnce(t, base, "local-development-secret", tc.secret)
			cfg, warnings, errs := Parse([]byte(text), "provenance.yaml", tc.env)
			if cfg == nil || len(errs) != 0 || len(warnings) != tc.want {
				t.Fatalf("config=%v warnings=%+v errors=%+v, want %d warning", cfg, warnings, errs, tc.want)
			}
		})
	}
}

func TestW1FindsTheOuterNodeOfAnInterpolatedBlockScalar(t *testing.T) {
	base := string(readFixtureBytes(t, warningFixture("W1_literal_secret")))
	block := "|\n            ${SIGNING_SECRET}"
	text := replaceOnce(t, base, "local-development-secret", block)
	cfg, warnings, errs := Parse([]byte(text), "block-secret.yaml", warningEnvironment)
	if cfg == nil || len(errs) != 0 || len(warnings) != 0 {
		t.Fatalf("config=%v warnings=%+v errors=%+v, want no warning", cfg, warnings, errs)
	}
}
