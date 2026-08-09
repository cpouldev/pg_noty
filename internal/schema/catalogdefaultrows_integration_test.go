//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is ADR-8's standing-condition observation: how many rows the DEFAULT partition holds.
// Step 11 stores it into MaintenanceStats.DefaultPartitionRows on every pass, and internal/cli
// alerts on it, so the reading has to be exact, cheap, and distinguishable from "could not
// observe".

// insertEvent writes one event at a chosen instant and lets the parent route it.
func insertEvent(t *testing.T, pool *pgxpool.Pool, at time.Time) {
	t.Helper()

	statement := "INSERT INTO " + mustQualify(t, harnessSchema, TableEvents) +
		" (occurred_at) VALUES ($1)"
	if _, err := pool.Exec(t.Context(), statement, at); err != nil {
		t.Fatalf("insert an event at %s: %v", at, err)
	}
}

// TestAHealthyEmptyDefaultPartitionReadsZeroRatherThanFailing is the reading a healthy system takes
// on every pass. Zero is the normal answer and has to be distinguishable from an observation that
// could not be made, because an alert keyed on the gauge would otherwise be armed by every failure
// to read it.
func TestAHealthyEmptyDefaultPartitionReadsZeroRatherThanFailing(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	plantPartitions(t, pool, harnessSchema, RequiredRanges(theObservedInstant, theObservedRetention))

	held, err := defaultPartitionRows(t.Context(), pool, harnessSchema, PartitionDefault)
	if err != nil {
		t.Fatalf("count the rows in an empty DEFAULT partition: %v", err)
	}
	if held != 0 {
		t.Errorf("an empty DEFAULT partition holds %d rows, want 0", held)
	}
}

// TestTheDefaultPartitionsRowsAreCountedAgainstItAndNeverThroughTheParent plants more rows in a
// bounded partition than in the DEFAULT one, so a count issued against the parent -- which is the
// full scan of the customer's largest table this observation exists to avoid -- answers the sum and
// fails the row rather than passing on a number that happens to be non-zero.
func TestTheDefaultPartitionsRowsAreCountedAgainstItAndNeverThroughTheParent(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	covered := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, covered)

	// Two rows land in DEFAULT because no partition covers a year later; five land in the first
	// covered range, which is inside the partition planted for it.
	const unrouted, routed = 2, 5
	for range unrouted {
		insertEvent(t, pool, theObservedInstant.AddDate(1, 0, 0))
	}
	for range routed {
		insertEvent(t, pool, covered[0].From)
	}

	held, err := defaultPartitionRows(t.Context(), pool, harnessSchema, PartitionDefault)
	if err != nil {
		t.Fatalf("count the rows in the DEFAULT partition: %v", err)
	}
	if held != unrouted {
		t.Errorf("the DEFAULT partition reads %d rows, want %d; %d is the whole parent",
			held, unrouted, unrouted+routed)
	}
}

// TestTheDefaultPartitionsCountIsAStandingConditionAndNotARunningTotal is the gauge half of ADR-8,
// asserted through the observation that feeds it. Two passes over an unchanged database answer the
// same number, and storing each answer leaves the gauge reading that number rather than their sum:
// an Add-shaped implementation would read 4 here, and an alert on it would fire and never clear.
func TestTheDefaultPartitionsCountIsAStandingConditionAndNotARunningTotal(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	plantPartitions(t, pool, harnessSchema, RequiredRanges(theObservedInstant, theObservedRetention))
	for range 2 {
		insertEvent(t, pool, theObservedInstant.AddDate(1, 0, 0))
	}

	var stats MaintenanceStats
	readings := make([]int64, 0, 2)
	for pass := range 2 {
		held, err := defaultPartitionRows(t.Context(), pool, harnessSchema, PartitionDefault)
		if err != nil {
			t.Fatalf("count the rows in the DEFAULT partition on pass %d: %v", pass, err)
		}
		stats.StoreDefaultPartitionRows(held)
		readings = append(readings, held)
	}

	if readings[0] != 2 || readings[1] != readings[0] {
		t.Errorf("two passes over an unchanged database read %v, want two readings of 2", readings)
	}
	if got := stats.DefaultPartitionRows(); got != readings[1] {
		t.Errorf("the gauge reads %d after two passes that each observed %d; a gauge carries the "+
			"standing condition and not their sum", got, readings[1])
	}
}

// TestAParentCarryingNoDefaultPartitionIsRefusedRatherThanReadAsHoldingNoRows reaches the refusal
// with the state a caller reaches it with: the observation found no DEFAULT partition and passed on
// the empty name it answers with. Reading that as zero would report a healthy safety net for a
// parent that has none.
func TestAParentCarryingNoDefaultPartitionIsRefusedRatherThanReadAsHoldingNoRows(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, PartitionDefault))

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe a parent with no DEFAULT partition: %v", err)
	}
	if found.Default != "" {
		t.Fatalf("the observation tagged %q as the DEFAULT partition of a parent with none",
			found.Default)
	}

	held, err := defaultPartitionRows(t.Context(), pool, harnessSchema, found.Default)
	if err == nil {
		t.Fatalf("counting the rows of a DEFAULT partition that does not exist answered %d", held)
	}
	if held != 0 {
		t.Errorf("the refusal came back alongside %d rows, want 0", held)
	}
}
