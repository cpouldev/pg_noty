//go:build integration

package source

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestTwoInstancesKnownGlobalGuardCostsLatencyNotCorrectness(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.instance_a (id int)`)
	mustExecOn(t, pool, `CREATE TABLE public.instance_b (id int)`)
	installInsertTrigger(t, pool, "instance_a", "a", "instance_a")
	installInsertTrigger(t, pool, "instance_b", "b", "instance_b")
	cfg := harnessConfig(t)
	listenerA := openNotificationConnection(t, cfg, "instance_a")
	listenerB := openNotificationConnection(t, cfg, "instance_b")
	writer, err := pgx.Connect(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(context.Background())
	if _, err := writer.Exec(t.Context(), `BEGIN`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(t.Context(), `INSERT INTO public.instance_a VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(t.Context(), `INSERT INTO public.instance_b VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(t.Context(), `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if !hasNotification(t, listenerA, time.Second) || hasNotification(t, listenerB, 250*time.Millisecond) {
		t.Fatal("global guard result changed: instance A should wake, B latency is polling-only")
	}
}
