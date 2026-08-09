//go:build integration

package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestRepairDefaultPartitionFollowOnCreate(t *testing.T) {
	pool, cfg, path := cliDatabase(t, false)
	ranges := schema.RequiredRanges(
		time.Now().Add(cfg.Retention.Precreate+2*cfg.Retention.PartitionInterval),
		cfg.Retention,
	)
	if len(ranges) == 0 {
		t.Fatal("required range arithmetic returned no follow-on range")
	}
	ranged := ranges[0]
	at := ranged.From.Add(cfg.Retention.PartitionInterval / 2)
	var id int64
	if err := pool.QueryRow(
		t.Context(), `INSERT INTO noty.events
 (listener, table_name, operation, payload, txid, occurred_at)
 VALUES ('orders', '"public"."orders"', 'INSERT', '{}'::jsonb, pg_current_xact_id(), $1) RETURNING id`, at,
	).Scan(&id); err != nil {
		t.Fatalf("plant DEFAULT event: %v", err)
	}
	var output bytes.Buffer
	args := []string{
		"--config", path, "repair-default-partition", "--from", ranged.From.Format(time.RFC3339Nano),
		"--to", ranged.To.Format(time.RFC3339Nano), "--name", ranged.Name,
	}
	if got := Execute(t.Context(), args, Streams{Out: &output, Err: &output}); got != 0 {
		t.Fatalf("repair status=%d output=%s", got, output.String())
	}
	var relation string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT tableoid::regclass::text FROM noty.events WHERE id=$1",
		id,
	).Scan(&relation); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(relation, ranged.Name) {
		t.Fatalf("event relation=%q, want partition %q", relation, ranged.Name)
	}
	if !strings.Contains(output.String(), fmt.Sprintf("repaired %s", ranged.Name)) {
		t.Fatalf("repair output=%s", output.String())
	}
}
