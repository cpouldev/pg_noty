//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The crossing half of the enqueue claim, and the reason it insists on crossing: a queue
// occurred_at differing from its event's by microseconds *inside one partition* still satisfies
// internal/schema's composite key, so no write confined to a single partition can fail when the two
// disagree. The two
// cases below are therefore placed either side of a boundary the partition arithmetic itself splits
// on, and each event's landing partition is read back, so "it crossed" is a measurement rather than
// an assumption about where the clock happened to be.
//
// Both instants come from schema.RequiredRanges -- internal/schema's own grid derivation -- rather
// than from literals, so a changed interval cannot leave these cases testing the wrong moments. The
// generated trigger writes clock_timestamp() and is not edited here, so the exact instant is placed by
// a BEFORE INSERT trigger on the log and the generated CTE returns the row as inserted, which is how
// the queue row inherits it. That override may only move occurred_at *within* the partition the row
// was routed to: measured on PostgreSQL 17.10, moving it across one is refused with `moving row to
// another partition during a BEFORE FOR EACH ROW trigger is not supported` (SQLSTATE 0A000). So the
// crossing is made by the clock reaching the boundary between the two writes, and the override only
// sharpens each write onto the boundary value its case is named for.

const (
	// theGridInterval is the partition width the crossing is derived over. Any positive interval is
	// legal (R10); this one is short because the second write waits for the clock to reach the
	// boundary, and long enough that neither write is racing it.
	theGridInterval = 2 * time.Second
	// theBoundaryMargin is how far past the boundary the second write waits, covering the small
	// difference between this process's clock and the server's clock_timestamp().
	theBoundaryMargin = 250 * time.Millisecond
	// oneTick is a microsecond, which is timestamptz's own resolution and therefore the smallest
	// step the server can hold apart from a boundary. A nanosecond would be rounded back onto the
	// boundary at insert time and the below-the-boundary case would silently become the other one.
	oneTick = time.Microsecond
	// theOverridingFunction places an event's occurred_at; theShiftingFunction points a queue row's
	// at another partition.
	theOverridingFunction = "noty.force_occurred_at"
	theShiftingFunction   = "noty.shift_queue_occurred_at"
)

// theCrossedGrid realises the range beginning one whole interval or more from now, and answers it
// with the range immediately below it, which nothing covers and whose instants therefore fall to the
// DEFAULT partition. Two ranges that meet, one of them a partition of its own and the other not, are
// what make the boundary between them a crossing rather than a value inside one partition.
func theCrossedGrid(t *testing.T, pool *pgxpool.Pool) (below, above schema.Range) {
	t.Helper()
	ranges := schema.RequiredRanges(
		time.Now(), config.Retention{
			PartitionInterval: theGridInterval, Precreate: 2 * theGridInterval,
		},
	)
	if len(ranges) != 3 || !ranges[1].To.Equal(ranges[2].From) {
		t.Fatalf("the grid gave %v; the crossing needs a covered range meeting an uncovered one", ranges)
	}
	attachPartition(t, pool, ranges[2])
	return ranges[1], ranges[2]
}

// attachPartition realises one range as a partition of the migrated log. The bounds are written as
// literals because FOR VALUES takes a constant expression that no bind parameter can carry, and they
// are the arithmetic's own instants rather than text transcribed here.
func attachPartition(t *testing.T, pool *pgxpool.Pool, ranged schema.Range) {
	t.Helper()
	mustExecOn(
		t, pool, "CREATE TABLE "+mustQualifyServiceTable(t, harnessSchema, ranged.Name)+
			" PARTITION OF "+mustQualifyServiceTable(t, harnessSchema, schema.TableEvents)+
			" FOR VALUES FROM ("+timestampLiteral(ranged.From)+") TO ("+timestampLiteral(ranged.To)+")",
	)
}

// timestampLiteral writes one instant in UTC at full precision, through the package's own literal
// authority so no second quoting rule is introduced for a fixture.
func timestampLiteral(at time.Time) string {
	return quoteLiteral(at.UTC().Format(time.RFC3339Nano)) + "::timestamptz"
}

// settingOccurredAt is the body both fixture triggers share: assign one chosen instant to the column
// deciding which partition the row belongs to.
func settingOccurredAt(name string, at time.Time) string {
	return "CREATE OR REPLACE FUNCTION " + name + "() RETURNS trigger LANGUAGE plpgsql AS " +
		"$fixture$ BEGIN NEW.occurred_at := " + timestampLiteral(at) + "; RETURN NEW; END; $fixture$"
}

// installOccurredAtOverride puts every event written afterwards at one chosen instant.
func installOccurredAtOverride(t *testing.T, pool *pgxpool.Pool, at time.Time) {
	t.Helper()
	mustExecOn(t, pool, settingOccurredAt(theOverridingFunction, at))
	mustExecOn(
		t, pool, "CREATE TRIGGER force_occurred_at BEFORE INSERT ON noty.events "+
			"FOR EACH ROW EXECUTE FUNCTION "+theOverridingFunction+"()",
	)
}

// waitForTheGridBoundary blocks until the clock is inside the attached range, which is what routes
// the write after it into that partition instead of into DEFAULT. Measured by removing this call:
// the write is then refused with SQLSTATE 0A000 rather than landing quietly on the near side, so a
// case cannot lose its crossing and still pass.
func waitForTheGridBoundary(t *testing.T, boundary time.Time) {
	t.Helper()
	time.Sleep(time.Until(boundary) + theBoundaryMargin)
	if reached := time.Now(); reached.Before(boundary) {
		t.Fatalf(
			"the clock is at %s and the boundary is %s; the write after this one would still be "+
				"routed by the range below it", reached.UTC(), boundary.UTC(),
		)
	}
}

// TestEnqueueCrossesTheGridBoundaryWithEveryQueueRowResolving is the same claim across a partition
// boundary: two watched writes whose events land in different partitions -- one below the boundary,
// which nothing covers, and one at the boundary exactly, which the half-open [from, to) grid places
// in the range above -- each producing exactly one queue row carrying the event's own occurred_at,
// pending with zero attempts and no lease, and whose composite key resolves in the partition the
// event actually landed in.
func TestEnqueueCrossesTheGridBoundaryWithEveryQueueRowResolving(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	installEnqueueTarget(t, pool, "enqueue_crossing")
	below, above := theCrossedGrid(t, pool)

	cases := []struct {
		name, wantPartition string
		at                  time.Time
		pastTheBoundary     bool
	}{
		{
			name: "one tick below the boundary, which no partition covers", at: below.To.Add(-oneTick),
			wantPartition: schema.PartitionDefault,
		},
		{
			name: "the boundary exactly, which the half-open range below excludes", at: above.From,
			wantPartition: above.Name, pastTheBoundary: true,
		},
	}
	// The override's function has to exist before the trigger can name it; the loop then re-states it
	// per case, so every case is placed the same way rather than the first one inheriting the install.
	installOccurredAtOverride(t, pool, cases[0].at)
	for index, tc := range cases {
		if tc.pastTheBoundary {
			waitForTheGridBoundary(t, above.From)
		}
		mustExecOn(t, pool, settingOccurredAt(theOverridingFunction, tc.at))
		mustExecOn(t, pool, "INSERT INTO public.enqueue_crossing VALUES ($1)", index)
	}

	written := eventsWritten(t, pool)
	if len(written) != len(cases) {
		t.Fatalf("%d watched writes wrote %d event rows, want one each", len(cases), len(written))
	}
	if queued := countOn(t, pool, "SELECT count(*) FROM noty.event_queue"); queued != len(cases) {
		t.Fatalf("%d watched writes wrote %d queue rows, want one each", len(cases), queued)
	}
	for index, tc := range cases {
		t.Run(
			tc.name, func(t *testing.T) {
				event := written[index]
				if !event.occurredAt.Equal(tc.at) || event.partition != tc.wantPartition {
					t.Fatalf(
						"the event is at %s in partition %s, want %s in %s; a case landing elsewhere "+
							"asserts nothing about the boundary it is named for",
						event.occurredAt.UTC(), event.partition, tc.at.UTC(), tc.wantPartition,
					)
				}
				assertOneFreshPendingQueueRow(t, pool, event)
				assertTheCompositeKeyHoldsTheEvent(t, pool, event)
			},
		)
	}
	if written[0].partition == written[1].partition {
		t.Fatalf(
			"both rows either side of the boundary landed in %s, so nothing crossed and a "+
				"mismatched occurred_at would satisfy the key anyway", written[0].partition,
		)
	}
}

// TestAQueueOccurredAtInAnotherPartitionAbortsTheCustomersWrite is what the carried value prevents.
// The queue row's occurred_at is pointed at the partition above the event's, where no event of that
// id exists; the enqueue runs inside the customer's transaction with no EXCEPTION block, so the
// customer's own INSERT is refused by the server rather than logged.
func TestAQueueOccurredAtInAnotherPartitionAbortsTheCustomersWrite(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	installEnqueueTarget(t, pool, "enqueue_mismatch")
	_, above := theCrossedGrid(t, pool)
	installOccurredAtOverride(t, pool, above.From.Add(-oneTick))
	mustExecOn(t, pool, settingOccurredAt(theShiftingFunction, above.From))
	mustExecOn(
		t, pool, "CREATE TRIGGER shift_queue_occurred_at BEFORE INSERT ON noty.event_queue "+
			"FOR EACH ROW EXECUTE FUNCTION "+theShiftingFunction+"()",
	)

	_, err := pool.Exec(t.Context(), "INSERT INTO public.enqueue_mismatch VALUES (1)")
	if got := sqlStateOf(err); got != foreignKeyViolation {
		t.Fatalf(
			"a write whose queue row names partition %s while its event is in %s answered "+
				"SQLSTATE %q (%v), want the server's %s aborting the customer's transaction",
			above.Name, schema.PartitionDefault, got, err, foreignKeyViolation,
		)
	}
	events, queued := countOn(t, pool, "SELECT count(*) FROM noty.events"),
		countOn(t, pool, "SELECT count(*) FROM noty.event_queue")
	if events != 0 || queued != 0 {
		t.Fatalf("the aborted write left events=%d queue=%d, want nothing at all", events, queued)
	}
}
