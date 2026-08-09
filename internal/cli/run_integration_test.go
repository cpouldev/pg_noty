//go:build integration

package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRunReconcilesAndServesWhenItIsAllowedTo is the path run always took, now reached by asking for
// it. The fixture's configuration does not name auto_reconcile, so this also pins that the flag can
// still turn the whole obligation on for a one-off.
func TestRunReconcilesAndServesWhenItIsAllowedTo(t *testing.T) {
	pool, _, path := cliDatabaseAwaitingBootstrap(t, true)

	serveUntilReady(t, []string{"--config", path, "run", "--reconcile"})

	if got := installedTriggers(t, pool); got == 0 {
		t.Error("run installed no trigger while it was allowed to reconcile")
	}
}

// TestRunServesAConvergedDatabaseWithoutChangingIt is the ordinary steady state under the default:
// an operator has already run bootstrap and apply, so run verifies, finds nothing to do and serves.
// The trigger count is read before and after, because "verified" and "quietly re-applied" look the
// same from outside if only the end state is asserted.
func TestRunServesAConvergedDatabaseWithoutChangingIt(t *testing.T) {
	pool, _, path := cliDatabase(t, true)
	var applied bytes.Buffer
	if got := Execute(t.Context(), []string{"--config", path, "apply", "--auto-approve"},
		Streams{Out: &applied, Err: &applied}); got != 0 {
		t.Fatalf("apply status=%d output=%s", got, applied.String())
	}
	before := installedTriggers(t, pool)

	serveUntilReady(t, []string{"--config", path, "run"})

	if after := installedTriggers(t, pool); after != before {
		t.Errorf("triggers went from %d to %d across a run that may not reconcile", before, after)
	}
}

// TestRunWithoutAutoReconcileCreatesNothingOnADatabaseNothingHasBootstrapped is success criterion 1,
// asserted by what the database holds rather than by inspecting a flag: a run that bootstrapped
// anyway and merely suppressed its log line would leave the schema behind and fail here.
func TestRunWithoutAutoReconcileCreatesNothingOnADatabaseNothingHasBootstrapped(t *testing.T) {
	pool, cfg, path := cliDatabaseAwaitingBootstrap(t, true)

	status, output := runUntilItReturns(t, []string{"--config", path, "run", "--listen", freeCLIAddress(t)})

	if status == 0 {
		t.Fatalf("run served a database with no schema, status=%d output=%s", status, output)
	}
	if !strings.Contains(output, "`pg_noty bootstrap`") {
		t.Errorf("run reported %q, want the command that answers an absent schema", output)
	}
	var exists bool
	if err := pool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", cfg.Database.Schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf("run created schema %s while auto_reconcile was off", cfg.Database.Schema)
	}
}

// TestRunWithoutAutoReconcileRefusesRatherThanServingAWorkerNoTriggerFeeds is the case a silent skip
// would have shipped as the default: the schema is present, the listener's trigger is not, and a run
// that served would poll a queue nothing fills while every health check answered green.
func TestRunWithoutAutoReconcileRefusesRatherThanServingAWorkerNoTriggerFeeds(t *testing.T) {
	pool, _, path := cliDatabase(t, true)

	status, output := runUntilItReturns(t, []string{"--config", path, "run", "--listen", freeCLIAddress(t)})

	if status == 0 {
		t.Fatalf("run served an unreconciled configuration, status=%d output=%s", status, output)
	}
	if got := installedTriggers(t, pool); got != 0 {
		t.Errorf("run installed %d triggers while auto_reconcile was off", got)
	}
}

// TestTheConfiguredKeyTurnsReconciliationOnWithoutAFlag drives the same decision from the file, so
// the key is pinned end to end rather than only through reconcileChoice's own unit test.
func TestTheConfiguredKeyTurnsReconciliationOnWithoutAFlag(t *testing.T) {
	pool, _, written := cliDatabaseAwaitingBootstrap(t, true)
	path := withRootKey(t, written, "auto_reconcile: true\n")

	serveUntilReady(t, []string{"--config", path, "run"})

	if got := installedTriggers(t, pool); got == 0 {
		t.Error("run installed no trigger though the configuration turned auto_reconcile on")
	}
}

// serveUntilReady starts run, waits for both endpoints, then cancels and requires a clean exit.
func serveUntilReady(t *testing.T, args []string) {
	t.Helper()
	address := freeCLIAddress(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	result := make(chan int, 1)
	go func() { result <- Execute(ctx, append(args, "--listen", address), Streams{Out: &output, Err: &output}) }()
	base := "http://" + address
	waitHTTP(t, base+"/healthz", http.StatusOK)
	waitHTTP(t, base+"/readyz", http.StatusOK)
	cancel()
	if got := <-result; got != 0 {
		t.Fatalf("run status=%d output=%s", got, output.String())
	}
}

// runUntilItReturns starts run and waits for it to come back on its own, which is what a refused
// startup does. A run that became ready would block until the test's own deadline.
func runUntilItReturns(t *testing.T, args []string) (int, string) {
	t.Helper()
	var output bytes.Buffer
	status := Execute(t.Context(), args, Streams{Out: &output, Err: &output})
	return status, output.String()
}

// installedTriggers counts the triggers on the fixture's target table, ignoring the internal ones
// PostgreSQL creates for constraints.
func installedTriggers(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid = 'public.orders'::regclass").
		Scan(&count); err != nil {
		t.Fatalf("count triggers: %v", err)
	}
	return count
}

// withRootKey rewrites the fixture's configuration with one more root key, so a test can drive the
// configured default rather than only the flag. It prepends, because appending would land inside the
// listener sequence the fixture ends with.
func withRootKey(t *testing.T, path, line string) string {
	t.Helper()
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := filepath.Join(t.TempDir(), "listeners.yaml")
	if err := os.WriteFile(updated, append([]byte(line), written...), 0o600); err != nil {
		t.Fatal(err)
	}
	return updated
}

func freeCLIAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func waitHTTP(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not return status %d", url, want)
}
