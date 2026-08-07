//go:build integration

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestBootstrapThenApplyConvergesADatabaseNothingHasSetUp runs the sequence an operator runs with
// auto_reconcile off, against a database whose service schema does not exist. It is the case the
// harness used to reach by calling schema.Bootstrap directly, because until this command existed the
// CLI could reach it only by starting run -- while internal/reconcile's refusal named "bootstrap" as
// the remedy the whole time.
func TestBootstrapThenApplyConvergesADatabaseNothingHasSetUp(t *testing.T) {
	_, cfg, path := cliDatabaseAwaitingBootstrap(t, true)

	var absent bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "bootstrap", "--dry-run"}, Streams{Out: &absent, Err: &absent}); got != 2 {
		t.Fatalf("dry-run status=%d output=%s, want 2 while the schema is absent", got, absent.String())
	}
	if !strings.Contains(absent.String(), "applied_version: absent") {
		t.Errorf("dry run reported %q, want an absent applied version", absent.String())
	}
	if !strings.Contains(absent.String(), "schema: "+cfg.Database.Schema) {
		t.Errorf("dry run reported %q, want the configured schema named", absent.String())
	}

	var output bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "bootstrap"}, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("bootstrap status=%d output=%s", got, output.String())
	}
	if got := Execute(t.Context(), []string{"--config", path, "apply", "--auto-approve"}, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("apply status=%d output=%s", got, output.String())
	}
	if got := Execute(t.Context(), []string{"--config", path, "plan"}, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("clean plan status=%d output=%s", got, output.String())
	}

	// The same dry run now answers clean, so the exit status separates the two states rather than
	// reporting changes pending whatever the database holds.
	var settled bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "bootstrap", "--dry-run"}, Streams{Out: &settled, Err: &settled}); got != 0 {
		t.Fatalf("settled dry-run status=%d output=%s, want 0", got, settled.String())
	}
	if strings.Contains(settled.String(), "absent") {
		t.Errorf("settled dry run reported %q, want a recorded applied version", settled.String())
	}
}

// TestApplyRefusesAnUnbootstrappedDatabaseAndNamesTheRemedy is the other side of the same state: the
// reconciler refuses rather than half-installing, and the operator is told which command answers it.
// The remediation half is the assertion that would have failed before reportRefusals existed, when
// Refusal.Remediation had no caller outside its own package.
func TestApplyRefusesAnUnbootstrappedDatabaseAndNamesTheRemedy(t *testing.T) {
	_, _, path := cliDatabaseAwaitingBootstrap(t, true)

	var output bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "apply", "--auto-approve"}, Streams{Out: &output, Err: &output}); got != 1 {
		t.Fatalf("apply status=%d output=%s, want 1 for a refused reconcile", got, output.String())
	}
	if !strings.Contains(output.String(), "required bootstrap object") {
		t.Errorf("apply reported %q, want the absent bootstrap object named", output.String())
	}
	if !strings.Contains(output.String(), "remedy: ") || !strings.Contains(output.String(), "bootstrap for this service schema") {
		t.Errorf("apply reported %q, want the refusal's own remediation printed beside it", output.String())
	}
}
