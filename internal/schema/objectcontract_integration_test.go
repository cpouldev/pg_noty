//go:build integration

package schema

import (
	"maps"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The object contract the packages above this one compile against, read back from a real catalog
// after Step 10's runner has applied the shipped corpus. This is Step 6's real verification: the
// DDL saying a column exists and the server holding it of that type are different claims, and only
// the second one is what a later package compiles against.
//
// Step 6's deferred object-inventory case -- "every object this DDL declares exists in the
// configured schema with its declared columns, types and nullability, read back from the catalog"
// -- is closed by TestEveryContractTableHoldsExactlyItsContractColumns below, and its row has been
// removed from deferredCases in migrationdeferred_test.go rather than left standing
// . The three files beside this one
// close the rest of the contract: keys and identity, constraints, and indexes.

// migratedSchema is a freshly restored database with the shipped corpus applied into harnessSchema,
// which is the state every inventory in these four files is read from. It goes through the real
// runner rather than executing the SQL directly, so what is inventoried is what a boot produces.
func migratedSchema(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, embeddedCorpusOrFail(t))
	return pool
}

// tableColumnsQuery reads one table's columns from the standard view rather than from the DDL text.
// Its data_type is format_type(atttypid, null) for a type in pg_catalog, which is what lets the
// contract's own spelling be resolved through to_regtype below instead of an alias table (`int` and
// `integer`, `timestamptz` and `timestamp with time zone`) being transcribed here.
const tableColumnsQuery = `
SELECT column_name, data_type, is_nullable = 'NO', character_maximum_length
  FROM information_schema.columns
 WHERE table_schema = $1 AND table_name = $2
 ORDER BY ordinal_position`

// catalogTypeQuery resolves one contract type spelling to the name the view above reports for a
// column of that type, or to the empty string for a spelling the server does not know.
const catalogTypeQuery = `SELECT coalesce(format_type(to_regtype($1), NULL), '')`

// observedColumn is one column as the catalog holds it.
type observedColumn struct {
	typeName string
	notNull  bool
	// maxLength is nil for an unbounded type, which is the property SC 9 asserts of
	// deliveries.response_snippet.
	maxLength *int
}

func observedColumnsOf(t *testing.T, pool *pgxpool.Pool, table string) map[string]observedColumn {
	t.Helper()

	rows, err := pool.Query(t.Context(), tableColumnsQuery, harnessSchema, table)
	if err != nil {
		t.Fatalf("read the columns of %s.%s: %v", harnessSchema, table, err)
	}
	defer rows.Close()

	observed := map[string]observedColumn{}
	for rows.Next() {
		var name string
		var found observedColumn
		if err := rows.Scan(&name, &found.typeName, &found.notNull, &found.maxLength); err != nil {
			t.Fatalf("scan a column of %s.%s: %v", harnessSchema, table, err)
		}
		observed[name] = found
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the columns of %s.%s: %v", harnessSchema, table, err)
	}
	return observed
}

// catalogTypeNames resolves every type spelling the contract uses to the name the catalog reports.
// Handing the contract's own spelling to the server derives the expectation, where a hand-written
// alias table would transcribe one -- and a spelling the server cannot resolve fails here rather
// than reading as a mismatched column.
func catalogTypeNames(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()

	canonical := map[string]string{}
	for _, column := range theObjectContract {
		if _, resolved := canonical[column.sqlType]; resolved {
			continue
		}
		var named string
		if err := pool.QueryRow(t.Context(), catalogTypeQuery, column.sqlType).Scan(&named); err != nil {
			t.Fatalf("resolve the contract's type spelling %q: %v", column.sqlType, err)
		}
		if named == "" {
			t.Fatalf("the server resolves the contract's type spelling %q to no type at all, so "+
				"every column declared with it would be compared against nothing", column.sqlType)
		}
		canonical[column.sqlType] = named
	}
	return canonical
}

// TestEveryContractTableHoldsExactlyItsContractColumns is criterion 3, as a closed inventory: the
// six tables exist in the configured schema, each holds exactly the number of columns pinned for it,
// and every column the contract names is present with its declared type and nullability. The two
// halves together close the set in both directions -- with the count pinned and every contract
// column found, a column the contract does not list has nowhere to hide.
func TestEveryContractTableHoldsExactlyItsContractColumns(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	canonical := catalogTypeNames(t, pool)
	observed := map[string]map[string]observedColumn{}

	for _, table := range theContractTables {
		observed[table] = observedColumnsOf(t, pool, table)
		assertTableIsClosedAtItsPinnedSize(t, table, observed[table])
	}
	for _, want := range theObjectContract {
		got, present := observed[want.table][want.name]
		if !present {
			t.Errorf("%s.%s is absent from the catalog, and the packages above this one compile against it",
				want.table, want.name)
			continue
		}
		assertColumnMatchesCatalog(t, got, want, canonical[want.sqlType])
	}
}

func assertTableIsClosedAtItsPinnedSize(t *testing.T, table string, observed map[string]observedColumn) {
	t.Helper()

	if len(observed) == 0 {
		t.Errorf("%s.%s does not exist, so every column of it is about to be reported absent one "+
			"by one and the table itself never named", harnessSchema, table)
		return
	}
	if want := theContractColumnCounts[table]; len(observed) != want {
		t.Errorf("%s holds %d columns %v, and the contract pins %d; a column here that the contract "+
			"does not list is one no later package was told about",
			table, len(observed), slices.Sorted(maps.Keys(observed)), want)
	}
}

func assertColumnMatchesCatalog(t *testing.T, got observedColumn, want contractColumn, canonical string) {
	t.Helper()

	if got.typeName != canonical {
		t.Errorf("%s.%s is %s in the catalog, and the contract declares %s, which this server "+
			"resolves to %s", want.table, want.name, got.typeName, want.sqlType, canonical)
	}
	if got.notNull != want.notNull {
		t.Errorf("%s.%s is notNull=%t in the catalog, and the contract declares %t",
			want.table, want.name, got.notNull, want.notNull)
	}
}

// TestTheEventLogHoldsNoneOfTheQueuesDeliveryState is criterion 5's absence half. The split storage
// design is the reason the claim path never touches the partitioned table, so these three columns
// being missing is the property rather than a side effect -- and each row carries why, so a later
// addition meets the reason rather than an unexplained gap.
func TestTheEventLogHoldsNoneOfTheQueuesDeliveryState(t *testing.T) {
	skipIfShort(t)

	observed := observedColumnsOf(t, migratedSchema(t), TableEvents)
	if len(observed) == 0 {
		t.Fatalf("%s.%s holds no columns at all, so every absence below is vacuous",
			harnessSchema, TableEvents)
	}

	for _, absent := range theEventLogAbsences {
		if _, present := observed[absent.column]; present {
			t.Errorf("%s carries %s, and it must not: %s", TableEvents, absent.column, absent.why)
		}
	}
}
