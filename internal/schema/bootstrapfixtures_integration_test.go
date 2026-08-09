//go:build integration

package schema

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is how Step 13's container-backed cases get a configuration and a first boot.
// bootstrapinventory_integration_test.go is the other half: the two inventories their negative
// claims are established by.

// aBootConfiguration is the configuration most cases run under: the container, the harness schema,
// and internal/config's built-in retention grid, which is what makes the eight partitions a pass
// creates a consequence of the documented defaults rather than a number chosen here.
func aBootConfiguration(t *testing.T) config.Config {
	t.Helper()

	cfg := harnessConfig(t)
	cfg.Retention = theMaintainedRetention
	return cfg
}

// aParsedConfiguration is the configuration a customer's *file* produces, loaded through
// internal/config rather than assembled here. That is what makes criterion 1's schema a default
// rather than a value this suite chose: leave the key out and internal/config supplies it.
//
// The schema key is written only when one is given, because "unset" is the case under test.
func aParsedConfiguration(t *testing.T, schema string) config.Config {
	t.Helper()

	written := "version: 1\ndatabase:\n  url: '" +
		strings.ReplaceAll(harnessConfig(t).Database.URL, "'", "''") + "'\n"
	if schema != "" {
		written += "  schema: " + schema + "\n"
	}
	written += "listeners: []\n"

	cfg, _, errs := config.Parse(
		[]byte(written), "bootstrap.yaml",
		func(string) (string, bool) { return "", false },
	)
	if len(errs) != 0 {
		t.Fatalf("internal/config refused the configuration this case rests on: %v\n%s", errs, written)
	}
	return *cfg
}

// mustBootstrap is a boot that has to succeed for the case around it to mean anything.
func mustBootstrap(t *testing.T, pool *pgxpool.Pool, cfg config.Config) {
	t.Helper()

	if err := Bootstrap(t.Context(), pool, cfg); err != nil {
		t.Fatalf("bootstrap %s: %v", cfg.Database.Schema, err)
	}
}
