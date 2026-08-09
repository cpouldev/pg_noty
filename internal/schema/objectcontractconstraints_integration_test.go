//go:build integration

package schema

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 4 and SC 9: the one CHECK this contract declares, in both directions, and the one it
// deliberately does not.

// enqueueStatement writes one queue row for one planted event. Every column the queue declares NOT
// NULL is written and the event it points at exists, so the only thing a refusal below can be about
// is the status.
const enqueueStatement = "INSERT INTO %s (event_id, occurred_at, listener, status, attempts, " +
	"next_attempt_at) VALUES ($1, $2, 'a-listener', $3, 0, $2)"

func enqueue(t *testing.T, pool *pgxpool.Pool, event plantedEvent, status string) error {
	t.Helper()

	_, err := pool.Exec(t.Context(),
		fmt.Sprintf(enqueueStatement, mustQualify(t, harnessSchema, TableEventQueue)),
		event.id, event.occurredAt, status)
	return err
}

// The refusals below are asserted against checkViolation -- ddlrefusal.go's own name for SQLSTATE
// 23514, read from there rather than spelled again here -- rather than against "an error", so a NOT
// NULL violation, a key collision or a column type change cannot read as a correct refusal. What
// makes that class name the *status* CHECK specifically is that the schema declares no other, which
// TestTheOnlyCheckConstraintInTheSchemaIsTheStatusOne asserts below.

// TestTheStatusCheckAdmitsTheThreeLiveStatesAndRefusesTheRest is criterion 4, in both directions.
// The accept direction alone cannot distinguish a correct three-value CHECK from one that still
// admits delivered and failed, which were deliberately retired from the original five-state design;
// the unlisted string is a row of its own because a five-state CHECK would reject it while admitting
// both retired states.
func TestTheStatusCheckAdmitsTheThreeLiveStatesAndRefusesTheRest(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	for _, tc := range theStatusVocabulary {
		t.Run(tc.value, func(t *testing.T) {
			// A fresh event per row: the queue is keyed on event_id, so a second accepted row
			// reusing one would be refused for its key rather than for its status.
			planted := plantEvent(t, pool, theObservedInstant)
			assertStatusVerdict(t, tc, enqueue(t, pool, planted, tc.value))
		})
	}
}

func assertStatusVerdict(t *testing.T, want statusCase, got error) {
	t.Helper()

	if want.accepted {
		if got != nil {
			t.Errorf("%q was refused (%v), and it is one of the three live states: %s",
				want.value, got, want.why)
		}
		return
	}

	var raised *pgconn.PgError
	if !errors.As(got, &raised) || raised.Code != checkViolation {
		t.Errorf("%q was answered %v, want SQLSTATE %s from the status CHECK: %s", want.value, got,
			checkViolation, want.why)
	}
}

// checkConstraintNamesQuery is every CHECK one table declares. NOT NULL is not one of them on this
// server -- it lives in pg_attribute.attnotnull and never in pg_constraint -- so this counts the
// declared CHECKs and nothing else.
const checkConstraintNamesQuery = `
SELECT conname::text FROM pg_constraint
 WHERE conrelid = to_regclass($1) AND contype = 'c'
 ORDER BY conname`

func checkConstraintsOn(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), checkConstraintNamesQuery,
		mustQualify(t, harnessSchema, table))
	if err != nil {
		t.Fatalf("read the CHECK constraints of %s.%s: %v", harnessSchema, table, err)
	}
	defer rows.Close()

	declared, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect the CHECK constraints of %s.%s: %v", harnessSchema, table, err)
	}
	return declared
}

// TestTheOnlyCheckConstraintInTheSchemaIsTheStatusOne is SC 9's absence half, closed over the six
// tables rather than asked of one column: an absence checked on response_snippet alone would not
// notice a CHECK arriving on error or on http_status, and the reason the cap is a comment is a
// reason about the whole of deliveries.
//
// A rejected deliveries insert would abort internal/delivery's record transaction, leaving the queue
// row delivering until lease reclaim and turning a truncation bug into an infinite redelivery loop.
// Failing closed is right for enqueue and wrong for forensics, so the absence is the property.
func TestTheOnlyCheckConstraintInTheSchemaIsTheStatusOne(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	for _, table := range theContractTables {
		got := checkConstraintsOn(t, pool, table)
		if want := theContractChecks[table]; len(got) != want {
			t.Errorf("%s declares %d CHECK constraints %v, and the contract puts %d there",
				table, len(got), got, want)
		}
	}
}

// theCappedColumn is the column SC 9 is about, with the cap its comment has to record.
const (
	theCappedColumn = "response_snippet"
	theRecordedCap  = "2 KiB"
)

// columnCommentQuery reads one column's comment from where COMMENT ON COLUMN put it.
const columnCommentQuery = `
SELECT coalesce(col_description(a.attrelid, a.attnum), '')
  FROM pg_attribute a
 WHERE a.attrelid = to_regclass($1) AND a.attname = $2`

// TestTheCatalogHoldsResponseSnippetUnboundedWithItsCapAsAComment is SC 9's presence half, and it
// is the catalog's answer rather than the DDL's: TestResponseSnippetIsUnboundedTextWithItsCapAsAComment
// in migrationqueue_test.go asserts the text was written, and this one asserts the server accepted it
// as intended. The expectation is that DDL's own comment rather than a phrase written out again here,
// so what is asserted is that the comment reached the catalog -- and the guard above the comparison
// keeps it from being satisfied by a DDL comment that stopped recording the cap.
func TestTheCatalogHoldsResponseSnippetUnboundedWithItsCapAsAComment(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	if got := observedColumnsOf(t, pool, TableDeliveries)[theCappedColumn]; got.maxLength != nil {
		t.Errorf("%s.%s is capped at %d characters by the server, and the cap belongs to "+
			"internal/delivery at write time", TableDeliveries, theCappedColumn, *got.maxLength)
	}

	declared, _ := declaredCommentsIn(migrationSQL(t, migrationCreating(t, TableDeliveries)))
	written := declared["COLUMN "+TableDeliveries+"."+theCappedColumn]
	if !strings.Contains(written, theRecordedCap) {
		t.Fatalf("the DDL's comment on %s.%s records no %s cap, so comparing the catalog against it "+
			"would assert nothing: %q", TableDeliveries, theCappedColumn, theRecordedCap, written)
	}

	var recorded string
	err := pool.QueryRow(t.Context(), columnCommentQuery,
		mustQualify(t, harnessSchema, TableDeliveries), theCappedColumn).Scan(&recorded)
	if err != nil {
		t.Fatalf("read the comment on %s.%s: %v", TableDeliveries, theCappedColumn, err)
	}
	assertCommentMatchesTheDDL(t, TableDeliveries+"."+theCappedColumn, recorded, written)
}
