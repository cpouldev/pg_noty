//go:build integration

package cli

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// TestReadinessDetectsASchemaBehindTheBinary is what makes the "schema version current" condition
// true of its own name. It used to call AppliedVersion and discard the number, so every database
// holding a ledger answered ready however far behind it was; deleting the last ledger row reproduces
// that state exactly, and the pre-fix condition passes this fixture.
//
// Both sides are asserted, because a condition that answered "not current" unconditionally would
// satisfy the second half on its own.
func TestReadinessDetectsASchemaBehindTheBinary(t *testing.T) {
	pool, cfg, _ := cliDatabase(t, true)
	logger := slog.New(slog.DiscardHandler)

	if err := schemaVersionIsCurrent(t.Context(), pool, cfg, logger); err != nil {
		t.Fatalf("a freshly bootstrapped schema reported itself not current: %v", err)
	}

	expected, err := schema.ExpectedVersion()
	if err != nil {
		t.Fatal(err)
	}
	drop := fmt.Sprintf(`DELETE FROM "noty".%s WHERE version = $1`, schema.TableSchemaVersion)
	if _, err := pool.Exec(t.Context(), drop, expected); err != nil {
		t.Fatalf("removing the last ledger row: %v", err)
	}

	err = schemaVersionIsCurrent(t.Context(), pool, cfg, logger)
	if err == nil {
		t.Fatal("a schema one migration behind the binary reported itself current")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(expected)) {
		t.Errorf("the condition reported %q, want the version this binary embeds named", err)
	}
	if !strings.Contains(err.Error(), cfg.Database.Schema) {
		t.Errorf("the condition reported %q, want the schema named", err)
	}
}
