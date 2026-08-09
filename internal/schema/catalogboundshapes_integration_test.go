//go:build integration

package schema

import (
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is what makes partitionboundcorpus_test.go's rows evidence rather than transcription:
// PostgreSQL is asked to render each of them again, under the settings each was measured with, and
// its answer is compared with the text the container-free rows take as input. A server that changed
// its rendering fails here by name, instead of leaving the reader agreeing with a corpus nothing
// produces any more.

// renderedBoundQuery reads one partition's bound expression back. It is a second, independent
// reading of what catalog.go observes, which is why it is written out here rather than shared with
// the observation query; the single-authority scan ranges over production sources only.
const renderedBoundQuery = `
SELECT pg_get_expr(child.relpartbound, child.oid)
FROM pg_catalog.pg_class AS child
JOIN pg_catalog.pg_namespace AS within ON within.oid = child.relnamespace
WHERE within.nspname = $1 AND child.relname = $2`

// TestTheServerStillRendersEveryShapeTheReaderWasWrittenFor plants one parent per row, so no two
// extents can overlap and each row stands alone.
func TestTheServerStillRendersEveryShapeTheReaderWasWrittenFor(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, harnessSchema))

	rendered := 0
	for index, tc := range recordedBounds {
		if tc.boundClause == "" {
			continue
		}
		rendered++
		t.Run(tc.name, func(t *testing.T) {
			partition := plantOneShape(t, pool, index, tc)

			if written := renderedBoundOf(t, pool, partition, tc.session); written != tc.written {
				t.Errorf("the server renders this shape as %q; the container-free rows read it "+
					"as %q", written, tc.written)
			}
		})
	}

	if rendered == 0 {
		t.Fatal("no corpus row was rendered against the server, so this reconciliation asserts nothing")
	}
}

// plantOneShape creates this row's own parent and its one partition, and answers with the
// partition's name.
func plantOneShape(t *testing.T, pool *pgxpool.Pool, index int, tc recordedBound) string {
	t.Helper()

	parent := "bound_shape_" + strconv.Itoa(index)
	partition := parent + "_partition"

	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, parent)+" "+tc.parentColumns)
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, harnessSchema, partition)+
		" PARTITION OF "+mustQualify(t, harnessSchema, parent)+" "+tc.boundClause)
	return partition
}

// renderedBoundOf reads one partition's bound under a set of session settings.
//
// The settings and the read share one pinned connection, deliberately: a SET issued through the
// pool lands on whichever connection served it, and the read that followed could be served by
// another -- so a session-dependent row would pass or fail by which connection it drew. The
// connection is reset before it goes back, so no later case inherits the setting.
func renderedBoundOf(t *testing.T, pool *pgxpool.Pool, partition string, session []string) string {
	t.Helper()

	pinned, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("pin a connection to render %s on: %v", partition, err)
	}
	defer pinned.Release()

	for _, setting := range session {
		if _, err := pinned.Exec(t.Context(), setting); err != nil {
			t.Fatalf("apply %s: %v", setting, err)
		}
	}
	defer func() {
		if _, err := pinned.Exec(t.Context(), "RESET ALL"); err != nil {
			t.Errorf("reset the pinned connection: %v", err)
		}
	}()

	var written string
	if err := pinned.QueryRow(t.Context(), renderedBoundQuery, harnessSchema, partition).Scan(&written); err != nil {
		t.Fatalf("read the bound of %s back: %v", partition, err)
	}
	return written
}

// TestEveryRenderingDifferenceTheCorpusRecordsIsAServerSettingAndNotAnInvention is the vacuity
// guard for the rows that differ only by session setting. Without it a corpus whose session fields
// were dropped would still reconcile -- every row would render under the defaults, and the three
// offset widths the reader carries layouts for would never be produced.
func TestEveryRenderingDifferenceTheCorpusRecordsIsAServerSettingAndNotAnInvention(t *testing.T) {
	skipIfShort(t)

	settings := 0
	for _, tc := range recordedBounds {
		if len(tc.session) != 0 {
			settings++
		}
	}

	if settings != 5 {
		t.Errorf("%d rows are rendered under a session setting, want 5: the three offset widths "+
			"the reader carries a layout for, the seconds-wide one, and the DateStyle it refuses",
			settings)
	}
}
