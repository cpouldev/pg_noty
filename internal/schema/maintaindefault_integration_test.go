//go:build integration

package schema

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 30 under ADR-8, Accepted: the pass detects the blocked range, counts it
// under its own name, logs it with the range and the command that clears it, and moves no row.
//
// M4 measured that the DEFAULT partition cannot self-heal: once rows land in it for a range,
// creating that range's partition fails and keeps failing until they are moved out. The claim under
// test is therefore not "the error is handled" but "the state degrades visibly without becoming an
// outage" -- which is why the customer's writes are asserted still working, and why the blocked
// case restores the shared database rather than leaving the next to inherit an uncreatable range.

// rowsInDefault is the standing condition the gauge carries, read through catalog.go's own counter
// so the case and the pass cannot disagree about what they are counting.
func rowsInDefault(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	held, err := defaultPartitionRows(t.Context(), pool, harnessSchema, PartitionDefault)
	if err != nil {
		t.Fatalf("count the rows in the DEFAULT partition: %v", err)
	}
	return held
}

// refusalsLogged is every record naming a range the pass was refused, one per range. A failed pass
// writes its summary at the same level deliberately -- an alerting rule keys on the level -- so a
// refusal is told apart by its message rather than by counting records above routine operation.
func refusalsLogged(report passReport) []loggedRecord {
	var refusals []loggedRecord
	for _, record := range report.loggedAt(slog.LevelError) {
		if record.message == refusedMessage {
			refusals = append(refusals, record)
		}
	}
	return refusals
}

// TestARangeTheDefaultPartitionBlocksIsRefusedCountedAndLoggedWithoutMovingARow is criterion 30's
// detect-and-count half, and ADR-8's prohibition alongside it.
//
// The seven ranges the blocked one does not block are asserted created, because "non-fatal per
// range" is the whole reason the pass does not stop at the first refusal: one blocked day must not
// leave the six behind it uncovered.
func TestARangeTheDefaultPartitionBlocksIsRefusedCountedAndLoggedWithoutMovingARow(t *testing.T) {
	skipIfShort(t)

	// One row at an instant no partition covers yet: it lands in the DEFAULT partition and belongs
	// to the very range the pass is about to create, which is the state M4 measured as permanent.
	const planted = 1
	pool, cfg := aMaintainedEventLog(t)
	insertEvent(t, pool, theClockOf(t, pool))
	if held := rowsInDefault(t, pool); held != planted {
		t.Fatalf("the DEFAULT partition holds %d rows before the pass and %d were planted; the range "+
			"about to be created is not blocked, so this case would assert nothing", held, planted)
	}

	before := theClockOf(t, pool)
	report := onePass(t, pool, cfg, Options{})
	wanted := theHorizonBetween(t, before, theClockOf(t, pool))
	blocked := wanted[0] // the range holding now, which is where the planted row sits

	if !errors.Is(report.refused, ErrDefaultBlocked) {
		t.Fatalf("the pass answered %v, want %v: rows for %s were already in the DEFAULT partition",
			report.refused, ErrDefaultBlocked, extentOf(blocked))
	}
	if want := (Result{Outcome: OutcomeFailed, Created: len(wanted) - 1}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v -- one range refused and every other created",
			report.settled, want)
	}
	assertBlockedRangeObserved(t, report, blocked, planted)

	// ADR-8's prohibition: the pass moves no row, so the standing condition it observed is still
	// exactly what it was. Step 15's source scan is the complementary assertion and neither cites
	// the other's row.
	if held := rowsInDefault(t, pool); held != planted {
		t.Errorf("the DEFAULT partition holds %d rows after the blocked pass and held %d before it; "+
			"this pass issues no DELETE and no INSERT against it under any input", held, planted)
	}
	// Degraded rather than broken: the customer can still write, and the write still commits.
	oneCommittedEvent(t, pool, before)

	// M4 makes this mandatory rather than tidy: the blocked range stays blocked, so a later case in
	// this shared container would inherit a range nothing can create.
	restoreToSnapshot(t)
}

// assertBlockedRangeObserved is what an operator and a caller with no metrics endpoint can see: the
// distinct counter, the gauge, and a log line naming the range and the command that clears it.
func assertBlockedRangeObserved(t *testing.T, report passReport, blocked Range, planted int64) {
	t.Helper()

	want := statsReading{failures: 1, defaultPartitionBlocked: 1, defaultPartitionRows: planted}
	if report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v: the blocked range moves a counter of its own, "+
			"because its remedy is a drain and not a retry", report.stats, want)
	}
	if !strings.Contains(report.refused.Error(), extentOf(blocked)) {
		t.Errorf("the refusal reads %s and does not name %s, which is the range an operator runs "+
			"the drain for", report.refused.Error(), extentOf(blocked))
	}

	refusals := refusalsLogged(report)
	if len(refusals) != 1 {
		t.Fatalf("the pass logged %d refusals at %s, want exactly one -- the single range the "+
			"DEFAULT partition blocked", len(refusals), slog.LevelError)
	}

	logged := refusals[0]
	if logged.attrs[logRange] != blocked.Name || logged.attrs[logExtent] != extentOf(blocked) {
		t.Errorf("the refusal was logged for range %s %s, want %s %s",
			logged.attrs[logRange], logged.attrs[logExtent], blocked.Name, extentOf(blocked))
	}
	if !strings.Contains(logged.attrs[logRemedy], repairCommand) {
		t.Errorf("the line reports the remedy as %s, which does not name %s -- the operator-invoked "+
			"drain, and the only thing that clears this state", logged.attrs[logRemedy], repairCommand)
	}
}
