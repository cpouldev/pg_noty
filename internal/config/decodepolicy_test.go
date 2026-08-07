package config

import (
	"testing"
)

// TestStageGReportsNothingButWrapperConversionFailures pins the stage policy on
// both sides: a fully readable tree is silent, and an unreadable wrapper contributes
// its own positioned conversion diagnostic without a stage-level aggregate.
func TestStageGReportsNothingButWrapperConversionFailures(t *testing.T) {
	t.Run("a readable tree is silent", func(t *testing.T) {
		if _, diags := stageG(t, everyKindOfValue("  concurrency: 8\n")); len(diags) != 0 {
			t.Errorf("stage G reported %q for readable values", messagesOf(diags))
		}
	})

	t.Run("a wrapper refusal is the only diagnostic", func(t *testing.T) {
		_, diags := stageG(t, everyKindOfValue("  concurrency: not-an-integer\n"))
		if len(diags) != 1 {
			t.Fatalf("stage G reported %q, want one wrapper conversion failure", messagesOf(diags))
		}
		if diags[0].Rule != RuleDecode || diags[0].Path != "worker.concurrency" {
			t.Errorf("diagnostic = %+v, want RuleDecode at worker.concurrency", diags[0])
		}
		// Twelve lines precede extraWorker -- version, instance, auto_reconcile, database and its
		// three keys, worker and its four scalars -- so `  concurrency: not-an-integer` is line 13.
		// Two spaces, `concurrency`, a colon and a space are fifteen runes, putting the `n` of the
		// value at column 16.
		if diags[0].Line != 13 || diags[0].Col != 16 {
			t.Errorf("diagnostic is at %d:%d, want the value at 13:16", diags[0].Line, diags[0].Col)
		}
	})
}

// everyKindOfValue writes every wrapper-bearing path in rawConfig exactly where
// the schema declares it. extraWorker is inserted after drain_timeout so tests can
// exercise worker.concurrency while retaining a stable 13:16 position.
func everyKindOfValue(extraWorker string) string {
	return "version: 1\n" +
		"instance: noty\n" +
		"auto_reconcile: true\n" +
		"database:\n" +
		"  url: postgres://noty:secret@db.internal/noty\n" +
		"  schema: public\n" +
		"  listen_url: postgres://noty:secret@replica.internal/noty\n" +
		"worker:\n" +
		"  batch_size: 100\n" +
		"  poll_interval: 1s\n" +
		"  lease_timeout: 30s\n" +
		"  drain_timeout: 45s\n" +
		extraWorker +
		"  allowed_destination_cidrs: [100.64.0.0/10]\n" +
		"retention:\n" +
		"  keep: 168h\n" +
		"  partition_interval: 24h\n" +
		"  precreate: 48h\n" +
		"defaults:\n" +
		"  timeout: 10s\n" +
		"  retry:\n" +
		"    max_attempts: 5\n" +
		"    backoff: exponential\n" +
		"    initial_interval: 1s\n" +
		"    max_interval: 1m\n" +
		"    jitter: true\n" +
		"  headers:\n" +
		"    X-Default: default\n" +
		"listeners:\n" +
		"- name: order_paid\n" +
		"  enabled: true\n" +
		"  table: public.orders\n" +
		"  operations:\n" +
		"    insert: {}\n" +
		"    update:\n" +
		"      columns: [id, total]\n" +
		"      when: NEW.total > 0\n" +
		"    delete: {}\n" +
		"  payload:\n" +
		"    mode: full\n" +
		"    columns: [id, total]\n" +
		"    exclude: [internal_note]\n" +
		"    include_old: true\n" +
		"    max_bytes: 4096\n" +
		"  destination:\n" +
		"    url: https://example.test/orders\n" +
		"    method: POST\n" +
		"    headers:\n" +
		"      X-Listener: order_paid\n" +
		"    signing:\n" +
		"      secrets: [current, previous]\n" +
		"  retry:\n" +
		"    max_attempts: 3\n" +
		"    backoff: linear\n" +
		"    initial_interval: 2s\n" +
		"    max_interval: 30s\n" +
		"    jitter: false\n" +
		"  timeout: 8s\n" +
		"  concurrency: 4\n"
}
