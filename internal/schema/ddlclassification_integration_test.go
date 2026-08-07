//go:build integration

package schema

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is SC-7 and SC-8 against the server, and the pin underneath ddlrefusal_test.go's rows.
//
// Those rows feed the classifier conditions written down by hand. That is only evidence about the
// classifier if the conditions are the ones the server actually reports, so both are measured here
// against the running server, in both directions: the condition this package classifies on, and the
// near miss that shares its SQLSTATE and must not be classified.

// probeChecked is a table with an ordinary check constraint: the near miss. Its violation arrives
// under the same SQLSTATE as the DEFAULT-partition refusal, so it is the input that says whether
// the classifier reads the condition or merely the class.
const probeChecked = "probe_checked"

// aRowInsideTheRangeAboutToBeCreated is what makes the DEFAULT partition refuse: once it holds a row
// belonging to a range, creating that range's partition fails and keeps failing, because nothing
// about retrying moves the row (M4).
const aRowInsideTheRangeAboutToBeCreated = "INSERT INTO " + probeParent +
	" (id, occurred_at) VALUES (1, '2026-03-05T00:00:00Z')"

// aCheckedTableAndTheRowItRefuses builds the near miss.
func aCheckedTableAndTheRowItRefuses(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	mustExecOn(t, pool, "CREATE TABLE "+probeChecked+" (total integer CHECK (total > 0))")
	return "INSERT INTO " + probeChecked + " (total) VALUES (-1)"
}

// aBlockedDefault is the hand-built fixture criterion 30 rests on: a range-partitioned parent, a
// permanent DEFAULT partition, and a row sitting in it that belongs to the range about to be
// created.
func aBlockedDefault(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	aPartitionedTable(t, pool)
	mustExecOn(t, pool, aRowInsideTheRangeAboutToBeCreated)
}

// TestCreatingAPartitionOverRowsAlreadyInDefaultIsRefusedAsBlocked is SC-7.
func TestCreatingAPartitionOverRowsAlreadyInDefaultIsRefusedAsBlocked(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	aBlockedDefault(t, pool)

	refused := boundedTx(t.Context(), pool, Options{}, creating(probePartition),
		oneStatement(createProbePartition()))

	if !errors.Is(refused, ErrDefaultBlocked) {
		t.Fatalf("the create answered %v, want %v", refused, ErrDefaultBlocked)
	}
	if errors.Is(refused, ErrLockTimeout) {
		t.Errorf("the DEFAULT refusal also matches %v, so the two conditions are one error with "+
			"two messages", ErrLockTimeout)
	}
	if !strings.Contains(refused.Error(), probePartition) {
		t.Errorf("the refusal reads %q and does not name %s, which is the range an operator runs "+
			"RepairDefaultPartition for", refused.Error(), probePartition)
	}
}

// TestAServerRefusalThisPackageDoesNotRecogniseIsHandedBackUnclassified is what stops a third
// condition masquerading as one of the two.
//
// It quantifies over the whole declared vocabulary rather than over the two sentinels this file is
// about, so a sentinel added later is covered by it without this test being touched.
func TestAServerRefusalThisPackageDoesNotRecogniseIsHandedBackUnclassified(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	refusedRow := aCheckedTableAndTheRowItRefuses(t, pool)

	refused := boundedTx(t.Context(), pool, Options{},
		ddlSubject{operation: "write a row " + probeChecked + " forbids", partition: probeChecked},
		oneStatement(refusedRow))

	if refused == nil {
		t.Fatal("the server accepted a row its own check constraint forbids")
	}
	for _, sentinel := range packageSentinels {
		if errors.Is(refused, sentinel) {
			t.Errorf("an ordinary check violation was classified as %v; a condition mapped to the "+
				"nearest sentinel sends an operator to fix something that is not wrong", sentinel)
		}
	}
	if !strings.Contains(refused.Error(), probeChecked) {
		t.Errorf("the unclassified refusal reads %q and no longer says what the server said",
			refused.Error())
	}
}

// TestTheServerStillReportsTheDefaultRefusalThisPackageClassifiesOn pins both halves of that
// condition against the server, bypassing ddl.go so the measurement is of PostgreSQL rather than of
// the classifier reading itself.
//
// The near miss is the second half and not a nicety: 23514 is the class the DEFAULT refusal arrives
// under, and an ordinary check violation arrives under it too. A release that stopped sharing the
// SQLSTATE would make the message clause redundant, and one that changed the wording would make the
// classifier silently inert -- this row says which of the two happened.
func TestTheServerStillReportsTheDefaultRefusalThisPackageClassifiesOn(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	aBlockedDefault(t, pool)
	nearMiss := aCheckedTableAndTheRowItRefuses(t, pool)

	blocked := serverRefusalTo(t, pool, createProbePartition())
	if blocked.Code != checkViolation {
		t.Errorf("the server reports the DEFAULT-partition refusal under SQLSTATE %s and this "+
			"package classifies on %s", blocked.Code, checkViolation)
	}
	if !strings.Contains(blocked.Message, defaultPartitionRefusal) {
		t.Errorf("the server words the DEFAULT-partition refusal %q and this package reads it for "+
			"%q", blocked.Message, defaultPartitionRefusal)
	}

	ordinary := serverRefusalTo(t, pool, nearMiss)
	if ordinary.Code != checkViolation {
		t.Errorf("an ordinary check violation now arrives under SQLSTATE %s rather than %s, so the "+
			"message clause is no longer what separates the two and the class alone would do",
			ordinary.Code, checkViolation)
	}
	if strings.Contains(ordinary.Message, defaultPartitionRefusal) {
		t.Errorf("an ordinary check violation is worded %q, which this package reads as the "+
			"DEFAULT-partition refusal", ordinary.Message)
	}
}

// TestTheServerStillReportsTheLockTimeoutConditionThisPackageClassifiesOn pins the other condition.
// It is read from the code alone, because 55P03 means exactly "gave up waiting for a lock" -- there
// is no near miss to separate it from, which is why no message is asserted here.
func TestTheServerStillReportsTheLockTimeoutConditionThisPackageClassifiesOn(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	aPartitionedTable(t, pool)
	holdAConflictingLock(t, pool)

	bounded := acquiredConn(t, pool)
	mustExecOn(t, bounded, "SET lock_timeout = "+strconv.FormatInt(conflictingBound.Milliseconds(), 10))

	timedOut := serverRefusalTo(t, bounded, createProbePartition())
	if timedOut.Code != lockNotAvailable {
		t.Errorf("the server reports a lock timeout under SQLSTATE %s (%q) and this package "+
			"classifies on %s", timedOut.Code, timedOut.Message, lockNotAvailable)
	}
}

// serverRefusalTo runs one statement that must fail and answers with what the server said about it,
// unclassified and unfinished.
func serverRefusalTo(t *testing.T, on runner, statement string) *pgconn.PgError {
	t.Helper()

	_, err := on.Exec(t.Context(), statement)
	if err == nil {
		t.Fatalf("the server accepted %s, so there is no refusal to read", statement)
	}

	var refusal *pgconn.PgError
	if !errors.As(err, &refusal) {
		t.Fatalf("%s failed with %v, which carries no server condition", statement, err)
	}
	return refusal
}
