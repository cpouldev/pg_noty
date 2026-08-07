package config

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEveryDeferredKindBuildsACompleteCheck(t *testing.T) {
	tests := []DeferredCheck{
		{
			Listener: "orders", Kind: TableExists, Schema: "public", Table: "orders",
			File: "listeners.yaml", Line: 7, Col: 12, Path: "listeners[0].table",
		},
		{
			Listener: "orders", Operation: updateOperation, Kind: ColumnsExist,
			Schema: "public", Table: "orders", Columns: []string{"status"},
			File: "listeners.yaml", Line: 10, Col: 18,
			Path: "listeners[0].operations.update.columns",
		},
		{
			Listener: "orders", Kind: PrimaryKeyPresent, Schema: "public", Table: "orders",
			File: "listeners.yaml", Line: 7, Col: 12, Path: "listeners[0].table",
		},
		{
			Listener: "orders", Operation: insertOperationKey, Kind: WhenParses,
			Schema: "public", Table: "orders", When: "NEW.total > 0",
			File: "listeners.yaml", Line: 9, Col: 15,
			Path: "listeners[0].operations.insert.when",
		},
	}

	wantKinds := []DeferredKind{TableExists, ColumnsExist, PrimaryKeyPresent, WhenParses}
	for i, check := range tests {
		if check.Kind != wantKinds[i] {
			t.Errorf("check %d kind = %q, want %q", i, check.Kind, wantKinds[i])
		}
		if check.Listener == "" || check.Schema == "" || check.Table == "" ||
			check.File == "" || check.Line == 0 || check.Col == 0 || check.Path == "" {
			t.Errorf("check %d does not carry the common fields: %+v", i, check)
		}
	}
}

func TestNoopDBValidatorReturnsNothing(t *testing.T) {
	diags, err := (NoopDBValidator{}).Validate(context.Background(), []DeferredCheck{
		{Kind: TableExists, Schema: "public", Table: "orders"},
	})
	if err != nil || diags != nil {
		t.Errorf("Validate returned diagnostics=%+v error=%v, want nil, nil", diags, err)
	}
}

func TestDeferredFixtureEmitsChecksAtItsExactTokens(t *testing.T) {
	cfg := parseDeferredFixture(t, "deferred_checks.yaml")
	checks := cfg.DeferredChecks()
	if len(checks) != 5 {
		t.Fatalf("DeferredChecks returned %d checks, want 5: %+v", len(checks), checks)
	}

	tests := []struct {
		kind      DeferredKind
		operation string
		line      int
		col       int
		path      string
		columns   []string
		when      string
	}{
		{TableExists, "", 7, 12, "listeners[0].table", nil, ""},
		{PrimaryKeyPresent, "", 7, 12, "listeners[0].table", nil, ""},
		{WhenParses, insertOperationKey, 10, 15, "listeners[0].operations.insert.when", nil, "NEW.total > 0"},
		{ColumnsExist, updateOperation, 12, 18, "listeners[0].operations.update.columns", []string{"status", "total"}, ""},
		{WhenParses, updateOperation, 13, 15, "listeners[0].operations.update.when", nil, "OLD.status IS DISTINCT FROM NEW.status"},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind)+"/"+tc.operation, func(t *testing.T) {
			got := deferredCheckOf(t, checks, tc.kind, tc.operation)
			if got.Listener != "enabled_deferred" || got.Schema != "tenant" || got.Table != "orders" {
				t.Errorf("target = %q %q.%q, want enabled_deferred tenant.orders",
					got.Listener, got.Schema, got.Table)
			}
			if got.File != "deferred_checks.yaml" || got.Line != tc.line ||
				got.Col != tc.col || got.Path != tc.path {
				t.Errorf("position = %s:%d:%d %s, want deferred_checks.yaml:%d:%d %s",
					got.File, got.Line, got.Col, got.Path, tc.line, tc.col, tc.path)
			}
			if !reflect.DeepEqual(got.Columns, tc.columns) || got.When != tc.when {
				t.Errorf("payload = columns %v when %q, want %v / %q",
					got.Columns, got.When, tc.columns, tc.when)
			}
		})
	}
	for _, check := range checks {
		if check.Listener == "disabled_deferred" {
			t.Errorf("disabled listener emitted %+v", check)
		}
	}
}

func TestDeferredChecksCoverEmptyOneEnabledBoundaries(t *testing.T) {
	tests := []struct {
		fixture string
		want    int
	}{
		{"deferred_zero_listeners.yaml", 0},
		{"R25_ok_listener_enabled_boolean.yaml", 1},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			checks := parseDeferredFixture(t, tc.fixture).DeferredChecks()
			if len(checks) != tc.want {
				t.Errorf("DeferredChecks returned %d checks, want %d: %+v", len(checks), tc.want, checks)
			}
			for _, check := range checks {
				if strings.Contains(check.Listener, "disabled") {
					t.Errorf("disabled listener emitted %+v", check)
				}
			}
		})
	}
}

func TestDisabledListenerStillReceivesStaticValidation(t *testing.T) {
	path := filepath.Join(validCorpus, "R25_ok_listener_enabled_boolean.yaml")
	written := string(readFixtureBytes(t, path))
	written = strings.Replace(written, "table: public.orders_archive", "table: orders_archive", 1)

	cfg, _, errs := Parse([]byte(written), filepath.Base(path), corpusEnvironment())
	if cfg != nil || len(errs) != 1 || errs[0].Rule != R26 ||
		errs[0].Path != "listeners[1].table" {
		t.Fatalf("Parse returned config=%v errors=%+v, want one R26 on the disabled listener", cfg, errs)
	}
}

func parseDeferredFixture(t *testing.T, name string) *Config {
	t.Helper()
	path := filepath.Join(validCorpus, name)
	cfg, warnings, errs := Parse(readFixtureBytes(t, path), name, corpusEnvironment())
	if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		t.Fatalf("Parse(%s) returned config=%v warnings=%+v errors=%+v", name, cfg, warnings, errs)
	}
	return cfg
}

func deferredCheckOf(t *testing.T, checks []DeferredCheck, kind DeferredKind, operation string) DeferredCheck {
	t.Helper()
	for _, check := range checks {
		if check.Kind == kind && check.Operation == operation {
			return check
		}
	}
	t.Fatalf("no %s/%s check in %+v", kind, operation, checks)
	return DeferredCheck{}
}
