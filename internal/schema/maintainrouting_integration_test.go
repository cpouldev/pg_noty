//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criteria 26, 27 and 28: where the server puts a row, asserted from the row's own
// tableoid rather than from a count -- a count cannot say which partition received it.
//
// The two equality rows are what distinguish half-open bounds from any other reading, and neither
// implies the other: FROM is inclusive and TO is exclusive are two facts, and a row written in the
// middle of a range establishes neither. Both instants are taken from the alignment arithmetic's
// own bounds rather than written as literals, so a change of interval cannot leave them testing the
// wrong moments.
//
// Every case asserts the writing transaction *committed*. That is the coupling this whole package
// exists to protect: the failure being excluded is `no partition of relation "events" found for
// row`, which does not lose a row quietly -- it aborts the customer's transaction.

// theRoutedInsert writes one event and answers with the partition the server routed it into. The
// tableoid is rendered through the same cast identityOf reads a name through, so neither can
// disagree with the other about a search_path.
const theRoutedInsert = " (occurred_at) VALUES ($1) RETURNING tableoid::regclass::text"

// theEventsWrittenAt counts the rows carrying one instant, which is how the commit is asserted as
// durable rather than as a call that returned nil.
const theEventsWrittenAt = " WHERE occurred_at = $1"

// oneCommittedEvent writes one event inside its own transaction, commits it, and answers with the
// partition it landed in.
//
// It is a second writer beside Step 9's insertEvent deliberately: that one lets the parent route an
// event and says nothing about where it went or whether the writer survived, and both of those are
// exactly what these cases are about.
func oneCommittedEvent(t *testing.T, pool *pgxpool.Pool, at time.Time) string {
	t.Helper()

	events := mustQualify(t, harnessSchema, TableEvents)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction writing an event at %s: %v", at, err)
	}
	defer tx.Rollback(t.Context())

	var landed string
	if err := tx.QueryRow(t.Context(), "INSERT INTO "+events+theRoutedInsert, at).Scan(&landed); err != nil {
		t.Fatalf("write an event at %s: %v -- a write no partition accepts is refused with `no "+
			"partition of relation ... found for row`, and that aborts the writer's transaction", at, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit the write of an event at %s: %v", at, err)
	}

	var written int
	if err := pool.QueryRow(t.Context(), countRowsPrefix+events+theEventsWrittenAt, at).Scan(&written); err != nil {
		t.Fatalf("count the rows written at %s: %v", at, err)
	}
	if written != 1 {
		t.Fatalf("%d rows carry occurred_at %s after a committed write, want 1", written, at)
	}
	return landed
}

// TestEveryRoutingEqualityLandsTheRowWhereItsBoundsClaim is criteria 26, 27 and 28 as five named
// rows: the two boundaries, the interior between them, and the two directions out of range.
func TestEveryRoutingEqualityLandsTheRowWhereItsBoundsClaim(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	stillTheSameHorizon(t, pool, cfg, wanted)

	interval := cfg.Retention.PartitionInterval
	first, second, last := wanted[0], wanted[1], wanted[len(wanted)-1]
	if !first.To.Equal(second.From) {
		t.Fatalf("the first two ranges are [%s, %s) and [%s, %s), which do not meet; the upper-bound "+
			"row below is not then the next partition's lower bound",
			first.From, first.To, second.From, second.To)
	}

	for _, tc := range []struct {
		name, wantIn string
		at           time.Time
	}{
		{name: "criterion 26: a row at exactly the lower bound T",
			at: first.From, wantIn: first.Name},
		{name: "criterion 27: a row at exactly T plus one partition_interval",
			at: first.From.Add(interval), wantIn: second.Name},
		{name: "the interior, so the two equalities read as the boundaries they are",
			at: first.From.Add(interval / 2), wantIn: first.Name},
		{name: "criterion 28: a row far beyond every declared range",
			at: last.To.AddDate(1, 0, 0), wantIn: PartitionDefault},
		{name: "criterion 28: a row far behind every declared range",
			at: first.From.AddDate(-1, 0, 0), wantIn: PartitionDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, want := identityOf(t, pool, harnessSchema, tc.wantIn)

			if landed := oneCommittedEvent(t, pool, tc.at); landed != want {
				t.Errorf("a row written at %s landed in %s, want %s", tc.at, landed, want)
			}
		})
	}
}
