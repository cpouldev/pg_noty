package config

import (
	"path/filepath"
	"testing"
	"time"
)

const (
	generatedListenerCount = 200
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
	if len(cfg.Listeners) != generatedListenerCount {
		t.Fatalf("loaded %d listeners, want %d", len(cfg.Listeners), generatedListenerCount)
	}
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
	b.ResetTimer()
	for range b.N {
		cfg, warnings, errs := Load(generatedConfigPath, generatedConfigEnvironment())
		if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
			b.Fatalf("config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
		}
	}
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
