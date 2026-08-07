package schema

import (
	"context"
	"fmt"
)

// This file is the boot's three schema-level refusals: workflow step 1's reserved name, step 4's
// ownership marker and step 5's create. bootstrap.go is the other half, and holds the order these
// run in.
//
// It reaches the driver only through the pinned connection bootstrap.go holds, so it imports no
// driver package of its own -- which is also why it is outside the unfinished-path sweep's subject
// set. Every error it builds is finished all the same, because the errors it builds *from* are the
// server's.

const (
	// createSchemaStatement is workflow step 5. IF NOT EXISTS is not what makes the concurrent case
	// safe -- two sessions creating one schema at once race on the catalog's own unique index
	// whatever it says -- step 3's lock is (criterion 19, skill Common Pitfalls).
	createSchemaStatement = "CREATE SCHEMA IF NOT EXISTS "
	// insufficientPrivilege is the SQLSTATE the server raises when the connected role may not do
	// what a statement asks. Measured on 17.10, a role without CREATE on the database meets it as
	// `permission denied for database ...` the moment it asks for a schema.
	insufficientPrivilege = "42501"
	// createSchemaPrivilege is the privilege criterion 17's refusal names. Together with the schema
	// it is what an operator needs in order to act on that refusal at all.
	createSchemaPrivilege = "CREATE"
	// claimSchemaPrivilege is the privilege step 4's claim needs, and it is ownership rather than a
	// grantable right: only an owner may COMMENT ON a schema. It is criterion 17's second refusal
	// and it is exactly the ADR-3 path -- a DBA pre-creating the schema and granting CREATE and
	// USAGE on it leaves this boot able to create objects in a schema it cannot mark as its own,
	// and an operator meeting that needs to be told to transfer ownership rather than handed the
	// driver's sentence.
	claimSchemaPrivilege = "OWNERSHIP"
)

// reservedSchemaName is workflow step 1's refusal, and it is the only thing closing its class:
// every configured name reaches DDL quoted, and the server accepts a quoted CREATE SCHEMA for any
// name whose first three bytes are not exactly lower-case `pg_` (M8) -- so a schema claimed out of
// PostgreSQL's own namespace would be created rather than refused. The prefix is named in the
// message because a refusal an operator cannot act on is barely better than none.
//
// The predicate is identifier.go's, which folds case for the server's own reason and refuses the
// exactly-three-byte spelling too.
//
// The format is one literal rather than a concatenation because the quoting scan excuses `%q` only
// where it is the format an Errorf call was given -- and here it is a diagnostic aid rather than a
// quoting decision, since the name is read by a person and never executed.
func reservedSchemaName(schema string) error {
	return finished(fmt.Errorf(
		"the configured schema %q begins with %q, which PostgreSQL reserves for its own schemas",
		schema, reservedSchemaPrefix))
}

// claimAndCreateSchema is workflow steps 4 and 5.
//
// Step 4 decides before step 5 acts, and that is what orders their two refusals: a schema another
// instance owns is refused rather than created into, and a role that may not create one is told
// about the privilege only where the marker did not already refuse it.
//
// The claim step 4 decides on is *written* after step 5, because it can only be written then:
// COMMENT ON SCHEMA needs the schema to exist, and on a first boot it does not yet. ADR-3 makes an
// unmarked schema claimable rather than refusable, since a DBA pre-creating it is the supported
// minimal-privilege path -- a path that additionally needs ownership of the schema, because only an
// owner may comment on one.
func (run boot) claimAndCreateSchema(ctx context.Context) error {
	marked, fault := schemaObject(run.cfg.Database.Schema, run.cfg.Instance)
	if fault != IdentifierOK {
		return finished(fmt.Errorf("the configured schema %q %s", run.cfg.Database.Schema, fault))
	}

	reading, err := markerOn(ctx, run.on, marked)
	if err != nil {
		return finished(err)
	}
	if refusal := run.ownershipRefusal(reading); refusal != nil {
		return refusal
	}

	if _, err := run.on.Exec(ctx, createSchemaStatement+marked.target); err != nil {
		return run.schemaRefusal(err, createSchemaPrivilege)
	}
	if reading.state != markerAbsent {
		return nil
	}
	if err := claimMarker(ctx, run.on, marked); err != nil {
		return run.schemaRefusal(err, claimSchemaPrivilege)
	}
	return nil
}

// ownershipRefusal is ADR-3's marker policy: which of the four states a boot may proceed from, and
// what each of the others is refused as. Every reason answers separately, so an operator is told
// what is actually wrong, and the foreign refusal is an arm of its own rather than a fallthrough.
func (run boot) ownershipRefusal(reading markerReading) error {
	switch reading.state {
	case markerOurs, markerAbsent:
		return nil // ours to carry on with; an unmarked one is claimed above and never refused
	case markerForeign:
		return finished(foreignInstance(reading.named, run.cfg.Instance))
	case markerUnreadable:
		return finished(fmt.Errorf("schema %s carries a comment that is not an ownership marker of "+
			"this format, so this boot cannot tell whose schema it is: remove the comment, or point "+
			"instance %s at a schema of its own", run.cfg.Database.Schema, run.cfg.Instance))
	}
	// Unreachable while markerStates holds those four, which ownershipmarker_test.go pins. Refusing
	// rather than proceeding, because a state this boot cannot name is not one it may claim a schema
	// on; the branch is reached directly by
	// TestAnOwnershipStateThisBootCannotNameIsRefusedRatherThanClaimed.
	return finished(fmt.Errorf(
		"the ownership marker on schema %s was read as %q, which is no state this boot can act on",
		run.cfg.Database.Schema, reading.state))
}

// schemaRefusal is what a refused statement about the service schema means, for whichever of the
// two privileges that statement needed. A role that lacks one is told which it lacks on which schema
// rather than handed the driver's own sentence (criterion 17); every other condition is passed
// through as it arrived, because mapping one this package does not recognise onto the nearest
// sentinel sends an operator to fix the wrong thing.
//
// One classifier for step 5's create and step 4's claim rather than one each: both meet the same
// SQLSTATE and both owe the same message, and a second spelling of the mapping is how one of them
// comes to answer something the other does not. The privilege is the parameter because it is the
// only thing that differs, and it is the one thing an operator has to act on.
//
// Classified before finishing, and that order is load-bearing: finished severs the chain to the
// driver's error deliberately (ADR-11), so an errors.As afterwards would find nothing. It is also
// why neither statement's error may be finished where it was raised, which is what ownershipmarker.go
// hands back unwrapped.
func (run boot) schemaRefusal(err error, privilege string) error {
	refusal, fromServer := serverRefusalIn(err)
	if !fromServer || refusal.code != insufficientPrivilege {
		return finished(err)
	}
	return finished(fmt.Errorf("%w (the server said: %s)",
		privilegeMissing(run.cfg.Database.Schema, privilege), refusal.message))
}
