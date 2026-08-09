//go:build integration

package reconcile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestValidatorReportsFourCatalogDefectsAtTheirFixtureTokens(t *testing.T) {
	skipIfShort(t)
	setup := freshDatabase(t)
	mustExecOn(t, setup, "CREATE TABLE public.step15_columns (id integer)")
	mustExecOn(t, setup, "CREATE TABLE public.step15_no_primary_key (id integer)")
	mustExecOn(t, setup, "CREATE TABLE public.step15_when (id integer)")
	pool, log := recordedValidatorPool(t, harnessConfig(t).Database.URL)
	cfg := parseValidatorConfig(t, validatorDefectsFixture, harnessConfig(t).Database.URL)

	log.clear()
	diagnostics, err := NewValidator(pool, *cfg).Validate(t.Context(), cfg.DeferredChecks())
	if err != nil {
		t.Fatalf("Validate() error = %v, want diagnostics", err)
	}
	if got, want := len(diagnostics), 4; got != want { // missing table + column + PK + when
		t.Fatalf("Validate() diagnostics = %#v, want exactly %d", diagnostics, want)
	}
	assertDiagnosticAt(t, diagnostics, deferredRules[config.TableExists], 6, 12, "missing")
	// Line 14 is eight spaces + "columns: " (nine bytes), so '[' begins at column 18.
	assertDiagnosticAt(t, diagnostics, deferredRules[config.ColumnsExist], 14, 18, "absent")
	assertDiagnosticAt(t, diagnostics, deferredRules[config.PrimaryKeyPresent], 18, 12, "primary key")
	// Line 28 is eight spaces + "when: " (six bytes), so 'N' begins at column 15.
	assertDiagnosticAt(t, diagnostics, deferredRules[config.WhenParses], 28, 15, "column")
	assertNoDDL(t, log.statements)
}

func TestValidatorSeparatesInfrastructureFailuresFromDiagnostics(t *testing.T) {
	skipIfShort(t)
	check := config.DeferredCheck{
		Kind: config.ColumnsExist, Schema: "public", Table: "step15_reachable", Columns: []string{"absent"},
		File: "checks.yaml", Line: 7, Col: 18, Path: "listeners[0].operations.update.columns",
	}
	unreachable, err := pgxpool.New(t.Context(), "postgres://noty@127.0.0.1:1/noty?connect_timeout=1")
	if err != nil {
		t.Fatalf("open unreachable pool: %v", err)
	}
	t.Cleanup(unreachable.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	diagnostics, err := NewValidator(unreachable, config.Config{}).Validate(ctx, []config.DeferredCheck{check})
	if err == nil || len(diagnostics) != 0 {
		t.Fatalf("unreachable Validate() = %#v, %v; want no diagnostics and an infrastructure error", diagnostics, err)
	}

	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.step15_reachable (id integer)")
	diagnostics, err = NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{check})
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("reachable Validate() = %#v, %v; want one diagnostic and nil error", diagnostics, err)
	}
}

func TestValidatorSkipsDisabledListenerCatalogWorkButKeepsTheListenerObject(t *testing.T) {
	skipIfShort(t)
	setup := freshDatabase(t)
	mustExecOn(t, setup, "CREATE TABLE public.step15_enabled (id integer PRIMARY KEY)")
	pool, log := recordedValidatorPool(t, harnessConfig(t).Database.URL)
	cfg := parseValidatorConfig(t, validatorDisabledFixture, harnessConfig(t).Database.URL)
	if got := len(cfg.Listeners); got != 2 {
		t.Fatalf("configuration listeners = %d, want enabled and disabled objects", got)
	}
	checks := cfg.DeferredChecks()
	for _, check := range checks {
		if check.Listener == "disabled_listener" {
			t.Fatalf("disabled listener emitted catalog work: %#v", check)
		}
	}

	log.clear()
	diagnostics, err := NewValidator(pool, *cfg).Validate(t.Context(), checks)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("Validate() = %#v, %v; want clean enabled listener", diagnostics, err)
	}
	if len(log.statements) == 0 {
		t.Fatal("validator issued no catalog queries for a clean enabled listener")
	}
	joined := strings.Join(log.statements, "\n")
	if strings.Contains(joined, "step15_disabled") {
		t.Fatalf("catalog log names disabled target: %s", joined)
	}
	if !strings.Contains(joined, "step15_enabled") {
		t.Fatalf("catalog log omits enabled target: %s", joined)
	}
	assertNoDDL(t, log.statements)
}

func TestValidatorChecksKeysOnlyWithAndWithoutAPrimaryKey(t *testing.T) {
	skipIfShort(t)
	setup := freshDatabase(t)
	mustExecOn(t, setup, "CREATE TABLE public.step15_keys_without (id integer)")
	mustExecOn(t, setup, "CREATE TABLE public.step15_keys_with (id integer PRIMARY KEY)")
	pool, log := recordedValidatorPool(t, harnessConfig(t).Database.URL)
	without := config.DeferredCheck{
		Kind: config.PrimaryKeyPresent, Schema: "public", Table: "step15_keys_without", File: "keys.yaml", Line: 8,
		Col: 12, Path: "listeners[0].table",
	}
	with := without
	with.Table = "step15_keys_with"

	log.clear()
	diagnostics, err := NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{without})
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("keys_only without key = %#v, %v; want one diagnostic", diagnostics, err)
	}
	assertDiagnosticAt(t, diagnostics, deferredRules[config.PrimaryKeyPresent], 8, 12, "primary key")
	assertNoDDL(t, log.statements)
	log.clear()
	diagnostics, err = NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{with})
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("keys_only with key = %#v, %v; want acceptance", diagnostics, err)
	}
	if len(log.statements) == 0 {
		t.Fatal("accepted keys_only check issued no catalog query")
	}
}

func parseValidatorConfig(t *testing.T, text, dsn string) *config.Config {
	t.Helper()
	cfg, warnings, diagnostics := config.Parse(
		[]byte(text),
		"validator.yaml",
		config.MapEnv(map[string]string{"DATABASE_URL": dsn}),
	)
	if cfg == nil || len(warnings) != 0 || len(diagnostics) != 0 {
		t.Fatalf("Parse() = %#v, %#v, %#v", cfg, warnings, diagnostics)
	}
	return cfg
}

const validatorDefectsFixture = `version: 1
database:
  url: ${DATABASE_URL}
listeners:
  - name: missing_table
    table: public.step15_missing
    operations: [insert]
    destination:
      url: https://example.test/missing
  - name: missing_column
    table: public.step15_columns
    operations:
      update:
        columns: [absent]
    destination:
      url: https://example.test/columns
  - name: no_primary_key
    table: public.step15_no_primary_key
    operations: [insert]
    payload:
      mode: keys_only
    destination:
      url: https://example.test/keys
  - name: unresolved_when
    table: public.step15_when
    operations:
      insert:
        when: NEW.absent > 0
    destination:
      url: https://example.test/when
`

const validatorDisabledFixture = `version: 1
database:
  url: ${DATABASE_URL}
listeners:
  - name: enabled_listener
    table: public.step15_enabled
    operations: [insert]
    destination:
      url: https://example.test/enabled
  - name: disabled_listener
    enabled: false
    table: public.step15_disabled
    operations: [insert]
    destination:
      url: https://example.test/disabled
`
