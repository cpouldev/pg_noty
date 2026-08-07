//go:build integration

package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEveryCatalogReadingIsTakenOnAPinnedSession proves the reader pins a hostile connection and
// its negative control drives the server directly outside the reader.
func TestEveryCatalogReadingIsTakenOnAPinnedSession(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	fixture := newOwnershipCatalogFixture(t, pool, "alpha", "orders", "catalog_pin_target")
	plantCatalogObject(t, pool, fixture, "trigger", &fixture.set.Marker)
	target := resolvedCatalogTarget(t, pool, "public.catalog_pin_target")

	hostile := readCatalogDefinitions(t, pool, target.OID, fixture.set.TriggerName, true)
	clean := readCatalogDefinitions(t, pool, target.OID, fixture.set.TriggerName, false)
	if hostile != clean {
		t.Fatalf("reader changed with caller search_path: hostile=%#v clean=%#v", hostile, clean)
	}
	outside := readOutsideCatalog(t, pool, target.OID, fixture.set.TriggerName)
	t.Logf("outside service-schema trigger definition: %s", outside[0])
	t.Logf("outside service-schema function definition: %s", outside[1])
	if outside[0] == hostile[0] || outside[1] != hostile[1] {
		t.Fatalf("outside service-schema reading=%#v; pinned reader=%#v; want the trigger's function qualifier to differ while the function reading remains stable", outside, hostile)
	}
}
func TestCatalogConnectionReadsBootstrapAndLockProvenance(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	reader, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	catalog := NewConnectionCatalog(reader)
	originalSearchPath := connectionSearchPath(t, reader)
	if exists, err := catalog.SchemaExists(t.Context(), "public"); err != nil || !exists {
		t.Fatalf("SchemaExists(public) = %t, %v; want true, nil", exists, err)
	}
	if exists, err := catalog.SchemaExists(t.Context(), "catalog_absent_schema"); err != nil || exists {
		t.Fatalf("SchemaExists(absent) = %t, %v; want false, nil", exists, err)
	}
	holder, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	const key int64 = 820317
	mustExecOn(t, holder, "SELECT pg_advisory_lock($1)", key)
	defer mustExecOn(t, holder, "SELECT pg_advisory_unlock($1)", key)
	var holderID string
	if err := holder.QueryRow(t.Context(), "SELECT pg_backend_pid()::text").Scan(&holderID); err != nil {
		t.Fatal(err)
	}
	if got, found, err := catalog.AdvisoryLockHolder(t.Context(), key); err != nil || !found || got != holderID {
		t.Fatalf("AdvisoryLockHolder() = %q, %t, %v; want %q, true, nil", got, found, err, holderID)
	}

	mustExecOn(t, pool, "CREATE TABLE public.catalog_blocker_target (id integer)")
	holderTx, err := holder.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holderTx.Rollback(t.Context())
	mustExecOn(t, holderTx, "LOCK TABLE public.catalog_blocker_target IN ACCESS EXCLUSIVE MODE")
	waiter, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Release()
	waiterTx, err := waiter.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer waiterTx.Rollback(t.Context())
	var waiterID int32
	if err := waiter.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&waiterID); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() {
		_, err := waiterTx.Exec(t.Context(), "LOCK TABLE public.catalog_blocker_target IN ACCESS EXCLUSIVE MODE")
		blocked <- err
	}()
	assertCatalogBlockingBackend(t, catalog, waiterID, holderID)
	if err := holderTx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	if got := connectionSearchPath(t, reader); got != originalSearchPath {
		t.Fatalf("connection search_path = %q; want restored %q", got, originalSearchPath)
	}
}

func TestConnectionCatalogRollsBackAfterObservationDeadline(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	on, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer on.Release()
	originalSearchPath := connectionSearchPath(t, on)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err = NewConnectionCatalog(on).pinSearchPath(ctx, func(reader catalogReader) error {
		var one int
		if err := reader.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired connection catalog read = %v", err)
	}
	if err := on.Ping(context.Background()); err != nil {
		t.Fatalf("expired catalog rollback left its pinned connection unusable: %v", err)
	}
	if got := connectionSearchPath(t, on); got != originalSearchPath {
		t.Fatalf("connection search_path = %q; want restored %q", got, originalSearchPath)
	}
}
func connectionSearchPath(t *testing.T, on *pgxpool.Conn) string {
	t.Helper()
	var path string
	if err := on.QueryRow(t.Context(), "SHOW search_path").Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}
func assertCatalogBlockingBackend(t *testing.T, catalog Catalog, waiterID int32, holderID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, found, err := catalog.BlockingBackend(t.Context(), waiterID)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			if got != holderID {
				t.Fatalf("BlockingBackend() = %q; want holder %q", got, holderID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("BlockingBackend() did not observe the waiting backend")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func readCatalogDefinitions(t *testing.T, pool *pgxpool.Pool, oid uint32, trigger string, hostile bool) [2]string {
	t.Helper()
	catalog, tx := openCatalogForTest(t, pool)
	defer tx.Rollback(t.Context())
	if hostile {
		mustExecOn(t, tx, "SET LOCAL search_path TO noty, public")
	}
	pair, found, err := catalog.ReadPair(t.Context(), oid, trigger)
	if err != nil || !found {
		t.Fatalf("pinned ReadPair() = %#v, %t, %v", pair, found, err)
	}
	return [2]string{pair.Trigger.Definition, pair.Function.Definition}
}

func readOutsideCatalog(t *testing.T, pool *pgxpool.Pool, oid uint32, trigger string) [2]string {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	mustExecOn(t, tx, "SET LOCAL search_path TO noty, public")
	return outsideCatalogDefinitions(t, tx, oid, trigger)
}

func outsideCatalogDefinitions(t *testing.T, tx pgx.Tx, oid uint32, trigger string) [2]string {
	t.Helper()
	var triggerDefinition, functionDefinition string
	err := tx.QueryRow(t.Context(), `SELECT pg_get_triggerdef(trigger.oid), pg_get_functiondef(function.oid)
FROM pg_trigger AS trigger JOIN pg_proc AS function ON function.oid = trigger.tgfoid
WHERE trigger.tgrelid = $1 AND trigger.tgname = $2`, oid, trigger).Scan(&triggerDefinition, &functionDefinition)
	if err != nil {
		t.Fatal(err)
	}
	return [2]string{triggerDefinition, functionDefinition}
}
