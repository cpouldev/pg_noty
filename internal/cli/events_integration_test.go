//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/source"
)

func TestEventsIntegrationHarnessIsPresent(t *testing.T) {
	pool, cfg, path := cliDatabase(t, true)
	ids := map[string]int64{
		"pending-orders":      cliEvent(t, pool, cfg, "orders", "pending", 0),
		"dead-orders":         cliEvent(t, pool, cfg, "orders", "dead", 0),
		"dead-payments":       cliEvent(t, pool, cfg, "payments", "dead", 0),
		"delivering-payments": cliEvent(t, pool, cfg, "payments", "delivering", 0),
	}
	for name, args := range map[string][]string{
		"all": {}, "dead": {"--status", "dead"}, "orders": {"--listener", "orders"},
		"both": {"--listener", "orders", "--status", "dead"},
	} {
		t.Run(
			name, func(t *testing.T) {
				var output bytes.Buffer
				args = append([]string{"--config", path, "events", "list"}, args...)
				if got := Execute(t.Context(), args, Streams{Out: &output, Err: &output}); got != 0 {
					t.Fatalf("list status=%d output=%s", got, output.String())
				}
				var events []source.QueueEvent
				if err := json.Unmarshal(output.Bytes(), &events); err != nil {
					t.Fatal(err)
				}
				if name == "both" && (len(events) != 1 || events[0].ID != ids["dead-orders"]) {
					t.Fatalf("both selector returned %#v", events)
				}
				if name == "all" && len(events) != len(ids) {
					t.Fatalf("unfiltered list length=%d, want %d", len(events), len(ids))
				}
			},
		)
	}
	var output bytes.Buffer
	var beforeStatus string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT status FROM noty.event_queue WHERE event_id=$1",
		ids["dead-payments"],
	).Scan(&beforeStatus); err != nil {
		t.Fatal(err)
	}
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "--dry-run", "events", "retry", "--listener", "payments", "--status", "dead"},
		Streams{Out: &output, Err: &output},
	); got != 0 {
		t.Fatalf("dry-run retry status=%d output=%s", got, output.String())
	}
	var afterStatus string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT status FROM noty.event_queue WHERE event_id=$1",
		ids["dead-payments"],
	).Scan(&afterStatus); err != nil {
		t.Fatal(err)
	}
	if beforeStatus != afterStatus {
		t.Fatalf("dry-run changed queue status from %s to %s", beforeStatus, afterStatus)
	}
	output.Reset()
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "events", "retry", "--id", fmt.Sprint(ids["dead-orders"])},
		Streams{Out: &output, Err: &output},
	); got != 0 {
		t.Fatalf("retry status=%d output=%s", got, output.String())
	}
	var status string
	var attempts int
	var reason *string
	var next time.Time
	if err := pool.QueryRow(
		t.Context(),
		`SELECT status, attempts, next_attempt_at, dead_reason FROM noty.event_queue WHERE event_id=$1`,
		ids["dead-orders"],
	).Scan(&status, &attempts, &next, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 || reason != nil || next.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("retry state=%s/%d/%s/%v", status, attempts, next, reason)
	}
	output.Reset()
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "events", "retry"},
		Streams{Out: &output, Err: &output},
	); got == 0 || got == 2 || strings.Contains(output.String(), "retried") {
		t.Fatalf("empty retry selector status=%d output=%s", got, output.String())
	}
}

func TestEventsRetryUsesBoundedBatchesAtOperatorScale(t *testing.T) {
	pool, _, path := cliDatabase(t, true)
	const rows = 100001
	_, err := pool.Exec(
		t.Context(), `WITH events AS (INSERT INTO noty.events
 (listener, table_name, operation, payload, txid, occurred_at)
 SELECT 'orders', '"public"."orders"', 'INSERT', '{}'::jsonb, pg_current_xact_id(), now()-(n * interval '1 second')
 FROM generate_series(1, 100001) AS n RETURNING id, occurred_at)
 INSERT INTO noty.event_queue
 (event_id, occurred_at, listener, status, attempts, next_attempt_at, dead_reason)
 SELECT id, occurred_at, 'orders', 'dead', 5, now()+interval '1 hour', 'batch' FROM events`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := pool.QueryRow(
		t.Context(),
		"SELECT xact_commit FROM pg_stat_database WHERE datname=current_database()",
	).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "events", "retry", "--listener", "orders", "--status", "dead"},
		Streams{Out: &output, Err: &output},
	); got != 0 {
		t.Fatalf("large retry status=%d output=%s", got, output.String())
	}
	var after, pending, attempts int64
	if err := pool.QueryRow(
		t.Context(),
		"SELECT xact_commit FROM pg_stat_database WHERE datname=current_database()",
	).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		t.Context(),
		"SELECT count(*) FILTER (WHERE status='pending'), coalesce(max(attempts),0) FROM noty.event_queue",
	).Scan(&pending, &attempts); err != nil {
		t.Fatal(err)
	}
	if pending != rows || attempts != 0 || after-before < 100 {
		t.Fatalf(
			"retry rows=%d attempts=%d commits=%d, want %d rows, zero attempts and >=100 commits",
			pending,
			attempts,
			after-before,
			rows,
		)
	}
}
