//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 7: the four indexes the contract declares, each with the columns, the predicate and the
// comment that make it the index it is. Criterion 10's closed per-relation inventory is next door,
// in objectcontractindexinventory_integration_test.go.
//
// What this file does not assert is the behaviour of any of them under a planner, which is Step
// 16's (criteria 8 and 9): here they exist with the right shape, there the planner is shown to use
// them.

// indexColumnsQuery names one index's columns in index order. pg_get_indexdef answers per column
// when given a position, so the columns are read as the server holds them rather than parsed back
// out of a reconstructed CREATE INDEX line.
const indexColumnsQuery = `
SELECT pg_get_indexdef(i.indexrelid, k, true)
  FROM pg_index i, generate_series(1, i.indnatts) AS k
 WHERE i.indexrelid = to_regclass($1)
 ORDER BY k`

// indexPredicateQuery is one index's WHERE clause, or the empty string when it has none.
const indexPredicateQuery = `
SELECT coalesce(pg_get_expr(i.indpred, i.indrelid), '')
  FROM pg_index i WHERE i.indexrelid = to_regclass($1)`

const indexCommentQuery = `SELECT coalesce(obj_description(to_regclass($1), 'pg_class'), '')`

func indexColumnsOf(t *testing.T, pool *pgxpool.Pool, index string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), indexColumnsQuery, mustQualify(t, harnessSchema, index))
	if err != nil {
		t.Fatalf("read the columns of index %s: %v", index, err)
	}
	defer rows.Close()

	columns, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect the columns of index %s: %v", index, err)
	}
	return columns
}

// oneStringOf runs one of the single-value catalog queries above against one object.
func oneStringOf(t *testing.T, pool *pgxpool.Pool, query, object string) string {
	t.Helper()

	var answered string
	if err := pool.QueryRow(t.Context(), query, mustQualify(t, harnessSchema, object)).
		Scan(&answered); err != nil {
		t.Fatalf("query %s about %s: %v", strings.TrimSpace(query), object, err)
	}
	return answered
}

// TestEachDeclaredIndexIsOnItsColumnsWithItsPredicateAndComment is criterion 7, ranging over
// theDeclaredIndexes -- whose size migrationindex_test.go pins at four, so a fifth index joining the
// DDL without joining that set fails there rather than passing unasserted here.
func TestEachDeclaredIndexIsOnItsColumnsWithItsPredicateAndComment(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	declared, _ := declaredCommentsIn(migrationSQL(t, theObjectsMigration))

	for _, want := range theDeclaredIndexes {
		t.Run(want.name, func(t *testing.T) {
			if got := indexColumnsOf(t, pool, want.name); !slices.Equal(got,
				strings.Split(want.columns, ", ")) {
				t.Errorf("%s is over %v in the catalog, and the contract declares (%s)",
					want.name, got, want.columns)
			}
			assertIndexPredicate(t, pool, want)
			assertCommentMatchesTheDDL(t, want.name,
				oneStringOf(t, pool, indexCommentQuery, want.name), declared["INDEX "+want.name])
		})
	}
}

// assertIndexPredicate compares one index's WHERE clause against the server's own rendering of the
// contract's predicate text, obtained by handing that text back as an index of its own. The
// expectation is therefore derived rather than transcribed from a run: a change in how the server
// renders a predicate moves both sides together, while a predicate dropped or widened moves one. A
// partial index that lost its predicate still exists and still gets scanned, but stops being the
// index the plan was reasoned about.
func assertIndexPredicate(t *testing.T, pool *pgxpool.Pool, want sqlIndex) {
	t.Helper()

	got := oneStringOf(t, pool, indexPredicateQuery, want.name)
	if want.predicate == "" {
		if got != "" {
			t.Errorf("%s carries the predicate %s, and the contract declares it total -- it answers "+
				"questions about every status rather than about one of them", want.name, got)
		}
		return
	}
	if rendered := renderedPredicate(t, pool, want); got != rendered {
		t.Errorf("%s carries the predicate %q, and the contract declares WHERE %s, which this "+
			"server renders as %q", want.name, got, want.predicate, rendered)
	}
}

// theProbeIndex is where the contract's own predicate text is handed back to the server so that its
// rendering can be read. It is created on the index's own table, so the column references in the
// predicate resolve exactly as they do in the shipped index, and dropped again immediately so that
// no inventory sees it.
const theProbeIndex = "contract_predicate_probe_idx"

func renderedPredicate(t *testing.T, pool *pgxpool.Pool, want sqlIndex) string {
	t.Helper()

	leading, _, _ := strings.Cut(want.columns, ", ")
	mustExecOn(t, pool, "CREATE INDEX "+theProbeIndex+" ON "+
		mustQualify(t, harnessSchema, want.table)+" ("+leading+") WHERE "+want.predicate)
	rendered := oneStringOf(t, pool, indexPredicateQuery, theProbeIndex)
	mustExecOn(t, pool, "DROP INDEX "+mustQualify(t, harnessSchema, theProbeIndex))

	return rendered
}

// assertCommentMatchesTheDDL compares what the catalog holds against the shipped DDL's own comment.
// Both callers read their comment through a different catalog function -- a column's lives with its
// attribute -- so the reading is theirs and only the comparison is shared.
func assertCommentMatchesTheDDL(t *testing.T, named, recorded, declared string) {
	t.Helper()

	// The DDL reader captures the literal as written, where a quote inside it is doubled; the
	// catalog holds what the server parsed out of it.
	want := strings.ReplaceAll(declared, "''", "'")
	if want == "" {
		t.Fatalf("the DDL declares no comment on %s, so comparing the catalog against it would "+
			"assert nothing", named)
	}
	if recorded != want {
		t.Errorf("the catalog holds %q on %s, and the DDL declares %q", recorded, named, want)
	}
}
