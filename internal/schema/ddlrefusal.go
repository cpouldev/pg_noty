package schema

import (
	"strings"
	"time"
)

// This file is what a refusal from the server *means*, which is a different question from how a
// statement is run and changes for different reasons: ddl.go changes when the shape of a bounded
// transaction changes, this file changes when PostgreSQL reports a condition differently.
//
// It reaches no driver, which is what lets it build errors without finishing them. That order is
// the whole point: finished severs the chain to foreign errors deliberately -- *pgconn.ParseConfigError
// carries an unredacted connection string in an exported field, so a chain a caller could unwrap
// back to the driver would hand back everything the text had just removed (ADR-11). An errors.As
// reaching for a *pgconn.PgError afterwards therefore finds nothing, so every server refusal has to
// be classified first and finished second
// (TestClassifyingAfterFinishingWouldLoseTheServersCondition).

const (
	// lockNotAvailable is `lock_not_available`, which the server raises as `canceling statement due
	// to lock timeout`. It *is* the condition -- it says nothing but "gave up waiting for a lock" --
	// so it is matched on its own, with no message read alongside it.
	lockNotAvailable = "55P03"
	// checkViolation is `check_violation`, which is only the *class* the DEFAULT-partition refusal
	// arrives under. An ordinary constraint violation arrives under it too (measured on 17.10:
	// `new row for relation ... violates check constraint ...`, same SQLSTATE), so this code alone
	// would map every rejected row in the database to ErrDefaultBlocked.
	checkViolation = "23514"
)

// defaultPartitionRefusal is what separates the DEFAULT-partition refusal from every other check
// violation. The full sentence names the partition and ends `would be violated by some row`; the
// prefix kept here is the part that identifies the condition rather than the object.
//
// Reading a message at all is a compromise, and a deliberate one: the code is the server's own
// condition and would be the better match, but it does not distinguish these two and mapping the
// near miss onto the nearest sentinel would send an operator to drain a DEFAULT partition over a
// row some application inserted wrongly. Both halves are pinned against the running server, in both
// directions, by TestTheServerStillReportsTheDefaultRefusalThisPackageClassifiesOn -- so a release
// that reworded it fails there by name rather than leaving this classifier quietly inert.
const defaultPartitionRefusal = "updated partition constraint for default partition"

// classified is the sentinel one server refusal means, or the error exactly as it arrived.
//
// A refusal this package does not recognise is handed back unclassified rather than mapped to the
// nearest sentinel. Steps 11, 14 and 15 each branch on one of these two, and a third condition
// arriving as a timeout would send an operator looking for a lock that never existed
// (TestAServerRefusalThisPackageDoesNotRecogniseIsHandedBackUnclassified).
func classified(err error, about ddlSubject, bound time.Duration) error {
	refusal, fromServer := serverRefusalIn(err)
	if !fromServer {
		return err
	}

	switch {
	case refusal.code == lockNotAvailable:
		return lockTimedOut(about.operation, bound)
	case refusal.code == checkViolation && strings.Contains(refusal.message, defaultPartitionRefusal):
		return defaultBlocked(about.partition)
	}
	return err
}
