//go:build integration

package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStatusIntegrationHarnessIsPresent(t *testing.T) {
	pool, cfg, path := cliDatabase(t, true)
	cliEvent(t, pool, cfg, "orders", "pending", 4*time.Second)
	cliEvent(t, pool, cfg, "orders", "delivering", 2*time.Second)
	cliEvent(t, pool, cfg, "orders", "dead", 3*time.Second)
	var output bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "status"}, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("status=%d output=%s", got, output.String())
	}
	for _, want := range []string{"schema_version: 2", "listeners: 1", "queue_pending: 1", "queue_delivering: 1", "queue_dead: 1", "lag_seconds:"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("status output lacks %q: %s", want, output.String())
		}
	}
	if got := Execute(t.Context(), []string{"--config", path, "status"}, Streams{Out: &output, Err: &output}); got == 2 {
		t.Fatal("status returned reconciliation code 2")
	}
}

// TestStatusOnAnUnbootstrappedDatabaseNamesTheRemedyRatherThanTheMissingRelation pins the reason
// status reads the ledger before the queue. Observing the queue first answers with
// `relation "noty.event_queue" does not exist (SQLSTATE 42P01)`, which is a driver diagnostic about
// a table an operator never asked about; troubleshooting sends people here first, so this is the
// message that has to name the command.
func TestStatusOnAnUnbootstrappedDatabaseNamesTheRemedyRatherThanTheMissingRelation(t *testing.T) {
	_, cfg, path := cliDatabaseAwaitingBootstrap(t, true)

	var output bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "status"}, Streams{Out: &output, Err: &output}); got != 1 {
		t.Fatalf("status=%d output=%s, want 1", got, output.String())
	}
	if !strings.Contains(output.String(), "`pg_noty bootstrap`") ||
		!strings.Contains(output.String(), cfg.Database.Schema) {
		t.Errorf("status reported %q, want the schema and the command that answers it", output.String())
	}
	if strings.Contains(output.String(), "SQLSTATE") || strings.Contains(output.String(), "event_queue") {
		t.Errorf("status reported %q, which is the driver diagnostic rather than the remedy", output.String())
	}
}
