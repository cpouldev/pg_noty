package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMinimalFixtureResolvesEveryOptionalBuiltInDefault(t *testing.T) {
	path := filepath.Join(validCorpus, "minimal.yaml")
	cfg, warnings, errs := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())
	if len(errs) != 0 || len(warnings) != 0 {
		t.Fatalf("minimal fixture returned warnings=%+v errors=%+v", warnings, errs)
	}
	if cfg == nil {
		t.Fatal("minimal fixture returned a nil Config")
	}

	listener := cfg.Listeners[0]
	tests := []struct {
		path string
		got  any
		want any
	}{
		{"database.schema", cfg.Database.Schema, "noty"},
		{"instance", cfg.Instance, "noty"},
		{"worker.concurrency", cfg.Worker.Concurrency, 16},
		{"worker.batch_size", cfg.Worker.BatchSize, 100},
		{"worker.poll_interval", cfg.Worker.PollInterval, 10 * time.Second},
		{"worker.lease_timeout", cfg.Worker.LeaseTimeout, 5 * time.Minute},
		{"worker.drain_timeout", cfg.Worker.DrainTimeout, 30 * time.Second},
		{"retention.keep", cfg.Retention.Keep, 168 * time.Hour},
		{"retention.partition_interval", cfg.Retention.PartitionInterval, 24 * time.Hour},
		{"retention.precreate", cfg.Retention.Precreate, 168 * time.Hour},
		{"listeners[0].enabled", listener.Enabled, true},
		{"listeners[0].timeout", listener.Delivery.Timeout, 5 * time.Second},
		{"listeners[0].retry.max_attempts", listener.Delivery.Retry.MaxAttempts, 5},
		{"listeners[0].retry.backoff", listener.Delivery.Retry.Backoff, "exponential"},
		{"listeners[0].retry.initial_interval", listener.Delivery.Retry.InitialInterval, 10 * time.Second},
		{"listeners[0].retry.max_interval", listener.Delivery.Retry.MaxInterval, time.Hour},
		{"listeners[0].retry.jitter", listener.Delivery.Retry.Jitter, true},
		{"listeners[0].payload.mode", listener.Trigger.Payload.Mode, "full"},
		{"listeners[0].payload.include_old", listener.Trigger.Payload.IncludeOld, false},
		{"listeners[0].payload.max_bytes", listener.Trigger.Payload.MaxBytes, 262144},
		{"listeners[0].destination.method", listener.Delivery.Destination.Method, "POST"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestInstanceFollowsAnOverriddenSchemaUnlessItIsWritten(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		instance   string
		wantSchema string
		want       string
	}{
		{name: "built-in schema and derived instance", wantSchema: "noty", want: "noty"},
		{name: "overridden schema and derived instance", schema: "tenant", wantSchema: "tenant", want: "tenant"},
		{name: "written instance wins over schema", schema: "tenant", instance: "owner", wantSchema: "tenant", want: "owner"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := resolvedInstanceDocument(t, tc.schema, tc.instance)
			if cfg.Database.Schema != tc.wantSchema {
				t.Errorf("database schema = %q, want %q", cfg.Database.Schema, tc.wantSchema)
			}
			if cfg.Instance != tc.want {
				t.Errorf("instance = %q, want %q", cfg.Instance, tc.want)
			}
		})
	}
}

func resolvedInstanceDocument(t *testing.T, schema, instance string) *Config {
	t.Helper()
	document := "version: 1\n"
	if instance != "" {
		document += "instance: " + instance + "\n"
	}
	document += "database:\n  url: postgres://noty@db/noty\n"
	if schema != "" {
		document += "  schema: " + schema + "\n"
	}
	document += "listeners:\n  - name: order_paid\n    table: public.orders\n" +
		"    operations: [insert]\n    destination:\n      url: https://hooks.example.test/orders\n"

	raw, diags := stageH(t, document)
	if len(diags) != 0 {
		t.Fatalf("instance fixture has diagnostics: %+v", diags)
	}
	return resolveConfig(raw)
}

func TestRootWorkerAndRetentionOverridesSurviveResolution(t *testing.T) {
	document := "version: 1\n" +
		"database:\n  url: postgres://noty@db/noty\n" +
		"worker:\n  concurrency: 8\n  batch_size: 250\n  poll_interval: 2s\n" +
		"  lease_timeout: 2m\n  drain_timeout: 45s\n" +
		"retention:\n  keep: 720h\n  partition_interval: 48h\n  precreate: 96h\n" +
		"listeners:\n  - name: order_paid\n    table: public.orders\n" +
		"    operations: [insert]\n    destination:\n" +
		"      url: https://hooks.example.test/orders\n"

	raw, diags := stageH(t, document)
	if len(diags) != 0 {
		t.Fatalf("root override fixture has diagnostics: %+v", diags)
	}
	cfg := resolveConfig(raw)
	tests := []struct {
		path string
		got  any
		want any
	}{
		{"worker.concurrency", cfg.Worker.Concurrency, 8},
		{"worker.batch_size", cfg.Worker.BatchSize, 250},
		{"worker.poll_interval", cfg.Worker.PollInterval, 2 * time.Second},
		{"worker.lease_timeout", cfg.Worker.LeaseTimeout, 2 * time.Minute},
		{"worker.drain_timeout", cfg.Worker.DrainTimeout, 45 * time.Second},
		{"retention.keep", cfg.Retention.Keep, 720 * time.Hour},
		{"retention.partition_interval", cfg.Retention.PartitionInterval, 48 * time.Hour},
		{"retention.precreate", cfg.Retention.Precreate, 96 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestResolutionPreservesCanonicalOperationsAndTheirPositions(t *testing.T) {
	document := "version: 1\ndatabase:\n  url: postgres://noty@db/noty\nlisteners:\n" +
		"  - name: order_paid\n    table: public.orders\n    operations:\n" +
		"      delete:\n        when: OLD.cancelled\n      insert: {}\n      update:\n" +
		"        columns: [status]\n        is_distinct: true\n        when: OLD.status <> NEW.status\n" +
		"    destination:\n      url: https://hooks.example.test/orders\n"
	cfg, warnings, errs := Parse([]byte(document), "positions.yaml", MapEnv(nil))
	if len(errs) != 0 || len(warnings) != 0 || cfg == nil {
		t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
	}
	trigger := cfg.Listeners[0].Trigger
	if got := operationKinds(trigger.Operations); !reflect.DeepEqual(got, []string{"insert", "update", "delete"}) {
		t.Errorf("operation order = %v, want insert, update, delete", got)
	}
	assertResolvedPosition(t, trigger.tablePosition, "listeners[0].table", 6, 12)
	assertResolvedPosition(t, trigger.Operations[1].columnsPosition,
		"listeners[0].operations.update.columns", 12, 18)
	assertResolvedPosition(t, trigger.Operations[1].isDistinctPosition,
		"listeners[0].operations.update.is_distinct", 13, 22)
	assertResolvedPosition(t, trigger.Operations[1].whenPosition,
		"listeners[0].operations.update.when", 14, 15)
	assertResolvedPosition(t, trigger.Operations[2].whenPosition,
		"listeners[0].operations.delete.when", 9, 15)
}

func operationKinds(operations Operations) []string {
	kinds := make([]string, 0, len(operations))
	for _, operation := range operations {
		kinds = append(kinds, operation.Kind)
	}
	return kinds
}

func assertResolvedPosition(t *testing.T, got Positioned, path string, line, col int) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s position is nil", path)
	}
	if got.File() != "positions.yaml" || got.Path() != path || got.Line() != line || got.Col() != col {
		t.Errorf("position = %s:%d:%d %s, want positions.yaml:%d:%d %s",
			got.File(), got.Line(), got.Col(), got.Path(), line, col, path)
	}
}
