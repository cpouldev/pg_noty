//go:build integration

package source

import (
	"strconv"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The harness case the shared one cannot see. harnessConfig fixes database.schema at the default,
// so every method agreed about which schema to read whether or not any of them derived it -- and a
// Claim naming the default outright read an empty queue while the configured one filled.
//
// Both schemas are installed here and both are seeded, so a method still naming the default finds
// work to do rather than erroring on a missing relation. That is the production symptom: an idle
// worker beside a queue that keeps growing.
//
// What is claimed cannot be told apart by event id alone -- each schema has its own identity
// sequence, so both would otherwise begin at 1 -- so the configured schema's sequence is moved
// first, and the assertions read the queue rows themselves rather than the returned batch.

// theOffsetIdentity is where the configured schema's event ids begin, far enough above the default
// schema's that no row of one can be mistaken for a row of the other.
const theOffsetIdentity = 1000

func configuredForServiceSchema(t *testing.T, serviceSchema string) config.Config {
	t.Helper()
	cfg := harnessConfig(t)
	cfg.Database.Schema = serviceSchema
	return cfg
}

func offsetEventIdentity(t *testing.T, pool *pgxpool.Pool, serviceSchema string, from int64) {
	t.Helper()
	events := mustQualifyServiceTable(t, serviceSchema, schema.TableEvents)
	mustExecOn(t, pool, "ALTER TABLE "+events+" ALTER COLUMN id RESTART WITH "+strconv.FormatInt(from, 10))
}

func queueRowCountIn(t *testing.T, pool *pgxpool.Pool, serviceSchema string) int {
	t.Helper()
	var count int
	queue := mustQualifyServiceTable(t, serviceSchema, schema.TableEventQueue)
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+queue).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", queue, err)
	}
	return count
}

// assertUntouchedByAnyWorker is the whole point of seeding the default schema: a method reaching it
// leaves a mark here, and every mark is checked rather than only the row count.
func assertUntouchedByAnyWorker(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	row := readQueueRowIn(t, pool, harnessSchema, id)
	switch {
	case !row.present:
		t.Errorf(
			"the default schema's queue row %d is gone, and no method may reach a schema the "+
				"configuration does not name", id,
		)
	case row.status != "pending":
		t.Errorf("the default schema's queue row is %s, want the pending it was seeded as", row.status)
	case row.leasedBy != nil:
		t.Errorf("the default schema's queue row is leased by %s", *row.leasedBy)
	case row.attempts != 0:
		t.Errorf(
			"the default schema's queue row counts %d attempts, want the 0 it was seeded with",
			row.attempts,
		)
	}
}

func TestClaimAndAckReadTheConfiguredServiceSchemaRatherThanTheDefault(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	applySourceMigrationsInto(t, pool, aNonDefaultServiceSchema)
	offsetEventIdentity(t, pool, aNonDefaultServiceSchema, theOffsetIdentity)
	configured := seedQueueEventIn(t, pool, aNonDefaultServiceSchema, "configured_listener")
	elsewhere := seedQueueEventIn(t, pool, harnessSchema, "default_listener")
	if configured.ID == elsewhere.ID {
		t.Fatalf("both schemas seeded event %d, so nothing below can tell them apart", configured.ID)
	}

	source, err := Open(
		t.Context(), pool, configuredForServiceSchema(t, aNonDefaultServiceSchema),
		Options{LeasedBy: "worker", Lease: time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	claimed, err := source.Claim(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != configured.ID {
		t.Fatalf(
			"Claim against service schema %s returned %#v, want only event %d",
			aNonDefaultServiceSchema, claimed, configured.ID,
		)
	}
	held := readQueueRowIn(t, pool, aNonDefaultServiceSchema, configured.ID)
	if held.status != "delivering" || held.leasedBy == nil || held.attempts != 1 {
		t.Fatalf(
			"the configured schema's row is status=%s leased_by=%v attempts=%d, want the lease "+
				"Claim reports having taken", held.status, held.leasedBy, held.attempts,
		)
	}
	assertUntouchedByAnyWorker(t, pool, elsewhere.ID)

	if err := source.Ack(t.Context(), claimed[0], Delivery{HTTPStatus: 200, Duration: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if got := queueRowCountIn(t, pool, aNonDefaultServiceSchema); got != 0 {
		t.Fatalf("the configured schema's queue holds %d rows after Ack, want 0", got)
	}
	assertUntouchedByAnyWorker(t, pool, elsewhere.ID)
}
