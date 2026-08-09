//go:build integration

package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestPlanAndApplyIntegrationHarnessIsPresent(t *testing.T) {
	_, _, path := cliDatabase(t, true)
	var output bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "plan"}, Streams{Out: &output, Err: &output}); got != 2 {
		t.Fatalf("pending plan status=%d output=%s, want 2", got, output.String())
	}
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "apply", "--auto-approve"},
		Streams{Out: &output, Err: &output},
	); got != 0 {
		t.Fatalf("apply status=%d output=%s", got, output.String())
	}
	if got := Execute(t.Context(), []string{"--config", path, "plan"}, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("clean plan status=%d output=%s", got, output.String())
	}
}

func TestApplyRunLockRefusalUsesCallerBoundAndNamesHolder(t *testing.T) {
	pool, cfg, path := cliDatabase(t, true)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	key := schema.ReconcileLockKey(cfg.Database.Schema, cfg.Instance)
	if _, err := conn.Exec(t.Context(), "SELECT pg_advisory_lock($1)", key); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(t.Context(), "SELECT pg_advisory_unlock($1)", key) }()
	var output bytes.Buffer
	status := Execute(
		t.Context(),
		[]string{"--config", path, "apply", "--auto-approve", "--run-lock-wait", (20 * time.Millisecond).String()},
		Streams{Out: &output, Err: &output},
	)
	if status != 1 || status == 2 || !strings.Contains(
		output.String(),
		cfg.Instance,
	) || !strings.Contains(output.String(), "holder backend is") {
		t.Fatalf("lock refusal status=%d output=%s", status, output.String())
	}
}
