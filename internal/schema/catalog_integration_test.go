//go:build integration

package schema

import (
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file plants a hand-built event log -- deliberately not this package's migration corpus, so
// the observation is asserted in isolation from the runner Step 10 owns -- and asks the catalog
// reader what it sees.
//
// theObservedRetention and theObservedInstant are what every expected bound is derived from. The
// interval is 90 minutes so that no boundary lands on midnight: a reader that dropped the time of
// day would still agree with a daily grid.
var (
	theObservedRetention = config.Retention{
		Keep:              72 * time.Hour,
		PartitionInterval: 90 * time.Minute,
		Precreate:         6 * time.Hour,
	}
	theObservedInstant = time.Date(2026, time.March, 14, 15, 9, 26, 535000000, time.UTC)
)

// eventLogFixture is a freshly restored database holding the service schema and a partitioned event
// log with its permanent DEFAULT partition, and nothing else.
func eventLogFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := freshDatabase(t)
	plantEventLog(t, pool, harnessSchema)
	return pool
}

// plantEventLog creates one schema holding an event log shaped like migration 2's: range
// partitioned on occurred_at, with the composite primary key the server requires of it and the
// permanent DEFAULT partition. It is hand-built rather than migrated so that this step's
// observation is asserted independently of the runner Step 10 owns.
func plantEventLog(t *testing.T, pool *pgxpool.Pool, schema string) {
	t.Helper()

	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, schema))
	mustExecOn(
		t, pool, "CREATE TABLE "+mustQualify(t, schema, TableEvents)+
			" (id bigint GENERATED ALWAYS AS IDENTITY, occurred_at timestamptz NOT NULL,"+
			" PRIMARY KEY (id, occurred_at)) PARTITION BY RANGE (occurred_at)",
	)
	mustExecOn(
		t, pool, "CREATE TABLE "+mustQualify(t, schema, PartitionDefault)+
			" PARTITION OF "+mustQualify(t, schema, TableEvents)+" DEFAULT",
	)
}

// mustQuote and mustQualify render an identifier through the package's own quoting authority, so a
// fixture cannot address an object differently from the code under test.
func mustQuote(t *testing.T, name string) string {
	t.Helper()
	rendered, fault := Quoted(name)
	if fault != IdentifierOK {
		t.Fatalf("quote %s: it %s", name, fault)
	}
	return rendered
}

func mustQualify(t *testing.T, schema, name string) string {
	t.Helper()
	rendered, fault := Qualified(schema, name)
	if fault != IdentifierOK {
		t.Fatalf("qualify %s.%s: it %s", schema, name, fault)
	}
	return rendered
}

// otherSchema is where every case that has something to keep out of the observation plants it.
const otherSchema = "elsewhere"

// forValues spells one range's bounds as the absolute instants the arithmetic produced, which is
// also what the expectations are taken from -- so a comparison afterwards is against those values
// and never against this text read back.
func forValues(wanted Range) string {
	return " FOR VALUES FROM ('" + wanted.From.Format(time.RFC3339Nano) +
		"') TO ('" + wanted.To.Format(time.RFC3339Nano) + "')"
}

// plantPartitions creates one partition per range.
func plantPartitions(t *testing.T, pool *pgxpool.Pool, schema string, ranges []Range) {
	t.Helper()

	for _, wanted := range ranges {
		mustExecOn(
			t, pool, "CREATE TABLE "+mustQualify(t, schema, wanted.Name)+
				" PARTITION OF "+mustQualify(t, schema, TableEvents)+forValues(wanted),
		)
	}
}

// TestABoundIsReadBackAsTheRangeTheArithmeticProduced is M12. The expectation is RequiredRanges'
// own output -- the same values the statements were written from -- and never the text of those
// statements read back, which would assert that the string survived rather than that the bound is
// right.
//
// Each range is compared against its own counterpart rather than for membership of the set, so two
// partitions sharing one extent cannot satisfy the row named for a third.
func TestABoundIsReadBackAsTheRangeTheArithmeticProduced(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	required := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, required)

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions just planted: %v", err)
	}

	// The catalog answers in name order; the arithmetic answers in time order. Both are sorted by
	// lower bound here so the comparison is about the extents and not about either ordering.
	observed := slices.Clone(found.Bounded)
	slices.SortFunc(observed, func(a, b Range) int { return a.From.Compare(b.From) })

	if len(observed) != len(required) {
		t.Fatalf("observed %d bounded partitions and planted %d", len(observed), len(required))
	}
	for i, wanted := range required {
		if !observed[i].From.Equal(wanted.From) || !observed[i].To.Equal(wanted.To) {
			t.Errorf(
				"partition %d was read back as [%s, %s), want the arithmetic's [%s, %s)",
				i, observed[i].From, observed[i].To, wanted.From, wanted.To,
			)
		}
		if observed[i].Name != wanted.Name {
			t.Errorf("partition %d is named %s, want %s", i, observed[i].Name, wanted.Name)
		}
	}
}

// TestTheDefaultPartitionIsTaggedRatherThanGivenABound is criterion 36's precondition. A DEFAULT
// partition carried as a Range with a zero upper bound would be exempted from retention by
// whyUndroppable and hold expired data forever; carried with any other upper bound it would be read
// as older than every cutoff and dropped. It is neither, because it is not a Range at all.
func TestTheDefaultPartitionIsTaggedRatherThanGivenABound(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	plantPartitions(t, pool, harnessSchema, RequiredRanges(theObservedInstant, theObservedRetention))

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions: %v", err)
	}

	if found.Default != PartitionDefault {
		t.Fatalf(
			"the observation tagged %q as the DEFAULT partition, want %q",
			found.Default, PartitionDefault,
		)
	}
	for _, observed := range found.Bounded {
		if observed.Name == PartitionDefault {
			t.Errorf(
				"%s was returned as a bounded range [%s, %s)",
				observed.Name, observed.From, observed.To,
			)
		}
		if observed.To.IsZero() {
			t.Errorf(
				"%s was returned with a zero upper bound, which whyUndroppable exempts from "+
					"retention for having no age", observed.Name,
			)
		}
	}
}

// TestTheDatabasesClockIsTheOneEveryDecisionIsTakenAgainst separates the two clocks by the one
// property that tells them apart: now() is the transaction timestamp, so two reads inside one
// transaction answer the same instant however long this process waits between them, and
// time.Now() cannot.
func TestTheDatabasesClockIsTheOneEveryDecisionIsTakenAgainst(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin a transaction to read the clock in: %v", err)
	}
	defer tx.Rollback(t.Context())

	const waited = 50 * time.Millisecond
	first, err := dbNow(t.Context(), tx)
	if err != nil {
		t.Fatalf("read now() the first time: %v", err)
	}
	time.Sleep(waited)
	second, err := dbNow(t.Context(), tx)
	if err != nil {
		t.Fatalf("read now() the second time: %v", err)
	}

	if !first.Equal(second) {
		t.Errorf(
			"two reads in one transaction answered %s and %s, %s apart; now() is the "+
				"transaction timestamp, so a differing pair means the clock is this process's",
			first, second, second.Sub(first),
		)
	}
	if first.IsZero() {
		t.Error("the clock read the zero instant, so nothing was read at all")
	}
}
