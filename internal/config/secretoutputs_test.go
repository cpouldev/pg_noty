package config

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	step13Secret       = "STEP13_DISTINCTIVE_SECRET_7f3a91"
	step13PhysicalTail = "STEP13_NONSECRET_PHYSICAL_TAIL_91a3f7"
)

func TestOneSecretFixtureIsContainedAcrossEveryOutputPath(t *testing.T) {
	path := warningFixture("W2_secret_outputs")
	data := readFixtureBytes(t, path)
	if !strings.Contains(string(data), "${SIGNING_SECRET}") {
		t.Fatalf("%s does not write the reference whose survival is asserted", path)
	}

	cfg, warnings, errs := Parse(data, filepath.Base(path), coverageEnvironmentFor(path))
	if cfg != nil || len(errs) != 1 || errs[0].Rule != R38 ||
		len(warnings) != 1 || warnings[0].Rule != W2 {
		t.Fatalf("error run returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
	}
	renderedErrors := errs.Render(data)
	renderedWarnings := warnings.Render(data)
	if !strings.Contains(renderedErrors, "${SIGNING_SECRET}") {
		t.Errorf("rendered diagnostic removed the safe source reference:\n%s", renderedErrors)
	}
	assertOutputOmitsStep13Secret(t, "rendered diagnostics", renderedErrors)
	assertOutputOmitsStep13Secret(t, "rendered warnings", renderedWarnings)

	cleanEnv := MapEnv(map[string]string{
		"DATABASE_URL":       databaseURLWithStep13Secret(),
		"SIGNING_SECRET":     step13Secret + "\r" + step13PhysicalTail,
		"SIGNING_SECRET_OLD": "different-rotation-secret",
	})
	cfg, warnings, errs = Parse(data, filepath.Base(path), cleanEnv)
	if cfg == nil || len(errs) != 0 || len(warnings) != 1 || warnings[0].Rule != W2 {
		t.Fatalf("clean run returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
	}
	logged := logConfig(t, *cfg)
	assertOutputOmitsStep13Secret(t, "Config.LogValue", logged)
	for _, useful := range []string{"postgres://noty:", "@db.internal:5432/noty", redactionPlaceholder} {
		if !strings.Contains(logged, useful) {
			t.Errorf("Config.LogValue omitted useful redacted URL text %q:\n%s", useful, logged)
		}
	}
}

func assertOutputOmitsStep13Secret(t *testing.T, surface, output string) {
	t.Helper()
	for _, forbidden := range []string{step13Secret, step13PhysicalTail} {
		if strings.Contains(output, forbidden) {
			t.Errorf("%s contains forbidden bytes %q:\n%s", surface, forbidden, output)
		}
	}
}

func databaseURLWithStep13Secret() string {
	return "postgres://noty:" + step13Secret + "@db.internal:5432/noty"
}
