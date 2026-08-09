package schema

import (
	"errors"
	"fmt"
	"time"
)

// This file is the package's error vocabulary: eight sentinels, and one constructor per *condition*
// giving it the detail its criterion requires it to name. The wording lives here rather than at the
// call sites, so one condition cannot come to be reported in several spellings.
//
// Conditions outnumber sentinels, and the two counts are kept apart deliberately. A sentinel is the
// class a caller branches on; a condition is what an operator is told and what they do about it.
// ErrUnservableHorizon carries three, so sentinelDetail is keyed on the condition rather than on the
// sentinel -- keyed on the sentinel, the second and third conditions would inherit the first one's
// row and acquire no coverage of their own.
//
// errorelision.go holds the other half -- the one point every error passes through on its way out.

var (
	// ErrSchemaAhead refuses a database recording a version this binary does not embed
	// (criterion 14). Step 10's runner returns it, having applied nothing.
	ErrSchemaAhead = errors.New("the database records a schema version this binary does not know")
	// ErrCoverageShort refuses a boot whose partitions already fall short of the configured
	// horizon (criterion 31). Step 13's bootstrap returns it.
	ErrCoverageShort = errors.New("the partitions in place do not reach the required horizon")
	// ErrDefaultBlocked reports a range whose partition cannot be created because the DEFAULT
	// partition already holds rows in it (criterion 30, ADR-8). Steps 8 and 11 return it, and no
	// row is moved.
	ErrDefaultBlocked = errors.New("the DEFAULT partition already holds rows in this range")
	// ErrLockTimeout reports DDL abandoned at the configured lock timeout with the schema
	// unchanged, so a later pass can retry (criterion 40). Steps 8, 14 and 15 return it.
	ErrLockTimeout = errors.New("the statement gave up waiting for its lock")
	// ErrForeignInstance refuses a schema carrying another instance's ownership marker. Step 13's
	// bootstrap returns it; an unmarked schema is claimed rather than refused.
	ErrForeignInstance = errors.New("the schema is marked as owned by another pg_noty instance")
	// ErrPrivilege reports a missing privilege by name rather than as an unwrapped driver error
	// (criterion 17). Step 13's bootstrap returns it, for the schema it could not create and for
	// the one it could not claim.
	ErrPrivilege = errors.New("the connected role is missing a privilege this package requires")
	// ErrUnservableHorizon refuses a configuration whose pre-creation horizon no database can serve.
	// Both entry points that own an error channel return it -- Step 13's bootstrap before it takes a
	// connection and Step 11's pass before it reads the catalog -- and neither allocates the set to
	// find out (horizon.go).
	//
	// It carries three conditions, and the sentence below is deliberately the class rather than any
	// of them: a horizon can be unservable because its interval steps the grid backwards, because
	// that interval is not a whole multiple of the instant a partition bound is stored to, or because
	// it asks for more partitions than a database has relation identifiers for. Each has its own
	// constructor below, and each is pinned by equality, because a registry keyed on the sentinel
	// alone would grant the later conditions the first one's coverage.
	ErrUnservableHorizon = errors.New("the configured pre-creation horizon cannot be served by any database")
	// ErrLedgerAbsent reports a configured schema holding no migration ledger, which is what a
	// database nothing has bootstrapped looks like. AppliedVersion returns it so that a caller
	// forbidden from bootstrapping can tell "not set up yet" from "the read failed" and answer the
	// first with the command that sets it up, rather than with a driver diagnostic.
	ErrLedgerAbsent = errors.New("the configured schema has no migration ledger")
)

// packageSentinels is the whole vocabulary as a value, so the distinguishability sweep and the
// finishing point both quantify over it rather than over a list copied into one of them. An eighth
// sentinel joins here or it is invisible to both.
var packageSentinels = []error{
	ErrSchemaAhead,
	ErrCoverageShort,
	ErrDefaultBlocked,
	ErrLockTimeout,
	ErrForeignInstance,
	ErrPrivilege,
	ErrUnservableHorizon,
	ErrLedgerAbsent,
}

// schemaAhead names both versions, which is what lets an operator see whether to roll the binary
// forward or the database back.
func schemaAhead(recorded, highest int) error {
	return fmt.Errorf("%w: the database records version %d and this binary embeds up to %d",
		ErrSchemaAhead, recorded, highest)
}

// coverageShort names the shortfall and the horizon it falls short of, so the gap is actionable
// without reading the configuration alongside the message.
func coverageShort(shortfall, horizon time.Duration) error {
	return fmt.Errorf("%w: coverage is short by %s of the required %s",
		ErrCoverageShort, shortfall, horizon)
}

// privilegeMissing names the schema and the privilege, because "permission denied" without both is
// a message an operator cannot act on.
func privilegeMissing(schema, privilege string) error {
	return fmt.Errorf("%w: the role needs %s on schema %s", ErrPrivilege, privilege, schema)
}

// defaultBlocked names the range whose partition could not be created, which is the one thing an
// operator needs in order to run the drain at RepairDefaultPartition.
func defaultBlocked(rangeName string) error {
	return fmt.Errorf("%w: partition %s cannot be created until those rows are moved",
		ErrDefaultBlocked, rangeName)
}

// foreignInstance names both instances, so an operator can tell a misconfigured instance name from
// two services sharing one schema.
func foreignInstance(marked, expected string) error {
	return fmt.Errorf("%w: the marker names instance %s and this instance is %s",
		ErrForeignInstance, marked, expected)
}

// unservableHorizon names the count the configuration asks for, the bound it broke, and the two
// durations whose ratio produced it. All four, because the remedy is to widen the interval or to
// shorten the horizon, and a count on its own says which of them to reach for no better than the
// crash it replaces did.
func unservableHorizon(count int64, interval, precreate time.Duration) error {
	return fmt.Errorf("%w: a %s partition interval over a %s horizon asks for %d partitions, and a "+
		"database holds at most %d relations -- of which this schema's event log and its DEFAULT "+
		"partition are already two", ErrUnservableHorizon, interval, precreate, count,
		relationIdentifiersOneDatabaseHolds)
}

// unstorableInterval names the interval, the granularity it is not a multiple of, and the consequence
// -- all three, because the remedy is to round the interval up to whole microseconds and a message
// naming only the interval would leave an operator to discover the rule by experiment.
//
// The consequence is worded as the round trip rather than as "the server refuses it", because
// refusal is the *lesser* of the two outcomes and only some intervals in this class earn it.
// Measured on 17.10: a bound is written at nanosecond precision and stored to the nearest
// microsecond, so `FROM ('...0000015Z') TO ('...000003Z')` -- a 1.5µs grid -- is accepted and reads
// back as `[.000002, .000003)`, an extent nothing asked for. Only where two adjacent bounds round
// to the *same* instant does the server refuse outright, with `empty range bound specified for
// partition` (SQLSTATE 42P17). The first outcome is worse: this package decides what exists by
// comparing observed extents and never by name (M6), so a partition it created is one it can never
// recognise again, and every later pass asks for it and is told it would overlap.
func unstorableInterval(interval time.Duration) error {
	return fmt.Errorf("%w: a %s partition interval is not a whole multiple of the %s a timestamptz "+
		"stores, so the bounds it puts on the grid are rounded when written and the catalog reports "+
		"back an extent this package never asked for", ErrUnservableHorizon, interval,
		storedInstantGranularity)
}

// negativeInterval names the interval and what a grid stepping backwards does to the set built on
// it. Its own message rather than the unstorable one's, because the two have different remedies and
// a new reason to refuse needs a new answer rather than an existing one's wording: rounding a
// negative interval to whole microseconds leaves it negative, and writing a positive one is what
// makes it servable.
func negativeInterval(interval time.Duration) error {
	return fmt.Errorf("%w: a %s partition interval steps the grid backwards, so the k-range a "+
		"horizon asks for runs from a later boundary to an earlier one and holds no partition at "+
		"all; a partition interval has to be a positive duration", ErrUnservableHorizon, interval)
}

// ledgerAbsent names the schema, because an operator running several instances against one database
// needs to know which of them was never bootstrapped, and because the remedy is a command that takes
// that schema's configuration file.
func ledgerAbsent(schema string) error {
	return fmt.Errorf("%w: schema %s records no applied migration", ErrLedgerAbsent, schema)
}

// lockTimedOut names the operation and the timeout it was given, which together say whether to
// raise the timeout or to look for the transaction holding the conflicting lock.
func lockTimedOut(operation string, timeout time.Duration) error {
	return fmt.Errorf("%w: %s gave up after %s", ErrLockTimeout, operation, timeout)
}
