package schema

import (
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is the container-free half of the drain: the names it addresses, the statements it
// writes, and the two reasons it refuses. None of them touches a server, so they are asserted here
// rather than through one -- and one of them cannot be reached through a fixture at all, which is
// the reason this file exists rather than only its tagged twin.

// theDrainedSchema is the configuration a drain is rendered against. The instance is the same string
// as the schema, which is what harnessConfig does, so a marker written here reads back as ours.
func theDrainedSchema() config.Config {
	return config.Config{
		Instance: harnessSchema,
		Database: config.Database{Schema: harnessSchema},
	}
}

// aRenderedDrain is one drain over theRenderedRange -- Step 11's own probe range, reused rather than
// re-invented.
func aRenderedDrain(t *testing.T) drain {
	t.Helper()

	work, err := drainFor(theDrainedSchema(), theRenderedRange)
	if err != nil {
		t.Fatalf("render a drain of %s: %v", extentOf(theRenderedRange), err)
	}
	return work
}

// TestEachReasonARangeCannotBeDrainedHasItsOwnAnswer reaches every arm of the refusal directly,
// including the one no fixture in the tagged half produces: a DEFAULT partition under a name this
// package does not own. Deleting from the wrong table is the data-loss defect the whole step
// exists to avoid, so that branch is behaviour and not decoration.
//
// The fourth row violates two rules at once and is the one that pins the documented order; the
// last pins that membership is decided by extent and never by name, which is the rule the pass
// itself uses (M6).
func TestEachReasonARangeCannotBeDrainedHasItsOwnAnswer(t *testing.T) {
	work := aRenderedDrain(t)
	sameExtent := Range{From: theRenderedRange.From, To: theRenderedRange.To, Name: "someone_elses"}

	for _, tc := range []struct {
		name, wants string
		found       observedPartitions
	}{
		{
			name: "a log with no DEFAULT partition at all", wants: "no DEFAULT partition",
			found: observedPartitions{},
		},
		{
			name: "a DEFAULT partition under a name this package does not own", wants: "events_spare",
			found: observedPartitions{Default: "events_spare"},
		},
		{
			name: "a range a partition already covers", wants: "already covers",
			found: observedPartitions{Default: PartitionDefault, Bounded: []Range{theRenderedRange}},
		},
		{
			name: "a covered range in a log with no DEFAULT partition", wants: "no DEFAULT partition",
			found: observedPartitions{Bounded: []Range{theRenderedRange}},
		},
		{
			name: "the extent covered under a name this process would never choose", wants: "already covers",
			found: observedPartitions{Default: PartitionDefault, Bounded: []Range{sameExtent}},
		},

		{
			name: "the state the drain is for", wants: "",
			found: observedPartitions{Default: PartitionDefault},
		},
		{
			name: "the state the drain is for, beside an unrelated partition", wants: "",
			found: observedPartitions{
				Default: PartitionDefault,
				Bounded: []Range{rangeAt(0, 24*time.Hour)},
			},
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				why := work.whyUnrepairable(tc.found)

				if tc.wants == "" && why != "" {
					t.Fatalf("the drain refuses a repairable state with %q", why)
				}
				if tc.wants != "" && !strings.Contains(why, tc.wants) {
					t.Errorf("the drain answers %q, which does not say %q", why, tc.wants)
				}
			},
		)
	}
}

// TestADrainRefusesAnExtentThatSelectsNoRow is the other refusal, and the row one nanosecond wide is
// what separates `After` from `!Before`: an extent whose bounds are equal selects nothing and
// describes no partition, while the smallest extent the arithmetic can produce is a real range.
func TestADrainRefusesAnExtentThatSelectsNoRow(t *testing.T) {
	from := theRenderedRange.From

	for _, tc := range []struct {
		name     string
		to       time.Time
		refusing bool
	}{
		{name: "an upper bound equal to the lower one", to: from, refusing: true},
		{name: "an upper bound before the lower one", to: from.Add(-time.Nanosecond), refusing: true},
		{name: "the narrowest extent there is", to: from.Add(time.Nanosecond)},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				_, err := drainFor(theDrainedSchema(), Range{From: from, To: tc.to, Name: "events_probe"})

				switch {
				case tc.refusing && err == nil:
					t.Errorf("a drain of [%s, %s) was rendered, and it selects no row", from, tc.to)
				case tc.refusing && !strings.Contains(err.Error(), "selects no row"):
					t.Errorf(
						"a drain of [%s, %s) was refused with %v, which says something else",
						from, tc.to, err,
					)
				case !tc.refusing && err != nil:
					t.Errorf("a drain of [%s, %s) was refused with %v", from, tc.to, err)
				}
			},
		)
	}
}

// TestTheTwoRelocationsAddressDistinctTablesThePostgresLimitCannotCollide is M9 at the two names
// this step generates. A holding table is `<relation>_repair_<range>`, and the range name alone is
// already 58 bytes, so both are past the 63-byte limit for every real range -- and PostgreSQL
// truncates rather than erroring, so an unguarded concatenation would collide the event log's
// holding table with the queue's and the drain would put the queue's rows into the event log.
func TestTheTwoRelocationsAddressDistinctTablesThePostgresLimitCannotCollide(t *testing.T) {
	for _, ranged := range []Range{theRenderedRange, rangeAt(0, 24*time.Hour), rangeAt(-1, time.Hour)} {
		events, queue := holdingName(TableEvents, ranged), holdingName(TableEventQueue, ranged)

		if events == queue {
			t.Errorf("both relations of a drain of %s hold their rows in %s", ranged.Name, events)
		}
		for _, name := range []string{events, queue} {
			if len(name) > MaxIdentifierBytes {
				t.Errorf(
					"the holding table %s is %d bytes, and PostgreSQL keeps %d of an identifier "+
						"and drops the rest with only a NOTICE", name, len(name), MaxIdentifierBytes,
				)
			}
		}
	}
}

// TestTheLiftAndTheRemoveSelectExactlyTheSameRows is the conservation property at the statement
// level, before any count reconciles it: the copy and the delete carry one predicate, so a row that
// left the source is a row the holding table holds. Two predicates written out separately would be
// two the day one of them gained a clause.
func TestTheLiftAndTheRemoveSelectExactlyTheSameRows(t *testing.T) {
	work := aRenderedDrain(t)

	for _, moved := range []relocation{work.events, work.queue} {
		if !strings.HasSuffix(moved.liftStatement(), theDrainedRows) {
			t.Errorf(
				"%s copies its rows out with %s, which does not end in the range predicate",
				moved.named, moved.liftStatement(),
			)
		}
		if !strings.HasSuffix(moved.removeStatement(), theDrainedRows) {
			t.Errorf(
				"%s removes its rows with %s, which does not end in the range predicate",
				moved.named, moved.removeStatement(),
			)
		}
		if !strings.Contains(moved.discardStatement(), moved.holding) {
			t.Errorf(
				"%s discards %s and its rows waited in %s",
				moved.named, moved.discardStatement(), moved.holding,
			)
		}
	}
	// Half-open, and the direction matters: `>` on the lower bound would leave the row at exactly
	// the bound behind in the DEFAULT partition, and `<=` on the upper one would take the next
	// range's first row with it (M11).
	if !strings.Contains(theDrainedRows, ">= $1") || !strings.Contains(theDrainedRows, "< $2") {
		t.Errorf("the drain selects its rows with %s, and a range is [FROM, TO)", theDrainedRows)
	}
}

// TestOnlyTheEventLogsReInsertOverridesTheIdentitySequence is the clause's other side. The queue
// carries no identity column, so the clause there would be refused by the server -- and asserting
// only that the event log has it would not notice a drain that put it on both.
func TestOnlyTheEventLogsReInsertOverridesTheIdentitySequence(t *testing.T) {
	work := aRenderedDrain(t)

	if !strings.Contains(work.events.restoreStatement(), overridingSystemValue) {
		t.Errorf(
			"the event log's rows are re-inserted with %s, which carries no %s",
			work.events.restoreStatement(), overridingSystemValue,
		)
	}
	if strings.Contains(work.queue.restoreStatement(), strings.TrimSpace(overridingSystemValue)) {
		t.Errorf(
			"the queue's rows are re-inserted with %s, and %s has no identity column",
			work.queue.restoreStatement(), TableEventQueue,
		)
	}
	// The event log's rows come back through the parent, which is what lets the server route them
	// into the partition created between the two steps; the queue returns to itself.
	if work.events.source == work.events.target {
		t.Errorf(
			"the event log's rows are re-inserted into %s, the very partition they left; the "+
				"server would put them straight back", work.events.target,
		)
	}
	if work.queue.source != work.queue.target {
		t.Errorf("the queue's rows leave %s and return to %s", work.queue.source, work.queue.target)
	}
}
