package schema

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// This file drives the classifier directly, one server condition per row. The conditions it feeds
// in are measured against the running server by ddlclassification_integration_test.go, which is
// what stops these rows from being the classifier's own description written twice.

// aBoundedSubject is a statement's two names, as ddl.go's callers write them.
var aBoundedSubject = ddlSubject{operation: "create partition events_probe", partition: "events_probe"}

// theTestBound is what the timeout rows expect to be told they waited.
const theTestBound = 1500 * time.Millisecond

// refusalRow is one server condition and what this package must make of it. A nil want means the
// refusal is none of ours and has to be handed back exactly as it arrived.
type refusalRow struct {
	name      string
	given     error
	want      error
	wantNamed string
	why       string
}

// TestEachServerConditionIsClassifiedAsItsOwnRefusal is SC-7 and SC-8 at the classifier. Every row
// asserts the sentinel it wants **and** that no other sentinel in the vocabulary matches, because a
// single "maintenance failed" error carrying two messages satisfies a one-sided check while leaving
// Steps 11, 14 and 15 with nothing to assert against.
func TestEachServerConditionIsClassifiedAsItsOwnRefusal(t *testing.T) {
	for _, row := range []refusalRow{{
		name:      "the server gave up waiting for a lock",
		given:     aServerRefusal(lockNotAvailable, "canceling statement due to lock timeout"),
		want:      ErrLockTimeout,
		wantNamed: aBoundedSubject.operation,
		why:       "55P03 means exactly this condition, so the code alone is the match",
	}, {
		name: "the DEFAULT partition already holds rows in the range",
		given: aServerRefusal(checkViolation, "updated partition constraint for default partition "+
			"events_default would be violated by some row"),
		want:      ErrDefaultBlocked,
		wantNamed: aBoundedSubject.partition,
		why:       "the refusal names the range an operator runs RepairDefaultPartition for",
	}, {
		name: "an ordinary check violation, which shares the SQLSTATE",
		given: aServerRefusal(checkViolation,
			"new row for relation orders violates check constraint orders_total_check"),
		why: "23514 is the class the DEFAULT refusal arrives under, not the condition itself",
	}, {
		name: "the DEFAULT wording arriving under some other condition",
		given: aServerRefusal("42P01",
			"updated partition constraint for default partition would be violated by some row"),
		why: "the phrase is read only inside the class it belongs to",
	}, {
		name:  "a failure the server never spoke about",
		given: context.DeadlineExceeded,
		why:   "a cancelled context and a dropped socket carry no condition to read",
	}} {
		t.Run(row.name, func(t *testing.T) { assertClassifiedAs(t, row) })
	}
}

// aServerRefusal is one condition as the driver reports it.
func aServerRefusal(code, message string) error {
	return &pgconn.PgError{Code: code, Message: message}
}

// assertClassifiedAs checks one row: which sentinel the refusal became, that no other in the
// vocabulary also matches, and that the message still names what an operator has to act on.
func assertClassifiedAs(t *testing.T, row refusalRow) {
	t.Helper()

	got := classified(row.given, aBoundedSubject, theTestBound)

	if row.want == nil && !errors.Is(got, row.given) {
		t.Errorf("classified rewrote %v into %v; %s, so it is handed back as it arrived rather "+
			"than mapped to the nearest sentinel", row.given, got, row.why)
	}
	for _, sentinel := range packageSentinels {
		if matched := errors.Is(got, sentinel); matched != (row.want != nil && errors.Is(row.want, sentinel)) {
			t.Errorf("classified(%v) = %v, which %s %v; want %s, because %s",
				row.given, got, matchedOrNot(matched), sentinel, wantedRefusal(row.want), row.why)
		}
	}
	if row.wantNamed != "" && !strings.Contains(got.Error(), row.wantNamed) {
		t.Errorf("the refusal reads %q and does not name %q, which is what an operator acts on",
			got.Error(), row.wantNamed)
	}
}

func matchedOrNot(matched bool) string {
	if matched {
		return "matches"
	}
	return "does not match"
}

func wantedRefusal(want error) string {
	if want == nil {
		return "it to match none of them"
	}
	return "it to match " + want.Error() + " alone"
}

// TestALockTimeoutRefusalNamesTheBoundItWasGiven is the half of the timeout message that says
// whether to raise the bound or to go looking for the transaction holding the conflicting lock.
func TestALockTimeoutRefusalNamesTheBoundItWasGiven(t *testing.T) {
	refused := classified(aServerRefusal(lockNotAvailable, "canceling statement due to lock timeout"),
		aBoundedSubject, theTestBound)

	if !strings.Contains(refused.Error(), theTestBound.String()) {
		t.Errorf("the refusal reads %q and does not name the %s it waited", refused.Error(), theTestBound)
	}
}

// TestAWrappedServerRefusalIsStillClassified keeps the classifier honest about how the driver hands
// its errors over: pgx wraps a *pgconn.PgError on several paths, and a type assertion written where
// an errors.As belongs would leave those unclassified while every direct row above still passed.
func TestAWrappedServerRefusalIsStillClassified(t *testing.T) {
	wrapped := fmt.Errorf("exec the statement: %w",
		aServerRefusal(lockNotAvailable, "canceling statement due to lock timeout"))

	if refused := classified(wrapped, aBoundedSubject, theTestBound); !errors.Is(refused, ErrLockTimeout) {
		t.Errorf("classified(%v) = %v, want %v; the condition is one unwrap away",
			wrapped, refused, ErrLockTimeout)
	}
}

// TestClassifyingAfterFinishingWouldLoseTheServersCondition is the test boundedTx's comment claims
// exists.
//
// The order is not stylistic. finished severs the chain to foreign errors on purpose, because
// *pgconn.ParseConfigError holds an unredacted connection string in an exported field -- so an
// errors.As reaching for a *pgconn.PgError afterwards finds nothing, and every server refusal would
// leave this package unclassified while the happy path stayed green.
func TestClassifyingAfterFinishingWouldLoseTheServersCondition(t *testing.T) {
	timedOut := aServerRefusal(lockNotAvailable, "canceling statement due to lock timeout")

	rightWayRound := finished(classified(timedOut, aBoundedSubject, theTestBound))
	if !errors.Is(rightWayRound, ErrLockTimeout) {
		t.Fatalf("classifying and then finishing answered %v, want %v", rightWayRound, ErrLockTimeout)
	}

	wrongWayRound := classified(finished(timedOut), aBoundedSubject, theTestBound)
	if errors.Is(wrongWayRound, ErrLockTimeout) {
		t.Errorf("classifying an already-finished error still reached the server's condition, so " +
			"the order boundedTx follows no longer matters and the reason recorded there is stale")
	}
}
