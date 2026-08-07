//go:build integration

package schema

import (
	"errors"
	"path"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 11 -- the ledger a run leaves behind -- and the setup the other runner suites share.
// Every case drives the shipped runner; the only thing a case ever substitutes is the corpus, which
// is exactly where ADR-10 put the seam. What no case here asserts is the *shape* of what the corpus
// creates, which is Step 12's.

// uniqueViolation is the SQLSTATE a primary key raises, which is ADR-5's whole mechanism.
const uniqueViolation = "23505"

// emptySchemas is a restored database with a pool onto it and the named schemas created. Every case
// needs at least one: the harness snapshot is an empty database and holds none.
func emptySchemas(t *testing.T, names ...string) *pgxpool.Pool {
	t.Helper()

	pool := freshDatabase(t)
	for _, name := range names {
		quotedName, fault := Quoted(name)
		if fault != IdentifierOK {
			t.Fatalf("the case names the schema %s, and it %s", name, fault)
		}
		mustExecOn(t, pool, "CREATE SCHEMA "+quotedName)
	}
	return pool
}

// mustMigrate runs one corpus into one schema through the real runner.
func mustMigrate(t *testing.T, db txBeginner, schemaName string, corpus []migration) migrationRun {
	t.Helper()

	run, err := migrate(t.Context(), db, schemaName, corpus)
	if err != nil {
		t.Fatalf("migrate %d migrations into %s: %v", len(corpus), schemaName, err)
	}
	return run
}

// syntheticCorpus is a corpus of text this suite wrote, read through Step 6's own parser rather
// than assembled by hand -- so its checksums are the real ones and its ordering has been validated
// by the same code the shipped corpus goes through.
func syntheticCorpus(t *testing.T, files map[string]string) []migration {
	t.Helper()

	written := fstest.MapFS{}
	for name, text := range files {
		written[path.Join(migrationDir, name)] = &fstest.MapFile{Data: []byte(text)}
	}
	corpus, err := corpusFrom(written, migrationDir)
	if err != nil {
		t.Fatalf("read a corpus of %d synthetic migrations: %v", len(files), err)
	}
	return corpus
}

// ledgerEntry is one recorded row as this suite reads it back. It is wider than the runner's own
// appliedVersion because criterion 11 asks for the timestamp the runner itself never reads.
type ledgerEntry struct {
	version   int
	checksum  string
	appliedAt time.Time
}

// recordedLedger reads the ledger independently of the runner: its own query, its own qualified
// name and no search_path.
func recordedLedger(t *testing.T, pool *pgxpool.Pool, schemaName string) []ledgerEntry {
	t.Helper()

	name, fault := Qualified(schemaName, TableSchemaVersion)
	if fault != IdentifierOK {
		t.Fatalf("the case names the schema %s, and it %s", schemaName, fault)
	}
	rows, err := pool.Query(t.Context(),
		"SELECT version, checksum, applied_at FROM "+name+" ORDER BY version")
	if err != nil {
		t.Fatalf("read the ledger in %s: %v", schemaName, err)
	}

	recorded, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledgerEntry, error) {
		var found ledgerEntry
		return found, row.Scan(&found.version, &found.checksum, &found.appliedAt)
	})
	if err != nil {
		t.Fatalf("collect the ledger in %s: %v", schemaName, err)
	}
	return recorded
}

// versionsOf is the version set a ledger records, in the order it was read.
func versionsOf(recorded []ledgerEntry) []int {
	found := make([]int, 0, len(recorded))
	for _, entry := range recorded {
		found = append(found, entry.version)
	}
	return found
}

// consecutiveVersions is the full range 1..n, which criterion 16 compares a recorded set against
// rather than comparing maxima -- a maximum cannot tell {1,2,3} from {1,2,4} or from {1,2,3,3}.
func consecutiveVersions(n int) []int {
	wanted := make([]int, 0, n)
	for version := firstMigrationVersion; version <= n; version++ {
		wanted = append(wanted, version)
	}
	return wanted
}

// TestAFreshDatabaseEndsWithOneLedgerRowPerEmbeddedMigration is criterion 11. Each row is checked
// against *that* migration's own checksum rather than against any checksum, so a runner writing one
// migration's hash under another's version fails here.
func TestAFreshDatabaseEndsWithOneLedgerRowPerEmbeddedMigration(t *testing.T) {
	skipIfShort(t)

	corpus := embeddedCorpusOrFail(t)
	pool := emptySchemas(t, harnessSchema)
	began := databaseClock(t, pool)

	// Through migrateEmbedded rather than migrate, so the entry point Step 13 composes -- corpus
	// included -- is the one this criterion is established against.
	run, err := migrateEmbedded(t.Context(), pool, harnessSchema)
	if err != nil {
		t.Fatalf("migrate the embedded corpus into %s: %v", harnessSchema, err)
	}

	recorded := recordedLedger(t, pool, harnessSchema)
	if len(recorded) != len(corpus) {
		t.Fatalf("the ledger holds %d rows %v, want one per embedded migration (%d)",
			len(recorded), versionsOf(recorded), len(corpus))
	}
	for i, entry := range recorded {
		assertRecordsItsOwnMigration(t, entry, corpus[i], began)
	}
	if run.recorded != highestEmbedded(corpus) {
		t.Errorf("the run reports version %d, want %d", run.recorded, highestEmbedded(corpus))
	}
}

func assertRecordsItsOwnMigration(t *testing.T, entry ledgerEntry, applied migration, began time.Time) {
	t.Helper()

	if entry.version != applied.version {
		t.Errorf("the ledger records version %d where the corpus carries %d",
			entry.version, applied.version)
	}
	if entry.checksum != applied.checksum {
		t.Errorf("version %d is recorded with checksum %s, and %s hashes to %s",
			entry.version, entry.checksum, applied.file, applied.checksum)
	}
	if entry.appliedAt.Before(began) {
		t.Errorf("version %d is recorded as applied at %s, which is before the run began at %s",
			entry.version, entry.appliedAt, began)
	}
}

// TestTheLedgersKeyIsOnVersionAndTurnsASecondApplicationIntoAUniqueViolation is criterion 11's key
// half, read from the catalog rather than from the DDL text -- the text saying PRIMARY KEY (version)
// is a different claim from the constraint existing.
//
// The second half is ADR-5's reason the key is the concurrency *mechanism* and not bookkeeping: a
// second application of a version already recorded does not leave a second row for someone to
// notice later, it raises inside that migration's own transaction and rolls the migration back.
func TestTheLedgersKeyIsOnVersionAndTurnsASecondApplicationIntoAUniqueViolation(t *testing.T) {
	skipIfShort(t)

	corpus := embeddedCorpusOrFail(t)
	pool := emptySchemas(t, harnessSchema)
	mustMigrate(t, pool, harnessSchema, corpus)

	if got := primaryKeyColumnsOf(t, pool, harnessSchema, TableSchemaVersion); !slices.Equal(got,
		[]string{"version"}) {
		t.Errorf("%s.%s is keyed on %v, want [version]", harnessSchema, TableSchemaVersion, got)
	}

	name, _ := Qualified(harnessSchema, TableSchemaVersion)
	_, err := pool.Exec(t.Context(), "INSERT INTO "+name+
		" (version, checksum, applied_at) VALUES ($1, $2, now())", corpus[0].version, corpus[0].checksum)

	var raised *pgconn.PgError
	if !errors.As(err, &raised) || raised.Code != uniqueViolation {
		t.Errorf("recording version %d twice answered %v, want SQLSTATE %s; without the key a "+
			"concurrent double application leaves two rows instead of rolling one back",
			corpus[0].version, err, uniqueViolation)
	}
}
