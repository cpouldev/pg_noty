package config

import (
	"testing"
	"time"
)

func TestBuiltInDefaultsEnumerateEveryContractValueIndividually(t *testing.T) {
	if got := len(builtInDefaults); got != 23 {
		t.Fatalf("builtInDefaults has %d entries, want 23", got)
	}

	got := resolvedBuiltInDefaults()
	tests := []struct {
		path string
		got  any
		want any
	}{
		{"database.schema", got.database.Schema, "noty"},
		{"instance", got.instance("tenant"), "tenant"},
		{"auto_reconcile", got.autoReconcile, false},
		{"worker.concurrency", got.worker.Concurrency, 16},
		{"worker.batch_size", got.worker.BatchSize, 100},
		{"worker.poll_interval", got.worker.PollInterval, 10 * time.Second},
		{"worker.lease_timeout", got.worker.LeaseTimeout, 5 * time.Minute},
		{"worker.drain_timeout", got.worker.DrainTimeout, 30 * time.Second},
		{"retention.keep", got.retention.Keep, 168 * time.Hour},
		{"retention.partition_interval", got.retention.PartitionInterval, 24 * time.Hour},
		{"retention.precreate", got.retention.Precreate, 168 * time.Hour},
		{"timeout", got.listener.timeout, 5 * time.Second},
		{"retry.max_attempts", got.listener.retry.MaxAttempts, 5},
		{"retry.backoff", got.listener.retry.Backoff, "exponential"},
		{"retry.initial_interval", got.listener.retry.InitialInterval, 10 * time.Second},
		{"retry.max_interval", got.listener.retry.MaxInterval, time.Hour},
		{"retry.jitter", got.listener.retry.Jitter, true},
		{"enabled", got.listener.enabled, true},
		{"operations.update.is_distinct", got.listener.operationDistinct, false},
		{"payload.mode", got.listener.payload.Mode, "full"},
		{"payload.include_old", got.listener.payload.IncludeOld, false},
		{"payload.max_bytes", got.listener.payload.MaxBytes, 262144},
		{"destination.method", got.listener.destinationMethod, "POST"},
	}

	for i, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if builtInDefaults[i].path != tc.path {
				t.Errorf("table entry %d is %q, want %q", i, builtInDefaults[i].path, tc.path)
			}
			if tc.got != tc.want {
				t.Errorf("default = %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestInstanceDefaultFollowsTheEffectiveDatabaseSchema(t *testing.T) {
	got := resolvedBuiltInDefaults()

	if got.database.Schema != "noty" {
		t.Fatalf("database.schema = %q, want %q", got.database.Schema, "noty")
	}
	if instance := got.instance(got.database.Schema); instance != "noty" {
		t.Errorf("instance from built-in schema = %q, want %q", instance, "noty")
	}
	if instance := got.instance("tenant"); instance != "tenant" {
		t.Errorf("instance from overridden schema = %q, want tenant", instance)
	}
}
