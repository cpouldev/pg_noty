//go:build integration

package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const cliHarnessImage = "postgres:17.10-alpine"

var (
	cliContainer *postgres.PostgresContainer
	cliStarted   atomic.Int64
)

func TestMain(m *testing.M) { os.Exit(runCLIHarness(m)) }

func runCLIHarness(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := postgres.Run(
		ctx, cliHarnessImage,
		postgres.WithDatabase("notycli"), postgres.WithUsername("noty"),
		postgres.WithPassword("noty-cli-secret"), postgres.WithSQLDriver("pgx"),
		postgres.BasicWaitStrategies(),
	)
	cliStarted.Add(1)
	if container != nil {
		defer func() { _ = container.Terminate(ctx) }()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "cli harness unavailable: %v\n", err)
		return 1
	}
	cliContainer = container
	if err := container.Snapshot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cli harness snapshot failed: %v\n", err)
		return 1
	}
	code := m.Run()
	if code == 0 && cliStarted.Load() != 1 {
		fmt.Fprintf(os.Stderr, "cli harness started %d containers, want one\n", cliStarted.Load())
		return 1
	}
	return code
}

func cliDatabase(t *testing.T, listeners bool) (*pgxpool.Pool, config.Config, string) {
	t.Helper()
	pool, cfg, path := cliDatabaseAwaitingBootstrap(t, listeners)
	if err := schema.Bootstrap(t.Context(), pool, cfg); err != nil {
		t.Fatalf("bootstrap CLI fixture: %v", err)
	}
	return pool, cfg, path
}

// cliDatabaseAwaitingBootstrap is the same fixture with its service schema still absent, which is
// what a database an operator has not yet run `pg_noty bootstrap` against looks like. cliDatabase is
// this plus the bootstrap, so the two cannot drift in anything but that one step.
func cliDatabaseAwaitingBootstrap(t *testing.T, listeners bool) (*pgxpool.Pool, config.Config, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the container-backed tier under -short")
	}
	if cliContainer == nil {
		t.Fatal("CLI harness container is unavailable")
	}
	if err := cliContainer.Restore(t.Context()); err != nil {
		t.Fatalf("restore CLI database: %v", err)
	}
	dsn, err := cliContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(
		t.Context(),
		"CREATE TABLE public.orders (id bigint, customer text, total_cents int)",
	); err != nil {
		t.Fatal(err)
	}
	path, cfg := writeCLIConfig(t, dsn, listeners)
	return pool, cfg, path
}

func writeCLIConfig(t *testing.T, dsn string, listeners bool) (string, config.Config) {
	t.Helper()
	listener := "listeners: []\n"
	if listeners {
		listener = "listeners:\n  - name: orders\n    enabled: true\n    table: public.orders\n    operations: [insert]\n    destination:\n      url: https://hooks.example.test/orders\n      signing:\n        secrets: [cli-secret]\n"
	}
	text := fmt.Sprintf("version: 1\ndatabase:\n  url: %q\n  schema: noty\n%s", dsn, listener)
	value, _, errs := config.Parse([]byte(text), "cli-harness.yaml", config.MapEnv(nil))
	if len(errs) != 0 || value == nil {
		t.Fatalf("parse CLI fixture: %v", errs)
	}
	path := filepath.Join(t.TempDir(), "listeners.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, *value
}

func cliEvent(t *testing.T, pool *pgxpool.Pool, cfg config.Config, listener, status string, age time.Duration) int64 {
	t.Helper()
	query := `WITH event AS (INSERT INTO "noty".events
 (listener, table_name, operation, payload, txid, occurred_at)
 VALUES ($1, '"public"."orders"', 'INSERT', '{}'::jsonb, pg_current_xact_id(), $2)
 RETURNING id, occurred_at)
 INSERT INTO "noty".event_queue
 (event_id, occurred_at, listener, status, attempts, next_attempt_at, dead_reason)
 SELECT id, occurred_at, $1, $3, 3, now()+interval '1 hour', 'retry me' FROM event
 RETURNING event_id`
	var id int64
	if err := pool.QueryRow(t.Context(), query, listener, time.Now().Add(-age), status).Scan(&id); err != nil {
		t.Fatalf("insert CLI queue row: %v", err)
	}
	return id
}
