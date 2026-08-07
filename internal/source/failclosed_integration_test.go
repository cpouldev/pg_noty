//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestImpossibleEnqueueAbortsTheCustomerTransaction(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.fail_target (id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "fail_target"
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	mustExecOn(
		t,
		connection,
		`CREATE OR REPLACE FUNCTION noty.fail_enqueue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'enqueue impossible'; END; $$`,
	)
	mustExecOn(
		t,
		connection,
		`CREATE TRIGGER fail_enqueue BEFORE INSERT ON noty.events FOR EACH ROW EXECUTE FUNCTION noty.fail_enqueue()`,
	)
	if _, err := connection.Exec(t.Context(), `INSERT INTO public.fail_target VALUES (1)`); err == nil {
		t.Fatal("impossible enqueue unexpectedly committed")
	}
	var events, queue int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM noty.events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM noty.event_queue").Scan(&queue); err != nil {
		t.Fatal(err)
	}
	if events != 0 || queue != 0 {
		t.Fatalf("failed write left events=%d queue=%d", events, queue)
	}
}
