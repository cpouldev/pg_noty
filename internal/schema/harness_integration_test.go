//go:build integration

package schema

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file asserts what the harness promises, against the running container. Its container-free
// twin is harness_test.go, and the division is deliberate: a constant naming the right database and
// a container serving it are different claims, and only the second one is the property.

// TestExactlyOneContainerServesTheWholePackage is SC-2's economy half, read from the counter rather
// than from the code: a single postgres.Run written in TestMain is equally consistent with a helper
// that starts one per test, and only a count can tell those apart.
//
// It is the weaker of the two checks by construction -- it sees only the containers started before
// it runs -- so harnessVerdict repeats it after every test has finished and fails the run there.
func TestExactlyOneContainerServesTheWholePackage(t *testing.T) {
	skipIfShort(t)

	if started := containersStarted.Load(); started != 1 {
		t.Errorf("%d containers have been started, want exactly 1", started)
	}
}

// TestTheHarnessRunsAgainstItsOwnDatabaseRatherThanTheSystemOne is SC-4 against the server. The
// mechanism drops and recreates its target, which cannot be done to the database the runner is
// connected through -- modules/postgres refuses outright, with a message that reads like a harness
// bug rather than like a wrong target.
func TestTheHarnessRunsAgainstItsOwnDatabaseRatherThanTheSystemOne(t *testing.T) {
	skipIfShort(t)

	var serving string
	if err := freshDatabase(t).QueryRow(t.Context(), "SELECT current_database()").Scan(&serving); err != nil {
		t.Fatalf("read the served database: %v", err)
	}

	if serving == "postgres" {
		t.Fatal("the container serves the postgres system database, which Restore cannot drop")
	}
	if serving != harnessDatabase {
		t.Errorf("the container serves %q and the harness snapshots %q, so a restore returns a "+
			"database no test is looking at", serving, harnessDatabase)
	}
}

// TestTheContainerServesThePostgresVersionEveryMeasurementWasTakenOn keeps the image pin honest.
// The expected version is derived from harnessImage rather than written twice, so the two cannot
// drift.
func TestTheContainerServesThePostgresVersionEveryMeasurementWasTakenOn(t *testing.T) {
	skipIfShort(t)

	want := strings.TrimSuffix(strings.TrimPrefix(harnessImage, "postgres:"), "-alpine")

	var serving string
	if err := freshDatabase(t).QueryRow(t.Context(), "SHOW server_version").Scan(&serving); err != nil {
		t.Fatalf("read the server version: %v", err)
	}
	if !strings.HasPrefix(serving, want) {
		t.Errorf("the container serves PostgreSQL %s and %s promises %s; every measurement in this "+
			"task file was taken on the second", serving, harnessImage, want)
	}
}

// TestRestoreLeavesTheNextTestNoneOfThePreviousTestsRows is SC-2's isolation half. Without it a
// shared container contaminates every case after the first, and Step 11's blocked-DEFAULT case
// cannot self-heal.
//
// It is a pair of subtests rather than a pair of top-level tests because a top-level pair is
// silently vacuous under `-run`: filtering to the second one leaves nothing for it to fail on. The
// first subtest asserts its row really landed, for the same reason.
func TestRestoreLeavesTheNextTestNoneOfThePreviousTestsRows(t *testing.T) {
	skipIfShort(t)

	const leftovers = "harness_leftovers"

	t.Run("a test that writes a row", func(t *testing.T) {
		pool := freshDatabase(t)
		mustExecOn(t, pool, "CREATE TABLE "+leftovers+" (note text)")
		mustExecOn(t, pool, "INSERT INTO "+leftovers+" (note) VALUES ('written by the previous test')")

		var written int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+leftovers).Scan(&written); err != nil {
			t.Fatalf("count the rows just written: %v", err)
		}
		if written != 1 {
			t.Fatalf("%d rows were written, want 1; the next subtest would find nothing whether or "+
				"not the restore worked", written)
		}
	})

	t.Run("the next test, after a restore", func(t *testing.T) {
		var survived bool
		err := freshDatabase(t).
			QueryRow(t.Context(), "SELECT to_regclass('public."+leftovers+"') IS NOT NULL").Scan(&survived)
		if err != nil {
			t.Fatalf("look for the previous subtest's table: %v", err)
		}
		if survived {
			t.Errorf("%s survived the restore, so every case in this package inherits the state of "+
				"the one before it", leftovers)
		}
	})
}

// TestRestoringIsCheaperThanStartingAnotherContainer is SC-2's third bullet, asserted as the
// comparison the design rests on rather than against a wall-clock constant that would be flaky on
// one machine and meaningless on another: restore is called between cases precisely because it
// costs less than the container it replaces.
func TestRestoringIsCheaperThanStartingAnotherContainer(t *testing.T) {
	skipIfShort(t)

	restoring := restoreToSnapshot(t)

	t.Logf("restore took %s, and starting the container took %s", restoring, containerStartup)
	if restoring >= containerStartup {
		t.Errorf("a restore takes %s and a container takes %s, so restoring between cases buys "+
			"nothing and the harness should start one per test instead", restoring, containerStartup)
	}
}

// TestSnapshotAndRestoreTakeTheNativeDriverPathRatherThanDockerExec is SC-3 against the running
// container. modules/postgres opens its snapshot connection with sql.Open(harnessSQLDriver, ...)
// followed by DB.Conn, and those two failing is the *only* route to the `docker exec psql`
// fallback; this test performs exactly that pair, so the fallback branch is shown unreachable
// rather than assumed to be.
//
// What it does not do is observe the module's own call: the fallback is announced through
// testcontainers-go's log package, and importing that would make the runtime a second direct
// requirement of this module for one assertion.
func TestSnapshotAndRestoreTakeTheNativeDriverPathRatherThanDockerExec(t *testing.T) {
	skipIfShort(t)

	if registered := sql.Drivers(); !slices.Contains(registered, harnessSQLDriver) {
		t.Fatalf("database/sql knows %v, which does not include %q, so the module's sql.Open fails "+
			"with an unknown driver and every restore shells out", registered, harnessSQLDriver)
	}

	opened, err := sql.Open(harnessSQLDriver, harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatalf("sql.Open under %q, the call the module makes: %v", harnessSQLDriver, err)
	}
	defer opened.Close()

	conn, err := opened.Conn(t.Context())
	if err != nil {
		t.Fatalf("DB.Conn under %q, the second call the module makes: %v", harnessSQLDriver, err)
	}
	conn.Close()
}

// runner is anything a statement runs on here: the pool, one pinned connection, or a transaction.
type runner interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

// mustExecOn runs one statement that has to succeed for the test around it to mean anything.
//
// It was two helpers until Steps 7 and 8 had both landed: this one, and a pool-only mustExec whose
// body was identical. A copy differing only in the handle it accepts is two behaviours the moment
// one of them gains a guard or a better message, and nothing fails when they disagree.
func mustExecOn(t *testing.T, on runner, statement string, arguments ...any) {
	t.Helper()

	if _, err := on.Exec(t.Context(), statement, arguments...); err != nil {
		t.Fatalf("exec %s: %v", statement, err)
	}
}
